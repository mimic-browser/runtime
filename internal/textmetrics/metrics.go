// Package textmetrics reads local OpenType resources and shapes text without
// rasterizing glyphs. An Engine belongs to one Page and is not shared between
// event loops. No native font, windowing or graphics backend is used.
package textmetrics

import (
	"encoding/binary"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
	"github.com/go-text/typesetting/harfbuzz"
	"github.com/go-text/typesetting/segmenter"
	"github.com/golang/freetype/truetype"
	xfont "golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

type resource struct {
	path   string
	index  int
	family string
	aspect font.Aspect
}

type resourceSelectionKey struct {
	families string
	choices  string
	weight   uint64
	italic   bool
}

type fallbackSelectionKey struct {
	primary, coverage, families, choices string
	weight                               uint64
	italic                               bool
}

type loaded struct {
	resource                 resource
	face                     *font.Face
	shaper                   *harfbuzz.Font
	ascent, descent, lineGap float64
	emAscent, emDescent      float64
	hintFont                 *truetype.Font
	hintGlyph                truetype.GlyphBuf
	hintLoaded               bool
	resourceBytes            int
	lastUse                  uint64
}

type Engine struct {
	fallbackFamilies       []string
	genericFamilies        map[string]string
	genericFallbacks       map[string][]string
	resources              map[string]resource
	localNames             map[string]resource
	dirs                   []string
	catalog                []resource
	scanned                bool
	faces                  map[string]*loaded
	bytes                  int
	fontUse                uint64
	missingCoverage        map[coverageKey]struct{}
	shapeScratch           *harfbuzz.Buffer
	shapePlans             map[shapePlanKey]struct{}
	resourceSelections     map[resourceSelectionKey]resource
	resourceSelectionOrder []resourceSelectionKey
	fallbackSelections     map[fallbackSelectionKey]resource
	fallbackSelectionOrder []fallbackSelectionKey
	spoolDir               string
	coverage               map[string]font.Cmap
}

type systemCatalog struct {
	once       sync.Once
	catalog    []resource
	localNames map[string]resource
	coverage   map[string]font.Cmap
}

var systemCatalogs sync.Map

// Close releases Page-owned reloadable web-font backing storage.
func (e *Engine) Close() error {
	if e == nil || e.spoolDir == "" {
		return nil
	}
	err := os.RemoveAll(e.spoolDir)
	e.spoolDir = ""
	return err
}

func New() *Engine {
	var dirs []string
	if configured := os.Getenv("MIMIC_FONT_DIR"); configured != "" {
		dirs = filepath.SplitList(configured)
	} else if runtime.GOOS == "windows" {
		dirs = append(dirs, filepath.Join(os.Getenv("WINDIR"), "Fonts"))
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			dirs = append(dirs, filepath.Join(local, "Microsoft", "Windows", "Fonts"))
		}
	} else if runtime.GOOS == "linux" {
		dirs = append(dirs, "/usr/local/share/fonts", "/usr/share/fonts")
		if data := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(data) {
			dirs = append(dirs, filepath.Join(data, "fonts"))
		} else if home, err := os.UserHomeDir(); err == nil {
			dirs = append(dirs, filepath.Join(home, ".local", "share", "fonts"))
		}
		if home, err := os.UserHomeDir(); err == nil {
			dirs = append(dirs, filepath.Join(home, ".fonts"))
		}
	}
	engine := NewDirectories(dirs)
	// A generic CSS family must resolve to a real local resource when the
	// reference family's font files are absent. Explicit resource profiles
	// (MIMIC_FONT_DIR / NewDirectories) retain their fail-closed boundary.
	if runtime.GOOS == "linux" && os.Getenv("MIMIC_FONT_DIR") == "" {
		engine.genericFallbacks = map[string][]string{
			"serif":      {"liberation serif", "dejavu serif", "noto serif"},
			"sans-serif": {"liberation sans", "dejavu sans", "noto sans"},
			"system-ui":  {"liberation sans", "dejavu sans", "noto sans"},
			"monospace":  {"liberation mono", "dejavu sans mono", "noto sans mono"},
		}
	}
	return engine
}

// NewDirectories permits explicit font resources on either host. The
// installed Windows reference fonts themselves are not distributed by Mimic.
func NewDirectories(dirs []string) *Engine {
	return &Engine{dirs: append([]string(nil), dirs...), faces: map[string]*loaded{}, resources: map[string]resource{}, localNames: map[string]resource{}}
}

// WarmSystemCatalog resolves immutable installed-font metadata before a Page
// can make a synchronous layout observation pay for a complete directory scan.
// Decoded faces and author fonts remain Page-owned.
func WarmSystemCatalog() {
	New().scan()
}

// SetGenericFamily selects a resource family for this isolated engine. It does
// not alter named-family lookup or synthesize unavailable metrics.
func (e *Engine) SetGenericFamily(generic, family string) {
	if family == "" {
		return
	}
	if e.genericFamilies == nil {
		e.genericFamilies = map[string]string{}
	}
	e.genericFamilies[strings.ToLower(generic)] = strings.ToLower(family)
	e.clearResourceSelections()
}

// SetFallbackFamilies selects an ordered resource policy for this engine.
func (e *Engine) SetFallbackFamilies(families []string) {
	e.fallbackFamilies = append([]string(nil), families...)
	e.clearResourceSelections()
}

func (e *Engine) clearResourceSelections() {
	e.resourceSelections = nil
	e.resourceSelectionOrder = nil
	e.fallbackSelections = nil
	e.fallbackSelectionOrder = nil
}

func resourceSelectionChoices(choices []FontReference) string {
	if len(choices) == 0 {
		return ""
	}
	var value strings.Builder
	for _, choice := range choices {
		fmt.Fprintf(&value, "%q:%q:%q:%q:%x:%q;", choice.ID, choice.Family, choice.Style, choice.Unsupported, math.Float64bits(choice.Weight), choice.UnicodeRange)
	}
	return value.String()
}

// Invalid or unsupported TrueType hint programs must not unwind a host callback.
// Their outline remains usable; callers can identify approximate ink metrics.
func hintGlyphBounds(face *loaded, size float64, glyph uint32) (ok bool) {
	defer func() {
		if recover() != nil {
			face.hintGlyph = truetype.GlyphBuf{}
			ok = false
		}
	}()
	if !face.hintLoaded {
		face.hintLoaded = true
		// TrueType hinting is only observable through Canvas ink bounds. The
		// regular shaping face has already loaded its own tables, so retaining
		// the entire source file for every DOM-only font is unnecessary.
		if face.resource.index == 0 {
			if data, err := os.ReadFile(face.resource.path); err == nil {
				face.hintFont, _ = truetype.Parse(data)
			}
		}
	}
	return face.hintFont != nil && face.hintGlyph.Load(face.hintFont, fixed.Int26_6(math.Round(size*64)), truetype.Index(glyph), xfont.HintingFull) == nil
}

func (e *Engine) scan() {
	if e.scanned {
		return
	}
	e.scanned = true
	key := strings.Join(e.dirs, "\x00")
	cached, _ := systemCatalogs.LoadOrStore(key, &systemCatalog{})
	shared := cached.(*systemCatalog)
	shared.once.Do(func() {
		shared.catalog, shared.localNames, shared.coverage = scanFontDirectories(e.dirs)
	})
	e.coverage = shared.coverage
	e.catalog = append(e.catalog, shared.catalog...)
	for name, resource := range shared.localNames {
		e.localNames[name] = resource
	}
}

func scanFontDirectories(dirs []string) ([]resource, map[string]resource, map[string]font.Cmap) {
	catalog := []resource{}
	localNames := map[string]resource{}
	coverage := map[string]font.Cmap{}
	var scratch []byte
	for _, dir := range dirs {
		// Linux distributions arrange fonts in nested family/format directories.
		// WalkDir is deterministic and does not follow directory symlinks.
		_ = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil || entry.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if ext != ".ttf" && ext != ".otf" && ext != ".ttc" {
				return nil
			}
			f, err := os.Open(path)
			if err != nil {
				return nil
			}
			loaders, err := ot.NewLoaders(f)
			if err == nil {
				for index, loader := range loaders {
					var d font.Description
					d, scratch = font.Describe(loader, scratch)
					d.Aspect.SetDefaults()
					if d.Family != "" {
						os2Raw, _ := loader.RawTable(ot.MustNewTag("OS/2"))
						os2, _, _ := tables.ParseOs2(os2Raw)
						if raw, err := loader.RawTable(ot.MustNewTag("cmap")); err == nil {
							if table, _, err := tables.ParseCmap(raw); err == nil {
								if cmap, _, err := font.ProcessCmap(table, os2.FontPage()); err == nil {
									coverage[fmt.Sprintf("%s#%d", path, index)] = cmap
								}
							}
						}
						catalog = append(catalog, resource{path, index, strings.ToLower(d.Family), d.Aspect})
						if raw, err := loader.RawTable(ot.MustNewTag("name")); err == nil {
							if names, _, err := tables.ParseName(raw); err == nil {
								for _, id := range []tables.NameID{4, 6} {
									if name := names.Name(id); name != "" {
										localNames[strings.ToLower(name)] = catalog[len(catalog)-1]
									}
								}
							}
						}

					}
				}
			}
			f.Close()
			return nil
		})
	}
	return catalog, localNames, coverage
}

func (e *Engine) selectResource(families string, weight float64, italic bool, choices []FontReference) (resource, error) {
	e.scan()
	key := resourceSelectionKey{families: families, choices: resourceSelectionChoices(choices), weight: math.Float64bits(weight), italic: italic}
	if selected, ok := e.resourceSelections[key]; ok {
		return selected, nil
	}
	remember := func(selected resource) (resource, error) {
		if e.resourceSelections == nil {
			e.resourceSelections = make(map[resourceSelectionKey]resource)
		}
		if len(e.resourceSelections) >= 1024 {
			oldest := e.resourceSelectionOrder[0]
			delete(e.resourceSelections, oldest)
			copy(e.resourceSelectionOrder, e.resourceSelectionOrder[1:])
			e.resourceSelectionOrder = e.resourceSelectionOrder[:len(e.resourceSelectionOrder)-1]
		}
		e.resourceSelections[key] = selected
		e.resourceSelectionOrder = append(e.resourceSelectionOrder, key)
		return selected, nil
	}
	names := strings.Split(families, ",")
	names = append(names, "serif")
	for _, name := range names {
		name = strings.TrimSpace(name)
		quoted := strings.HasPrefix(name, "\"") || strings.HasPrefix(name, "'")
		name = strings.ToLower(strings.Trim(name, "\"'"))
		bestChoice := -1
		choiceScore := math.Inf(1)
		for i, choice := range choices {
			if strings.ToLower(strings.Trim(choice.Family, "\"'")) != name {
				continue
			}
			if _, ok := e.resources[choice.ID]; !ok {
				continue
			}
			score := math.Abs(choice.Weight - weight)
			if (choice.Style != "normal") != italic {
				score += 10000
			}
			if score <= choiceScore {
				choiceScore = score
				bestChoice = i
			}
		}
		if bestChoice >= 0 {
			if reason := choices[bestChoice].Unsupported; reason != "" {
				return resource{}, fmt.Errorf("font descriptor semantics unsupported: %s", reason)
			}
			return remember(e.resources[choices[bestChoice].ID])
		}

		generic := name
		configuredGeneric := false
		if !quoted {
			if family, ok := e.genericFamilies[name]; ok {
				configuredGeneric = true
				name = family
			} else {
				switch name {
				case "serif":
					name = "times new roman"
				case "sans-serif":
					name = "arial"
				case "monospace":
					name = "consolas"
				case "system-ui":
					name = "segoe ui"
				}
			}
		}
		candidates := []string{name}
		if !quoted {
			candidates = append(candidates, e.genericFallbacks[generic]...)
		}
		for _, candidate := range candidates {
			best := -1
			score := math.Inf(1)
			for i, r := range e.catalog {
				if r.family != candidate {
					continue
				}
				s := math.Abs(float64(r.aspect.Weight)-weight) + 1000*math.Abs(float64(r.aspect.Stretch)-1)
				if (r.aspect.Style == font.StyleItalic) != italic {
					s += 10000
				}
				if s < score {
					score = s
					best = i
				}
			}
			if best >= 0 {
				r := e.catalog[best]
				if (r.aspect.Style == font.StyleItalic) != italic {
					return resource{}, fmt.Errorf("synthetic font style is unsupported")
				}
				return remember(r)
			}
		}
		if configuredGeneric {
			return resource{}, fmt.Errorf("selected generic font resource is unavailable: %s", name)
		}
	}
	return resource{}, fmt.Errorf("no usable reference font resource")
}

func (e *Engine) load(r resource) (*loaded, error) {
	key := fmt.Sprintf("%s#%d", r.path, r.index)
	e.fontUse++
	if f := e.faces[key]; f != nil {
		f.lastUse = e.fontUse
		return f, nil
	}
	f, err := os.Open(r.path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	stat, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if err := e.makeFontRoom(int(stat.Size())); err != nil {
		return nil, err
	}
	loaders, err := ot.NewLoaders(f)
	if err != nil || r.index >= len(loaders) {
		return nil, fmt.Errorf("invalid font container")
	}
	loader := loaders[r.index]
	// Mimic observes horizontal shaping and ink extents, never glyph outlines.
	// The metrics-only decoder still validates every glyph and retains full
	// outlines for variable fonts, whose extents depend on contour points.
	ft, err := font.NewFontMetricsOnly(loader)
	if err != nil {
		return nil, err
	}
	face := font.NewFace(ft)
	metrics, ok := face.FontHExtents()
	if !ok {
		return nil, fmt.Errorf("missing horizontal font metrics")
	}
	value := &loaded{resource: r, face: face, shaper: harfbuzz.NewFont(face), ascent: float64(metrics.Ascender), descent: -float64(metrics.Descender), lineGap: float64(metrics.LineGap), emAscent: float64(metrics.Ascender), emDescent: -float64(metrics.Descender), resourceBytes: int(stat.Size())}
	// The Windows Chrome profile uses Windows ascender/descender rather than
	// the optional typographic line metrics (notably different in Consolas).
	if os2, err := loader.RawTable(ot.MustNewTag("OS/2")); err == nil && len(os2) >= 78 {
		value.ascent = float64(binary.BigEndian.Uint16(os2[74:76]))
		value.descent = float64(binary.BigEndian.Uint16(os2[76:78]))
		value.emAscent = float64(int16(binary.BigEndian.Uint16(os2[68:70])))
		value.emDescent = -float64(int16(binary.BigEndian.Uint16(os2[70:72])))
	}
	e.faces[key] = value
	value.lastUse = e.fontUse
	e.bytes += value.resourceBytes
	return value, nil
}

type Glyph struct {
	Cluster int     `json:"cluster"`
	Advance float64 `json:"advance"`
	XOffset float64 `json:"xOffset"`
	YOffset float64 `json:"yOffset"`
	Left    float64 `json:"left"`
	Right   float64 `json:"right"`
	Top     float64 `json:"top"`
	Bottom  float64 `json:"bottom"`
	Ink     bool    `json:"ink"`
}

type Result struct {
	InkApproximate bool    `json:"inkApproximate,omitempty"`
	Glyphs         []Glyph `json:"glyphs"`
	Ascent         float64 `json:"ascent"`
	LineGap        float64 `json:"lineGap"`
	Descent        float64 `json:"descent"`
	XHeight        float64 `json:"xHeight"`
	Family         string  `json:"family"`
	EmAscent       float64 `json:"emAscent"`
	EmDescent      float64 `json:"emDescent"`
}

func (e *Engine) Shape(text, families string, size, weight float64, italic, noKern, noLigatures bool) (Result, error) {
	return e.ShapeWithFonts(text, families, size, weight, italic, noKern, noLigatures, nil)
}
func (e *Engine) ShapeWithFonts(text, families string, size, weight float64, italic, noKern, noLigatures bool, choices []FontReference) (Result, error) {
	return e.shapeWithFonts(text, families, size, weight, italic, noKern, noLigatures, choices, false, nil)
}

// ShapeCanvasWithFonts also evaluates TrueType vertical hinting for observable
// ink bounds. DOM line boxes and SVG advances do not need this extra work.
func (e *Engine) ShapeCanvasWithFonts(text, families string, size, weight float64, italic, noKern, noLigatures bool, choices []FontReference) (Result, error) {
	return e.shapeWithFonts(text, families, size, weight, italic, noKern, noLigatures, choices, true, nil)
}

// Metrics is the DOM line-box projection. It has no glyph storage or ink bounds.
type Metrics struct {
	Advance float64 `json:"advance"`
	Ascent  float64 `json:"ascent"`
	Descent float64 `json:"descent"`
	LineGap float64 `json:"lineGap"`
}

// MeasureWithFonts uses the same font selection, clusters and HarfBuzz shaping
// as ShapeWithFonts, but skips unobserved glyph records and ink extents.
func (e *Engine) MeasureWithFonts(text, families string, size, weight float64, italic, noKern, noLigatures bool, choices []FontReference) (Metrics, error) {
	var metrics Metrics
	_, err := e.shapeWithFonts(text, families, size, weight, italic, noKern, noLigatures, choices, false, &metrics)
	return metrics, err
}

func (e *Engine) shapeWithFonts(text, families string, size, weight float64, italic, noKern, noLigatures bool, choices []FontReference, hintInk bool, aggregate *Metrics) (Result, error) {
	if !finite(size) || size <= 0 || size > 4096 || !finite(weight) {
		return Result{}, fmt.Errorf("unsupported font size or weight")
	}
	runes := []rune(text)
	if len(runes) > 16384 {
		return Result{}, fmt.Errorf("text complexity limit")
	}
	r, err := e.selectResource(families, weight, italic, choices)
	if err != nil {
		return Result{}, err
	}
	f, err := e.load(r)
	if err != nil {
		return Result{}, err
	}
	// Keep grapheme clusters intact (combining marks, selectors, emoji ZWJ).
	type run struct {
		start, end int
		face       *loaded
	}
	runs := []run{}
	var segments segmenter.Segmenter
	segments.Init(runes)
	iterator := segments.GraphemeIterator()
	fallbackCache := map[string]*loaded{}
	for iterator.Next() {
		cluster := iterator.Grapheme()
		key := string(cluster.Text)
		selected := fallbackCache[key]
		if selected == nil {
			selected, err = e.fallbackFace(f, cluster.Text, families, weight, italic, choices)
			if err == nil {
				fallbackCache[key] = selected
			}
		}
		if err != nil {
			return Result{}, err
		}
		end := cluster.Offset + len(cluster.Text)
		if len(runs) > 0 && runs[len(runs)-1].face == selected {
			runs[len(runs)-1].end = end
		} else {
			runs = append(runs, run{cluster.Offset, end, selected})
		}
	}
	var features []harfbuzz.Feature
	for i, name := range [...]string{"kern", "liga", "clig"} {
		disabled := noLigatures
		if i == 0 {
			disabled = noKern
		}
		if disabled {
			feature, _ := harfbuzz.ParseFeature(name + "=0")
			features = append(features, feature)
		}
	}
	primaryScale := size / float64(f.face.Upem())
	result := Result{Glyphs: []Glyph{}, Ascent: math.Round(f.ascent * primaryScale), Descent: math.Round(f.descent * primaryScale), LineGap: math.Round(f.lineGap * primaryScale), XHeight: float64(f.face.LineMetric(font.XHeight)) * primaryScale, Family: r.family}
	result.EmAscent = f.emAscent * primaryScale
	result.EmDescent = f.emDescent * primaryScale
	if aggregate != nil {
		aggregate.Ascent, aggregate.Descent, aggregate.LineGap = result.Ascent, result.Descent, result.LineGap
	}
	for _, run := range runs {
		buffer := e.shapingBuffer(runes, run.start, run.end, run.face, noKern, noLigatures)
		if buffer.Props.Direction != harfbuzz.LeftToRight && buffer.Props.Direction != harfbuzz.RightToLeft {
			buffer.Clear()
			return Result{}, fmt.Errorf("non-horizontal shaping is unsupported")
		}
		// HarfBuzz orders horizontal RTL glyphs visually and supplies positive
		// advances, so the same metrics projection applies to either direction.
		buffer.Shape(run.face.shaper, features)
		scale := size / float64(run.face.face.Upem())
		for i, info := range buffer.Info {
			p := buffer.Pos[i]
			if aggregate != nil {
				// Preserve the full result's left-to-right floating-point sum.
				aggregate.Advance += float64(p.XAdvance) * scale
				continue
			}
			g := Glyph{Cluster: info.Cluster, Advance: float64(p.XAdvance) * scale, XOffset: float64(p.XOffset) * scale, YOffset: -float64(p.YOffset) * scale}
			if bounds, ok := run.face.face.GlyphExtents(info.Glyph); ok && bounds.Width != 0 && bounds.Height != 0 {
				g.Ink = true
				g.Left = math.Floor(float64(bounds.XBearing) * scale)
				g.Right = math.Ceil(float64(bounds.XBearing+bounds.Width) * scale)
				g.Top = math.Round(-float64(bounds.YBearing) * scale)
				g.Bottom = math.Round(-float64(bounds.YBearing+bounds.Height) * scale)
				if hintInk {
					if hintGlyphBounds(run.face, size, uint32(info.Glyph)) {
						g.Top = -float64(run.face.hintGlyph.Bounds.Max.Y) / 64
						g.Bottom = -float64(run.face.hintGlyph.Bounds.Min.Y) / 64
					} else {
						result.InkApproximate = true
					}
				}
			}
			result.Glyphs = append(result.Glyphs, g)
		}
		// Clear author text immediately, retaining only bounded scratch storage
		// and immutable shaping plans. Large runs never enter the scratch cache.
		buffer.Clear()
	}
	return result, nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
