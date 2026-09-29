//go:build linux && amd64

package prebuilt

import _ "embed"

const (
	ABI      = 44
	Size     = int64(59005088)
	SHA256   = "c4638e6f2bf608eb3118101b31c1b7e098986d16b8d742bf5213e7124134c901"
	fileName = "libgov8_shim.so"
)

//go:embed linux_amd64/libgov8_shim.so.gz
var compressed []byte
