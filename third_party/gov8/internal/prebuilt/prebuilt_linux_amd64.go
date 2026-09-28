//go:build linux && amd64

package prebuilt

import _ "embed"

const (
	ABI      = 44
	Size     = int64(58996888)
	SHA256   = "863dbf62a7919aa56ac75ad11c3674b3e2ea3a6b48e31815334571153846acf2"
	fileName = "libgov8_shim.so"
)

//go:embed linux_amd64/libgov8_shim.so.gz
var compressed []byte
