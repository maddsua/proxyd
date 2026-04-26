package static

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"

	"github.com/maddsua/proxyd/proxytable"
	"github.com/maddsua/proxyd/utils"
)

type Manager struct {
	ConfigLocation string

	mtx        sync.Mutex
	execInit   atomic.Bool
	execCtx    context.Context
	cancelExec context.CancelFunc

	orch proxytable.Orchestrator
}

func (mgr *Manager) Exec() error {

	if err := mgr.initExec(); err != nil {
		return err
	}

	if err := mgr.loadConfig(); err != nil {
		return err
	}

	configWatcher, cancelWatcher := utils.WatchFile(mgr.ConfigLocation)
	defer cancelWatcher()

	for {
		select {
		case <-configWatcher:
			if err := mgr.loadConfig(); err != nil {
				slog.Error("Reload config",
					slog.String("err", err.Error()))
				continue
			}
			slog.Info("Config updated")
		case <-mgr.execCtx.Done():
			return mgr.execCtx.Err()
		}
	}
}

func (mgr *Manager) initExec() error {

	mgr.mtx.Lock()
	defer mgr.mtx.Unlock()

	if !mgr.execInit.CompareAndSwap(false, true) {
		return errors.New("manager instance in use")
	}

	mgr.execCtx, mgr.cancelExec = context.WithCancel(context.Background())

	return nil
}

func (mgr *Manager) loadConfig() error {

	cfg, err := utils.LoadConfigLocation[ConfigurationWrapper](mgr.ConfigLocation)
	if err != nil {
		return err
	}

	mgr.orch.RefreshTable(mgr.execCtx, ProxyServiceTable(cfg.Manager.Services))

	return nil
}

func (mgr *Manager) Shutdown(ctx context.Context) error {

	mgr.mtx.Lock()
	defer mgr.mtx.Unlock()

	if !mgr.execInit.CompareAndSwap(true, false) {
		return nil
	}

	mgr.cancelExec()

	return mgr.orch.Shutdown(ctx)
}
