//go:build darwin && arm64

package videocodec

import "embed"

//go:embed bundled/darwin_arm64.bz2 bundled/LICENSE bundled/provenance.json
var bundled embed.FS
