package radiusmanager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"sync"
	"sync/atomic"

	radius "github.com/maddsua/layeh-radius"
	"github.com/maddsua/proxyd"
	http_pkg "github.com/maddsua/proxyd/http"
	radiuspkg "github.com/maddsua/proxyd/radius"
	"github.com/maddsua/proxyd/socks"
	"github.com/maddsua/proxyd/utils"
)

type ProxySlotOptions struct {
	BindAddr string `json:"bind_addr" yaml:"bind_addr"`
	Service  string `json:"service" yaml:"service"`

	http_pkg.HttpServiceOptions `yaml:",inline"`
}

type RadiusOptions struct {
	AuthAddr string `json:"radius_auth_addr" yaml:"radius_auth_addr"`
	AcctAddr string `json:"radius_acct_addr" yaml:"radius_acct_addr"`
	DacAddr  string `json:"dac_listen_addr" yaml:"dac_listen_addr"`
	Secret   string `json:"radius_secret" yaml:"radius_secret"`
}

type Manager struct {
	Opts  RadiusOptions
	Slots []ProxySlotOptions

	mtx      sync.Mutex
	init     atomic.Bool
	done     atomic.Bool
	doneChan chan struct{}
	auth     peerAuthenticator
	dac      radius.PacketServer
	services map[string]proxyd.ProxyService
}

func (mgr *Manager) Exec() error {

	if err := mgr.SetSlots(context.Background(), mgr.Slots); err != nil {
		return err
	}

	<-mgr.doneChan

	return nil
}

func (mgr *Manager) initEx() error {

	if !mgr.init.CompareAndSwap(false, true) {
		return nil
	} else if mgr.done.Load() {
		return errors.New("manager done")
	}

	mgr.doneChan = make(chan struct{})

	opts := mgr.Opts

	mgr.auth.client = radiuspkg.Client{
		AuthAddr: opts.AuthAddr,
		AcctAddr: opts.AcctAddr,
		Secret:   opts.Secret,
	}

	if opts.DacAddr != "" {
		if err := mgr.initDac(opts.DacAddr, opts.Secret); err != nil {
			return fmt.Errorf("init dac: %v", err)
		}
	}

	return nil
}

func (mgr *Manager) initDac(addr, secret string) error {

	mgr.dac = radius.PacketServer{
		Addr:         addr,
		SecretSource: radius.StaticSecretSource([]byte(secret)),
		Handler:      mgr.auth.DACHandler(),
		ErrorLog: utils.LegacyLogger{
			Prefix: "RADIUS DAC",
			Level:  slog.LevelError,
		},
	}

	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return err
	}

	go func() {
		defer conn.Close()
		mgr.dac.Serve(conn)
	}()

	return nil
}

func (mgr *Manager) SetSlots(ctx context.Context, slots []ProxySlotOptions) error {

	mgr.mtx.Lock()
	defer mgr.mtx.Unlock()

	if err := mgr.initEx(); err != nil {
		return err
	}

	staleMap := map[string]proxyd.ProxyService{}
	if mgr.services == nil {
		mgr.services = map[string]proxyd.ProxyService{}
	} else {
		maps.Copy(staleMap, mgr.services)
	}

	for _, entry := range slots {

		delete(staleMap, entry.BindAddr)

		if svc := mgr.services[entry.BindAddr]; svc != nil {

			if svc.ProxyService() == entry.Service {
				continue
			}

			if err := svc.Shutdown(ctx); err != nil {
				slog.Error("RADIUS Manager: Shutdown service",
					slog.String("bind_addr", svc.BindAddr().String()),
					slog.String("type", svc.ProxyService()),
					slog.String("err", err.Error()))
				continue
			}

			slog.Info("RADIUS Manager: Stop service",
				slog.String("bind_addr", svc.BindAddr().String()),
				slog.String("type", svc.ProxyService()))
		}

		svc, err := newService(entry, &mgr.auth)
		if err != nil {
			slog.Error("RADIUS Manager: Start service",
				slog.String("bind_addr", entry.BindAddr),
				slog.String("type", entry.Service),
				slog.String("err", err.Error()))
			continue
		}

		slog.Info("RADIUS Manager: Start service",
			slog.String("bind_addr", svc.BindAddr().String()),
			slog.String("type", svc.ProxyService()))

		mgr.services[entry.BindAddr] = svc
	}

	for _, svc := range staleMap {

		if err := svc.Shutdown(ctx); err != nil {
			slog.Error("RADIUS Manager: Shutdown service",
				slog.String("bind_addr", svc.BindAddr().String()),
				slog.String("type", svc.ProxyService()),
				slog.String("err", err.Error()))
			continue
		}

		slog.Info("RADIUS Manager: Stop service",
			slog.String("bind_addr", svc.BindAddr().String()),
			slog.String("type", svc.ProxyService()))
	}

	mgr.Slots = slots

	return nil
}

func (mgr *Manager) Shutdown(ctx context.Context) error {

	mgr.mtx.Lock()
	defer mgr.mtx.Unlock()

	if !mgr.done.CompareAndSwap(false, true) {
		return nil
	}

	close(mgr.doneChan)

	var errList []error

	for _, svc := range mgr.services {
		if err := svc.Shutdown(ctx); err != nil {
			errList = append(errList, err)
		}
	}

	if err := mgr.auth.Shutdown(ctx); err != nil {
		errList = append(errList, err)
	}

	if err := mgr.dac.Shutdown(ctx); err != nil {
		errList = append(errList, err)
	}

	return utils.JoinInlineErrors(errList...)
}

func newService(slot ProxySlotOptions, auth *peerAuthenticator) (proxyd.ProxyService, error) {
	switch slot.Service {
	case http_pkg.ServiceType:
		return http_pkg.NewService(slot.BindAddr, auth, slot.HttpServiceOptions)
	case socks.ServiceType:
		return socks.NewService(slot.BindAddr, auth)
	default:
		return nil, fmt.Errorf("unsupported service type '%v'", slot.Service)
	}
}
