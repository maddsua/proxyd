package proxyd_test

import (
	"crypto/rand"
	"io"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maddsua/proxyd"
)

func Test_BandwidthLimitWriter_1(t *testing.T) {

	data := make([]byte, 100_000)
	var limit atomic.Int64
	limit.Store(250_000)

	started := time.Now()

	total, err := proxyd.NewBandwidthLimitWriter(io.Discard, &limit).Write(data)
	if err != nil {
		t.Fatalf("err: %v", err)
		return
	} else if expect := len(data); total != expect {
		t.Fatalf("len error: %d instead of %d", total, expect)
		return
	}

	elapsed := time.Since(started)

	const expectMs = 400

	//	calculate millisecond-scale deviation in percent
	deviation := (math.Abs(float64(elapsed.Milliseconds())-float64(expectMs)) / float64(expectMs)) * 100

	if deviation > 1 {
		t.Fatalf("unexpected write duration: %v, deviated: %.2f%%", elapsed, deviation)
		return
	}

	t.Logf("deviation: %.2f%%", deviation)
}

func Test_BandwidthLimitWriter_2(t *testing.T) {

	data := make([]byte, 1_000_000)
	var limit atomic.Int64
	limit.Store(250_000)

	started := time.Now()

	total, err := proxyd.NewBandwidthLimitWriter(io.Discard, &limit).Write(data)
	if err != nil {
		t.Fatalf("err: %v", err)
		return
	} else if expect := len(data); total != expect {
		t.Fatalf("len error: %d instead of %d", total, expect)
		return
	}

	elapsed := time.Since(started)

	const expectMs = 4_000

	//	calculate millisecond-scale deviation in percent
	deviation := (math.Abs(float64(elapsed.Milliseconds())-float64(expectMs)) / float64(expectMs)) * 100

	if deviation > 1 {
		t.Fatalf("unexpected write duration: %v, deviated: %.2f%%", elapsed, deviation)
		return
	}

	t.Logf("deviation: %.2f%%", deviation)
}

func Test_BandwidthLimitWriter_3(t *testing.T) {

	data := make([]byte, math.MaxUint16)
	var limit atomic.Int64
	limit.Store(1_000_000)

	started := time.Now()

	total, err := proxyd.NewBandwidthLimitWriter(io.Discard, &limit).Write(data)
	if err != nil {
		t.Fatalf("err: %v", err)
		return
	} else if expect := len(data); total != expect {
		t.Fatalf("len error: %d instead of %d", total, expect)
		return
	}

	elapsed := time.Since(started)

	const expectMs = 66

	//	calculate millisecond-scale deviation in percent
	deviation := (math.Abs(float64(elapsed.Milliseconds())-float64(expectMs)) / float64(expectMs)) * 100

	if deviation > 1 {
		t.Fatalf("unexpected write duration: %v, deviated: %.2f%%", elapsed, deviation)
		return
	}

	t.Logf("deviation: %.2f%%", deviation)
}

func Test_BandwidthLimitWriter_4(t *testing.T) {

	data := make([]byte, 100*math.MaxUint16)
	var limit atomic.Int64
	limit.Store(1_000_000)

	started := time.Now()

	total, err := proxyd.NewBandwidthLimitWriter(io.Discard, &limit).Write(data)
	if err != nil {
		t.Fatalf("err: %v", err)
		return
	} else if expect := len(data); total != expect {
		t.Fatalf("len error: %d instead of %d", total, expect)
		return
	}

	elapsed := time.Since(started)

	const expectMs = 6_600

	//	calculate millisecond-scale deviation in percent
	deviation := (math.Abs(float64(elapsed.Milliseconds())-float64(expectMs)) / float64(expectMs)) * 100

	if deviation > 1 {
		t.Fatalf("unexpected write duration: %v, deviated: %.2f%%", elapsed, deviation)
		return
	}

	t.Logf("deviation: %.2f%%", deviation)
}

type triggerWriter struct {
	Trigger func(count int)
	Count   int
}

func (wrt *triggerWriter) Write(data []byte) (int, error) {
	wrt.Count++
	wrt.Trigger(wrt.Count)
	return len(data), nil
}

func Test_BandwidthLimitWriter_Dynamic_1(t *testing.T) {

	data := make([]byte, 100*math.MaxUint16)
	var limit atomic.Int64
	limit.Store(1_000_000)

	started := time.Now()

	total, err := proxyd.NewBandwidthLimitWriter(&triggerWriter{
		Trigger: func(count int) {
			limit.Store(10_000_000)
		},
	}, &limit).Write(data)

	if err != nil {
		t.Fatalf("err: %v", err)
		return
	} else if expect := len(data); total != expect {
		t.Fatalf("len error: %d instead of %d", total, expect)
		return
	}

	elapsed := time.Since(started)

	const expectMs = 1_556

	//	calculate millisecond-scale deviation in percent
	deviation := (math.Abs(float64(elapsed.Milliseconds())-float64(expectMs)) / float64(expectMs)) * 100

	if deviation > 1 {
		t.Fatalf("unexpected write duration: %v, deviated: %.2f%%", elapsed, deviation)
		return
	}

	t.Logf("deviation: %.2f%%", deviation)
}

func Test_BandwidthLimitWriter_Dynamic_2(t *testing.T) {

	data := make([]byte, 2_000_000)
	var limit atomic.Int64
	limit.Store(1_000_000)

	started := time.Now()

	total, err := proxyd.NewBandwidthLimitWriter(&triggerWriter{
		Trigger: func(count int) {
			limit.Store(500_000)
		},
	}, &limit).Write(data)

	if err != nil {
		t.Fatalf("err: %v", err)
		return
	} else if expect := len(data); total != expect {
		t.Fatalf("len error: %d instead of %d", total, expect)
		return
	}

	elapsed := time.Since(started)

	const expectMs = 3_000

	//	calculate millisecond-scale deviation in percent
	deviation := (math.Abs(float64(elapsed.Milliseconds())-float64(expectMs)) / float64(expectMs)) * 100

	if deviation > 1 {
		t.Fatalf("unexpected write duration: %v, deviated: %.2f%%", elapsed, deviation)
		return
	}

	t.Logf("deviation: %.2f%%", deviation)
}

func Test_BandwidthLimitWriter_Dynamic_3(t *testing.T) {

	data := make([]byte, 3_000_000)
	var limit atomic.Int64

	limit.Store(1_000_000)

	started := time.Now()

	total, err := proxyd.NewBandwidthLimitWriter(&triggerWriter{
		Trigger: func(count int) {
			switch count {
			case 1:
				limit.Store(500_000)
			case 2:
				limit.Store(1_000_000)
			}
		},
	}, &limit).Write(data)

	if err != nil {
		t.Fatalf("err: %v", err)
		return
	} else if expect := len(data); total != expect {
		t.Fatalf("len error: %d instead of %d", total, expect)
		return
	}

	elapsed := time.Since(started)

	const expectMs = 3_500

	//	calculate millisecond-scale deviation in percent
	deviation := (math.Abs(float64(elapsed.Milliseconds())-float64(expectMs)) / float64(expectMs)) * 100

	if deviation > 1 {
		t.Fatalf("unexpected write duration: %v, deviated: %.2f%%", elapsed, deviation)
		return
	}

	t.Logf("deviation: %.2f%%", deviation)
}

func Test_BandwidthLimitReader_1(t *testing.T) {

	upstream := io.LimitReader(rand.Reader, 16_384)

	var limit atomic.Int64

	limit.Store(1_000_000)

	started := time.Now()

	if total, err := io.Copy(io.Discard, proxyd.NewBandwidthLimitReader(upstream, &limit)); err != nil {
		t.Fatalf("err: %v", err)
		return
	} else if total != 16_384 {
		t.Fatalf("read wrong size: %v", total)
		return
	}

	elapsed := time.Since(started)

	const expectMs = 16

	//	calculate millisecond-scale deviation in percent
	deviation := (math.Abs(float64(elapsed.Milliseconds())-float64(expectMs)) / float64(expectMs)) * 100

	if deviation > 15 {
		t.Fatalf("unexpected read duration: %v, deviated: %.2f%%", elapsed, deviation)
		return
	}

	t.Logf("deviation: %.2f%%", deviation)
}

func Test_BandwidthLimitReader_2(t *testing.T) {

	upstream := io.LimitReader(rand.Reader, 4*math.MaxUint16)
	var limit atomic.Int64

	limit.Store(100_000)

	started := time.Now()

	if total, err := io.Copy(io.Discard, proxyd.NewBandwidthLimitReader(upstream, &limit)); err != nil {
		t.Fatalf("err: %v", err)
		return
	} else if total != 4*math.MaxUint16 {
		t.Fatalf("read wrong size: %v", total)
		return
	}

	elapsed := time.Since(started)

	const expectMs = 2_621

	//	calculate millisecond-scale deviation in percent
	deviation := (math.Abs(float64(elapsed.Milliseconds())-float64(expectMs)) / float64(expectMs)) * 100

	if deviation > 1 {
		t.Fatalf("unexpected read duration: %v, deviated: %.2f%%", elapsed, deviation)
		return
	}

	t.Logf("deviation: %.2f%%", deviation)
}

func Test_BandwidthLimitReader_3(t *testing.T) {

	upstream := io.LimitReader(rand.Reader, math.MaxUint16)
	var limit atomic.Int64

	limit.Store(100_000)

	started := time.Now()

	if total, err := io.Copy(io.Discard, proxyd.NewBandwidthLimitReader(upstream, &limit)); err != nil {
		t.Fatalf("err: %v", err)
		return
	} else if total != math.MaxUint16 {
		t.Fatalf("read wrong size: %v", total)
		return
	}

	elapsed := time.Since(started)

	const expectMs = 659

	//	calculate millisecond-scale deviation in percent
	deviation := (math.Abs(float64(elapsed.Milliseconds())-float64(expectMs)) / float64(expectMs)) * 100

	if deviation > 1 {
		t.Fatalf("unexpected read duration: %v, deviated: %.2f%%", elapsed, deviation)
		return
	}

	t.Logf("deviation: %.2f%%", deviation)
}

func Test_AccountWriter_1(t *testing.T) {

	const size = math.MaxUint16

	source := io.LimitReader(rand.Reader, size)
	var acct atomic.Int64

	total, err := io.Copy(proxyd.NewAccountWriter(io.Discard, &acct), source)
	if err != nil {
		t.Fatalf("err: %v", err)
		return
	}

	if val := acct.Load(); total != int64(val) || val != size {
		t.Fatalf("doesn't add up: written: %d; accounted: %d; should have: %d", total, val, size)
		return
	}
}

func Test_AccountReader_1(t *testing.T) {

	const size = math.MaxUint16

	source := io.LimitReader(rand.Reader, size)
	var acct atomic.Int64

	total, err := io.Copy(io.Discard, proxyd.NewAccountReader(source, &acct))
	if err != nil {
		t.Fatalf("err: %v", err)
		return
	}

	if val := acct.Load(); total != int64(val) || val != size {
		t.Fatalf("doesn't add up: read: %d; accounted: %d; should have: %d", total, val, size)
		return
	}
}
