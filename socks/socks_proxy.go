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

	svc.srvMtx.Lock()
	go svc.serve()

	return svc, nil
}

type socksService struct {
	Auth proxyd.ProxyAuthenticator

	listener net.Listener
	srvMtx   sync.Mutex
	wg       sync.WaitGroup
	err      error
	mtx      sync.Mutex
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

	defer svc.srvMtx.Unlock()

	for svc.ctx.Err() == nil {

		conn, err := svc.listener.Accept()
		if err != nil {
			if svc.ctx.Err() == nil {
				svc.err = err
			}
			break
		}

		svc.wg.Add(1)
		go svc.serveConn(conn)
	}
}

func (svc *socksService) serveConn(conn net.Conn) {

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

	svc.mtx.Lock()
	defer svc.mtx.Unlock()

	// cancel base context and close the listener
	svc.cancel()

	if err := svc.listener.Close(); err != nil {
		svc.err = err
	}

	// wait until listener exits
	svc.srvMtx.Lock()
	defer svc.srvMtx.Unlock()

	// wait until all spawned routines exit as well
	select {
	case <-utils.GroupDoneChan(&svc.wg):
	case <-ctx.Done():
		return ctx.Err()
	}

	return svc.err
}
