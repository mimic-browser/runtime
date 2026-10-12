//go:build windows

// Package monotime measures real elapsed time without changing Windows timer
// resolution. Browser schedulers remain authoritative for virtual time.
package monotime

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel                                  = windows.NewLazySystemDLL("kernel32.dll")
	qpc                                     = kernel.NewProc("QueryPerformanceCounter")
	frequency                               = kernel.NewProc("QueryPerformanceFrequency")
	clockFrequency, clockStart, clockOrigin = initialize()
)

func readCounter() int64 {
	var value int64
	if ok, _, err := qpc.Call(uintptr(unsafe.Pointer(&value))); ok == 0 {
		panic(err)
	}
	return value
}

func initialize() (int64, int64, time.Time) {
	var hz int64
	if ok, _, err := frequency.Call(uintptr(unsafe.Pointer(&hz))); ok == 0 {
		panic(err)
	}
	if hz <= 0 {
		panic("invalid QueryPerformanceFrequency")
	}
	origin := time.Now()
	return hz, readCounter(), origin
}

// Now's monotonic component is anchored once, then advanced only by QPC.
// Do not mix time.Since with these timestamps: Go's Windows monotonic source
// uses interrupt time and can have a roughly 500us observable tick.
func Now() time.Time {
	ticks := readCounter() - clockStart
	seconds, fraction := ticks/clockFrequency, ticks%clockFrequency
	elapsed := time.Duration(seconds)*time.Second + time.Duration(fraction*int64(time.Second)/clockFrequency)
	return clockOrigin.Add(elapsed)
}

func Since(start time.Time) time.Duration { return Now().Sub(start) }

// FloatMilliseconds returns the platform monotonic coordinate as a double.
// Blink converts TimeTicks through this coordinate before subtracting a
// document origin. Its magnitude therefore contributes observable IEEE-754
// rounding to DOMHighResTimeStamp values.
func FloatMilliseconds() float64 {
	return float64(readCounter()) * 1000 / float64(clockFrequency)
}
