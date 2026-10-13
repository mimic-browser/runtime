package browser

import _ "embed"

// The self-contained CPU raster module uses neither a GPU nor an OS graphics
// backend. Its source and pinned dependency graph live beside this artifact.
//
//go:embed canvas_raster/raster.wasm
var canvasRasterModule []byte
