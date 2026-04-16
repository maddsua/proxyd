package proxyd

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/maddsua/proxyd/utils"
)

const MaxConnectionLimit = math.MaxUint16
const DefaultConnectionLimit = math.MaxUint8

var ErrConnectionCtlClosed = errors.New("conn ctl closed")

type ProxyConnectionPool struct {
	mtx      sync.Mutex
	pool     []*ProxyConnCtl
	maxslots int
	nactive  int

	bandwidthRx atomic.Int64
	bandwidthTx atomic.Int64

	TrafficRx atomic.Int64
	TrafficTx atomic.Int64
}

func (pool *ProxyConnectionPool) accountSlot(slot *ProxyConnCtl) {

	if val := slot.TrafficRx.Swap(0); val > 0 {
		pool.TrafficRx.Add(val)
	}

	if val := slot.TrafficTx.Swap(0); val > 0 {
		pool.TrafficTx.Add(val)
	}
}

func (pool *ProxyConnectionPool) ConnectionLimit() int {
	return pool.maxslots
}

func (pool *ProxyConnectionPool) SetConnectionLimit(limit int) error {

	if limit < 0 {
		return fmt.Errorf("invalid pool size")
	} else if limit > MaxConnectionLimit {
		return fmt.Errorf("pool size is limited to %d", MaxConnectionLimit)
	}

	pool.mtx.Lock()
	defer pool.mtx.Unlock()

	if limit == 0 {
		_ = pool.refreshActive()
	} else if len(pool.pool) > limit {
		pool.shrink(limit)
	}

	pool.maxslots = limit

	return nil
}

func (pool *ProxyConnectionPool) shrink(newSize int) {

	var slotIdx int
	var nActive int

	newPool := make([]*ProxyConnCtl, newSize)

	for _, slot := range pool.pool {

		if slot == nil {
			continue
		}

		// account and dump inactive slots
		if slot.done.Load() {
			pool.accountSlot(slot)
			continue
		}

		nActive++

		//	if the new pool is already full, start dropping connections
		if slotIdx >= len(newPool) {

			if err := slot.Close(); err != nil {
				slog.Warn("ProxyConnectionPool.SetConnectionLimit: Close slot",
					slog.String("err", err.Error()))
			}

			pool.accountSlot(slot)

			continue
		}

		//	otherwise, just move slots to the new pool
		newPool[slotIdx] = slot
		slotIdx++
	}

	pool.pool = newPool
	pool.nactive = nActive
}

func (pool *ProxyConnectionPool) slot() *ProxyConnCtl {

	slot := &ProxyConnCtl{}

	rx, tx := pool.baselineBandwidth()
	slot.SetBandwidth(int(rx), int(tx))

	return slot
}

func (pool *ProxyConnectionPool) append() (*ProxyConnCtl, error) {

	if len(pool.pool) >= MaxConnectionLimit {
		return nil, &ConnectionLimitError{Message: "pool size too large", Limit: MaxConnectionLimit}
	}

	slot := pool.slot()
	pool.pool = append(pool.pool, slot)

	return slot, nil
}

func (pool *ProxyConnectionPool) Add() (*ProxyConnCtl, error) {

	pool.mtx.Lock()
	defer pool.mtx.Unlock()

	var createSlot = func(idx int) *ProxyConnCtl {

		slot := pool.slot()

		pool.pool[idx] = slot
		pool.nactive++

		return slot
	}

	// try finding a slot to reuse
	for idx, slot := range pool.pool {

		// try to find an empty slot first
		if slot == nil {
			return createSlot(idx), nil
		}

		// if there's no completely empty slots,
		// try finding one with a closed controller
		if slot.done.Load() {
			pool.accountSlot(slot)
			return createSlot(idx), nil
		}
	}

	// add a new slot if no empty ones are available and if no connection limit is set
	// OR if the pool is smaller than the connection limit
	if pool.maxslots == 0 || len(pool.pool) < pool.maxslots {
		return pool.append()
	}

	return nil, &ConnectionLimitError{Message: "no available slots", Limit: len(pool.pool)}
}

func (pool *ProxyConnectionPool) WithConnection(conn net.Conn) (net.Conn, error) {

	if conn == nil {
		return nil, fmt.Errorf("a connection may not be nil")
	}

	ctl, err := pool.Add()
	if err != nil {
		return nil, err
	}
	return ctl.WithConnection(conn)
}

func (pool *ProxyConnectionPool) SetBandwidth(rx, tx int64) {
	pool.bandwidthRx.Store(max(0, rx))
	pool.bandwidthTx.Store(max(0, tx))
	pool.Rebalance()
}

func (pool *ProxyConnectionPool) Bandwidth() (rx, tx int64) {
	return pool.bandwidthRx.Load(), pool.bandwidthTx.Load()
}

func (pool *ProxyConnectionPool) SetByteRate(rxBytes, txBytes int64) {
	pool.SetBandwidth(rxBytes*8, txBytes*8)
}

func (pool *ProxyConnectionPool) ByteRate() (rxBytes, txBytes int64) {
	rx, tx := pool.Bandwidth()
	return max(0, (rx / 8)), max(0, (tx / 8))
}

func (pool *ProxyConnectionPool) baselineBandwidth() (rxBytes, txBytes int64) {

	rx, tx := pool.ByteRate()

	if n := int64(pool.nactive); n > 1 {
		return rx / n, tx / n
	}

	return rx, tx
}

func (pool *ProxyConnectionPool) Rebalance() {

	pool.mtx.Lock()
	defer pool.mtx.Unlock()

	activeSlots := pool.refreshActive()
	if len(activeSlots) == 0 {
		return
	}

	baselineRx, baselineTx := pool.baselineBandwidth()

	now := time.Now()

	var throttledRx, throttledTx int
	var availableRxBytes, availableTxBytes int64

	//	get used total equivalent bandwidth
	for _, slot := range activeSlots {

		if used := slot.TrafficRx.Load(); used >= baselineRx {
			throttledRx++
		} else if delta := utils.MomentaryEffectiveByteRate(baselineRx, now, slot.lastBalanced) - used; used > 0 && delta > 0 {
			availableRxBytes += delta
		}

		if used := slot.TrafficTx.Load(); used >= baselineTx {
			throttledTx++
		} else if delta := utils.MomentaryEffectiveByteRate(baselineTx, now, slot.lastBalanced) - used; used > 0 && delta > 0 {
			availableTxBytes += delta
		}
	}

	boostRx := availableRxBytes / int64(max(1, throttledRx))
	boostTx := availableTxBytes / int64(max(1, throttledTx))

	//	redistribute extra bandwidth and account traffic
	for _, slot := range activeSlots {

		if used := slot.TrafficRx.Load(); used >= int64(baselineRx) {
			slot.BandwidthRx.Store(baselineRx + boostRx)
		} else {
			slot.BandwidthRx.Store(baselineRx)
		}

		if used := slot.TrafficTx.Load(); used >= int64(baselineTx) {
			slot.BandwidthTx.Store(baselineTx + boostTx)
		} else {
			slot.BandwidthTx.Store(baselineTx)
		}

		pool.accountSlot(slot)
		slot.lastBalanced = now
	}
}

func (pool *ProxyConnectionPool) refreshActive() []*ProxyConnCtl {

	var entries []*ProxyConnCtl

	for idx, slot := range pool.pool {

		if slot == nil {
			continue
		}

		if slot.done.Load() {
			pool.accountSlot(slot)
			pool.pool[idx] = nil
			continue
		}

		entries = append(entries, slot)
	}

	// overwrite pool with only active slots if we don't have a limit set
	if pool.maxslots == 0 {
		pool.pool = entries
	}

	pool.nactive = len(entries)

	return entries
}

func (pool *ProxyConnectionPool) CloseConnections() {

	pool.mtx.Lock()
	defer pool.mtx.Unlock()

	for idx, slot := range pool.pool {

		if slot == nil {
			continue
		}

		if err := slot.Close(); err != nil {
			slog.Warn("ProxyConnectionPool.CloseConnections: Close connection",
				slog.String("err", err.Error()))
		}

		pool.accountSlot(slot)

		pool.pool[idx] = nil
	}
}

type ProxyConnCtl struct {
	done       atomic.Bool
	connCloser io.Closer
	mtx        sync.Mutex

	lastBalanced time.Time

	TrafficRx atomic.Int64
	TrafficTx atomic.Int64

	BandwidthRx atomic.Int64
	BandwidthTx atomic.Int64
}

func (ctl *ProxyConnCtl) WithConnection(conn net.Conn) (net.Conn, error) {

	ctl.mtx.Lock()
	defer ctl.mtx.Unlock()

	if ctl.done.Load() {
		return nil, ErrConnectionCtlClosed
	}

	if ctl.connCloser != nil {
		return nil, fmt.Errorf("connctl already tracks a connection")
	}

	ctl.connCloser = conn

	return &proxyConnection{
		Reader: NewAccountReader(NewBandwidthLimitReader(conn, &ctl.BandwidthRx), &ctl.TrafficRx),
		Writer: NewAccountWriter(NewBandwidthLimitWriter(conn, &ctl.BandwidthTx), &ctl.TrafficTx),
		ctl:    ctl,
		conn:   conn,
	}, nil
}

func (pool *ProxyConnCtl) SetBandwidth(rxBytes, txBytes int) {
	pool.BandwidthRx.Store(int64(rxBytes))
	pool.BandwidthTx.Store(int64(txBytes))
}

func (ctl *ProxyConnCtl) Close() (err error) {

	ctl.mtx.Lock()
	defer ctl.mtx.Unlock()

	if !ctl.done.CompareAndSwap(false, true) {
		return nil
	}

	if ctl.connCloser != nil {
		err = ctl.connCloser.Close()
	}

	return
}

type ConnectionLimitError struct {
	Message string
	Limit   int
}

func (err *ConnectionLimitError) Error() string {

	if err.Message == "" {
		return fmt.Sprintf("too many connections (max: %d)", err.Limit)
	}

	return err.Message
}

// proxyConnection is a wrapper on top of a net.Conn
// that provides both accounting and bandwidth limiting for a network connection
type proxyConnection struct {
	io.Reader
	io.Writer

	conn net.Conn
	ctl  *ProxyConnCtl
}

func (conn *proxyConnection) IsClosed() bool {
	return conn.ctl.done.Load()
}

func (conn *proxyConnection) Close() error {
	return conn.ctl.Close()
}

func (conn *proxyConnection) LocalAddr() net.Addr {
	return conn.conn.LocalAddr()
}

func (conn *proxyConnection) RemoteAddr() net.Addr {
	return conn.conn.RemoteAddr()
}

func (conn *proxyConnection) SetDeadline(t time.Time) error {
	return conn.conn.SetDeadline(t)
}

func (conn *proxyConnection) SetReadDeadline(t time.Time) error {
	return conn.conn.SetReadDeadline(t)
}

func (conn *proxyConnection) SetWriteDeadline(t time.Time) error {
	return conn.conn.SetWriteDeadline(t)
}
