//go:build !cgo || (!windows && !linux && !darwin)

package camera

import (
	"context"
	"fmt"
)

type unavailableProvider struct{}

func System() Provider                                                { return unavailableProvider{} }
func (unavailableProvider) Devices(context.Context) ([]Device, error) { return []Device{}, nil }
func (unavailableProvider) Open(context.Context, string, func([]Format) (Format, error)) (Capture, Format, error) {
	return nil, Format{}, fmt.Errorf("camera capture requires a native Windows, Linux or macOS build with cgo")
}
