package static

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"

	"github.com/maddsua/proxyd/proxytable"
	"github.com/maddsua/proxyd/utils"
)

type Manager struct {
	ConfigLocation string

	active atomic.Bool
	ctx    context.Context
	cancel context.CancelFunc

	orch proxytable.Orchestrator
}

func (mgr *Manager) Exec() error {

	if !mgr.active.CompareAndSwap(false, true) {
		return errors.New("manager instance in use")
	}

	mgr.ctx, mgr.cancel = context.WithCancel(context.Background())

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
		case <-mgr.ctx.Done():
			return mgr.ctx.Err()
		}
	}
}

func (mgr *Manager) loadConfig() error {

	cfg, err := utils.LoadConfigLocation[ConfigurationWrapper](mgr.ConfigLocation)
	if err != nil {
		return err
	}

	mgr.orch.RefreshTable(mgr.ctx, ProxyServiceTable(cfg.Manager.Services))

	return nil
}

func (mgr *Manager) Shutdown(ctx context.Context) error {

	if !mgr.active.Load() {
		return nil
	}

	defer mgr.active.Store(false)

	mgr.cancel()

	return mgr.orch.Shutdown(ctx)
}
