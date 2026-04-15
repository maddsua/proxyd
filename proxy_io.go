package proxyd

import (
	"io"
	"sync/atomic"
	"time"
)

// Sets a maximal throttlable bandwidth limit at around 10 GBit/s;
// Values over this limit should be treated as no limit at all
const BandwidthLimitMax = 1_250_000_000

func NewBandwidthLimitWriter(writer io.Writer, limiter *atomic.Int64) io.Writer {
	return &BandwidthLimitWriter{Writer: writer, Limiter: limiter}
}

// BandwidthLimitWriter implements a simple writer wrapper that allows for the bandwidth to be externally controlled
type BandwidthLimitWriter struct {
	Writer  io.Writer
	Limiter *atomic.Int64
}

func (wrt *BandwidthLimitWriter) Write(data []byte) (int, error) {

	var total int

	dataSize := len(data)
	if dataSize == 0 {
		return 0, nil
	}

	bandwidth := ioBandwidthBytes(wrt.Limiter)

	//	write data in small chunks and wait for the appropriate amount of time to fascilitate bandwidth limiting
	for ; total < dataSize && bandwidth > 0; bandwidth = ioBandwidthBytes(wrt.Limiter) {

		chunkSize := min(bandwidth, dataSize-total)
		started := time.Now()

		written, err := wrt.Writer.Write(data[total : total+chunkSize])
		total += written

		//	impose an artificial write delay
		if written > 0 {
			if written == bandwidth {
				ioWaitChunk(started, time.Second)
			} else {
				ioWaitChunk(started, ioOperationDuration(bandwidth, written))
			}
		}

		if err != nil {
			return total, err
		} else if written < chunkSize {
			return total, io.ErrShortWrite
		}
	}

	//	handles cases when bandwidth was changed to zero mid write or when it wasn't set to begin with
	if total < dataSize {

		written, err := wrt.Writer.Write(data[total:])
		total += written

		if err != nil {
			return total, err
		}
	}

	return total, nil
}

// unwraps a nullable atomic.Int64 bandwidth value into a native int representation
func ioBandwidthBytes(limiter *atomic.Int64) int {

	if limiter == nil {
		return 0
	}

	val := limiter.Load()
	if val >= BandwidthLimitMax {
		return 0
	}

	return int(val)
}

func ioWaitChunk(started time.Time, expected time.Duration) {

	//	short circuit possible logic errors, this MAY NOT possibly block for longer than a second
	if expected > time.Second {
		return
	}

	//	make sure we do actually have to wait in order ot meet the expectation
	elapsed := time.Since(started)
	if elapsed >= expected {
		return
	}

	time.Sleep(expected - elapsed)
}

func ioOperationDuration(rate, size int) time.Duration {
	return time.Duration((int64(time.Second) * int64(size)) / int64(rate))
}

// BandwidthLimitReader implements a simple reader wrapper that allows for the bandwidth to be externally controlled
// It is quite a lot more quirky when it comes to precision than the writer,
// therefore the latter should be preferred when possible
type BandwidthLimitReader struct {
	Reader  io.Reader
	Limiter *atomic.Int64
}

func NewBandwidthLimitReader(reader io.Reader, limiter *atomic.Int64) io.Reader {
	return &BandwidthLimitReader{Reader: reader, Limiter: limiter}
}

func (reader *BandwidthLimitReader) Read(buff []byte) (int, error) {

	bandwidth := ioBandwidthBytes(reader.Limiter)
	if bandwidth == 0 {
		return reader.Reader.Read(buff)
	}

	readSize := min(bandwidth, len(buff))
	started := time.Now()

	read, err := reader.Reader.Read(buff[0:readSize])

	//	impose an artificial read delay
	if read > 0 {
		if read == bandwidth {
			ioWaitChunk(started, time.Second)
		} else {
			ioWaitChunk(started, ioOperationDuration(bandwidth, read))
		}
	}

	return read, err
}

func NewAccountWriter(writer io.Writer, acc *atomic.Int64) io.Writer {
	return &AccountWriter{Writer: writer, Acc: acc}
}

// AccountWriter is a wrapper on top of a normal writer
// that counts total data volume in realtime using an accumulator (atomic int)
type AccountWriter struct {
	Writer io.Writer
	Acc    *atomic.Int64
}

func (wrt *AccountWriter) Write(data []byte) (int, error) {
	written, err := wrt.Writer.Write(data)
	ioAccumulateDataVolumeDelta(wrt.Acc, written)
	return written, err
}

func ioAccumulateDataVolumeDelta(acc *atomic.Int64, delta int) {
	if acc == nil || delta <= 0 {
		return
	}
	acc.Add(int64(delta))
}

func NewAccountReader(reader io.Reader, acc *atomic.Int64) io.Reader {
	return &AccountReader{Reader: reader, Acc: acc}
}

// AccountReader is a wrapper on top of a normal reader
// that counts total data volume in realtime using an accumulator (atomic int)
type AccountReader struct {
	Reader io.Reader
	Acc    *atomic.Int64
}

func (reader *AccountReader) Read(buff []byte) (int, error) {
	read, err := reader.Reader.Read(buff)
	ioAccumulateDataVolumeDelta(reader.Acc, read)
	return read, err
}
