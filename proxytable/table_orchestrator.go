package proxytable

import (
	"context"
	"log/slog"
	"maps"
	"sync"
	"sync/atomic"
	"time"

	"github.com/maddsua/proxyd"
	"github.com/maddsua/proxyd/utils"
)

type Orchestrator struct {
	mtx           sync.Mutex
	slots         map[string]*serviceSlot
	rebalanceInit atomic.Bool
	rebalanceDone chan struct{}

	deltas   map[string]*TrafficDelta
	deltaMtx sync.Mutex

	dnsTester proxyd.DNSTester
}

func (orch *Orchestrator) rebalanceRoutine() {

	const interval = time.Second

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var longestDelay time.Duration

	for {
		select {
		case <-ticker.C:

			started := time.Now()

			orch.Rebalance()

			elapsed := time.Since(started)
			if elapsed > interval && elapsed > longestDelay {

				slog.Warn("Orchestrator: Pool rebalance took too long",
					slog.Duration("timeout", interval),
					slog.Duration("t", elapsed))

				longestDelay = elapsed

			} else if elapsed < interval {
				longestDelay = 0
			}

		case <-orch.rebalanceDone:
			return
		}
	}
}

func (orch *Orchestrator) Rebalance() {

	orch.mtx.Lock()
	defer orch.mtx.Unlock()

	for _, slot := range orch.slots {
		slot.auth.RebalancePools()
	}
}

func (orch *Orchestrator) Services() []ServiceStatus {

	orch.mtx.Lock()
	defer orch.mtx.Unlock()

	var entries []ServiceStatus

	for _, slot := range orch.slots {

		next := ServiceStatus{
			Up:    slot.err == nil,
			Peers: slot.auth.Peers(),
		}

		if svc := slot.svc; svc != nil {
			next.BindAddr = svc.BindAddr().String()
			next.Type = svc.ProxyService()
		}

		if err := slot.err; err != nil {
			if ext, ok := err.(*ServiceStartError); ok {
				next.BindAddr = ext.BindAddr
				next.Type = ext.Service
				next.Error = ext.Message
			} else {
				next.Error = err.Error()
			}
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
		deltas = append(deltas, *entry)
		delete(orch.deltas, key)
	}

	return deltas
}

func (orch *Orchestrator) collectSlotDeltas(slot *serviceSlot) {

	orch.deltaMtx.Lock()
	defer orch.deltaMtx.Unlock()

	for _, next := range slot.auth.Deltas() {
		orch.sumDelta(next)
	}
}

func (orch *Orchestrator) sumDelta(next TrafficDelta) {

	if orch.deltas == nil {
		orch.deltas = map[string]*TrafficDelta{}
	}

	delta := orch.deltas[next.PeerID]
	if delta == nil {
		delta = &TrafficDelta{PeerID: next.PeerID}
		orch.deltas[next.PeerID] = delta
	}

	delta.RxBytes += next.RxBytes
	delta.TxBytes += next.TxBytes
}

func (orch *Orchestrator) ReturnDeltas(entries []TrafficDelta) {

	orch.deltaMtx.Lock()
	defer orch.deltaMtx.Unlock()

	for _, entry := range entries {
		orch.sumDelta(entry)
	}
}

func (orch *Orchestrator) RefreshTable(ctx context.Context, services []ProxyServiceEntry) error {

	orch.mtx.Lock()
	defer orch.mtx.Unlock()

	staleMap := map[string]*serviceSlot{}
	if orch.slots == nil {
		orch.slots = map[string]*serviceSlot{}
	} else {
		maps.Copy(staleMap, orch.slots)
	}

	if orch.rebalanceInit.CompareAndSwap(false, true) {
		orch.rebalanceDone = make(chan struct{})
		go orch.rebalanceRoutine()
	}

	// compare the new proxy table agains existing state
	for _, entry := range services {

		bindKey := entry.bindKey()

		// mark this slot as updated
		delete(staleMap, bindKey)

		slot := orch.slots[bindKey]

		// repalce slot if it isn't up to date
		if !slot.satisfies(entry.ProxyServiceOptions) {

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

				if slot.err = slot.shutdown(ctx); slot.err != nil {
					slog.Error("Orchestrator: Slot shutdown",
						slog.String("bind_addr", slot.svc.BindAddr().String()),
						slog.String("service", slot.svc.ProxyService()),
						slog.String("err", slot.err.Error()))
					continue
				}

			} else {
				slog.Info("Orchestrator: Restart slot",
					slog.String("bind_addr", entry.BindAddr),
					slog.String("new_service", entry.Service))
			}

			if slot.svc, slot.err = newSlotService(entry.ProxyServiceOptions, &slot.auth); slot.err != nil {
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

		if err := slot.shutdown(ctx); err != nil {

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

	if orch.rebalanceInit.CompareAndSwap(true, false) {
		close(orch.rebalanceDone)
	}

	if len(orch.slots) == 0 {
		return nil
	}

	var errList []error

	for key, slot := range orch.slots {

		if err := slot.shutdown(ctx); err != nil && ctx.Err() == nil {
			errList = append(errList, err)
		} else {
			delete(orch.slots, key)
		}

		orch.collectSlotDeltas(slot)
	}

	return utils.JoinInlineErrors(errList...)
}
