// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import "strings"

// resolveLightDark replaces every top-level light-dark(light, dark) function in
// value with whichever branch the engine's colour scheme picks, returning false
// (declaration dropped, same as an unresolvable var()) when a light-dark() is
// malformed: unterminated, or without exactly one top-level comma splitting its
// two arguments.
//
// This engine has no live "current appearance" concept (no window, no OS
// theme) — it picks the SAME fixed answer window.go's matchMedia uses for
// `prefers-color-scheme`: dark. That is not an arbitrary default; the bench
// suite's reference screenshots come from a real headless Chromium that
// inherits the host's actual (dark) appearance, so matching Chrome requires
// matching that same preference here, not the CSS spec's neutral fallback.
// See mediaQueryMatches in js/window.go for the sibling decision this mirrors.
func resolveLightDark(value string) (string, bool) {
	if findLightDarkFunc(value, 0) < 0 {
		return value, true
	}
	var b strings.Builder
	i := 0
	for i < len(value) {
		rel := findLightDarkFunc(value[i:], 0)
		if rel < 0 {
			b.WriteString(value[i:])
			break
		}
		abs := i + rel
		b.WriteString(value[i:abs])
		open := abs + len("light-dark") // index of '('
		closeIdx, ok := matchParen(value, open)
		if !ok {
			return "", false
		}
		inner := value[open+1 : closeIdx]
		_, dark, hasDark := splitVarArgs(inner)
		if !hasDark {
			return "", false
		}
		b.WriteString(strings.TrimSpace(dark))
		i = closeIdx + 1
	}
	return b.String(), true
}

// findLightDarkFunc returns the index (relative to from) of the next
// "light-dark(" token in s at or after from, or -1 if none. Mirrors
// findVarFunc's guard against matching inside a longer identifier.
func findLightDarkFunc(s string, from int) int {
	for i := from; ; {
		j := indexFold(s[i:], "light-dark(")
		if j < 0 {
			return -1
		}
		pos := i + j
		if pos == 0 || !isIdentChar(s[pos-1]) {
			return pos
		}
		i = pos + 1
	}
}
