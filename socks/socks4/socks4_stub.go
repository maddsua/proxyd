package socks4

import (
	"log/slog"
	"net"
)

// socks4 is not supported in this application
// as we absolutely need to authenticate users using a secure-enough method,
// however we still want to serve a proper error response
func ServeStub(conn net.Conn) {

	req, err := ReadRequest(conn)
	if err != nil {
		slog.Debug("SOCKSv4: ServeStub: ReadRequest",
			slog.String("proxy_host", conn.LocalAddr().String()),
			slog.String("client_addr", conn.RemoteAddr().String()),
			slog.String("err", err.Error()))
		return
	}

	slog.Debug("SOCKSv4: ServeStub: SOCKS version unsupported",
		slog.String("proxy_host", conn.LocalAddr().String()),
		slog.String("client_addr", conn.RemoteAddr().String()),
		slog.String("dst_addr", req.DstAddr.String()))

	if _, err := req.Reply(ReplyCodeIDRejected).Write(conn); err != nil {
		slog.Debug("SOCKSv4: ServeStub: Write rejection reply",
			slog.String("proxy_host", conn.LocalAddr().String()),
			slog.String("client_addr", conn.RemoteAddr().String()),
			slog.String("err", err.Error()))
		return
	}
}
