package proxyd_test

import (
	"crypto/rand"
	"io"
	"math"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/maddsua/proxyd"
)

type dummyConnection struct {
	io.Reader
	Closed bool
}

func (conn *dummyConnection) Write(data []byte) (int, error) {
	return len(data), nil
}

func (conn *dummyConnection) Close() error {
	conn.Closed = true
	return nil
}

func (conn *dummyConnection) LocalAddr() net.Addr {
	return &net.TCPAddr{}
}

func (conn *dummyConnection) RemoteAddr() net.Addr {
	return &net.TCPAddr{}
}

func (conn *dummyConnection) SetDeadline(t time.Time) error {
	return nil
}

func (conn *dummyConnection) SetReadDeadline(t time.Time) error {
	return nil
}

func (conn *dummyConnection) SetWriteDeadline(t time.Time) error {
	return nil
}

func Test_ProxyConnectionPoolManagement_1(t *testing.T) {

	var pool proxyd.ProxyConnectionPool
	pool.SetConnectionLimit(1)

	dc := &dummyConnection{}

	conn, err := pool.WithConnection(dc)
	if err != nil {
		t.Fatalf("pool.WithConnection: %v", err)
		return
	}

	if _, err := pool.WithConnection(&dummyConnection{}); err == nil {
		t.Fatalf("pool accepted more connections than expected")
		return
	}

	if err := conn.Close(); err != nil {
		t.Fatalf("conn.Close: %v", err)
		return
	}

	if !dc.Closed {
		t.Fatalf("underlying connection wasn't closed by the controller")
		return
	}

	if _, err := pool.WithConnection(&dummyConnection{}); err != nil {
		t.Fatalf("pool failed to free connection slot")
		return
	}
}

func Test_ProxyConnectionWrapper_1(t *testing.T) {

	var pool proxyd.ProxyConnectionPool

	const readSize = math.MaxUint16
	const writeSize = math.MaxUint16 / 5

	ctl, err := pool.Add()
	if err != nil {
		t.Fatalf("pool.Add: %v", err)
		return
	}

	conn, err := ctl.WithConnection(&dummyConnection{Reader: io.LimitReader(rand.Reader, readSize)})
	if err != nil {
		t.Fatalf("ctl.WithConnection: %v", err)
		return
	}

	if n, err := io.Copy(conn, io.LimitReader(rand.Reader, writeSize)); err != nil {
		t.Fatalf("io.Copy: %v", err)
		return
	} else if n != writeSize {
		t.Fatalf("invalid write size: %v", n)
		return
	}

	if n, err := io.Copy(io.Discard, conn); err != nil {
		t.Fatalf("io.Copy: %v", err)
		return
	} else if n != readSize {
		t.Fatalf("invalid read size: %v", n)
		return
	}

	if rx, tx := ctl.TrafficRx.Load(), ctl.TrafficTx.Load(); rx != readSize {
		t.Fatalf("invalid accounted received traffic: %v", rx)
		return
	} else if tx != writeSize {
		t.Fatalf("invalid accounted sent traffic: %v", tx)
		return
	}
}

func Test_ProxyPool_Accounting_1(t *testing.T) {

	var pool proxyd.ProxyConnectionPool

	const readSize = math.MaxUint16
	const writeSize = math.MaxUint16 / 5

	conn, err := pool.WithConnection(&dummyConnection{Reader: io.LimitReader(rand.Reader, readSize)})
	if err != nil {
		t.Fatalf("pool.WithConnection: %v", err)
		return
	}

	if n, err := io.Copy(conn, io.LimitReader(rand.Reader, writeSize)); err != nil {
		t.Fatalf("io.Copy: %v", err)
		return
	} else if n != writeSize {
		t.Fatalf("invalid write size: %v", n)
		return
	}

	if n, err := io.Copy(io.Discard, conn); err != nil {
		t.Fatalf("io.Copy: %v", err)
		return
	} else if n != readSize {
		t.Fatalf("invalid read size: %v", n)
		return
	}

	// call rebalance to account traffic
	pool.Rebalance()
	// call it twice to make sure that traffic is accounted correctly
	pool.Rebalance()

	if rx := pool.TrafficRx.Load(); rx != readSize {
		t.Fatalf("invalid accounted received traffic: %v", rx)
		return
	} else if tx := pool.TrafficTx.Load(); tx != writeSize {
		t.Fatalf("invalid accounted sent traffic: %v", tx)
		return
	}
}

func Test_ProxyPool_Accounting_2(t *testing.T) {

	var pool proxyd.ProxyConnectionPool

	const readSize = math.MaxUint16
	const writeSize = math.MaxUint16 / 5

	conn, err := pool.WithConnection(&dummyConnection{Reader: io.LimitReader(rand.Reader, readSize)})
	if err != nil {
		t.Fatalf("pool.WithConnection: %v", err)
		return
	}

	if n, err := io.Copy(conn, io.LimitReader(rand.Reader, writeSize)); err != nil {
		t.Fatalf("io.Copy: %v", err)
		return
	} else if n != writeSize {
		t.Fatalf("invalid write size: %v", n)
		return
	}

	if n, err := io.Copy(io.Discard, conn); err != nil {
		t.Fatalf("io.Copy: %v", err)
		return
	} else if n != readSize {
		t.Fatalf("invalid read size: %v", n)
		return
	}

	pool.CloseConnections()

	if rx := pool.TrafficRx.Load(); rx != readSize {
		t.Fatalf("invalid accounted received traffic: %v", rx)
		return
	} else if tx := pool.TrafficTx.Load(); tx != writeSize {
		t.Fatalf("invalid accounted sent traffic: %v", tx)
		return
	}
}

func Test_PoolResize_Shrink(t *testing.T) {

	var pool proxyd.ProxyConnectionPool

	var list []*proxyd.ProxyConnCtl
	for range 10 {
		ctl, err := pool.Add()
		if err != nil {
			t.Fatal("pool.Add:", err)
		}
		list = append(list, ctl)
	}

	const limit = 5

	if err := pool.SetConnectionLimit(limit); err != nil {
		t.Fatal("pool.SetConnectionLimit:", err)
	}

	pool.Rebalance()

	var totalActive int
	for _, ctl := range list {
		if _, err := ctl.WithConnection(nil); err != proxyd.ErrConnectionCtlClosed {
			totalActive++
		}
	}

	if totalActive != limit {
		t.Fatal("unexpected active slot count", totalActive)
	}
}

func Test_PoolResize_Limit(t *testing.T) {

	const limit = 20

	var pool proxyd.ProxyConnectionPool

	if err := pool.SetConnectionLimit(limit); err != nil {
		t.Fatal("pool.SetConnectionLimit:", err)
	}

	var total int

	for n := range 100 {

		if n > limit {
			t.Fatal("pool accepted more connections than allowed by the limit")
		}

		if _, err := pool.Add(); err != nil {
			break
		}
		total++
	}

	if total != limit {
		t.Fatal("unexpected factual pool size", total)
	}

}

func Test_PoolResize_Reuse(t *testing.T) {

	const initialLimit = 20

	var pool proxyd.ProxyConnectionPool

	if err := pool.SetConnectionLimit(initialLimit); err != nil {
		t.Fatal("pool.SetConnectionLimit:", err)
	}

	var latest *proxyd.ProxyConnCtl
	for n := range 100 {

		if n > initialLimit {
			t.Fatal("pool accepted more connections than allowed by the limit")
		}

		ctl, err := pool.Add()
		if err != nil {
			break
		}
		latest = ctl
	}

	if err := latest.Close(); err != nil {
		t.Fatal("latest.Close:", err)
	}

	if _, err := pool.Add(); err != nil {
		t.Fatal("pool.Add:", err)
	}
}

func Test_PoolResize_Grow(t *testing.T) {

	const initialLimit = 20

	var pool proxyd.ProxyConnectionPool

	if err := pool.SetConnectionLimit(initialLimit); err != nil {
		t.Fatal("pool.SetConnectionLimit:", err)
	}

	var total int

	for n := range 100 {

		if n > initialLimit {
			t.Fatal("pool accepted more connections than allowed by the limit")
		}

		if _, err := pool.Add(); err != nil {
			break
		}
		total++
	}

	const finalLimit = 25

	if err := pool.SetConnectionLimit(finalLimit); err != nil {
		t.Fatal("pool.SetConnectionLimit:", err)
	}

	pool.Rebalance()

	for range finalLimit - initialLimit {

		if _, err := pool.Add(); err != nil {
			t.Fatal("pool.Add:", err)
		}
		total++
	}

	if total != finalLimit {
		t.Fatal("unexpected factual pool size", total)
	}
}

func Test_PoolBandwidth_Read_1(t *testing.T) {

	const bandwidth = 10_000
	const expectMs = 1_500
	const volume = int64((bandwidth / 8) * (float64(expectMs) / 1000))

	var pool proxyd.ProxyConnectionPool
	pool.SetBandwidth(bandwidth, bandwidth)

	conn, err := pool.WithConnection(&dummyConnection{Reader: io.LimitReader(rand.Reader, volume)})
	if err != nil {
		t.Fatalf("pool.WithConnection: %v", err)
		return
	}

	started := time.Now()

	if _, err := io.Copy(io.Discard, conn); err != nil {
		t.Fatalf("io.Copy: %v", err)
		return
	}

	elapsed := time.Since(started)

	//	calculate millisecond-scale deviation in percent
	deviation := (math.Abs(float64(elapsed.Milliseconds())-float64(expectMs)) / float64(expectMs)) * 100

	if deviation > 1 {
		t.Fatalf("unexpected read duration: %v, deviated: %.2f%%", elapsed, deviation)
		return
	}

	t.Logf("deviation: %.2f%%", deviation)
}

func Test_PoolBandwidth_Read_2(t *testing.T) {

	const nconn = 3
	const bandwidth = 30_000 * nconn
	const volumePerConn = (bandwidth / 8) / nconn
	const expectMs = 1_000

	var pool proxyd.ProxyConnectionPool
	pool.SetBandwidth(bandwidth, bandwidth)

	started := time.Now()

	connChan := make(chan net.Conn, nconn)

	for range nconn {

		conn, err := pool.WithConnection(&dummyConnection{Reader: io.LimitReader(rand.Reader, volumePerConn)})
		if err != nil {
			t.Fatalf("pool.WithConnection: %v", err)
			return
		}

		connChan <- conn
	}

	close(connChan)

	pool.Rebalance()

	var wg sync.WaitGroup

	for conn := range connChan {

		wg.Add(1)

		go func() {

			defer wg.Done()

			if _, err := io.Copy(io.Discard, conn); err != nil {
				panic("copy: " + err.Error())
			}

		}()
	}

	wg.Wait()

	elapsed := time.Since(started)

	//	calculate millisecond-scale deviation in percent
	deviation := (math.Abs(float64(elapsed.Milliseconds())-float64(expectMs)) / float64(expectMs)) * 100

	if deviation > 1 {
		t.Fatalf("unexpected read duration: %v, deviated: %.2f%%", elapsed, deviation)
		return
	}

	t.Logf("deviation: %.2f%%", deviation)
}

func Test_PoolBandwidth_Write_1(t *testing.T) {

	const bandwidth = 10_000
	const expectMs = 1_500
	const volume = int64((bandwidth / 8) * (float64(expectMs) / 1000))

	var pool proxyd.ProxyConnectionPool
	pool.SetBandwidth(bandwidth, bandwidth)

	conn, err := pool.WithConnection(&dummyConnection{})
	if err != nil {
		t.Fatalf("pool.WithConnection: %v", err)
		return
	}

	started := time.Now()

	if _, err := io.Copy(conn, io.LimitReader(rand.Reader, volume)); err != nil {
		t.Fatalf("io.Copy: %v", err)
		return
	}

	elapsed := time.Since(started)

	//	calculate millisecond-scale deviation in percent
	deviation := (math.Abs(float64(elapsed.Milliseconds())-float64(expectMs)) / float64(expectMs)) * 100

	if deviation > 1 {
		t.Fatalf("unexpected write duration: %v, deviated: %.2f%%", elapsed, deviation)
		return
	}

	t.Logf("deviation: %.2f%%", deviation)
}

func Test_PoolBandwidth_Write_2(t *testing.T) {

	const nconn = 3
	const bandwidth = 50_000 * nconn
	const volumePerConn = (bandwidth / 8) / nconn
	const expectMs = 1_000

	var pool proxyd.ProxyConnectionPool
	pool.SetBandwidth(bandwidth, bandwidth)

	started := time.Now()

	connChan := make(chan net.Conn, nconn)

	for range nconn {

		conn, err := pool.WithConnection(&dummyConnection{})
		if err != nil {
			t.Fatalf("pool.WithConnection: %v", err)
			return
		}

		connChan <- conn
	}

	close(connChan)

	pool.Rebalance()

	var wg sync.WaitGroup

	for conn := range connChan {

		wg.Add(1)

		go func() {

			defer wg.Done()

			if _, err := io.Copy(conn, io.LimitReader(rand.Reader, volumePerConn)); err != nil {
				panic("copy: " + err.Error())
			}

		}()
	}

	wg.Wait()

	elapsed := time.Since(started)

	//	calculate millisecond-scale deviation in percent
	deviation := (math.Abs(float64(elapsed.Milliseconds())-float64(expectMs)) / float64(expectMs)) * 100

	if deviation > 1 {
		t.Fatalf("unexpected write duration: %v, deviated: %.2f%%", elapsed, deviation)
		return
	}

	t.Logf("deviation: %.2f%%", deviation)
}
