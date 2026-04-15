package proxytable

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/maddsua/proxyd"
	"github.com/maddsua/proxyd/utils"
)

type Orchestrator struct {
	mtx  sync.Mutex
	init bool
	done bool

	doneChan chan struct{}

	slots  map[string]*serviceSlot
	deltas map[string]TrafficDelta

	dnsTester proxyd.DNSTester
}

func (orch *Orchestrator) initEx() error {

	if orch.done {
		return errors.New("orchestrator done")
	} else if orch.init {
		return nil
	}

	orch.init = true
	orch.doneChan = make(chan struct{})

	go orch.rebalanceRoutine()

	return nil
}

func (orch *Orchestrator) rebalanceRoutine() {

	var rebalance = func() {

		orch.mtx.Lock()
		defer orch.mtx.Unlock()

		for _, slot := range orch.slots {
			slot.auth.RebalancePools()
		}
	}

	const interval = time.Second

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var longestDelay time.Duration

	for {
		select {
		case <-ticker.C:

			started := time.Now()

			rebalance()

			elapsed := time.Since(started)
			if elapsed > interval && elapsed > longestDelay {

				slog.Warn("Orchestrator: Pool rebalance took too long",
					slog.Duration("timeout", interval),
					slog.Duration("t", elapsed))

				longestDelay = elapsed

			} else if elapsed < interval {
				longestDelay = 0
			}

		case <-orch.doneChan:
			return
		}
	}

}

func (orch *Orchestrator) Services() []ServiceStatus {

	orch.mtx.Lock()
	defer orch.mtx.Unlock()

	var entries []ServiceStatus

	for _, slot := range orch.slots {

		next := ServiceStatus{
			BindAddr: slot.svc.BindAddr().String(),
			Type:     slot.svc.ProxyService(),
			Up:       slot.err == nil,
			Peers:    slot.auth.Peers(),
		}

		if err := slot.err; err != nil {
			next.Error = err.Error()
		}

		entries = append(entries, next)
	}

	return entries
}

func (orch *Orchestrator) CollectDeltas() []TrafficDelta {

	orch.mtx.Lock()
	defer orch.mtx.Unlock()

	for _, slot := range orch.slots {
		orch.collectSlotDeltas(slot)
	}

	var deltas []TrafficDelta
	for key, entry := range orch.deltas {
		deltas = append(deltas, entry)
		delete(orch.deltas, key)
	}

	return deltas
}

func (orch *Orchestrator) collectSlotDeltas(slot *serviceSlot) {

	if orch.deltas == nil {
		orch.deltas = map[string]TrafficDelta{}
	}

	for _, next := range slot.auth.Deltas() {

		delta := orch.deltas[next.PeerID]

		delta.RxBytes += next.RxBytes
		delta.TxBytes += next.TxBytes

		orch.deltas[next.PeerID] = delta
	}
}

func (orch *Orchestrator) ReturnDeltas(entries []TrafficDelta) {

	orch.mtx.Lock()
	defer orch.mtx.Unlock()

	for _, entry := range entries {

		delta := orch.deltas[entry.PeerID]

		delta.RxBytes += entry.RxBytes
		delta.TxBytes += entry.TxBytes

		orch.deltas[entry.PeerID] = delta
	}
}

func (orch *Orchestrator) RefreshTable(ctx context.Context, services []ProxyServiceEntry) error {

	orch.mtx.Lock()
	defer orch.mtx.Unlock()

	if err := orch.initEx(); err != nil {
		return err
	}

	staleMap := map[string]*serviceSlot{}
	if orch.slots == nil {
		orch.slots = map[string]*serviceSlot{}
	} else {
		maps.Copy(staleMap, orch.slots)
	}

	// compare the new proxy table agains existing state
	for _, entry := range services {

		bindKey := entry.bindKey()

		// mark this slot as updated
		delete(staleMap, bindKey)

		slot := orch.slots[bindKey]

		// repalce slot if it's empty or if it's service is incompatible
		if slot == nil || slot.svc == nil || slot.svc.ProxyService() != entry.Service {

			if slot == nil {

				slog.Info("Orchestrator: New slot",
					slog.String("bind_addr", entry.BindAddr),
					slog.String("service", entry.Service))

				slot = &serviceSlot{
					auth: peerAuthenticator{
						slotName:  entry.slotName(),
						dnsTester: &orch.dnsTester,
					},
				}
				orch.slots[bindKey] = slot

			} else if slot.svc != nil {

				slog.Info("Orchestrator: Replace slot",
					slog.String("bind_addr", entry.BindAddr),
					slog.String("old_service", slot.svc.ProxyService()),
					slog.String("new_service", entry.Service))

				if slot.err = slot.Shutdown(ctx); slot.err != nil {
					slog.Error("Orchestrator: Slot shutdown",
						slog.String("bind_addr", slot.svc.BindAddr().String()),
						slog.String("service", slot.svc.ProxyService()),
						slog.String("err", slot.err.Error()))
					continue
				}

			} else {
				slog.Info("Orchestrator: Restart slot",
					slog.String("bind_addr", entry.BindAddr),
					slog.String("old_service", slot.svc.ProxyService()),
					slog.String("new_service", entry.Service))
			}

			if slot.svc, slot.err = newService(&entry.ProxyServiceOptions, &slot.auth); slot.err != nil {
				slog.Error("Orchestrator: Start service",
					slog.String("bind_addr", entry.BindAddr),
					slog.String("service", entry.Service),
					slog.String("err", slot.err.Error()))

				continue
			}

			// patch and keep auth instance
			slot.auth.slotName = entry.slotName()
		}

		slot.auth.RefreshPeers(ctx, entry.Peers)
	}

	//	shutdown and remove all stale slots (ones that aren't present in the new proxy table)
	for key, slot := range staleMap {

		// silently remove a slot if its service failed to start
		if slot.svc == nil {

			slot.auth.ResetPeers()
			orch.collectSlotDeltas(slot)

			delete(orch.slots, key)

			continue
		}

		if err := slot.Shutdown(ctx); err != nil {

			slog.Error("Orchestrator: Slot shutdown",
				slog.String("bind_addr", slot.svc.BindAddr().String()),
				slog.String("service", slot.svc.ProxyService()),
				slog.String("err", err.Error()))

			continue
		}

		slog.Info("Orchestrator: Slot shutdown",
			slog.String("bind_addr", slot.svc.BindAddr().String()),
			slog.String("service", slot.svc.ProxyService()))

		orch.collectSlotDeltas(slot)

		delete(orch.slots, key)
	}

	return nil
}

func (orch *Orchestrator) Shutdown(ctx context.Context) error {

	orch.mtx.Lock()
	defer orch.mtx.Unlock()

	if !orch.init || orch.done {
		return nil
	}
	orch.done = true

	close(orch.doneChan)

	if len(orch.slots) == 0 {
		return nil
	}

	var wg sync.WaitGroup
	wg.Add(len(orch.slots))

	errChan := make(chan error, len(orch.slots))

	for key, slot := range orch.slots {

		go func(slot *serviceSlot) {

			if err := slot.Shutdown(ctx); err != nil && ctx.Err() == nil {
				errChan <- err
			} else {
				delete(orch.slots, key)
			}
			orch.collectSlotDeltas(slot)

			wg.Done()

		}(slot)
	}

	wg.Wait()
	close(errChan)

	var errList []error
	for err := range errChan {
		errList = append(errList, err)
	}

	return utils.JoinInlineErrors(errList...)
}
