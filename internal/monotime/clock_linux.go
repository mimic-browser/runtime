//go:build linux

package monotime

import (
	"time"

	"golang.org/x/sys/unix"
)

func Now() time.Time                      { return time.Now() }
func Since(start time.Time) time.Duration { return time.Since(start) }

// FloatMilliseconds uses the same boot-relative monotonic coordinate exposed
// by Chromium's TimeTicks on Linux.
func FloatMilliseconds() float64 {
	var value unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_MONOTONIC, &value); err != nil {
		return 0
	}
	return float64(value.Sec)*1000 + float64(value.Nsec)/float64(time.Millisecond)
}
