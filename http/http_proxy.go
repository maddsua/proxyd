package http

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/maddsua/proxyd"
)

const ServiceType = "http"

type HttpServiceOptions struct {
	HttpForwardEnabled bool `json:"http_forward_enabled" yaml:"http_forward_enabled"`
}

func (opts HttpServiceOptions) ProxyService() string {
	return ServiceType
}

func (opts HttpServiceOptions) String() string {
	return fmt.Sprintf("forward=%v", opts.HttpForwardEnabled)
}

func (opts HttpServiceOptions) allowMethodList() []string {

	methods := []string{http.MethodConnect}

	if opts.HttpForwardEnabled {
		return append(methods, http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
			http.MethodHead)
	}

	return methods
}

func (opts HttpServiceOptions) methodAllowed(method string) bool {

	switch method {
	case http.MethodConnect:
		return true
	case http.MethodGet,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodHead:
		return opts.HttpForwardEnabled
	}

	return false
}

func NewService(addr string, auth proxyd.ProxyAuthenticator, opts HttpServiceOptions) (proxyd.ProxyService, error) {

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	svc := &httpService{
		handler: requestHandler{
			HttpServiceOptions: opts,
			auth:               auth,
			hostAddr:           listener.Addr(),
		},
		srv: http.Server{
			Addr:              addr,
			ReadHeaderTimeout: 15 * time.Second,
			IdleTimeout:       15 * time.Second,
			MaxHeaderBytes:    16 * 1024,
		},
	}

	svc.ctx, svc.cancel = context.WithCancel(context.Background())

	svc.srv.Handler = &svc.handler
	svc.srv.BaseContext = func(l net.Listener) context.Context {
		return svc.ctx
	}

	go svc.serve(listener)

	return svc, nil
}

type httpService struct {
	srv     http.Server
	handler requestHandler
	err     error
	ctx     context.Context
	cancel  context.CancelFunc
}

func (svc *httpService) ProxyService() string {
	return ServiceType
}

func (svc *httpService) BindAddr() net.Addr {

	host, port, _ := net.SplitHostPort(svc.srv.Addr)

	hostIp := net.ParseIP(host)
	if hostIp == nil {
		hostIp = net.IPv4(0, 0, 0, 0)
	}

	portNumber, _ := strconv.Atoi(port)
	if portNumber <= 0 {
		portNumber = 80
	}

	return &net.TCPAddr{IP: hostIp, Port: portNumber}
}

func (svc *httpService) Options() proxyd.ProxyServiceOptions {
	return svc.handler.HttpServiceOptions
}

func (svc *httpService) serve(listener net.Listener) {
	if err := svc.srv.Serve(listener); err != nil && svc.ctx.Err() == nil {
		svc.err = err
	}
}

func (svc *httpService) Shutdown(ctx context.Context) error {

	svc.cancel()
	svc.err = svc.srv.Close()

	doneCh := make(chan struct{}, 1)

	go func() {
		svc.handler.wg.Wait()
		doneCh <- struct{}{}
	}()

	select {
	case <-doneCh:
	case <-ctx.Done():
		return ctx.Err()
	}

	return svc.err
}

type requestHandler struct {
	HttpServiceOptions

	auth     proxyd.ProxyAuthenticator
	hostAddr net.Addr
	wg       sync.WaitGroup
}

type allowMethodSet struct {
	keys map[string]struct{}
}

func (set *allowMethodSet) Allowed(method string) bool {
	_, has := set.keys[method]
	return has
}

func (set *allowMethodSet) List() []string {
	var list []string
	for method := range set.keys {
		list = append(list, method)
	}
	return list
}

func (handler *requestHandler) ServeHTTP(wrt http.ResponseWriter, req *http.Request) {

	if req.Method == http.MethodOptions {

		wrt.Header().Set("Allow", strings.Join(handler.allowMethodList(), ", "))
		wrt.Header().Set("Proxy-Authenticate", "Basic")
		wrt.WriteHeader(http.StatusNoContent)

		return
	}

	if !handler.methodAllowed(req.Method) {

		slog.Debug("HTTP: ServeHTTP: Method not allowed",
			slog.String("proxy_addr", req.Host),
			slog.String("peer_addr", req.RemoteAddr),
			slog.String("method", req.Method))

		wrt.Header().Set("Allow", strings.Join(handler.allowMethodList(), ", "))
		wrt.Header().Set("Proxy-Connection", "Close")
		wrt.WriteHeader(http.StatusMethodNotAllowed)

		return
	}

	handler.wg.Add(1)
	defer handler.wg.Done()

	userinfo, err := ProxyRequestCredentials(req)
	if err != nil || userinfo == nil {

		if err != nil {
			slog.Debug("HTTP: ServeHTTP: ProxyAuthorization",
				slog.String("proxy_addr", req.Host),
				slog.String("peer_addr", req.RemoteAddr),
				slog.String("err", err.Error()))
		} else {
			slog.Debug("HTTP: ServeHTTP: Unauthorized",
				slog.String("proxy_addr", req.Host),
				slog.String("peer_addr", req.RemoteAddr))
		}

		wrt.Header().Set("Proxy-Authenticate", "Basic")
		wrt.WriteHeader(http.StatusProxyAuthRequired)

		return
	}

	sess, err := handler.auth.AuthenticateWithPassword(req.Context(), handler.hostAddr, ProxyClientIP(req), userinfo.Username, userinfo.Password)
	if err != nil {

		if err, ok := err.(*proxyd.ProxyCredentialsError); ok {

			slog.Debug("HTTP: ServeHTTP: AuthenticateWithPassword",
				slog.String("proxy_addr", req.Host),
				slog.String("peer_addr", req.RemoteAddr),
				slog.String("err", err.Error()))

			if !err.RetryAfter.IsZero() {
				wrt.Header().Set("Retry-After", err.RetryAfter.In(time.UTC).Format(time.RFC1123))
				wrt.WriteHeader(http.StatusTooManyRequests)
				return
			}

			wrt.Header().Set("Proxy-Authenticate", "Basic")
			wrt.WriteHeader(http.StatusProxyAuthRequired)

			return
		}

		slog.Error("HTTP: ServeHTTP: AuthenticateWithPassword",
			slog.String("proxy_addr", req.Host),
			slog.String("peer_addr", req.RemoteAddr),
			slog.String("err", err.Error()))

		wrt.WriteHeader(http.StatusInternalServerError)

		return
	}

	if !sess.PeerEnabled {

		slog.Debug("HTTP: ServeHTTP: Request cancelled; Peer disabled",
			slog.String("proxy_addr", req.Host),
			slog.String("peer_addr", req.RemoteAddr),
			slog.String("peer_id", sess.PeerID))

		wrt.WriteHeader(http.StatusPaymentRequired)

		return
	}

	dstAddr, err := ProxyDestinationAddr(req)
	if err != nil {

		slog.Debug("HTTP: ServeHTTP: ProxyDestination",
			slog.String("proxy_addr", req.Host),
			slog.String("peer_addr", req.RemoteAddr),
			slog.String("peer_id", sess.PeerID),
			slog.String("err", err.Error()))

		wrt.WriteHeader(http.StatusBadRequest)

		return
	}

	dstResolved, err := sess.DNS.ResolveDestination(req.Context(), sess.Dialer.OutboundAddr.Network(), dstAddr)
	if err != nil {

		slog.Debug("HTTP: ServeHTTP: ResolveDestination",
			slog.String("proxy_addr", req.Host),
			slog.String("peer_addr", req.RemoteAddr),
			slog.String("peer_id", sess.PeerID),
			slog.String("peer_ip", sess.Dialer.OutboundAddr.String()),
			slog.String("peer_dns", sess.DNS.ServerName()),
			slog.String("dst_addr", dstAddr),
			slog.String("err", err.Error()))

		switch err.(type) {
		case *proxyd.PeerNetworkMismatchError:
			wrt.WriteHeader(http.StatusServiceUnavailable)
		case *proxyd.NetworkPolicyError:
			wrt.WriteHeader(http.StatusForbidden)
		default:
			wrt.WriteHeader(http.StatusBadGateway)
		}

		return
	}

	switch req.Method {
	case http.MethodConnect:
		ServeConnect(wrt, req, sess, dstResolved)

	default:
		ServeForward(wrt, req, sess)
	}
}
