package manager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
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

	mtx        sync.Mutex
	execInit   atomic.Bool
	execCtx    context.Context
	cancelExec context.CancelFunc

	runID   uuid.UUID
	started time.Time

	orch proxytable.Orchestrator
}

func (mgr *Manager) Exec() error {

	if err := mgr.initExec(); err != nil {
		return err
	}

	if err := mgr.Client.ReportStatus(mgr.execCtx, model.InstanceStatus{RunID: mgr.runID}); err != nil {
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

			case <-mgr.execCtx.Done():
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
			case <-mgr.execCtx.Done():
				return
			}
		}
	}()

	<-mgr.execCtx.Done()
	return mgr.execCtx.Err()
}

func (mgr *Manager) initExec() error {

	mgr.mtx.Lock()
	defer mgr.mtx.Unlock()

	if !mgr.execInit.CompareAndSwap(false, true) {
		return errors.New("manager instance in use")
	}

	mgr.execCtx, mgr.cancelExec = context.WithCancel(context.Background())
	mgr.runID = uuid.New()
	mgr.started = time.Now()

	return nil
}

func (mgr *Manager) postStatus() error {

	ctx, cancel := context.WithTimeout(mgr.execCtx, 15*time.Second)
	defer cancel()

	return mgr.Client.ReportStatus(ctx, model.InstanceStatus{
		RunID:    mgr.runID,
		Uptime:   time.Since(mgr.started).Milliseconds(),
		Services: mgr.orch.Services(),
	})
}

func (mgr *Manager) postDeltas() error {

	entries := mgr.orch.CollectDeltas()

	ctx, cancel := context.WithTimeout(mgr.execCtx, 15*time.Second)
	defer cancel()

	if err := mgr.Client.ReportTraffic(ctx, model.InstanceTrafficUpdate{Deltas: entries}); err != nil {
		mgr.orch.ReturnDeltas(entries)
		return err
	}

	return nil
}

func (mgr *Manager) refreshTable() error {

	ctx, cancel := context.WithTimeout(mgr.execCtx, 15*time.Second)
	defer cancel()

	table, err := mgr.Client.GetProxyTable(ctx)
	if err != nil {
		return err
	}

	mgr.orch.RefreshTable(ctx, table.Services)

	return nil
}

func (mgr *Manager) Shutdown(ctx context.Context) error {

	mgr.mtx.Lock()
	defer mgr.mtx.Unlock()

	if !mgr.execInit.CompareAndSwap(true, false) {
		return nil
	}

	mgr.cancelExec()

	var errList []error

	if err := mgr.orch.Shutdown(ctx); err != nil {
		errList = append(errList, fmt.Errorf("service shutdown: %v", err))
	}

	if deltas := mgr.orch.CollectDeltas(); len(deltas) > 0 {
		if err := mgr.Client.ReportTraffic(ctx, model.InstanceTrafficUpdate{Deltas: deltas}); err != nil {
			errList = append(errList, fmt.Errorf("report traffic: %v", err))
		}
	}

	return utils.JoinInlineErrors(errList...)
}
