package browser

import (
	"errors"

	"github.com/pion/webrtc/v4/pkg/rtcerr"
)

// Native errors retain their WebRTC error class across the host Promise boundary.
// In particular, invalid signaling state is not an operational transport failure.
func rtcOperationFailure(err error) map[string]any {
	name := "OperationError"
	var state *rtcerr.InvalidStateError
	var modification *rtcerr.InvalidModificationError
	var access *rtcerr.InvalidAccessError
	var unsupported *rtcerr.NotSupportedError
	var argument *rtcerr.TypeError
	var syntax *rtcerr.SyntaxError
	var bounds *rtcerr.RangeError
	switch {
	case errors.As(err, &state):
		name = "InvalidStateError"
	case errors.As(err, &modification):
		name = "InvalidModificationError"
	case errors.As(err, &access):
		name = "InvalidAccessError"
	case errors.As(err, &unsupported):
		name = "NotSupportedError"
	case errors.As(err, &argument):
		name = "TypeError"
	case errors.As(err, &syntax):
		name = "SyntaxError"
	case errors.As(err, &bounds):
		name = "RangeError"
	}
	return cameraFailure(name, err.Error(), "")
}
