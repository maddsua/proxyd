package utils

import "time"

type TimingWatcher struct {
	Quota time.Duration

	started time.Time
	longest time.Duration
}

func (ctl *TimingWatcher) Watch() {
	ctl.started = time.Now()
}

func (ctl *TimingWatcher) Measure() (time.Duration, bool) {

	if ctl.started.IsZero() {
		return 0, false
	}

	elapsed := time.Since(ctl.started)
	if elapsed < ctl.Quota {
		ctl.longest = 0
	} else if elapsed > ctl.Quota && elapsed > ctl.longest {
		ctl.longest = elapsed
		return elapsed, true
	}

	return elapsed, false
}
