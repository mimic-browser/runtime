// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"strconv"
	"strings"
)

// resolveCalc replaces every top-level calc(...) expression in value with its
// evaluated plain-length text (e.g. "48px"), the same substitution-level
// treatment resolveLightDark gives light-dark() — run after var()
// substitution, so a var() reference inside calc() has already become a
// literal by the time this runs.
//
// This covers the pure-arithmetic subset (+, -, *, / over lengths and bare
// numbers, with nested parens) that calc() is used for almost everywhere in
// practice — which is EXACTLY what a var()-driven design-token spacing scale
// compiles to. Tailwind v4 (seen live on tailwindcss.com) defines its ENTIRE
// numeric spacing scale this way: `--spacing: .25rem` once at :root, then
// every sized/spaced utility (`size-48`, `h-48`, `p-4`, `gap-4`, `mt-4`, …)
// as `calc(var(--spacing) * 48)`. Without this, every one of those utilities
// failed to parse as a length and its declaration was silently dropped —
// e.g. an `<img class="size-48">` (meant to be a 192×192px avatar) fell back
// to its raw intrinsic pixel size, rendering many times too large.
//
// A calc() that mixes a percentage (or vw/vh, approximated as a percentage
// elsewhere in this package) with an absolute length cannot be collapsed to
// a single number here — percentages resolve only against a containing
// block at LAYOUT time, which this text-level pass has no access to. That
// case, and any other expression this evaluator does not recognise, makes
// resolveCalc report false, exactly like an unresolvable var(): the
// declaration is dropped, unchanged from this engine's behaviour before
// calc() was understood at all.
func resolveCalc(value string) (string, bool) {
	if findCalcFunc(value, 0) < 0 {
		return value, true
	}
	var b strings.Builder
	i := 0
	for i < len(value) {
		rel := findCalcFunc(value[i:], 0)
		if rel < 0 {
			b.WriteString(value[i:])
			break
		}
		abs := i + rel
		b.WriteString(value[i:abs])
		open := abs + len("calc") // index of '('
		closeIdx, ok := matchParen(value, open)
		if !ok {
			return "", false
		}
		v, hasUnit, ok := evalCalcExpr(value[open+1 : closeIdx])
		if !ok || !hasUnit {
			return "", false
		}
		b.WriteString(strconv.FormatFloat(v, 'f', -1, 64) + "px")
		i = closeIdx + 1
	}
	return b.String(), true
}

// findCalcFunc returns the index (relative to from) of the next "calc(" token
// in s at or after from, or -1 if none. Mirrors findVarFunc's guard against
// matching inside a longer identifier.
func findCalcFunc(s string, from int) int {
	for i := from; ; {
		j := indexFold(s[i:], "calc(")
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

// evalCalcExpr evaluates the content of one calc(...) call. hasUnit reports
// whether the result is a length (true) or a dimensionless number (false,
// which is invalid as a final calc() result but valid as an intermediate
// term — a caller wanting a length should require hasUnit).
func evalCalcExpr(s string) (val float64, hasUnit bool, ok bool) {
	p := &calcParser{s: s}
	val, hasUnit, ok = p.expr()
	if !ok {
		return 0, false, false
	}
	p.skipSpace()
	if p.i != len(p.s) {
		return 0, false, false
	}
	return val, hasUnit, true
}

// calcParser is a small recursive-descent parser for the arithmetic grammar
// CSS calc() supports: expr := term (('+'|'-') term)*, term := factor
// (('*'|'/') factor)*, factor := number[unit] | '(' expr ')'.
type calcParser struct {
	s string
	i int
}

func (p *calcParser) skipSpace() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t' || p.s[p.i] == '\n' || p.s[p.i] == '\r') {
		p.i++
	}
}

func (p *calcParser) expr() (float64, bool, bool) {
	v, u, ok := p.term()
	if !ok {
		return 0, false, false
	}
	for {
		p.skipSpace()
		if p.i >= len(p.s) || (p.s[p.i] != '+' && p.s[p.i] != '-') {
			break
		}
		op := p.s[p.i]
		p.i++
		p.skipSpace()
		v2, u2, ok := p.term()
		if !ok {
			return 0, false, false
		}
		// + and - require both sides to be the same kind (both lengths or
		// both bare numbers) — CSS calc() does not let a length and a
		// dimensionless number be added.
		if u != u2 {
			return 0, false, false
		}
		if op == '+' {
			v += v2
		} else {
			v -= v2
		}
	}
	return v, u, true
}

func (p *calcParser) term() (float64, bool, bool) {
	v, u, ok := p.factor()
	if !ok {
		return 0, false, false
	}
	for {
		p.skipSpace()
		if p.i >= len(p.s) || (p.s[p.i] != '*' && p.s[p.i] != '/') {
			break
		}
		op := p.s[p.i]
		p.i++
		p.skipSpace()
		v2, u2, ok := p.factor()
		if !ok {
			return 0, false, false
		}
		if op == '*' {
			// length * length has no meaning; a plain number multiplies a
			// length (or another plain number) in either order.
			if u && u2 {
				return 0, false, false
			}
			v *= v2
			u = u || u2
		} else {
			// Dividing BY a length has no meaning; the divisor must be a
			// plain number.
			if u2 || v2 == 0 {
				return 0, false, false
			}
			v /= v2
		}
	}
	return v, u, true
}

func (p *calcParser) factor() (float64, bool, bool) {
	p.skipSpace()
	if p.i < len(p.s) && p.s[p.i] == '(' {
		p.i++
		v, u, ok := p.expr()
		if !ok {
			return 0, false, false
		}
		p.skipSpace()
		if p.i >= len(p.s) || p.s[p.i] != ')' {
			return 0, false, false
		}
		p.i++
		return v, u, true
	}
	start := p.i
	if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
		p.i++
	}
	digitsStart := p.i
	for p.i < len(p.s) && (isCalcDigit(p.s[p.i]) || p.s[p.i] == '.') {
		p.i++
	}
	if p.i == digitsStart {
		return 0, false, false
	}
	numStr := p.s[start:p.i]
	unitStart := p.i
	for p.i < len(p.s) && isIdentChar(p.s[p.i]) {
		p.i++
	}
	if p.i < len(p.s) && p.s[p.i] == '%' {
		// A percentage cannot be resolved to a fixed number at this
		// text-level stage (see the doc comment on resolveCalc).
		return 0, false, false
	}
	unit := p.s[unitStart:p.i]
	n, err := strconv.ParseFloat(numStr, 64)
	if err != nil {
		return 0, false, false
	}
	if unit == "" {
		return n, false, true
	}
	// emRef only matters for an "em" term, which real-world calc() usage
	// (a var()-driven spacing scale, a two-length subtraction) essentially
	// never contains; 16 mirrors this package's existing "approximate as the
	// Phase-0 root size" treatment of rem elsewhere in parseLength.
	l, ok := parseLength(numStr+unit, 16)
	if !ok || l.IsPercent {
		return 0, false, false
	}
	return l.Px, true, true
}

func isCalcDigit(c byte) bool { return c >= '0' && c <= '9' }
