package textmetrics

import (
	"os"
	"sync"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
)

// Catalog names and face order are eager, but nominal coverage is only needed
// when a cluster cannot use its primary face. The immutable result is shared
// across Pages; initialization synchronizes only callers of this one resource.
// Decoded shaping faces and author font state remain owned by each Engine.
func deferredFontCoverage(path string, index int) func() font.Cmap {
	return sync.OnceValue(func() font.Cmap {
		file, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer file.Close()
		loaders, err := ot.NewLoaders(file)
		if err != nil || index >= len(loaders) {
			return nil
		}
		loader := loaders[index]
		raw, err := loader.RawTable(ot.MustNewTag("cmap"))
		if err != nil {
			return nil
		}
		table, _, err := tables.ParseCmap(raw)
		if err != nil {
			return nil
		}
		os2Raw, _ := loader.RawTable(ot.MustNewTag("OS/2"))
		os2, _, _ := tables.ParseOs2(os2Raw)
		cmap, _, err := font.ProcessCmap(table, os2.FontPage())
		if err != nil {
			return nil
		}
		return cmap
	})
}
