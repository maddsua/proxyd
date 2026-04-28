package radiusmanager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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

func (entry *ProxySlotOptions) bindKey() string {
	return "tcp/" + entry.BindAddr
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
	doneChan chan struct{}

	auth *peerAuthenticator
	dac  *radius.PacketServer

	services map[string]proxyd.ProxyService
}

func (mgr *Manager) Exec() error {

	if err := mgr.initExec(); err != nil {
		return err
	}

	<-mgr.doneChan

	return nil
}

func (mgr *Manager) initExec() error {

	mgr.mtx.Lock()
	defer mgr.mtx.Unlock()

	if !mgr.init.CompareAndSwap(false, true) {
		return errors.New("manager already running")
	}

	mgr.doneChan = make(chan struct{})

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

	if err := mgr.initDac(); err != nil {
		return fmt.Errorf("init dac: %v", err)
	}

	return mgr.initServices()
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

func (mgr *Manager) initServices() error {

	mgr.services = map[string]proxyd.ProxyService{}

	for _, entry := range mgr.Slots {

		key := entry.bindKey()

		if _, exists := mgr.services[key]; exists {
			slog.Warn("Duplicated service bind detected. Service NOT started",
				slog.String("bind", entry.BindAddr),
				slog.String("type", entry.Service))
			continue
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

		mgr.services[key] = svc
	}

	return nil
}

func (mgr *Manager) Shutdown(ctx context.Context) error {

	mgr.mtx.Lock()
	defer mgr.mtx.Unlock()

	if mgr.init.CompareAndSwap(true, false) {
		close(mgr.doneChan)
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
	switch slot.Service {
	case http_pkg.ServiceType:
		return http_pkg.NewService(slot.BindAddr, auth, slot.HttpServiceOptions)
	case socks.ServiceType:
		return socks.NewService(slot.BindAddr, auth)
	default:
		return nil, fmt.Errorf("unsupported service type '%v'", slot.Service)
	}
}
