package utils

import (
	"net"
)

func SplitIPPort(addr net.Addr) (net.IP, int) {

	if addr == nil {
		return nil, 0
	}

	if addr, _ := addr.(*net.TCPAddr); addr != nil {
		return addr.IP, addr.Port
	}

	if addr, _ := addr.(*net.UDPAddr); addr != nil {
		return addr.IP, addr.Port
	}

	if addr, _ := addr.(*net.IPAddr); addr != nil {
		return addr.IP, 0
	}

	if host, _, _ := net.SplitHostPort(addr.String()); host != "" {
		if ip := net.ParseIP(host); ip != nil {
			return ip, 0
		}
	}

	return nil, 0
}

func IPNetwork(ip net.IP) string {

	if ip == nil {
		return "ip"
	}

	if ip.To4() != nil {
		return "ip4"
	}

	return "ip6"
}

func IpAddrLen(ip net.IP) int {
	if ip.To4() != nil {
		return net.IPv4len
	} else if ip.To16() != nil {
		return net.IPv6len
	}
	return 0
}
