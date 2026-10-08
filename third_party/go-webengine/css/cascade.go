// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"sort"
	"strings"

	"github.com/go-webengine/engine/dom"
)

// StyleMap maps each element node to its fully-computed style.
type StyleMap map[*dom.Node]*Style

// precedence levels: the origin of a declaration dominates specificity.
const (
	precUA     = 0
	precAuthor = 1
	precInline = 2
)

type candidate struct {
	decl        Declaration
	precedence  int
	specificity int
	order       int
}

// Cascade computes styles at the default viewport width. See CascadeVW.
func Cascade(root *dom.Node) StyleMap {
	return CascadeVW(root, DefaultViewportWidth, nil)
}

// CascadeVW computes a style for every element in the tree rooted at root,
// applying the user-agent stylesheet, author rules from every <style> element
// plus any externalSheets (already-fetched CSS text, e.g. <link> stylesheets),
// and inline style="" attributes, with proper specificity and inheritance.
// @media width queries are evaluated against viewport width vw. Every
// @container rule is treated as not matching (see CascadeVWContainers) since
// no container sizes are known yet — the caller re-cascades with
// CascadeVWContainers once a layout pass has measured them.
func CascadeVW(root *dom.Node, vw float64, externalSheets []string) StyleMap {
	return CascadeVWContainers(root, vw, externalSheets, nil)
}

// CascadeMedia is CascadeVW evaluated against a Media rather than a bare
// screen width: with Media{Type: Print, Width: w} the page's "@media print"
// rules apply and its "@media screen" rules do not — a print preview's
// cascade. externalSheets should then come from a fetch that selected
// <link media> the same way (engine.LoadStylesheets with the same Media).
func CascadeMedia(root *dom.Node, m Media, externalSheets []string) StyleMap {
	return CascadeMediaContainers(root, m, externalSheets, nil)
}

// CascadeVWContainers is CascadeVW extended with container-query support:
// containers maps a query-container element (one whose computed style sets
// container-type) to its size as measured by the most recent layout pass.
// A nil/empty containers (as CascadeVW passes) makes every @container rule
// evaluate to "not matching" — the correct, conservative behaviour before any
// layout has run, and the reason this is a separate entry point rather than a
// parameter CascadeVW always threads through: most callers (tests, the first
// cascade of a render) have no layout yet and want exactly today's CascadeVW
// behaviour with zero conceptual overhead.
//
// Resolving an @container condition against real geometry is inherently a
// multi-pass affair — this single call only ever consults the containers map
// it is given; iterating cascade→layout→re-measure to a fixpoint is the
// caller's job (see engine's layoutWithContainers, which plays the same role
// for container queries that dynamic.go's settle loop plays for JavaScript:
// same shape, bounded passes, deterministic termination).
func CascadeVWContainers(root *dom.Node, vw float64, externalSheets []string, containers map[*dom.Node]ContainerSize) StyleMap {
	return CascadeMediaContainers(root, Media{Width: vw}, externalSheets, containers)
}

// CascadeMediaContainers is CascadeVWContainers against a Media — see
// CascadeMedia.
func CascadeMediaContainers(root *dom.Node, m Media, externalSheets []string, containers map[*dom.Node]ContainerSize) StyleMap {
	// root is the synthetic Document node dom.Parse returns, which is where
	// Node.Quirks is set (see its doc comment) — read it once here rather
	// than re-deriving it per element.
	quirks := root.Type == dom.Document && root.Quirks
	// External <link> stylesheets precede in-document <style> rules (they load in
	// the head), so they take lower precedence at equal specificity.
	var rules []Rule
	for _, sheet := range externalSheets {
		rules = append(rules, ParseStylesheetMedia(sheet, m)...)
	}
	rules = append(rules, collectAuthorRules(root, m)...)
	sm := StyleMap{}
	var counter int
	// walk's rules and host parameters together identify the current
	// cascade SCOPE: outside any shadow tree, rules is always the document's
	// (docRules, captured below) and host is nil. Descending into an attached
	// shadow root's content (n.Shadow != nil) switches BOTH for that
	// subtree's recursive calls only: rules becomes that shadow tree's own
	// scoped stylesheet (never the document's — see shadowStylesheet) and
	// host becomes n, so ":host"/":host(...)" compounds in it can bind (see
	// Selector.MatchesHost). The host element's OWN style is computed in the
	// unchanged outer scope (it is a light-DOM element), with its shadow's
	// :host-keyed declarations folded in as an extra rule source — see
	// filterHostSelectors below — so cascade order for the whole document is
	// otherwise untouched by any of this: a page with no Shadow DOM never
	// takes any of these branches (n.Shadow is nil, host stays nil, rules
	// stays docRules throughout).
	docRules := rules
	docIdx := buildRuleIndex(docRules)
	var walk func(n *dom.Node, parent Style, containerStack []containerFrame, rules []Rule, idx *ruleIndex, host *dom.Node)
	walk = func(n *dom.Node, parent Style, containerStack []containerFrame, rules []Rule, idx *ruleIndex, host *dom.Node) {
		if n.Type != dom.Element {
			// Text/Document nodes have no computed style; recurse with parent.
			for _, c := range n.Children {
				walk(c, parent, containerStack, rules, idx, host)
			}
			return
		}
		var shadowRules, hostRules []Rule
		if n.Shadow != nil {
			shadowRules = shadowStylesheet(n.Shadow, m)
			hostRules = filterHostSelectors(shadowRules)
		}
		// hostRules — n's OWN shadow's ":host"/":host(...)" declarations, if
		// any — are matched with host=n (self-referential: THIS is the
		// element ":host" inside n's own <style> refers to), which is why
		// they are a separate argument from rules/host: those reflect the
		// AMBIENT scope n is being cascaded in (nil outside any shadow tree,
		// or an OUTER shadow's host when n is itself shadow-tree content),
		// an entirely different (and, for the host itself, unrelated) binding.
		st := computeElement(n, parent, rules, idx, hostRules, &counter, containerStack, containers, host, quirks)
		sm[n] = &st
		childStack := containerStack
		if st.ContainerType != ContainerNormal {
			// A fresh backing array, not an append onto containerStack: two
			// sibling subtrees must never share (and risk overwriting each
			// other's view of) the same underlying array.
			childStack = make([]containerFrame, len(containerStack)+1)
			copy(childStack, containerStack)
			childStack[len(containerStack)] = containerFrame{node: n, name: st.ContainerName, typ: st.ContainerType}
		}
		// n's own light-DOM children (if n has an attached shadow root, these
		// still get a computed style here — layout simply never visits them
		// directly, see layout.renderedChildren — so a slotted child's style
		// is, correctly per spec, computed via the SAME outer scope as any
		// other light-DOM element, never restyled by the shadow's rules).
		for _, c := range n.Children {
			walk(c, st, childStack, rules, idx, host)
		}
		// n's shadow tree, if it has one: a NEW scope (shadowRules, host=n),
		// plus any "::part(name)" selectors from n's OWN ambient scope
		// (rules) — the outer document's (or outer shadow's) sanctioned way
		// to reach a specific, opted-in element inside n's shadow tree from
		// outside it. See filterPartSelectors and compound.Part.
		if n.Shadow != nil {
			shadowScope := append(append([]Rule{}, shadowRules...), filterPartSelectors(rules)...)
			for _, c := range n.Shadow.Children {
				walk(c, st, childStack, shadowScope, nil, n)
			}
		}
	}
	// The synthetic Document root has no style; seed children with initial.
	walk(root, initialStyle(), nil, docRules, docIdx, nil)
	return sm
}

// computeElement resolves the style of a single element. containerStack is
// the chain of ancestor query containers (nearest last) available to any
// @container rule matched below; containers supplies their measured sizes.
// host is the enclosing shadow tree's host element (nil outside one), bound
// so ":host"/":host(...)" compounds in rules can match (see
// Selector.MatchesHost) — nil makes every such compound fail, so this is a
// no-op for every element outside a shadow tree. hostRules is n's OWN
// attached shadow's ":host"-keyed declarations (nil if n has none), always
// matched with n as its own host — an independent binding from rules/host,
// which describe the scope n is being cascaded IN, not n's own shadow (an
// element is never a member of the shadow tree it hosts). quirks is the
// document's quirks-mode flag (see dom.Node.Quirks), consulted by
// uaDeclarations for the handful of UA defaults that differ in quirks mode.
func computeElement(n *dom.Node, parent Style, rules []Rule, idx *ruleIndex, hostRules []Rule, counter *int, containerStack []containerFrame, containers map[*dom.Node]ContainerSize, host *dom.Node, quirks bool) Style {
	st := inheritFrom(parent)
	// ownProps tracks whether st.CustomProps is this element's own (already
	// cloned) map versus the parent's shared one, so we clone at most once.
	ownProps := false

	var cands []candidate
	add := func(decls []Declaration, prec, spec int) {
		for _, d := range decls {
			cands = append(cands, candidate{d, prec, spec, *counter})
			*counter++
		}
	}

	// User-agent defaults: the per-tag declarations (specificity 0) plus the few
	// descendant UA rules (matched at their real specificity, still at UA origin)
	// that alternate the nested-list marker glyph disc→circle→square by depth.
	add(uaDeclarations(n.Tag, quirks), precUA, 0)
	// The HTML `hidden` attribute maps to `display:none` via the UA rule
	// `[hidden]{display:none}`. It is added at UA origin so any author `display`
	// (even a low-specificity or normal one) still wins per the cascade — matching
	// a browser. The modern `hidden="until-found"` value is revealable content and
	// is NOT hidden here.
	if v, ok := n.Attribute("hidden"); ok && !strings.EqualFold(strings.TrimSpace(v), "until-found") {
		add([]Declaration{{Property: "display", Value: "none"}}, precUA, 0)
	}
	for _, r := range uaDescendantRules {
		spec := -1
		for _, sel := range r.Selectors {
			if sel.Matches(n) {
				if s := sel.Specificity(); s > spec {
					spec = s
				}
			}
		}
		if spec >= 0 {
			add(r.Declarations, precUA, spec)
		}
	}
	// Legacy HTML presentational attributes (width, bgcolor, align, <font> …) map
	// to declarations at author origin with zero specificity, placed BEFORE any
	// author stylesheet — so a real CSS rule always overrides them, a UA default
	// does not. They are added here (ahead of the author-rule loop) so their
	// cascade order precedes every matched author rule.
	add(presentationalHints(n), precAuthor, 0)
	// Author rules whose selectors match, each at its selector's specificity.
	// MatchesHost (rather than plain Matches) is what lets a ":host"/
	// ":host(...)" compound bind to host — everywhere outside a shadow scope
	// host is nil, so this is identical to Matches(n) for every existing page.
	matchRule := func(r Rule, matchHost *dom.Node) {
		spec := -1
		for _, sel := range r.Selectors {
			if sel.MatchesHost(n, matchHost) {
				if s := sel.Specificity(); s > spec {
					spec = s
				}
			}
		}
		if spec >= 0 {
			if r.Container != nil && !r.Container.satisfied(containerStack, containers) {
				return // selector matched, but its @container condition does not (yet, or ever)
			}
			add(r.Declarations, precAuthor, spec)
		}
	}
	if idx != nil {
		for _, i := range idx.candidates(n) {
			matchRule(rules[i], host)
		}
	} else {
		for _, r := range rules {
			matchRule(r, host)
		}
	}
	// n's OWN shadow's ":host"-keyed rules (if any) — see computeElement's doc
	// comment: always matched with n as its own host, independent of the
	// ambient host binding above (an element is never inside the shadow tree
	// it hosts). This runs after the ambient rules in cascade ORDER, so at
	// equal specificity a component's own ":host" declaration wins over a
	// same-specificity document rule that also happens to match the host —
	// consistent with how this engine otherwise treats "later in the effective
	// stylesheet" as winning ties.
	for _, r := range hostRules {
		matchRule(r, n)
	}
	// Inline style attribute (highest origin).
	if inline, ok := n.Attribute("style"); ok {
		add(ParseDeclarations(inline), precInline, 0)
	}

	// Sort ascending so the last-applied declaration wins.
	sort.SliceStable(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		// !important forms its own cascade tier above every normal declaration,
		// regardless of origin or specificity: an author `!important` beats an
		// inline non-important style, which otherwise would always win. Within
		// each tier (important or not), origin/specificity/order still apply
		// exactly as before — this is the one addition to an otherwise-unchanged
		// comparator, so a stylesheet with no `!important` sorts identically.
		if a.decl.Important != b.decl.Important {
			return !a.decl.Important
		}
		if a.precedence != b.precedence {
			return a.precedence < b.precedence
		}
		if a.specificity != b.specificity {
			return a.specificity < b.specificity
		}
		return a.order < b.order
	})

	// Pass 0: resolve custom properties into the element's map (in cascade order,
	// last wins). They inherit from the parent (copied on first write so the
	// parent's shared map is never mutated) and are consulted by var().
	for _, c := range cands {
		if isCustomProperty(c.decl.Property) {
			if st.CustomProps == nil {
				st.CustomProps = map[string]string{}
			} else if !ownProps {
				st.CustomProps = cloneProps(st.CustomProps)
				ownProps = true
			}
			st.CustomProps[c.decl.Property] = c.decl.Value
		}
	}

	// applyResolved substitutes var() in a declaration's value before applying
	// it; a var() that cannot resolve (and has no fallback) drops the whole
	// declaration, leaving the inherited/initial value in place.
	applyResolved := func(d Declaration, emRef float64) {
		v, ok := resolveDeclValue(d.Value, st.CustomProps)
		if !ok {
			return
		}
		d.Value = v
		st.apply(d, emRef, &parent)
	}

	// Pass 1: resolve font-size first (its em is relative to the parent).
	for _, c := range cands {
		if c.decl.Property == "font-size" {
			applyResolved(c.decl, parent.FontSize)
		}
	}
	// Pass 2: everything else, with em relative to the element's own size.
	for _, c := range cands {
		if c.decl.Property != "font-size" && !isCustomProperty(c.decl.Property) {
			applyResolved(c.decl, st.FontSize)
		}
	}
	// See CenterAsBlock's doc comment: every element uses its own final
	// TextAlign, except a quirks-mode <table> also inherits its PARENT's
	// AlignCenterBlocks-ness — its own TextAlign was just reset to AlignLeft
	// by the quirks-mode UA rule above, which must not also erase the
	// separate "am I inside a <center>, so should I be centred as a block"
	// signal.
	st.CenterAsBlock = st.TextAlign == AlignCenterBlocks ||
		(quirks && n.Tag == "table" && parent.TextAlign == AlignCenterBlocks)
	return st
}

// cloneProps returns a shallow copy of a custom-property map, used for
// copy-on-write so an element's overrides never mutate the parent's inherited
// map.
func cloneProps(m map[string]string) map[string]string {
	c := make(map[string]string, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// collectAuthorRules parses every <style> element's text into rules, evaluating
// @media width queries against viewport width vw.
func collectAuthorRules(root *dom.Node, m Media) []Rule {
	return collectAuthorRulesFrom([]*dom.Node{root}, m)
}

// collectAuthorRulesFrom is collectAuthorRules generalised to a list of
// top-level nodes rather than a single root — used both for the whole
// document (collectAuthorRules) and, scoped to one shadow tree's own content,
// by shadowStylesheet (shadowdom.go). Neither walk descends into a NESTED
// shadow root's content: attachDeclarativeShadowRoots already hoists it out
// of the light-DOM Children this walks (into Node.Shadow instead), so a
// nested shadow tree's <style> is automatically picked up only by its own,
// separate collectAuthorRulesFrom(sr.Children, vw) call — never by this one,
// and never by the document's — with no explicit check needed here.
func collectAuthorRulesFrom(nodes []*dom.Node, m Media) []Rule {
	return ParseStylesheetMedia(styleElementText(nodes), m)
}

// styleElementText concatenates the text of every <style> element under
// nodes, in document order, one element per line: the document's embedded
// stylesheet, as both the cascade (collectAuthorRulesFrom) and the page
// context (DocumentPage, page.go) read it.
func styleElementText(nodes []*dom.Node) string {
	var sb strings.Builder
	var walk func(n *dom.Node)
	walk = func(n *dom.Node) {
		if n.Type == dom.Element && n.Tag == "style" {
			for _, c := range n.Children {
				if c.Type == dom.Text {
					sb.WriteString(c.Text)
					sb.WriteByte('\n')
				}
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	return sb.String()
}

// ruleIndex buckets a document stylesheet's rules by the id, class or tag their
// selectors' SUBJECT compound (the rightmost one, the element a selector
// styles) requires, so computeElement tests only the rules that can match an
// element instead of every rule in the document. Rules whose subject carries
// no such key (a bare "*", ":root", a ::part or :host subject) are tested for
// every element. Candidates come back in source order, so the cascade sees
// declarations in exactly the order a linear scan would produce.
type ruleIndex struct {
	byID, byClass, byTag map[string][]int
	always               []int
}

func buildRuleIndex(rules []Rule) *ruleIndex {
	ix := &ruleIndex{byID: map[string][]int{}, byClass: map[string][]int{}, byTag: map[string][]int{}}
	for i, r := range rules {
		for _, sel := range r.Selectors {
			ix.add(i, sel)
		}
	}
	return ix
}

func (ix *ruleIndex) add(i int, sel Selector) {
	if len(sel.parts) == 0 {
		ix.always = append(ix.always, i)
		return
	}
	subj := sel.parts[len(sel.parts)-1]
	switch {
	case subj.Part != "" || subj.Host:
		ix.always = append(ix.always, i)
	case subj.ID != "":
		ix.byID[subj.ID] = append(ix.byID[subj.ID], i)
	case len(subj.Classes) > 0:
		c := subj.Classes[0]
		ix.byClass[c] = append(ix.byClass[c], i)
	case subj.Tag != "" && !subj.Univ:
		ix.byTag[subj.Tag] = append(ix.byTag[subj.Tag], i)
	default:
		ix.always = append(ix.always, i)
	}
}

// candidates returns the sorted, de-duplicated indices of every rule that could
// match n.
func (ix *ruleIndex) candidates(n *dom.Node) []int {
	var out []int
	out = append(out, ix.always...)
	if id := n.ID(); id != "" {
		out = append(out, ix.byID[id]...)
	}
	for _, c := range n.Classes() {
		out = append(out, ix.byClass[c]...)
	}
	out = append(out, ix.byTag[n.Tag]...)
	sort.Ints(out)
	w := 0
	for i, v := range out {
		if i == 0 || v != out[w-1] {
			out[w] = v
			w++
		}
	}
	return out[:w]
}
