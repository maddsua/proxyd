//go:build unix

package utils

import (
	"net"
	"syscall"
)

func IPBindable(ipaddr net.IP) bool {

	sockaddr, domain := afAddrDomain(ipaddr)
	if sockaddr == nil {
		return false
	}

	fd, err := syscall.Socket(domain, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return false
	}
	defer syscall.Close(fd)

	return syscall.Bind(fd, sockaddr) == nil
}

func afAddrDomain(ipaddr net.IP) (syscall.Sockaddr, int) {

	if ip := ipaddr.To4(); ip != nil {
		addr := &syscall.SockaddrInet4{Port: 0}
		copy(addr.Addr[:], ip)
		return addr, syscall.AF_INET
	}

	if ip := ipaddr.To16(); ip != nil {
		addr := &syscall.SockaddrInet6{Port: 0}
		copy(addr.Addr[:], ip)
		return addr, syscall.AF_INET6
	}

	return nil, 0
}
