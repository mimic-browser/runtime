package textmetrics

import (
	"crypto/sha256"
	"fmt"
	"os"
)

// Raster IDs name resources selected by this Page's shaper. The Canvas bridge
// can request their bytes without accepting an arbitrary path from JavaScript.
func (e *Engine) rasterResourceID(r resource) string {
	id := fmt.Sprintf("%x", sha256.Sum256([]byte(fmt.Sprintf("%s#%d", r.path, r.index))))
	if e.rasterResources == nil {
		e.rasterResources = make(map[string]resource)
	}
	e.rasterResources[id] = r
	return id
}

func (e *Engine) RasterFontData(id string) ([]byte, error) {
	r, ok := e.rasterResources[id]
	if !ok {
		return nil, fmt.Errorf("unknown selected font resource")
	}
	const limit = 64 << 20
	info, err := os.Stat(r.path)
	if err != nil {
		return nil, err
	}
	if info.Size() > limit {
		return nil, fmt.Errorf("font raster resource exceeds %d bytes", limit)
	}
	data, err := os.ReadFile(r.path)
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("font raster resource exceeds %d bytes", limit)
	}
	return data, nil
}
