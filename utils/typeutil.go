package utils

import (
	"errors"
	"strings"
	"time"
)

func NonZeroDuration(opts ...time.Duration) time.Duration {
	for _, val := range opts {
		if val >= time.Second {
			return val
		}
	}
	return 0
}

func NonZeroString(opts ...string) string {
	for _, val := range opts {
		if val != "" {
			return val
		}
	}
	return ""
}

func JoinInlineErrors(errList ...error) error {

	var messages []string

	for _, err := range errList {
		if err != nil {
			messages = append(messages, err.Error())
		}
	}

	if len(messages) == 0 {
		return nil
	}

	return errors.New(strings.Join(messages, "; "))
}
