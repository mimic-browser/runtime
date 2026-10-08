// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/go-webengine/engine/dom"
)

// ContainerType is the computed value of the `container-type` property: it
// controls whether an element establishes a query container that descendant
// `@container` rules can condition on, and which of its axes are available to
// query. It is NOT inherited (an element resets to ContainerNormal unless its
// own declarations set it).
type ContainerType uint8

const (
	// ContainerNormal is the initial value: the element establishes no query
	// container (but may still be a size/layout containment target for other
	// reasons this engine does not model).
	ContainerNormal ContainerType = iota
	// ContainerInlineSize establishes a query container queryable on its
	// inline axis (width, in this engine's horizontal writing mode) only.
	ContainerInlineSize
	// ContainerTypeSize establishes a query container queryable on BOTH axes
	// (inline and block / width and height). Named ContainerTypeSize (rather
	// than the more obvious ContainerSize) because that name is already taken
	// by the ContainerSize struct just below, for a container's RESOLVED size
	// — a different, unrelated concept that also needed the name.
	ContainerTypeSize
)

// ContainerSize is a query container's resolved size, gathered from a layout
// pass and fed back into the cascade so `@container` conditions can be
// evaluated against it. InlineSize is the container's border-box width;
// BlockSize (border-box height) is only populated — and only ever consulted —
// when the container has size containment on both axes (ContainerType ==
// ContainerTypeSize): querying the block axis of an inline-size-only
// container is invalid per spec and always fails to match here, the same way
// it does in a browser.
type ContainerSize struct {
	InlineSize float64
	BlockSize  float64
}

// containerFrame is one entry in the ancestor "container stack" threaded
// through the cascade walk: an ancestor element that established a query
// container, its name, and its type (which axes it may be queried on).
type containerFrame struct {
	node *dom.Node
	name string
	typ  ContainerType
}

// ContainerCondition is an `@container` rule's condition, attached to every
// Rule nested (directly or via @media/@layer) inside the at-rule. Unlike a
// `@media` condition — known before any element is visited, since it only
// depends on the viewport — a `@container` condition depends on an ANCESTOR
// ELEMENT's actual laid-out size, which is per-matched-element (different
// elements matching the same selector can have different nearest containers)
// and only known once at least one layout pass has run. So, unlike @media,
// this is not resolved away at parse time: it rides along on the Rule and is
// evaluated during cascade, per candidate element, against whatever container
// sizes the caller supplies (nil before the first layout pass, which makes
// every @container rule inactive until real geometry is available).
type ContainerCondition struct {
	// Name filters which ancestor container this condition may match against
	// ("" means: the nearest ancestor container of ANY name).
	Name string
	// Cond is the lowercased, trimmed size-feature condition text (e.g.
	// "(min-width: 400px)"), or "" for a name-only condition with no size
	// feature (matches unconditionally once a qualifying container exists).
	Cond string
}

// mergeContainerCondition combines an outer @container condition with
// whatever condition (possibly nil) a Rule coming out of a nested body
// already carries — the shape needed for `@container a (...) { @container b
// (...) { ... } }` (or, more realistically, @media/@layer nested inside
// @container and vice versa). Both conditions must hold, so a name is kept
// from whichever side specifies one (the inner one wins if both do — the
// nearer @container is more specific) and the size conditions are ANDed by
// concatenation (both regex-based matchers below simply scan for every
// feature they recognise in the string, so concatenating two condition
// strings with a separator ANDs their features).
func mergeContainerCondition(inner *ContainerCondition, outer ContainerCondition) *ContainerCondition {
	if inner == nil {
		c := outer
		return &c
	}
	merged := ContainerCondition{Name: inner.Name, Cond: inner.Cond}
	if merged.Name == "" {
		merged.Name = outer.Name
	}
	switch {
	case merged.Cond == "":
		merged.Cond = outer.Cond
	case outer.Cond != "":
		merged.Cond = merged.Cond + " " + outer.Cond
	}
	return &merged
}

// parseContainerCondition parses an `@container` at-rule prelude (the text
// between "@container" and the opening '{', e.g. "sidebar (min-width: 400px)"
// or "(min-width: 400px)"), returning the parsed condition and true, or
// ok=false when the prelude uses a construct this engine does not support —
// currently just `@container style(...)` (CSS "container style queries", a
// newer, separate part of the spec; see FIDELITY.md's known gaps). A false
// return means the whole at-rule body should be dropped, matching how any
// other unrecognised at-rule is skipped wholesale.
func parseContainerCondition(prelude string) (ContainerCondition, bool) {
	rest := strings.TrimSpace(prelude[len("@container"):])
	if strings.Contains(strings.ToLower(rest), "style(") {
		return ContainerCondition{}, false
	}
	name := ""
	if rest != "" && !strings.HasPrefix(rest, "(") {
		if paren := strings.IndexByte(rest, '('); paren >= 0 {
			name = strings.TrimSpace(rest[:paren])
			rest = rest[paren:]
		} else {
			// A bare "@container name" with no condition at all: not valid CSS
			// (a condition is required), but leniently treat it as a name-only
			// filter rather than dropping the content — consistent with this
			// engine's general bias toward including content over discarding it.
			name = strings.TrimSpace(rest)
			rest = ""
		}
	}
	return ContainerCondition{Name: name, Cond: strings.ToLower(strings.TrimSpace(rest))}, true
}

// satisfied reports whether cond holds for an element whose ancestor
// container stack is stack (nearest last) and whose containers' resolved
// sizes (from the most recent layout pass) are in sizes. A nil/missing sizes
// entry for the chosen container (no layout has run yet, or it never got
// boxed) means the condition cannot be evaluated and is treated as not
// matching — never guessed — the same conservative default parseContainerCondition's
// caller relies on for the very first (pre-layout) cascade pass.
func (c *ContainerCondition) satisfied(stack []containerFrame, sizes map[*dom.Node]ContainerSize) bool {
	for i := len(stack) - 1; i >= 0; i-- {
		f := stack[i]
		if c.Name != "" && f.name != c.Name {
			continue // keep looking further out for a container with this name
		}
		sz, ok := sizes[f.node]
		if !ok {
			return false
		}
		if c.Cond == "" {
			return true
		}
		return containerConditionMatches(c.Cond, f.typ, sz)
	}
	return false // no ancestor establishes a (name-)qualifying container at all
}

// containerFeatureRe captures the colon size-feature syntax for a container
// query ("min-width:400px", "max-height:20rem", ...) — the container-query
// analogue of mediaWidthRe, extended to also accept "height" since a
// `container-type: size` container is queryable on the block axis too.
var containerFeatureRe = regexp.MustCompile(`(min|max)-(width|height)\s*:\s*([0-9.]+)(px|rem)`)

// containerFeatureCmpRe is the CSS Media Queries Level 4 range-comparison
// syntax ("width<=48rem", "48rem<=width", ...) generalised to width/height —
// the container-query analogue of mediaWidthCmpRe.
var containerFeatureCmpRe = regexp.MustCompile(
	`(width|height)\s*(<=|>=|<|>)\s*([0-9.]+)(px|rem)|([0-9.]+)(px|rem)\s*(<=|>=|<|>)\s*(width|height)`)

// containerConditionMatches evaluates a parsed (lowercased) size-feature
// condition against a container of type typ and resolved size sz. A feature
// on an axis the container does not expose (height/min-height/max-height on
// anything but a ContainerTypeSize container) makes the whole condition fail, per
// spec — querying an axis without size containment is invalid. Any other,
// unrecognised feature (aspect-ratio, orientation, ...) is left unmatched by
// either regex and so does not affect the result, matching mediaMatches'
// existing "unknown feature: match optimistically" stance for @media.
func containerConditionMatches(cond string, typ ContainerType, sz ContainerSize) bool {
	cond = evalMediaCalcs(cond)
	axisValue := func(axis string) (float64, bool) {
		if axis == "width" {
			return sz.InlineSize, true
		}
		if typ == ContainerTypeSize {
			return sz.BlockSize, true
		}
		return 0, false
	}
	for _, m := range containerFeatureRe.FindAllStringSubmatch(cond, -1) {
		if _, err := strconv.ParseFloat(m[3], 64); err != nil {
			continue
		}
		v, ok := axisValue(m[2])
		if !ok {
			return false
		}
		n := lengthToPx(m[3], m[4])
		if m[1] == "min" && v < n {
			return false
		}
		if m[1] == "max" && v > n {
			return false
		}
	}
	for _, m := range containerFeatureCmpRe.FindAllStringSubmatch(cond, -1) {
		var axis, op, numStr, unit string
		if m[1] != "" {
			axis, op, numStr, unit = m[1], m[2], m[3], m[4]
		} else {
			numStr, unit, op, axis = m[5], m[6], flipCmp(m[7]), m[8]
		}
		if _, err := strconv.ParseFloat(numStr, 64); err != nil {
			continue
		}
		v, ok := axisValue(axis)
		if !ok {
			return false
		}
		n := lengthToPx(numStr, unit)
		switch op {
		case "<":
			if v >= n {
				return false
			}
		case "<=":
			if v > n {
				return false
			}
		case ">":
			if v <= n {
				return false
			}
		case ">=":
			if v < n {
				return false
			}
		}
	}
	return true
}

// containerTypeKeyword parses a `container-type` (or a component of the
// `container` shorthand) keyword. ok is false for anything unrecognised, so
// the caller can tell "normal" (explicit) apart from "not a type keyword at
// all" (used to disambiguate the shorthand's bare-type-only form).
func containerTypeKeyword(s string) (ContainerType, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "inline-size":
		return ContainerInlineSize, true
	case "size":
		return ContainerTypeSize, true
	case "normal":
		return ContainerNormal, true
	}
	return ContainerNormal, false
}

// applyContainerShorthand parses the `container` shorthand:
// `container: <name> / <type>`, or either component alone (`container:
// sidebar`, `container: inline-size`). A shorthand always resets BOTH
// components to their initial value before applying whichever were given, per
// ordinary CSS shorthand semantics.
func applyContainerShorthand(s *Style, v string) {
	parts := strings.SplitN(v, "/", 2)
	nameToken := strings.TrimSpace(parts[0])
	typeToken := ""
	if len(parts) == 2 {
		typeToken = strings.TrimSpace(parts[1])
	} else if _, ok := containerTypeKeyword(nameToken); ok {
		// Bare type-only form ("container: inline-size;"): no name given.
		typeToken = nameToken
		nameToken = "none"
	}
	s.ContainerName = ""
	if !strings.EqualFold(nameToken, "none") && nameToken != "" {
		s.ContainerName = nameToken
	}
	s.ContainerType = ContainerNormal
	if ct, ok := containerTypeKeyword(typeToken); ok {
		s.ContainerType = ct
	}
}
