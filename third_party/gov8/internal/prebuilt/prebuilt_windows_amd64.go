//go:build windows && amd64

package prebuilt

import _ "embed"

const (
	ABI      = 44
	Size     = int64(45943296)
	SHA256   = "81dda49d5d92f2d9b4c8649345509a6375984eae78d6e3f8d3116edb3be54ac9"
	fileName = "gov8_shim.dll"
)

//go:embed windows_amd64/gov8_shim.dll.gz
var compressed []byte
