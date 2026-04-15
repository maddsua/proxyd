package utils

import (
	"fmt"
	"os"
	"strings"
)

type Unlocker interface {
	Unlock() error
}

func NewInstanceLock(mode string) Unlocker {
	lock, err := NewNamedLock(fmt.Sprintf("proxyd-%s-instance-lock", mode))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Another instance is already running in %s mode\n", strings.ToUpper(mode))
		os.Exit(1)
	}
	return lock
}
