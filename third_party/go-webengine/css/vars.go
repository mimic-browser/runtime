// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import "strings"

// maxVarDepth bounds var() substitution recursion. Custom properties may
// reference one another (var(--a) -> var(--b) -> ...); a self- or mutual-
// reference forms a cycle, which the spec treats as invalid at computed-value
// time. Bounding the depth turns any cycle into a resolution failure rather than
// unbounded recursion. The bound is generous relative to real stylesheets, whose
// var() chains are only a few levels deep.
const maxVarDepth = 64

// isCustomProperty reports whether a property name is a CSS custom property
// (its name starts with two dashes, e.g. "--main-color"). Custom-property names
// are case-sensitive, unlike normal property names.
func isCustomProperty(prop string) bool {
	return strings.HasPrefix(prop, "--")
}

// resolveDeclValue substitutes any var() references in a declaration value
// using the element's resolved custom-property map, then resolves any
// calc()/light-dark() call the substitution exposed (see resolveCalc,
// resolveLightDark), returning the final value. It reports false when a
// var() cannot be resolved and has no usable fallback (or forms a cycle), or
// a calc()/light-dark() is malformed or outside what this engine evaluates:
// such a declaration is invalid at computed-value time and must be dropped
// by the caller (so the property keeps its inherited/initial value). A value
// with none of these constructs is returned unchanged.
func resolveDeclValue(value string, props map[string]string) (string, bool) {
	if findVarFunc(value, 0) >= 0 {
		v, ok := substituteVars(value, props, 0)
		if !ok {
			return "", false
		}
		value = v
	}
	v, ok := resolveCalc(value)
	if !ok {
		return "", false
	}
	return resolveLightDark(v)
}

// substituteVars replaces every var() function in value with the referenced
// custom property's value (recursively resolving nested var() in both the
// substituted value and in fallbacks). depth guards against reference cycles.
func substituteVars(value string, props map[string]string, depth int) (string, bool) {
	if depth > maxVarDepth {
		return "", false
	}
	var b strings.Builder
	i := 0
	for i < len(value) {
		rel := findVarFunc(value[i:], 0)
		if rel < 0 {
			b.WriteString(value[i:])
			break
		}
		abs := i + rel
		b.WriteString(value[i:abs])
		open := abs + len("var") // index of '('
		closeIdx, ok := matchParen(value, open)
		if !ok {
			// Unterminated var( ... : the value is malformed / invalid.
			return "", false
		}
		inner := value[open+1 : closeIdx]
		name, fallback, hasFallback := splitVarArgs(inner)
		name = strings.TrimSpace(name)

		resolved, ok := resolveOneVar(name, fallback, hasFallback, props, depth)
		if !ok {
			return "", false
		}
		b.WriteString(resolved)
		i = closeIdx + 1
	}
	return b.String(), true
}

// resolveOneVar resolves a single var(name[, fallback]) to its substitution
// text. It prefers the custom property's own value; when that property is
// unset OR its own value is itself invalid at computed-value time (it
// recursively references another var() that cannot resolve) it uses the
// fallback; with neither available the reference is invalid.
//
// The "itself invalid" case is not a hypothetical: it is the entire mechanism
// behind the "CSS toggle" pattern real-world CSS ships (postcss-preset-env's
// light-dark() polyfill, seen live on developer.mozilla.org, is exactly this).
// It declares one custom property per theme, only ONE of which a selector
// like html[data-theme=dark] actually sets — the other is simply never
// declared for this element — then threads it through an intermediate
// property with no fallback of its own:
//
//	--toggle: var(--scheme-light) var(--gray-40);   // no fallback here
//	--color:  var(--toggle, var(--gray-60));        // fallback HERE
//
// When html carries neither data-theme attribute (the page's pre-JS SSR
// state), --scheme-light is unset, so --toggle is invalid at computed-value
// time — and per the CSS Custom Properties spec that makes it behave as
// UNSET for any var() that references it, not as a hard failure: --color
// must still resolve via its own fallback (--gray-60). Treating this the same
// as "the reference is invalid" instead (the previous behaviour here) drops
// EVERY declaration built this way — on MDN, effectively the whole design
// token system (colours, borders, spacing) collapses at once.
func resolveOneVar(name, fallback string, hasFallback bool, props map[string]string, depth int) (string, bool) {
	if raw, found := props[name]; found {
		if v, ok := substituteVars(raw, props, depth+1); ok {
			return v, true
		}
		// raw is invalid at computed-value time — fall through as if name
		// were never declared, so the reference's OWN fallback (if any)
		// still gets a chance below, instead of failing outright.
	}
	if hasFallback {
		r, ok := substituteVars(fallback, props, depth+1)
		if !ok {
			return "", false
		}
		return strings.TrimSpace(r), true
	}
	return "", false
}

// splitVarArgs splits a var() argument list at its first top-level comma into
// the custom-property name and the fallback (everything after that comma).
// Commas nested inside parentheses (e.g. a fallback of rgb(0, 0, 0)) are not
// treated as the separator. hasFallback is false when there is no top-level
// comma.
func splitVarArgs(inner string) (name, fallback string, hasFallback bool) {
	depth := 0
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '(':
			depth++
		case ')':
			depth--
		case ',':
			if depth == 0 {
				return inner[:i], inner[i+1:], true
			}
		}
	}
	return inner, "", false
}

// findVarFunc returns the index (relative to from) of the next "var(" token in
// s at or after from, or -1 if none. A match only counts when the "var" is not
// preceded by an identifier character, so identifiers ending in "var" (e.g.
// "sidebar(") do not match.
func findVarFunc(s string, from int) int {
	for i := from; ; {
		j := indexFold(s[i:], "var(")
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

// isIdentChar reports whether c can appear inside a CSS identifier (so that a
// preceding one prevents a spurious "var(" match).
func isIdentChar(c byte) bool {
	return c == '-' || c == '_' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9')
}

// indexFold returns the index of the first case-insensitive occurrence of sub
// in s, or -1. sub must be lowercase ASCII.
func indexFold(s, sub string) int {
	if len(sub) == 0 {
		return 0
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if equalFoldASCII(s[i:i+len(sub)], sub) {
			return i
		}
	}
	return -1
}

// equalFoldASCII reports whether a equals lower (an already-lowercase ASCII
// string) ignoring ASCII case.
func equalFoldASCII(a, lower string) bool {
	for k := 0; k < len(a); k++ {
		c := a[k]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != lower[k] {
			return false
		}
	}
	return true
}

// matchParen returns the index of the ')' matching the '(' at open.
func matchParen(s string, open int) (int, bool) {
	depth := 0
	for i := open; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i, true
			}
		}
	}
	return 0, false
}
