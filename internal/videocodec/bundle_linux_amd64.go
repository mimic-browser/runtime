//go:build linux && amd64

package videocodec

import "embed"

//go:embed bundled/linux_amd64.bz2 bundled/LICENSE bundled/provenance.json
var bundled embed.FS
