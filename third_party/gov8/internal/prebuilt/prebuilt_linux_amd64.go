//go:build linux && amd64

package prebuilt

import _ "embed"

const (
	ABI      = 44
	Size     = int64(57305168)
	SHA256   = "05a0ab1203b9fdacfcc9c31407d8806b9db0faa21487dbbe7adc987babf8ca44"
	fileName = "libgov8_shim.so"
)

//go:embed linux_amd64/libgov8_shim.so.gz
var compressed []byte
