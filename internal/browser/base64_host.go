package browser

import (
	"encoding/base64"
	"fmt"

	"github.com/moreveal/mimic/internal/engine"
)

// The shared binding owns WebIDL conversion. Engines with a native Base64
// intrinsic pass its fresh byte array here; other engines pass validated ASCII.
// Both paths return owned Latin-1 text, never a UTF-8 interpretation of bytes.
func installBase64Host(host map[string]any, runtime engine.Runtime) {
	function := runtime.Function
	if borrowed, ok := runtime.(interface{ TransientFunction(engine.Function) any }); ok {
		function = borrowed.TransientFunction
	}
	// Capture the engine intrinsic before browser exposure shaping installs its
	// public constructor wrappers. The function remains private to this realm.
	constructor := runtime.Get("Uint8Array")
	host["nativeBase64Decode"] = runtime.GetProperty(constructor, "fromBase64")
	releaseRuntimeValues(runtime, constructor)

	if native, ok := runtime.(interface{ BinaryStringFunction() any }); ok {
		host["binaryStringFromBytes"] = native.BinaryStringFunction()
	} else {
		host["binaryStringFromBytes"] = function(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
			data, ok := arg(args, 0).([]byte)
			if !ok {
				return nil, fmt.Errorf("binary string requires bytes")
			}
			return runtime.Value(engine.BinaryString(data)), nil
		})
	}

	host["decodeBase64String"] = function(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		data, err := base64.RawStdEncoding.DecodeString(strarg(args, 0))
		if err != nil {
			return nil, err
		}
		return runtime.Value(engine.BinaryString(data)), nil
	})
}
