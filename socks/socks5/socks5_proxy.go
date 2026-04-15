package socks5

import (
	"context"
	"log/slog"
	"net"
	"time"

	"github.com/maddsua/proxyd"
	"github.com/maddsua/proxyd/utils"
)

func ServeProxy(ctx context.Context, conn net.Conn, auth proxyd.ProxyAuthenticator) {

	sess, err := AuthenticateConnection(ctx, conn, auth)
	if err != nil {

		if err, ok := err.(*proxyd.ProxyCredentialsError); ok {
			slog.Debug("SOCKSv5: ServeProxy: Invalid credentials",
				slog.String("proxy_host", conn.LocalAddr().String()),
				slog.String("client_addr", conn.RemoteAddr().String()),
				slog.String("err", err.Error()))
			return
		}

		slog.Error("SOCKSv5: ServeProxy: AuthenticateConnection",
			slog.String("proxy_host", conn.LocalAddr().String()),
			slog.String("client_addr", conn.RemoteAddr().String()),
			slog.String("err", err.Error()))
		return
	}

	req, err := ReadRequest(conn)
	if err != nil {
		slog.Debug("SOCKSv5: ServeProxy: ReadRequest",
			slog.String("proxy_host", conn.LocalAddr().String()),
			slog.String("client_addr", conn.RemoteAddr().String()),
			slog.String("err", err.Error()))
		return
	}

	rw := &ReplyWriter{}
	defer rw.Write(conn)

	if !sess.PeerEnabled {

		slog.Debug("SOCKSv5: ServeProxy: Request cancelled; Peer disabled",
			slog.String("proxy_host", conn.LocalAddr().String()),
			slog.String("client_addr", conn.RemoteAddr().String()),
			slog.String("peer_id", sess.PeerID))

		rw.Reply(ReplyCodeConnectionNotAllowed, nil)
		return
	}

	if err := conn.SetDeadline(time.Time{}); err != nil {
		slog.Debug("SOCKS5: ServeProxy: SetDeadline",
			slog.String("proxy_host", conn.LocalAddr().String()),
			slog.String("client_addr", conn.RemoteAddr().String()),
			slog.String("err", err.Error()))
		return
	}

	switch req.Cmd {
	case CommandConnect:
		ServeConnect(ctx, conn, sess, rw, req)
	default:
		ServeUnsupportedCommand(conn, rw, req)
	}
}

func ServeConnect(ctx context.Context, conn net.Conn, sess *proxyd.ProxySession, wrt *ReplyWriter, req *Request) {

	dstResolved, err := sess.DNS.ResolveDestination(ctx, sess.Dialer.OutboundAddr.Network(), req.DstAddr.String())
	if err != nil {

		slog.Debug("SOCKSv5: ServeConnect: ResolveDestination",
			slog.String("proxy_host", conn.LocalAddr().String()),
			slog.String("client_addr", conn.RemoteAddr().String()),
			slog.String("peer_id", sess.PeerID),
			slog.String("peer_ip", sess.Dialer.OutboundAddr.String()),
			slog.String("peer_dns", sess.DNS.ServerAddr),
			slog.String("dst_addr", req.DstAddr.String()),
			slog.String("err", err.Error()))

		switch err.(type) {
		case *proxyd.IpVersionError:
			wrt.Reply(ReplyCodeAddressTypeNotSupported, nil)
		case *proxyd.NetworkPolicyError:
			wrt.Reply(ReplyCodeConnectionNotAllowed, nil)
		default:
			wrt.Reply(ReplyCodeHostUnreachable, nil)
		}

		return
	}

	dstConn, err := sess.DialDestinationContext(context.Background(), "tcp", dstResolved)
	if err != nil {

		if err, ok := err.(*proxyd.ConnectionLimitError); ok {

			slog.Debug("SOCKSv5: ServeConnect: Too many connections",
				slog.String("proxy_host", conn.LocalAddr().String()),
				slog.String("client_addr", conn.RemoteAddr().String()),
				slog.String("peer_id", sess.PeerID),
				slog.Int("limit", err.Limit))

			wrt.Reply(ReplyCodeConnectionNotAllowed, nil)
			return
		}

		slog.Debug("SOCKSv5: ServeConnect: sess.DialContext",
			slog.String("proxy_host", conn.LocalAddr().String()),
			slog.String("client_addr", conn.RemoteAddr().String()),
			slog.String("peer_id", sess.PeerID),
			slog.String("dst_addr", dstResolved),
			slog.String("dns", sess.DNS.ServerName()),
			slog.String("err", err.Error()))

		wrt.Reply(ReplyCodeConnectionRefused, nil)
		return
	}

	defer dstConn.Close()

	if _, err := NewReply(ReplyCodeSuccess, dstConn.LocalAddr()).Write(conn); err != nil {
		slog.Debug("SOCKSv5: ServeConnect: Write reply",
			slog.String("proxy_host", conn.LocalAddr().String()),
			slog.String("peer_id", sess.PeerID),
			slog.String("client_addr", conn.RemoteAddr().String()),
			slog.String("dst_addr", dstResolved),
			slog.String("err", err.Error()))
		return
	}

	slog.Debug("SOCKSv5: ServeConnect: Connected",
		slog.String("proxy_host", conn.LocalAddr().String()),
		slog.String("peer_id", sess.PeerID),
		slog.String("client_addr", conn.RemoteAddr().String()),
		slog.String("dst_addr", dstResolved),
		slog.String("dns", sess.DNS.ServerName()))

	if err := utils.PipeDuplexContext(ctx, dstConn, conn); err != nil {
		slog.Debug("SOCKSv5: ServeConnect: PipeDuplexContext",
			slog.String("proxy_host", conn.LocalAddr().String()),
			slog.String("client_addr", conn.RemoteAddr().String()),
			slog.String("peer_id", sess.PeerID),
			slog.String("dst_addr", dstResolved),
			slog.String("err", err.Error()))
	}
}

func ServeUnsupportedCommand(conn net.Conn, rw *ReplyWriter, req *Request) {

	slog.Debug("SOCKSv5: ServeProxy: Command not supported",
		slog.String("client_addr", conn.RemoteAddr().String()),
		slog.String("cmd", req.Cmd.String()))

	rw.Reply(ReplyCodeCommandNotSupported, nil)
}
