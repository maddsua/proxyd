package socks5

import (
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"strconv"
	"strings"

	"github.com/maddsua/proxyd/utils"
)

const VersionNumber = byte(5)

type AuthMethod byte

const (
	AuthMethodNone         = AuthMethod(0)
	AuthMethodGSSAPI       = AuthMethod(1)
	AuthMethodPassword     = AuthMethod(2)
	AuthMethodUnacceptable = AuthMethod(0xff)
)

type Command byte

func (cmd Command) String() string {
	switch cmd {
	case CommandAssociate:
		return "associate"
	case CommandBind:
		return "bind"
	case CommandConnect:
		return "connect"
	default:
		return fmt.Sprintf("(%d)", cmd)
	}
}

const (
	CommandConnect   = Command(1)
	CommandBind      = Command(2)
	CommandAssociate = Command(3)
)

type AddressType byte

const (
	AddressTypeIPv4     = AddressType(1)
	AddressTypeHostname = AddressType(3)
	AddressTypeIPv6     = AddressType(4)
)

type ReplyCode byte

const (
	ReplyCodeSuccess = ReplyCode(iota)
	ReplyCodeServerFail
	ReplyCodeConnectionNotAllowed
	ReplyCodeNetworkUnreachable
	ReplyCodeHostUnreachable
	ReplyCodeConnectionRefused
	ReplyCodeTTLExpired
	ReplyCodeCommandNotSupported
	ReplyCodeAddressTypeNotSupported
)

type MethodRequest struct {
	Methods []AuthMethod
}

func NewMethodReply(method AuthMethod) *MethodReply {
	return &MethodReply{Method: method}
}

func ReadMethodRequest(reader io.Reader) (*MethodRequest, error) {

	nmethods, err := utils.ReadByte(reader)
	if err != nil {
		return nil, fmt.Errorf("read nmethods: %v", err)
	}

	methodBuff, err := utils.ReadN(reader, int(nmethods))
	if err != nil {
		return nil, fmt.Errorf("read methods: %v", err)
	}

	methods := make([]AuthMethod, len(methodBuff))
	for idx, val := range methodBuff {
		methods[idx] = AuthMethod(val)
	}

	return &MethodRequest{
		Methods: methods,
	}, nil
}

type MethodReply struct {
	Method AuthMethod
}

func (repl *MethodReply) MarshalBinary() ([]byte, error) {
	return []byte{VersionNumber, byte(repl.Method)}, nil
}

func (repl *MethodReply) Write(writer io.Writer) (int, error) {
	buff, _ := repl.MarshalBinary()
	return writer.Write(buff)
}

type Request struct {
	Cmd     Command
	DstAddr net.Addr
}

func (req *Request) String() string {

	if req == nil {
		return "<nil>"
	}

	return fmt.Sprintf("%s %v", strings.ToUpper(req.Cmd.String()), req.DstAddr)
}

func ReadRequest(reader io.Reader) (*Request, error) {

	var cmd Command

	if buff, err := utils.ReadN(reader, 3); err != nil {
		return nil, fmt.Errorf("read request: %v", err)
	} else if version := buff[0]; version != VersionNumber {
		return nil, fmt.Errorf("protocol error: invalid request version: %v", version)
	} else if buff[2] != 0x00 {
		return nil, fmt.Errorf("protocol error: non-null rsv")
	} else {
		cmd = Command(buff[1])
	}

	dst, err := ReadHostAddr(reader)
	if err != nil {
		return nil, fmt.Errorf("read dst: %v", err)
	}

	return &Request{
		Cmd:     cmd,
		DstAddr: dst,
	}, nil
}

type HostAddr struct {
	IP       net.IP
	Hostname string

	Port uint16
}

func (addr *HostAddr) MarshalBinary() ([]byte, error) {

	buff := []byte{byte(AddressTypeIPv4)}

	if addr == nil {
		buff = append(buff, make([]byte, net.IPv4len)...)
		buff = binary.BigEndian.AppendUint16(buff, 0)
		return buff, nil
	}

	if hostname := addr.Hostname; hostname != "" {

		hostnameLen := len(hostname)
		if hostnameLen > math.MaxUint8 {
			return nil, fmt.Errorf("hostname too large")
		}

		buff[0] = byte(AddressTypeHostname)
		buff = append(buff, byte(hostnameLen))
		buff = append(buff, []byte(hostname)...)

	} else if ip4 := addr.IP.To4(); ip4 != nil {
		buff = append(buff, ip4...)
	} else if ip6 := addr.IP.To16(); ip6 != nil {
		buff[0] = byte(AddressTypeIPv6)
		buff = append(buff, ip6...)
	} else {
		buff = append(buff, make([]byte, net.IPv4len)...)
	}

	return binary.BigEndian.AppendUint16(buff, addr.Port), nil
}

func (addr *HostAddr) Network() string { return "any" }

func (addr *HostAddr) String() string {

	if addr == nil {
		return "<nil>"
	}

	hostname := addr.Hostname
	if hostname == "" {
		hostname = addr.IP.String()
	}

	return net.JoinHostPort(hostname, strconv.Itoa(int(addr.Port)))
}

func ReadHostAddr(reader io.Reader) (*HostAddr, error) {

	var addr HostAddr

	addrType, err := utils.ReadByte(reader)
	if err != nil {
		return nil, fmt.Errorf("read atype: %v", err)
	}

	switch AddressType(addrType) {
	case AddressTypeIPv4, AddressTypeIPv6:

		//	this works because by the spec the int code for ipv6 is 4 times larger than for ipv4,
		//	which incidentally is the same as their size difference. Definitely not a coincidense xD
		addrLen := net.IPv4len * int(addrType)

		ipAddr, err := utils.ReadN(reader, addrLen)
		if err != nil {
			return nil, fmt.Errorf("read dst.addr: %v", err)
		}

		addr.IP = ipAddr

	case AddressTypeHostname:

		hostnameLen, err := utils.ReadByte(reader)
		if err != nil {
			return nil, fmt.Errorf("read dst.addr len: %v", err)
		} else if hostnameLen <= 0 {
			return nil, fmt.Errorf("invalid hostname len: %d", hostnameLen)
		}

		hostname, err := utils.ReadN(reader, int(hostnameLen))
		if err != nil {
			return nil, fmt.Errorf("read dst.addr: %v", err)
		}

		addr.Hostname = string(hostname)

	default:
		return nil, fmt.Errorf("protocol error: invalid dst.addr type: %v", addrType)
	}

	portBytes, err := utils.ReadN(reader, 2)
	if err != nil {
		return nil, err
	}

	addr.Port = binary.BigEndian.Uint16(portBytes)

	return &addr, nil
}

func NewReply(code ReplyCode, bindAddr net.Addr) *Reply {
	ip, port := utils.SplitIPPort(bindAddr)
	return &Reply{Code: code, BindAddr: HostAddr{IP: ip, Port: uint16(port)}}
}

type Reply struct {
	Code     ReplyCode
	BindAddr HostAddr
}

func (repl *Reply) MarshalBinary() ([]byte, error) {

	header := []byte{VersionNumber, byte(repl.Code), 0x00}

	addrBuff, err := repl.BindAddr.MarshalBinary()
	if err != nil {
		return nil, err
	}

	return append(header, addrBuff...), nil
}

func (repl *Reply) Write(writer io.Writer) (int, error) {

	buff, err := repl.MarshalBinary()
	if err != nil {
		return 0, err
	}

	return writer.Write(buff)
}

// ReplyWriter helps to avoid duplicating calls to Reply.Write() every time we need to return because of an error
type ReplyWriter struct {
	reply *Reply
}

func (rw *ReplyWriter) Reply(code ReplyCode, bindAddr net.Addr) {
	rw.reply = NewReply(code, bindAddr)
}

func (rw *ReplyWriter) Write(conn net.Conn) {

	if rw.reply == nil {
		return
	}

	if _, err := rw.reply.Write(conn); err != nil {
		slog.Debug("SOCKSv5: ReplyWriter",
			slog.String("proxy_host", conn.LocalAddr().String()),
			slog.String("client_addr", conn.RemoteAddr().String()),
			slog.String("err", err.Error()))
	}
}
