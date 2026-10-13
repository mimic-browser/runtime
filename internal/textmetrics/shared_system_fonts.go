package textmetrics

import (
	"fmt"
	"os"
	"sync"

	"github.com/go-text/typesetting/font"
	"github.com/golang/freetype/truetype"
)

// Parsed installed-font tables are immutable. Faces, HarfBuzz state, glyph
// buffers and author fonts remain owned by their individual Page engines.
// Bound this process cache so browsing many font families cannot retain every
// installed font indefinitely.
const sharedFontLimit = 128 << 20

type sharedSystemFont struct {
	font                     *font.Font
	hint                     *truetype.Font
	ascent, descent, lineGap float64
	emAscent, emDescent      float64
	useTypoMetrics           bool
	legacyKern               map[uint32]int16
	bytes                    int
}

var systemFontCache = struct {
	sync.Mutex
	entries map[string]*sharedSystemFont
	order   []string
	bytes   int
}{entries: make(map[string]*sharedSystemFont)}

func systemFontKey(path string, index int, info os.FileInfo) string {
	return fmt.Sprintf("%s#%d#%d#%d", path, index, info.Size(), info.ModTime().UnixNano())
}

func getSystemFont(key string) *sharedSystemFont {
	systemFontCache.Lock()
	defer systemFontCache.Unlock()
	return systemFontCache.entries[key]
}

func putSystemFont(key string, value *sharedSystemFont) {
	if value == nil || value.bytes > sharedFontLimit {
		return
	}
	systemFontCache.Lock()
	defer systemFontCache.Unlock()
	if systemFontCache.entries[key] != nil {
		return
	}
	for systemFontCache.bytes+value.bytes > sharedFontLimit && len(systemFontCache.order) != 0 {
		oldest := systemFontCache.order[0]
		systemFontCache.order = systemFontCache.order[1:]
		systemFontCache.bytes -= systemFontCache.entries[oldest].bytes
		delete(systemFontCache.entries, oldest)
	}
	systemFontCache.entries[key] = value
	systemFontCache.order = append(systemFontCache.order, key)
	systemFontCache.bytes += value.bytes
}

func getSystemHint(key string) *truetype.Font {
	systemFontCache.Lock()
	defer systemFontCache.Unlock()
	if value := systemFontCache.entries[key]; value != nil {
		return value.hint
	}
	return nil
}

func putSystemHint(key string, hint *truetype.Font) {
	if hint == nil {
		return
	}
	systemFontCache.Lock()
	defer systemFontCache.Unlock()
	if value := systemFontCache.entries[key]; value != nil {
		value.hint = hint
	}
}

var warmDefaultFontsOnce sync.Once

// WarmProfileFontFaces lets normal first-use work proceed concurrently with
// navigation. The families come from the browser profile, not page content.
// Later Pages reuse only the parsed, immutable installed tables.
func WarmProfileFontFaces(families, fallbacks []string) {
	warmDefaultFontsOnce.Do(func() {
		families = append([]string(nil), families...)
		fallbacks = append([]string(nil), fallbacks...)
		go func() {
			engine := New()
			defer engine.Close()
			engine.SetFallbackFamilies(fallbacks)
			for _, family := range families {
				resource, err := engine.selectResource(family, 400, false, nil)
				if err != nil {
					continue
				}
				face, err := engine.load(resource)
				if err == nil {
					loadHintFont(face)
				}
			}
			// Installed script fallbacks can differ from the profile's named
			// families. Warm one representative glyph per broad writing system,
			// so first author canvas use does not decode a large font on its turn.
			for _, sample := range []string{"À", "Ω", "Ж", "字", "😀", japaneseSyllabaries()} {
				_, _ = engine.ShapeCanvasWithFonts(sample, "system-ui", 16, 400, false, false, false, nil)
			}
		}()
	})
}

func japaneseSyllabaries() string {
	// Cover voiced and unvoiced syllables without depending on a workload's
	// chosen text. Installed CJK fonts do not all cover the same subset.
	var runes []rune
	for _, bounds := range [][2]rune{{0x3041, 0x3096}, {0x30a1, 0x30fa}} {
		for codepoint := bounds[0]; codepoint <= bounds[1]; codepoint++ {
			runes = append(runes, codepoint)
		}
	}
	return string(runes)
}
