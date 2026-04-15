//go:build !unix

package utils

import "net"

func IPBindable(ipaddr net.IP) bool {
	listener, err := net.ListenUDP("udp", &net.UDPAddr{IP: ipaddr})
	if listener != nil {
		listener.Close()
	}
	return err == nil
}
