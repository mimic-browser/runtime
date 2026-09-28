//go:build windows && amd64

package prebuilt

import _ "embed"

const (
	ABI      = 44
	Size     = int64(46458880)
	SHA256   = "801ffa1be81c6c063a93e655c084dd23af23c301edf3a72cb61b544823f81a94"
	fileName = "gov8_shim.dll"
)

//go:embed windows_amd64/gov8_shim.dll.gz
var compressed []byte
