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
	execInit atomic.Bool
	execDone chan struct{}

	auth *peerAuthenticator
	dac  *radius.PacketServer

	services map[string]proxyd.ProxyService
}

func (mgr *Manager) Exec() error {

	if err := mgr.initExec(); err != nil {
		return err
	}

	<-mgr.execDone

	return nil
}

func (mgr *Manager) initExec() error {

	mgr.mtx.Lock()
	defer mgr.mtx.Unlock()

	if !mgr.execInit.CompareAndSwap(false, true) {
		return errors.New("manager already running")
	}

	if err := mgr.setSlotsLocked(context.Background(), mgr.Slots); err != nil {
		return err
	}

	if err := mgr.initDac(); err != nil {
		return err
	}

	mgr.execDone = make(chan struct{})

	return nil
}

func (mgr *Manager) initDac() error {

	dacAddr := mgr.Opts.DacAddr
	if dacAddr == "" {
		return nil
	}

	secret := mgr.Opts.Secret
	if secret == "" {
		return fmt.Errorf("no dac secret set")
	}

	srv := &radius.PacketServer{
		Addr:         dacAddr,
		SecretSource: radius.StaticSecretSource([]byte(secret)),
		Handler:      mgr.auth.DACHandler(),
		ErrorLog: utils.LegacyLogger{
			Prefix: "RADIUS DAC",
			Level:  slog.LevelError,
		},
	}

	conn, err := net.ListenPacket("udp", dacAddr)
	if err != nil {
		return err
	}

	go func() {
		defer conn.Close()
		srv.Serve(conn)
	}()

	mgr.dac = srv

	return nil
}

func (mgr *Manager) setSlotsLocked(ctx context.Context, slots []ProxySlotOptions) error {

	if mgr.auth == nil {

		client := radiuspkg.Client{
			AuthAddr: mgr.Opts.AuthAddr,
			AcctAddr: mgr.Opts.AcctAddr,
			Secret:   mgr.Opts.Secret,
		}

		if client.AuthAddr == "" {
			return fmt.Errorf("no auth/acct server addr set")
		} else if client.Secret == "" {
			return fmt.Errorf("no auth/acct secret set")
		}

		mgr.auth = &peerAuthenticator{Client: client}
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

		svc, err := newService(entry, mgr.auth)
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

func (mgr *Manager) SetSlots(ctx context.Context, slots []ProxySlotOptions) error {
	mgr.mtx.Lock()
	defer mgr.mtx.Unlock()
	return mgr.setSlotsLocked(ctx, slots)
}

func (mgr *Manager) Shutdown(ctx context.Context) error {

	mgr.mtx.Lock()
	defer mgr.mtx.Unlock()

	if mgr.execInit.CompareAndSwap(true, false) {
		close(mgr.execDone)
	}

	var errList []error

	for _, svc := range mgr.services {
		if err := svc.Shutdown(ctx); err != nil {
			errList = append(errList, err)
		}
	}

	if auth := mgr.auth; auth != nil {
		_ = auth.Shutdown(ctx)
	}

	if dac := mgr.dac; dac != nil {
		_ = dac.Shutdown(ctx)
	}

	return utils.JoinInlineErrors(errList...)
}

func newService(slot ProxySlotOptions, auth *peerAuthenticator) (proxyd.ProxyService, error) {

	if auth == nil {
		return nil, errors.New("nil peer authenticator")
	}

	switch slot.Service {
	case http_pkg.ServiceType:
		return http_pkg.NewService(slot.BindAddr, auth, slot.HttpServiceOptions)
	case socks.ServiceType:
		return socks.NewService(slot.BindAddr, auth)
	default:
		return nil, fmt.Errorf("unsupported service type '%v'", slot.Service)
	}
}
