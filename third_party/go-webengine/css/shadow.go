// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import "strings"

// parseBoxShadow parses a box-shadow value: a comma-separated list of layers,
// each `[inset] <offset-x> <offset-y> [blur] [spread] [color]` in any order of
// the colour and the inset keyword. It reports false when no layer is valid
// (leaving the property unset). The keyword `none` yields an empty (cleared)
// list with ok=true so the shorthand resets prior shadows.
func parseBoxShadow(v string, emRef float64) ([]BoxShadow, bool) {
	if strings.EqualFold(strings.TrimSpace(v), "none") {
		return nil, true
	}
	var out []BoxShadow
	for _, part := range splitTopLevelSep(v, ',') {
		if sh, ok := parseShadowLayer(strings.TrimSpace(part), emRef); ok {
			out = append(out, sh)
		}
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// parseShadowLayer parses one box-shadow layer.
func parseShadowLayer(part string, emRef float64) (BoxShadow, bool) {
	var sh BoxShadow
	sh.Color = Color{0, 0, 0, 255} // CSS default shadow colour is currentColor≈black
	var lens []float64
	for _, tok := range strings.Fields(part) {
		if strings.EqualFold(tok, "inset") {
			sh.Inset = true
			continue
		}
		if l, ok := parseLength(tok, emRef); ok && !l.Auto && !l.IsPercent {
			lens = append(lens, l.Px)
			continue
		}
		if c, ok := parseColor(tok); ok {
			sh.Color = c
			continue
		}
		// Unknown token: reject the layer to avoid mispainting.
		return BoxShadow{}, false
	}
	// Need at least the two offsets.
	if len(lens) < 2 {
		return BoxShadow{}, false
	}
	sh.OffsetX = lens[0]
	sh.OffsetY = lens[1]
	if len(lens) >= 3 {
		sh.Blur = lens[2]
	}
	if len(lens) >= 4 {
		sh.Spread = lens[3]
	}
	return sh, true
}
