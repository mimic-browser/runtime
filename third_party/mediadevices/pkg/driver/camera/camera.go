/*
Package camera provides a video camera driver.

Device Label Generation Rules

On Linux, the device label will be in the format of:
	pci-0000:00:00.0-usb-0:0:0.0-video-index0;video0
If /dev/v4l/by-path/* is not available (for example in a docker container without
bindings in /dev/v4l/by-path/), it will be:
	video0;video0
*/
package camera

import (
	"errors"
	"github.com/pion/mediadevices/pkg/driver"
)

// LabelSeparator is used to separate labels for a driver that
// is found from multiple locations on a host.
const LabelSeparator = ";"

var ErrReadTimeout = errors.New("camera frame timeout")

// Device is a fresh, independently owned platform adapter. Discovery does not
// register process-global mutable camera instances.
type Device struct {
	Adapter driver.Adapter
	Info    driver.Info
}
