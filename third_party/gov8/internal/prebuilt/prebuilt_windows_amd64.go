//go:build windows && amd64

package prebuilt

import _ "embed"

const (
	ABI      = 45
	Size     = int64(46466048)
	SHA256   = "2225ffe7d0f40b4334c4597fb106dbace3668dd61714789c9ea538abd619cf7e"
	fileName = "gov8_shim.dll"
)

//go:embed windows_amd64/gov8_shim.dll.gz
var compressed []byte
