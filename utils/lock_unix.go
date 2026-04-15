//go:build unix

package utils

import (
	"net"
)

func NewNamedLock(key string) (Unlocker, error) {

	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: "@" + key, Net: "unix"})
	if err != nil {
		return nil, err
	}

	return &unixInstanceLocker{UnixListener: listener}, nil
}

type unixInstanceLocker struct {
	*net.UnixListener
}

func (lock *unixInstanceLocker) Unlock() error {
	return lock.Close()
}
