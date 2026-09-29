//go:build windows && amd64

package prebuilt

import _ "embed"

const (
	ABI      = 44
	Size     = int64(46464512)
	SHA256   = "55d964aa01113bf7d2f3abf3f6998d6c9afecffe0a678d368277d784b55a7610"
	fileName = "gov8_shim.dll"
)

//go:embed windows_amd64/gov8_shim.dll.gz
var compressed []byte
