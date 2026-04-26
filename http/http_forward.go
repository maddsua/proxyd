package http

import (
	"context"
	"io"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/maddsua/proxyd"
)

func ServeForward(wrt http.ResponseWriter, req *http.Request, sess *proxyd.ProxySession) {

	wrt.Header().Set("X-Forwarded-Host", req.URL.Host)
	wrt.Header().Set("X-Forwarded-Proto", req.URL.Scheme)

	fwreq, err := http.NewRequest(req.Method, req.URL.String(), req.Body)
	if err != nil {

		slog.Debug("HTTP: ServeForward: Unable to create forward request",
			slog.String("proxy_host", req.Host),
			slog.String("peer_addr", req.RemoteAddr),
			slog.String("peer_id", sess.PeerID),
			slog.String("dst_host", req.URL.Host),
			slog.String("err", err.Error()))

		wrt.Header().Set("Proxy-Connection", "Close")
		wrt.WriteHeader(http.StatusBadRequest)
		return
	}

	fwreq.Header = cloneForwardHeaders(req.Header)

	fwresp, err := forwardClient(sess).Do(fwreq.WithContext(req.Context()))
	if err != nil {

		slog.Debug("HTTP: ServeForward: Request",
			slog.String("proxy_host", req.Host),
			slog.String("peer_addr", req.RemoteAddr),
			slog.String("peer_id", sess.PeerID),
			slog.String("dst_host", req.URL.Host),
			slog.String("err", err.Error()))

		wrt.Header().Set("Proxy-Connection", "Close")
		wrt.WriteHeader(http.StatusBadGateway)
		return
	}

	defer fwresp.Body.Close()

	slog.Debug("HTTP: ServeForward",
		slog.String("proxy_host", req.Host),
		slog.String("peer_addr", req.RemoteAddr),
		slog.String("peer_id", sess.PeerID),
		slog.String("dst_host", req.URL.Host),
		slog.String("dns", sess.DNS.ServerName()))

	writeResponseHeaders(wrt, fwresp)

	if err := streamResponseBody(req.Context(), wrt, fwresp.Body); err != nil {
		slog.Debug("HTTP: ServeForward: ForwardBodyStream",
			slog.String("proxy_host", req.Host),
			slog.String("peer_addr", req.RemoteAddr),
			slog.String("peer_id", sess.PeerID),
			slog.String("dst_host", req.URL.Host),
			slog.String("err", err.Error()))
		return
	}
}

func cloneForwardHeaders(src http.Header) http.Header {

	fwHeader := make(http.Header)

	for key, values := range src {

		if !headerForwardable(key) {
			continue
		}

		for _, val := range values {
			fwHeader.Add(key, val)
		}
	}

	return fwHeader
}

func writeResponseHeaders(wrt http.ResponseWriter, resp *http.Response) {

	for key, values := range resp.Header {

		if !headerForwardable(key) {
			continue
		}

		for _, val := range values {
			wrt.Header().Add(key, val)
		}
	}

	wrt.WriteHeader(resp.StatusCode)
}

func headerForwardable(header string) bool {
	switch http.CanonicalHeaderKey(header) {
	case
		"Host",
		"Connection",
		"Keep-Alive",
		"Upgrade",
		"Te", // Canonicized 'TE'
		"Trailer",
		"Transfer-Encoding",
		"Proxy-Authorization",
		"Proxy-Connection":
		return false
	default:
		return true
	}
}

func streamResponseBody(ctx context.Context, dst io.Writer, src io.Reader) error {

	buff := make([]byte, math.MaxUint16)

	for ctx.Err() == nil {

		readBytes, err := src.Read(buff)

		if readBytes > 0 {

			if _, err := dst.Write(buff[:readBytes]); err != nil {
				if ctx.Err() != nil {
					break
				}
				return err
			}

			if flusher, ok := dst.(http.Flusher); ok {
				flusher.Flush()
			}
		}

		if err != nil {
			if err == io.EOF || ctx.Err() != nil {
				break
			}
			return err
		}
	}

	return nil
}

func forwardClient(sess *proxyd.ProxySession) *http.Client {
	attr, _ := sess.Attributes.WithValue(sessionForwardClientAttribute{}, newSessionForwardClient(sess))
	return attr.(*sessionForwardClient).client
}

type sessionForwardClientAttribute struct{}

type sessionForwardClient struct {
	client   *http.Client
	peerAddr string
}

func (state *sessionForwardClient) EqualAttribute(attr proxyd.ProxyAttribute) bool {

	other, _ := attr.(*sessionForwardClient)
	if other == nil {
		return false
	}

	return other.peerAddr == state.peerAddr
}

func (state *sessionForwardClient) Destroy() {
	state.client.CloseIdleConnections()
}

func newSessionForwardClient(sess *proxyd.ProxySession) *sessionForwardClient {
	return &sessionForwardClient{
		peerAddr: sess.Dialer.OutboundAddr.String(),
		client: &http.Client{
			Transport: &http.Transport{
				DialContext:           sess.DialDestinationContext,
				ForceAttemptHTTP2:     false,
				MaxIdleConns:          10,
				IdleConnTimeout:       30 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 5 * time.Second,
			},
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}
