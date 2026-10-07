// Package camera owns portable camera capture, independently of browser realms.
package camera

import (
	"context"
	"errors"
	"image"
)

var ErrFrameTimeout = errors.New("camera frame timeout")

type Format struct {
	Width     int     `json:"width"`
	Height    int     `json:"height"`
	FrameRate float64 `json:"frameRate"`
	Native    any     `json:"-"`
}

type Device struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// Provider enumerates without opening a stream. Open returns an independently
// owned capture; only physical device exclusivity can prevent concurrent use.
type Provider interface {
	Devices(context.Context) ([]Device, error)
	Open(context.Context, string, func([]Format) (Format, error)) (Capture, Format, error)
}

type Capture interface {
	Read() (image.Image, func(), error)
	Close() error
}
