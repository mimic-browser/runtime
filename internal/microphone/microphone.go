// Package microphone owns portable PCM capture without a media subprocess.
package microphone

import "context"

type Device struct {
	ID, Label string
	Default   bool
}

// Format describes the PCM delivered to consumers, after native conversion.
type Format struct{ SampleRate, Channels int }
type Provider interface {
	Devices(context.Context) ([]Device, error)
	Open(context.Context, string, Format) (Capture, error)
}
type Capture interface {
	// Read returns independently owned, interleaved float PCM and honors cancellation.
	Read(context.Context) ([]float32, error)
	Close() error
}
