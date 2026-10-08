package browser

import (
	"encoding/json"
	"math"
	"os"
	"strings"

	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/state"
	"github.com/moreveal/mimic/internal/textmetrics"
	"github.com/moreveal/mimic/internal/trace"
)

type textShapeKey struct {
	text, families string
	size, weight   uint64
	flags          uint8
}

const textShapeCacheBytes = 1 << 20
const textShapeEntryOverhead = 128

// Derive the secondary entry bound from the same memory budget. A separate
// small count limit needlessly evicts compact metrics even when their entire
// working set fits in the byte budget. Payload/key sizes still control actual
// admission, so realistic entries reach the byte bound before this ceiling.
const textShapeCacheEntries = textShapeCacheBytes / textShapeEntryOverhead

// Values are serialized immutable results, so no engine-owned object or mutable
// glyph slice crosses calls. FIFO bounds include strings and estimated map/key
// overhead; repeated measurements only perform one map lookup.
type textShapeCache struct {
	values        map[textShapeKey]string
	order         []textShapeKey
	oldest, bytes int
}

func (c *textShapeCache) get(key textShapeKey) (string, bool) {
	if c == nil {
		return "", false
	}
	value, ok := c.values[key]
	return value, ok
}

func textShapeEntryBytes(key textShapeKey, value string) int {
	return len(key.text) + len(key.families) + len(value) + textShapeEntryOverhead
}

func (c *textShapeCache) put(key textShapeKey, value string) {
	cost := textShapeEntryBytes(key, value)
	if cost > textShapeCacheBytes {
		return
	}
	if c.values == nil {
		c.values = make(map[textShapeKey]string)
	}
	for len(c.values) >= textShapeCacheEntries || c.bytes+cost > textShapeCacheBytes {
		old := c.order[c.oldest]
		c.bytes -= textShapeEntryBytes(old, c.values[old])
		delete(c.values, old)
		c.order[c.oldest] = textShapeKey{}
		c.oldest++
	}
	if len(c.order) == textShapeCacheEntries {
		count := copy(c.order, c.order[c.oldest:])
		clear(c.order[count:])
		c.order = c.order[:count]
		c.oldest = 0
	}
	c.values[key] = value
	c.order = append(c.order, key)
	c.bytes += cost
}

func newTextMetricsEngine(fonts state.Fonts) *textmetrics.Engine {
	result := textmetrics.New()
	result.SetFallbackFamilies(fonts.Fallback)
	for generic, family := range map[string]string{"serif": fonts.Serif, "sans-serif": fonts.SansSerif, "monospace": fonts.Monospace, "system-ui": fonts.SystemUI} {
		result.SetGenericFamily(generic, family)
	}
	return result
}
func (p *Page) textMetricsEngine() *textmetrics.Engine {
	if p.textMetrics == nil {
		p.textMetrics = newTextMetricsEngine(p.environmentView().Fonts)
	}
	return p.textMetrics
}

// SetGenericFontFamilies applies Page-level generic-family overrides. CDP
// clients issue this while creating a target, before its first document realm
// can observe or cache text metrics.
func (p *Page) SetGenericFontFamilies(serif, sansSerif, monospace string) error {
	if err := p.CheckProfileMutation(); err != nil {
		current := p.Environment().Fonts
		equal := func(requested, configured, generic string) bool {
			if requested == "" {
				return true
			}
			if configured == "" {
				configured = textmetrics.DefaultGenericFamily(generic)
			}
			return strings.EqualFold(requested, configured)
		}
		if equal(serif, current.Serif, "serif") && equal(sansSerif, current.SansSerif, "sans-serif") && equal(monospace, current.Monospace, "monospace") {
			return nil
		}
		return err
	}
	p.mu.Lock()
	if serif != "" {
		p.env.Fonts.Serif = serif
	}
	if sansSerif != "" {
		p.env.Fonts.SansSerif = sansSerif
	}
	if monospace != "" {
		p.env.Fonts.Monospace = monospace
	}
	metrics := p.textMetrics
	p.textMetrics = nil
	p.mu.Unlock()
	if metrics != nil {
		_ = metrics.Close()
	}
	return nil
}

func (r *Realm) installTextMetrics(host map[string]any) {
	if r.textCacheProfile == nil && os.Getenv("MIMIC_PROFILE_TEXT_CACHE") == "1" {
		r.textCacheProfile = newTextCacheProfile()
	}
	installFontResourceHosts(host, r.runtime, func() *textmetrics.Engine {
		p := r.agent.Page()
		return p.textMetricsEngine()
	})
	r.installFontCollection(host)

	host["shapeText"] = r.textShapeHost(false)
	host["shapeTextMetrics"] = r.textShapeHost(true)
}

// Geometry only observes these aggregate metrics. Keep the same authoritative
// shaping operation, but do not serialize, transfer, parse or retain its glyphs.
// The projection flag separates compact and full results within the same bounded
// document cache and shares every font-collection invalidation with canvas.
func (r *Realm) textShapeHost(compact bool) any {
	return r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		text, families := strarg(args, 0), strarg(args, 1)
		size, weight := numarg(args, 2), numarg(args, 3)
		italic, noKern, noLigatures, canvas := numarg(args, 4) != 0, numarg(args, 5) != 0, numarg(args, 6) != 0, numarg(args, 7) != 0
		key := textShapeKey{text: text, families: families, size: math.Float64bits(size), weight: math.Float64bits(weight)}
		if compact {
			key.flags |= 1 << 4
		}
		for i, flag := range []bool{italic, noKern, noLigatures, canvas} {
			if flag {
				key.flags |= 1 << i
			}
		}
		cachedValue, hit := r.textShapeCache.get(key)
		if r.textCacheProfile != nil {
			r.textCacheProfile.observe(key, hit)
		}
		if hit {
			return r.val(cachedValue), nil
		}
		p := r.agent.Page()
		shape := p.textMetricsEngine().ShapeWithFonts
		if canvas {
			shape = p.textMetricsEngine().ShapeCanvasWithFonts
		}
		var output any
		var err error
		if compact {
			output, err = p.textMetricsEngine().MeasureWithFonts(text, families, size, weight, italic, noKern, noLigatures, r.fontChoices)
		} else {
			output, err = shape(text, families, size, weight, italic, noKern, noLigatures, r.fontChoices)
		}
		if err != nil {
			// Private trace only: author-facing exceptions must not expose local paths.
			if r.textCacheProfile != nil {
				r.textCacheProfile.mode(key).FailedMisses++
			}
			bounded := func(value string, limit int) string {
				runes := []rune(value)
				if len(runes) > limit {
					return string(runes[:limit])
				}
				return value
			}
			p.trace.Add(trace.SemanticMissing, "Text.shapeFailure", map[string]any{
				"realm": r.ID, "reason": bounded(err.Error(), 512), "reasonAvailable": true, "category": "unsupported-boundary", "site": "text_metrics.go/installTextMetrics", "operation": "shapeText",
				"text": bounded(strarg(args, 0), 160), "textRunes": len([]rune(strarg(args, 0))),
				"families": bounded(strarg(args, 1), 256), "size": numarg(args, 2), "weight": numarg(args, 3),
				"italic": numarg(args, 4) != 0, "noKern": numarg(args, 5) != 0, "noLigatures": numarg(args, 6) != 0,
			})
			return r.val(`{"error":"Unsupported local font resource or shaping operation"}`), nil
		}
		data, err := json.Marshal(output)
		if err != nil {
			if r.textCacheProfile != nil {
				r.textCacheProfile.mode(key).FailedMisses++
			}
			return nil, err
		}
		value := string(data)
		if r.textShapeCache == nil {
			r.textShapeCache = &textShapeCache{}
		}
		beforeEntries := len(r.textShapeCache.values)
		r.textShapeCache.put(key, value)
		if r.textCacheProfile != nil {
			r.textCacheProfile.success(key, value, beforeEntries, r.textShapeCache)
		}
		return r.val(value), nil
	})
}
