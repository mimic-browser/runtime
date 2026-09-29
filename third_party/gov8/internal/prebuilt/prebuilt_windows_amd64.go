//go:build windows && amd64

package prebuilt

import _ "embed"

const (
	ABI      = 44
	Size     = int64(46464512)
	SHA256   = "8397fa50856f64a1550a5eaad84efb3871ce9429f152457bc4fedad6f696202c"
	fileName = "gov8_shim.dll"
)

//go:embed windows_amd64/gov8_shim.dll.gz
var compressed []byte
