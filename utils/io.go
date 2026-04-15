package utils

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// Read a set number of bytes into a buffer
func ReadN(reader io.Reader, n int) ([]byte, error) {

	if n <= 0 {
		return nil, nil
	}

	buff := make([]byte, n)
	read, err := reader.Read(buff)
	if read == len(buff) {
		return buff, nil
	} else if err == nil && read != len(buff) {
		return buff[:read], io.EOF
	}

	return buff, err
}

// Read just one byte
func ReadByte(reader io.Reader) (byte, error) {
	buff, err := ReadN(reader, 1)
	return buff[0], err
}

// Reads a null-terminated string
//
// Note: it's slow as balls for larger sizes and you should use bufio instead
func ReadNullTerminatedString(reader io.Reader, limit int) (string, error) {

	var buff bytes.Buffer

	for limit > 0 && buff.Len() < limit {

		next, err := ReadByte(reader)
		if err != nil {
			//	 don't handle EOF specially as a valid null-termianted string should not result in an EOF
			return "", err
		}

		if next == 0x00 {
			return buff.String(), nil
		}

		buff.WriteByte(next)
	}

	return "", fmt.Errorf("input too large")
}

// Pipes data between two connections
func PipeDuplexContext(ctx context.Context, remote, local net.Conn) (err error) {

	doneCh := make(chan error, 2)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		doneCh <- pipeConnection(remote, local)
	}()

	go func() {
		defer wg.Done()
		doneCh <- pipeConnection(local, remote)
	}()

	select {
	case err = <-doneCh:
		// when any of the pipes exits, just wait until the other one does so too
	case <-ctx.Done():
		// but if the context gets cancelled before that, set deadlines to 0,
		// causing copy operations to error out and pipes to exit
		_ = remote.SetDeadline(time.Unix(1, 0))
		_ = local.SetDeadline(time.Unix(1, 0))
	}

	wg.Wait()

	return err
}

type ClosedReporter interface {
	IsClosed() bool
}

func pipeConnection(dst, src net.Conn) error {

	// Prevent reading more data from the opposite pipe as soon as we failed
	// to read more data to be sent or failed to send it,
	defer dst.SetReadDeadline(time.Unix(1, 0))

	// copy data normally until something happens
	_, err := io.Copy(dst, src)

	// exit with no error reported if any of the pipe ends was closed intentionally
	if cr, ok := dst.(ClosedReporter); ok && cr.IsClosed() {
		return nil
	} else if cr, ok = src.(ClosedReporter); ok && cr.IsClosed() {
		return nil
	}

	return err
}

func KbitsToRawBandwidth(val int) int {
	return max(0, val*125)
}

func BitsToRawBandwidth(val int) int {
	return max(0, val/8)
}

func RawBandwidthToBits(val int) int {
	return max(0, val*8)
}

func MomentaryEffectiveBandwidth(base int64, moment, after time.Time) int64 {

	if base <= 0 {
		return 0
	}

	if after.IsZero() {
		return base
	}

	elapsed := moment.Sub(after).Seconds()
	effective := int64(elapsed * float64(base))

	// santify check to make sure that floating point multiplication doesn't produce completely bogus results.
	bound := (int64(elapsed) + 1) * base
	return max(0, min(effective, bound))
}
