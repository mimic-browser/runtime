package textmetrics

import (
	"fmt"
	"math"
	"strings"
	"unicode"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype/tables"
	"github.com/go-text/typesetting/harfbuzz"
	"golang.org/x/text/unicode/norm"
)

type coverageKey struct{ resource, cluster string }

func coversCluster(face *loaded, cluster []rune) bool {
	if coversNominalCluster(face, cluster) {
		return true
	}
	// HarfBuzz can canonically compose a base and combining mark even when
	// the font has no standalone mark glyph. Coverage must not prematurely
	// switch the entire cluster to a fallback face. Keep original runes and
	// offsets for shaping; normalization here is only a coverage check.
	source := string(cluster)
	composed := norm.NFC.String(source)
	return composed != source && coversNominalCluster(face, []rune(composed))
}
func coversNominalCluster(face *loaded, cluster []rune) bool {
	for i, ch := range cluster {
		if i > 0 && (ch == 0xfe0e || ch == 0xfe0f) {
			gid, ok := face.face.VariationGlyph(cluster[i-1], ch)
			if !ok {
				gid, ok = face.face.NominalGlyph(cluster[i-1])
			}
			if !ok {
				return false
			}
			_, color := face.face.GlyphDataColor(tables.GlyphID(gid))
			if (ch == 0xfe0f) != color {
				return false
			}
			continue
		}
		// Joiners and selectors participate in shaping but need no visible glyph.
		if harfbuzz.IsDefaultIgnorable(ch) {
			continue
		}
		if _, ok := face.face.NominalGlyph(ch); !ok {
			return false
		}
	}
	return true
}

func (e *Engine) fallbackFace(primary *loaded, cluster []rune, families string, weight float64, italic bool, choices []FontReference) (*loaded, error) {
	if len(choices) > 0 {
		allowed := make([]FontReference, 0, len(choices))
		for _, choice := range choices {
			if choice.covers(cluster) {
				allowed = append(allowed, choice)
			}
		}
		choices = allowed
		r, err := e.selectResource(families, weight, italic, choices)
		if err != nil {
			return nil, err
		}
		primary, err = e.load(r)
		if err != nil {
			return nil, err
		}
	}
	if coversCluster(primary, cluster) {
		return primary, nil
	}
	selectionKey := fallbackSelectionKey{
		primary:  fmt.Sprintf("%s#%d", primary.resource.path, primary.resource.index),
		coverage: string(cluster),
		families: families,
		choices:  resourceSelectionChoices(choices),
		weight:   math.Float64bits(weight),
		italic:   italic,
	}
	if selected, ok := e.fallbackSelections[selectionKey]; ok {
		face, err := e.load(selected)
		if err != nil {
			return nil, err
		}
		if coversCluster(face, cluster) {
			return face, nil
		}
		delete(e.fallbackSelections, selectionKey)
		for i, key := range e.fallbackSelectionOrder {
			if key == selectionKey {
				e.fallbackSelectionOrder = append(e.fallbackSelectionOrder[:i], e.fallbackSelectionOrder[i+1:]...)
				break
			}
		}
	}
	remember := func(face *loaded) *loaded {
		if e.fallbackSelections == nil {
			e.fallbackSelections = make(map[fallbackSelectionKey]resource)
		}
		if len(e.fallbackSelections) >= 8192 {
			oldest := e.fallbackSelectionOrder[0]
			delete(e.fallbackSelections, oldest)
			copy(e.fallbackSelectionOrder, e.fallbackSelectionOrder[1:])
			e.fallbackSelectionOrder = e.fallbackSelectionOrder[:len(e.fallbackSelectionOrder)-1]
		}
		e.fallbackSelections[selectionKey] = face.resource
		e.fallbackSelectionOrder = append(e.fallbackSelectionOrder, selectionKey)
		return face
	}
	// Control characters use this font's real missing-glyph observation. They
	// must not force a search through every installed graphical font.
	if len(cluster) == 1 && unicode.IsControl(cluster[0]) && cluster[0] != 0x7f {
		return primary, nil
	}
	// Try the author's remaining families before Windows fallback families.
	// This is a profile-level preference, not a codepoint-specific substitution.
	names := strings.Split(families, ",")
	for _, ch := range cluster {
		if ch == 0xfe0e {
			names = append(names, "serif")
			break
		}
	}
	if len(e.fallbackFamilies) > 0 {
		names = append(names, e.fallbackFamilies...)
	} else {
		names = append(names, "Segoe UI Emoji", "Segoe UI Symbol", "Segoe UI", "Arial")
	}
	seen := map[string]bool{}
	clusterText := string(cluster)
	try := func(r resource) (*loaded, error) {
		key := fmt.Sprintf("%s#%d", r.path, r.index)
		if seen[key] {
			return nil, nil
		}
		seen[key] = true
		// Reject using immutable nominal coverage before decoding outlines and
		// shaping tables. This must not change candidate order or replace the
		// final variation-selector / color-glyph check on the selected face.
		if lookup := e.coverage[key]; lookup != nil {
			if cmap := lookup(); cmap != nil && !nominalCoverage(cmap, cluster) && !nominalCoverage(cmap, []rune(norm.NFC.String(clusterText))) {
				return nil, nil
			}
		}
		coverage := coverageKey{key, clusterText}
		if _, missing := e.missingCoverage[coverage]; missing {
			return nil, nil
		}
		retained := e.faces[key] != nil
		face, err := e.load(r)
		if err != nil {
			// Unusable optional fallback resources are not selected fonts. Their
			// decode/size failures must not poison otherwise valid primary text.
			return nil, nil
		}
		if coversCluster(face, cluster) {
			return face, nil
		}
		// Remember proven coverage misses without retaining decoded fallback
		// faces. Otherwise every layout read reopens and reparses the entire
		// installed font catalog for the same missing character. This bounded
		// cache belongs to the Page's font resources, never to a global engine.
		if len(clusterText) <= 128 {
			if e.missingCoverage == nil || len(e.missingCoverage) >= 8192 {
				e.missingCoverage = make(map[coverageKey]struct{})
			}
			e.missingCoverage[coverage] = struct{}{}
		}
		// Unsuccessful coverage candidates are temporary, not selected faces.
		// Retaining them exhausted the Page cache and broke unrelated later text.
		if !retained {
			delete(e.faces, key)
			e.bytes -= face.resourceBytes
		}
		return nil, nil
	}
	for _, name := range names {
		r, err := e.selectResource(name, weight, italic, choices)
		if err != nil {
			continue
		}
		face, err := try(r)
		if err != nil {
			return nil, fmt.Errorf("fallback resource %s: %w", r.family, err)
		}
		if face != nil {
			return remember(face), nil
		}
	}
	// Coverage search remains bounded by the Page's face/byte limits. No
	// process-wide cache, synthetic .notdef widths or per-character random data.
	for _, r := range e.catalog {
		if (r.aspect.Style == font.StyleItalic) != italic {
			continue
		}
		face, err := try(r)
		if err != nil {
			return nil, fmt.Errorf("fallback resource %s: %w", r.family, err)
		}
		if face != nil {
			return remember(face), nil
		}
	}
	// A valid font can still lack a character. Shape its actual .notdef glyph;
	// inventing a width or failing the entire browser operation is incorrect.
	return primary, nil
}

// This is a rejection filter only. Variation presentation, color glyphs and
// final selection still use the fully loaded face in coversCluster.
func nominalCoverage(cmap font.Cmap, cluster []rune) bool {
	for _, ch := range cluster {
		if harfbuzz.IsDefaultIgnorable(ch) {
			continue
		}
		if _, ok := cmap.Lookup(ch); !ok {
			return false
		}
	}
	return true
}
