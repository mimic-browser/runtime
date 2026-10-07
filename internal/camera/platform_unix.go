//go:build cgo && (linux || darwin)

package camera

func beginPlatform() error { return nil }
func endPlatform()         {}
