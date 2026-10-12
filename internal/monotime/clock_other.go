//go:build !windows && !linux

package monotime

import "time"

var processOrigin = time.Now()

func Now() time.Time                      { return time.Now() }
func Since(start time.Time) time.Duration { return time.Since(start) }
func FloatMilliseconds() float64 {
	return float64(time.Since(processOrigin)) / float64(time.Millisecond)
}
