package http

import (
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/maddsua/proxyd"
	"github.com/maddsua/proxyd/utils"
)

const DefaultTunnelInitTimeout = 15 * time.Second

func ServeConnect(wrt http.ResponseWriter, req *http.Request, sess *proxyd.ProxySession, dstAddr string) {

	wrt.Header().Set("X-Forwarded-Host", dstAddr)
	wrt.Header().Set("X-Forwarded-Proto", "tcp")

	dstConn, err := sess.DialDestinationContext(req.Context(), "tcp", dstAddr)
	if err != nil {

		if err, ok := err.(*proxyd.ConnectionLimitError); ok {

			slog.Debug("HTTP: ServeConnect: Too many connections",
				slog.String("proxy_host", req.Host),
				slog.String("peer_addr", req.RemoteAddr),
				slog.String("peer_id", sess.PeerID),
				slog.Int("limit", err.Limit))

			wrt.Header().Set("X-Connection-Limit", strconv.Itoa(err.Limit))
			wrt.Header().Set("Proxy-Connection", "Close")

			wrt.WriteHeader(http.StatusTooManyRequests)
			return
		}

		slog.Debug("HTTP: ServeConnect: sess.DialContext",
			slog.String("proxy_host", req.Host),
			slog.String("peer_addr", req.RemoteAddr),
			slog.String("peer_id", sess.PeerID),
			slog.String("dst_addr", dstAddr),
			slog.String("dns", sess.DNS.ServerName()),
			slog.String("err", err.Error()))

		wrt.WriteHeader(http.StatusBadGateway)
		return
	}

	defer dstConn.Close()

	hihacker, ok := wrt.(http.Hijacker)
	if !ok {

		slog.Error("HTTP: ServeConnect: Transport doesn't implement http.Hijacker",
			slog.String("proxy_host", req.Host),
			slog.String("peer_addr", req.RemoteAddr))

		wrt.Header().Set("Proxy-Connection", "Close")
		wrt.WriteHeader(http.StatusNotImplemented)
		return
	}

	conn, rw, err := hihacker.Hijack()
	if err != nil {

		slog.Error("HTTP: ServeConnect: Hijack",
			slog.String("proxy_host", req.Host),
			slog.String("peer_addr", req.RemoteAddr),
			slog.String("err", err.Error()))

		wrt.Header().Set("Proxy-Connection", "Close")
		wrt.WriteHeader(http.StatusInternalServerError)
		return
	}

	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(DefaultTunnelInitTimeout)); err != nil {
		slog.Debug("HTTP: ServeConnect: Set connection deadline",
			slog.String("peer_addr", req.RemoteAddr),
			slog.String("peer_id", sess.PeerID),
			slog.String("err", err.Error()))
		return
	}

	if rw.Reader.Buffered() > 0 {

		slog.Debug("HTTP: ServeConnect: Client sent data before tunnel initiated",
			slog.String("proxy_host", req.Host),
			slog.String("peer_addr", req.RemoteAddr),
			slog.String("peer_id", sess.PeerID))

		setErrorMessage(wrt.Header(), "client sent data before tunnel initiated")

		_ = tunnelResponse(conn, wrt.Header(), http.StatusBadRequest)

		return
	}

	//	prevent potential foot-shooting by explicitly disabling them
	rw.Reader.Reset(nil)
	rw.Writer.Reset(nil)
	req.Body = nil

	//	respond with ACK
	if err := tunnelResponse(conn, wrt.Header(), http.StatusOK); err != nil {
		slog.Debug("HTTP: ServeConnect: Write ACK",
			slog.String("peer_addr", req.RemoteAddr),
			slog.String("peer_id", sess.PeerID),
			slog.String("err", err.Error()))
		return
	}

	slog.Debug("HTTP: ServeConnect: Connected",
		slog.String("proxy_host", req.Host),
		slog.String("peer_addr", req.RemoteAddr),
		slog.String("peer_id", sess.PeerID),
		slog.String("dst_addr", dstAddr),
		slog.String("dns", sess.DNS.ServerName()))

	if err := utils.PipeDuplexContext(req.Context(), dstConn, conn); err != nil {
		slog.Debug("HTTP: ServeConnect: utils.PipeDuplex",
			slog.String("proxy_host", req.Host),
			slog.String("peer_addr", req.RemoteAddr),
			slog.String("peer_id", sess.PeerID),
			slog.String("dst_addr", dstAddr),
			slog.String("err", err.Error()))
	}
}

func tunnelResponse(writer io.Writer, header http.Header, statusCode int) error {

	resp := http.Response{
		ProtoMajor: 1,
		ProtoMinor: 1,
		Header:     header.Clone(),
	}

	resp.Header.Set("Date", time.Now().UTC().Format(http.TimeFormat))

	if statusCode < http.StatusBadRequest {

		resp.StatusCode = 200
		resp.Status = "Connection established"
		resp.Header.Set("Proxy-Connection", "Keep-Alive")

		//	prevents response from writing zero Content-Length, TE or sending Connection: Close
		resp.Uncompressed = true
		resp.ContentLength = -1

	} else {
		resp.StatusCode = statusCode
		resp.Status = http.StatusText(statusCode)
		resp.Header.Set("Proxy-Connection", "Close")
	}

	return resp.Write(writer)
}
