package utils

import "time"

func UnwrapDuration(opts ...time.Duration) time.Duration {
	for _, val := range opts {
		if val >= time.Second {
			return val
		}
	}
	return 0
}

func UnwrapString(opts ...string) string {
	for _, val := range opts {
		if val != "" {
			return val
		}
	}
	return ""
}
