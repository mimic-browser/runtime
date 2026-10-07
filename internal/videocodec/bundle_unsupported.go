//go:build !(windows && amd64) && !(linux && (amd64 || arm64)) && !(darwin && (amd64 || arm64))

package videocodec

import "embed"

//go:embed bundled/LICENSE bundled/provenance.json
var bundled embed.FS
