package proxyd

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/maddsua/proxyd/utils"
)

// ProxyDialer simplifies dialing proxy destinations
type ProxyDialer struct {
	OutboundAddr *PeerAddr
}

func (pd *ProxyDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {

	dialer := net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	switch network {
	case "tcp":
		dialer.LocalAddr = pd.OutboundAddr.TCPDialAddr()
		return dialer.DialContext(ctx, network, address)
	case "udp":
		return nil, fmt.Errorf("udp dialer not implemented")
	case "tcp4", "tcp6",
		"udp4", "udp6":
		return nil, fmt.Errorf("versioned protocols not allowed: use OutboundAddr to set network type instead")
	default:
		return nil, fmt.Errorf("unsupported dial network: '%v'", network)
	}
}

// PeerAddr is normally a public IP address that's used for outbound connections
type PeerAddr struct {
	IP net.IP
}

func (addr *PeerAddr) String() string {
	if addr == nil {
		return "<auto>"
	}
	return addr.IP.String()
}

func (addr *PeerAddr) Network() string {
	if addr == nil || addr.IP == nil {
		return "ip"
	}
	return utils.IPNetwork(addr.IP)
}

func (addr *PeerAddr) dialAddr() net.IP {
	if addr == nil {
		return nil
	}
	return addr.IP
}

func (addr *PeerAddr) TCPDialAddr() net.Addr {
	if ip := addr.dialAddr(); ip != nil {
		return &net.TCPAddr{IP: ip}
	}
	return nil
}

func (addr *PeerAddr) UDPDialAddr() net.Addr {
	if ip := addr.dialAddr(); ip != nil {
		return &net.UDPAddr{IP: ip}
	}
	return nil
}
