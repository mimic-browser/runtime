//go:build linux && amd64

package prebuilt

import _ "embed"

const (
	ABI      = 44
	Size     = int64(58996888)
	SHA256   = "1d2969c26bb1787e379088d7585049185377a7538f0405bbffb1076c80e40221"
	fileName = "libgov8_shim.so"
)

//go:embed linux_amd64/libgov8_shim.so.gz
var compressed []byte
