package manager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/maddsua/proxyd/proxytable"
	"github.com/maddsua/proxyd/rpc/client"
	"github.com/maddsua/proxyd/rpc/model"
	"github.com/maddsua/proxyd/utils"
)

type Manager struct {
	Client *client.Client

	ctx    context.Context
	cancel context.CancelFunc
	active atomic.Bool

	orch proxytable.Orchestrator

	runID   uuid.UUID
	started time.Time
}

func (mgr *Manager) Exec() error {

	if !mgr.active.CompareAndSwap(false, true) {
		return errors.New("manager instance in use")
	}

	mgr.ctx, mgr.cancel = context.WithCancel(context.Background())
	mgr.runID = uuid.New()
	mgr.started = time.Now()

	defer mgr.cancel()

	if err := mgr.Client.ReportStatus(mgr.ctx, model.InstanceStatus{RunID: mgr.runID}); err != nil {
		return fmt.Errorf("report init: %v", err)
	}

	if err := mgr.refreshTable(); err != nil {
		return fmt.Errorf("load proxy table: %v", err)
	}

	if err := mgr.postStatus(); err != nil {
		return fmt.Errorf("report service status: %v", err)
	}

	// proxy table updater routine
	go func() {

		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:

				if err := mgr.refreshTable(); err != nil {
					slog.Error("RPC manager: Refresh table",
						slog.String("err", err.Error()))
				}

				if err := mgr.postStatus(); err != nil {
					slog.Error("RPC manager: Post status",
						slog.String("err", err.Error()))
				}

			case <-mgr.ctx.Done():
				return
			}
		}
	}()

	// traffic reporter routine
	go func() {

		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := mgr.postDeltas(); err != nil {
					slog.Error("RPC manager: PostDeltas",
						slog.String("err", err.Error()))
				}
			case <-mgr.ctx.Done():
				return
			}
		}
	}()

	<-mgr.ctx.Done()
	return mgr.ctx.Err()
}

func (mgr *Manager) postStatus() error {

	ctx, cancel := context.WithTimeout(mgr.ctx, 15*time.Second)
	defer cancel()

	return mgr.Client.ReportStatus(ctx, model.InstanceStatus{
		RunID:    mgr.runID,
		Uptime:   time.Since(mgr.started).Milliseconds(),
		Services: mgr.orch.Services(),
	})
}

func (mgr *Manager) postDeltas() error {

	entries := mgr.orch.CollectDeltas()

	ctx, cancel := context.WithTimeout(mgr.ctx, 15*time.Second)
	defer cancel()

	if err := mgr.Client.ReportTraffic(ctx, model.InstanceTrafficUpdate{Deltas: entries}); err != nil {
		mgr.orch.ReturnDeltas(entries)
		return err
	}

	return nil
}

func (mgr *Manager) refreshTable() error {

	ctx, cancel := context.WithTimeout(mgr.ctx, 15*time.Second)
	defer cancel()

	table, err := mgr.Client.GetProxyTable(ctx)
	if err != nil {
		return err
	}

	mgr.orch.RefreshTable(ctx, table.Services)

	return nil
}

func (mgr *Manager) Shutdown(ctx context.Context) error {

	if !mgr.active.Load() {
		return nil
	}

	defer mgr.active.Store(false)

	mgr.cancel()

	var errList []error

	if err := mgr.orch.Shutdown(ctx); err != nil {
		errList = append(errList, err)
	}

	if err := mgr.Client.ReportTraffic(ctx, model.InstanceTrafficUpdate{Deltas: mgr.orch.CollectDeltas()}); err != nil {
		errList = append(errList, err)
	}

	return utils.JoinInlineErrors(errList...)
}
