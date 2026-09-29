//go:build windows && amd64

package prebuilt

import _ "embed"

const (
	ABI      = 44
	Size     = int64(46458880)
	SHA256   = "a4f771213c2feacd047ec8559d9c3b3311fbdabdd16b8d1efef6ec2b268d7d8a"
	fileName = "gov8_shim.dll"
)

//go:embed windows_amd64/gov8_shim.dll.gz
var compressed []byte
