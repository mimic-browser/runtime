//go:build !cgo || (!windows && !linux && !darwin)

package microphone

import (
	"context"
	"fmt"
)

type unavailable struct{}

func System() Provider { return unavailable{} }
func (unavailable) Devices(ctx context.Context) ([]Device, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("native microphone capture is unavailable on this build")
}
func (unavailable) Open(ctx context.Context, _ string, _ Format) (Capture, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("native microphone capture is unavailable on this build")
}
