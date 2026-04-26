package socks

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/maddsua/proxyd"
	"github.com/maddsua/proxyd/socks/socks4"
	"github.com/maddsua/proxyd/socks/socks5"
	"github.com/maddsua/proxyd/utils"
)

const ServiceType = "socks"

const DefaultConnInitTimeout = 90 * time.Second

func NewService(addr string, auth proxyd.ProxyAuthenticator) (proxyd.ProxyService, error) {

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}

	svc := &socksService{
		Auth:     auth,
		listener: listener,
	}

	svc.ctx, svc.cancel = context.WithCancel(context.Background())

	go svc.serve()

	return svc, nil
}

type socksService struct {
	Auth proxyd.ProxyAuthenticator

	listener net.Listener
	wg       sync.WaitGroup
	err      error
	ctx      context.Context
	cancel   context.CancelFunc
}

func (svc *socksService) ProxyService() string {
	return ServiceType
}

func (svc *socksService) BindAddr() net.Addr {
	return svc.listener.Addr()
}

func (svc *socksService) Options() proxyd.ProxyServiceOptions {
	return nil
}

func (svc *socksService) serve() {

	for svc.ctx.Err() == nil {

		conn, err := svc.listener.Accept()
		if err != nil {
			if svc.ctx.Err() == nil {
				svc.err = err
			}
			break
		}

		go svc.serveConn(conn)
	}
}

func (svc *socksService) serveConn(conn net.Conn) {

	svc.wg.Add(1)

	defer svc.wg.Done()
	defer conn.Close()

	if err := conn.SetDeadline(time.Now().Add(DefaultConnInitTimeout)); err != nil {
		slog.Debug("SOCKS: ServeTCP: SetDeadline",
			slog.String("remote_addr", conn.RemoteAddr().String()),
			slog.String("err", err.Error()))
		return
	}

	version, err := utils.ReadByte(conn)
	if err != nil {
		slog.Debug("SOCKS: ServeTCP: Read version byte",
			slog.String("remote_addr", conn.RemoteAddr().String()),
			slog.String("err", err.Error()))
		return
	}

	switch version {
	case socks4.VersionNumber:
		socks4.ServeStub(conn)
	case socks5.VersionNumber:
		socks5.ServeProxy(svc.ctx, conn, svc.Auth)
	default:
		slog.Debug("SOCKS: ServeTCP: Invalid version byte",
			slog.String("remote_addr", conn.RemoteAddr().String()))
	}
}

func (svc *socksService) Shutdown(ctx context.Context) error {

	// cancel the context first so that the listener routine won't overwrite the error value
	svc.cancel()

	// then close the listener itself
	svc.err = svc.listener.Close()

	// wait until all subroutines exit or untile the shutdown context itself gets cancelled
	doneCh := make(chan struct{}, 1)

	go func() {
		svc.wg.Wait()
		doneCh <- struct{}{}
	}()

	select {
	case <-doneCh:
	case <-ctx.Done():
		return ctx.Err()
	}

	return svc.err
}
