//go:build windows && amd64

package prebuilt

import _ "embed"

const (
	ABI      = 44
	Size     = int64(45946368)
	SHA256   = "30531d17fa82704faeaee4b47d9d89fbf65052d01b306b4a2164f82800d8d6e3"
	fileName = "gov8_shim.dll"
)

//go:embed windows_amd64/gov8_shim.dll.gz
var compressed []byte
