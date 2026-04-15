package socks4

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"

	"github.com/maddsua/proxyd/utils"
)

//	Referencing: https://www.openssh.org/txt/socks4.protocol
//	and https://www.ietf.org/archive/id/draft-vance-socks-v4-02.html

type Command byte

const (
	VersionNumber = byte(4)

	CommandConnect = Command(1)
	CommandBind    = Command(2)
)

type Request struct {
	Cmd     Command
	DstAddr net.TCPAddr
	UserID  string
}

func (req *Request) Reply(code ReplyCode) *Reply {
	return &Reply{
		Code:    code,
		DstAddr: req.DstAddr,
	}
}

// Reads a socks request directly from a reader WITHOUT the leading version byte
func ReadRequest(reader io.Reader) (*Request, error) {

	cd, err := utils.ReadByte(reader)
	if err != nil {
		return nil, fmt.Errorf("read cd: %v", err)
	}

	dstport, err := utils.ReadN(reader, 2)
	if err != nil {
		return nil, fmt.Errorf("read dstport: %v", err)
	}

	dstip, err := utils.ReadN(reader, net.IPv4len)
	if err != nil {
		return nil, fmt.Errorf("read dstip: %v", err)
	}

	userid, err := utils.ReadNullTerminatedString(reader, 128)
	if err != nil {
		return nil, fmt.Errorf("read userid: %v", err)
	}

	return &Request{
		Cmd:     Command(cd),
		DstAddr: net.TCPAddr{IP: dstip, Port: int(binary.BigEndian.Uint16(dstport))},
		UserID:  userid,
	}, nil
}

type ReplyCode byte

const (
	ReplyCodeGranted    = ReplyCode(90)
	ReplyCodeRejected   = ReplyCode(91)
	ReplyCodeIDFailed   = ReplyCode(92)
	ReplyCodeIDRejected = ReplyCode(93)
)

type Reply struct {
	Code    ReplyCode
	DstAddr net.TCPAddr
}

func (repl *Reply) MarshalBinary() ([]byte, error) {

	buff := []byte{0x00, byte(repl.Code)}
	buff = binary.BigEndian.AppendUint16(buff, uint16(repl.DstAddr.Port))

	ip := repl.DstAddr.IP.To4()
	if ip == nil {
		ip = net.IPv4(0, 0, 0, 0)
	}

	return append(buff, ip...), nil
}

func (repl *Reply) Write(writer io.Writer) (int, error) {
	msg, _ := repl.MarshalBinary()
	return writer.Write(msg)
}
