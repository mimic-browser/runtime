// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

// Package dom is a small, owned DOM node tree built from golang.org/x/net/html.
//
// The engine deliberately owns its node type rather than passing x/net/html
// nodes around: it keeps the rest of the engine (css, layout, paint)
// independent of the parser and gives a stable, minimal surface — element tag,
// attributes, text — that is trivial to construct in tests.
package dom

import (
	"strings"

	"golang.org/x/net/html"
)

// NodeType enumerates the kinds of node the engine cares about.
type NodeType uint8

const (
	// Document is the synthetic root of a parsed tree.
	Document NodeType = iota
	// Element is a tag node (Tag/Attr populated).
	Element
	// Text is a run of character data (Text populated).
	Text
	// Comment is an HTML comment (`<!--…-->`), its data held in Text like a
	// Text node's. Previously dropped entirely during parsing (see
	// convertChildren's own doc comment) — a real gap, since React's
	// streaming-SSR hydration protocol marks Suspense boundaries with literal
	// `<!--$-->`/`<!--/$-->` comment nodes it expects to find in the DOM;
	// their absence made hydration see a structurally different tree than the
	// server sent, confirmed live as react.dev's own "Minified React error
	// #418" (hydration mismatch) cascading into a full client-side crash.
	Comment
)

// Node is a single DOM node. Elements carry a lowercased Tag and an attribute
// map; text and comment nodes carry Text. Children are in document order.
type Node struct {
	Type     NodeType
	Tag      string            // lowercased tag name (Element only)
	Attr     map[string]string // lowercased attribute names (Element only)
	Text     string            // character data (Text and Comment)
	Parent   *Node
	Children []*Node

	// Quirks is set on the Document (root) node when the source had no
	// `<!DOCTYPE ...>` at all — the single most common real-world trigger for
	// "quirks mode" (a page that never opted into standards mode). It is the
	// scoped case this engine detects; the HTML spec's fuller algorithm also
	// triggers quirks mode for specific legacy PUBLIC/SYSTEM doctype
	// identifiers (e.g. HTML 4.01 Transitional without a system ID), which
	// are vanishingly rare on real pages that reach this engine and are not
	// modelled — a page with ANY doctype is treated as standards mode here.
	// Confirmed load-bearing live: news.ycombinator.com ships no doctype at
	// all (`<html lang="en" op="news">` directly), and real browsers'
	// quirks-mode UA stylesheet resets `table{text-align:initial}` — without
	// which its `<center><table>...</table></center>` page shell (used only
	// to centre the table AS A BLOCK on the page) inherits centered text
	// into every cell, visibly wrong for cells the real page never intended
	// to centre (see css/ua.go's quirks-mode table rule).
	Quirks bool

	// Indeterminate is an <input type=checkbox>'s own "indeterminate" IDL
	// state (HTML Standard §4.10.5.1.19): unlike Checked, it has NO backing
	// content attribute at all — script is the only way to set it, so it
	// lives here as a plain runtime field, the same shape as Quirks/Shadow
	// above, rather than in Attr. Confirmed real usage: github.com's own
	// behaviors.js sets `checkbox.indeterminate = true` on page load for any
	// `[data-indeterminate]`-marked checkbox (its "select all" bulk-action
	// tri-state pattern). Backs the JS `.indeterminate` accessor and the
	// `:indeterminate` CSS pseudo-class (css/selector.go).
	Indeterminate bool

	// CustomValidity holds a form control's own "custom error message" set by
	// script via `.setCustomValidity(msg)` (HTML Standard's Constraint
	// Validation API) — empty means no custom error. This engine models NO
	// native constraints at all (no required/pattern/min/max/step checking
	// against a value), so this is the ONLY thing that can ever make
	// `.validity.valid`/`.checkValidity()` false — a deliberate, disclosed
	// scope boundary: this backs the real, evidenced usage (see js/dom.go's
	// own doc comment), not a full constraint-validation engine. Runtime-only
	// field, the same shape as Indeterminate above — there is no
	// "customvalidity" content attribute.
	CustomValidity string

	// Shadow is the shadow root attached to this element (a declarative
	// <template shadowrootmode> hoisted out at parse time — see
	// attachDeclarativeShadowRoots), or nil for a plain element. When set,
	// cascade and layout render Shadow.Children in place of this element's
	// own Children — the light-DOM Children above are NOT rendered directly;
	// they are visible only where a <slot> inside the shadow tree projects
	// them (see layout.renderedChildren / assignedSlotNodes).
	Shadow *ShadowRoot
	// ShadowHost is set on every node belonging to a shadow tree (recursively,
	// via markShadowHost) to that tree's host element; nil for an ordinary
	// light-DOM node. A <slot> element uses its own ShadowHost to find the
	// host whose light-DOM children it may project.
	ShadowHost *Node

	// Content is set on a <template> element (and only a <template> element)
	// to an inert "#fragment" node holding what would otherwise be its light-
	// DOM children — the HTMLTemplateElement.content DocumentFragment a real
	// browser exposes, per spec never rendered and never a template's own
	// Children. NewElement and convertChildren both set this up so every
	// <template>, however created, has one; SetInnerHTML/InnerHTML/serialize
	// all read and write through it instead of Children for this tag.
	// Real, confirmed live usage: lit-html's own template-cloning path reads
	// `templateEl.content` (see js.newTreeWalker's own doc comment, which
	// already documents `this.el.content` as the exact real expression
	// caniuse.com's lit-html-based web components read) — with no Content
	// field at all, `.content` was undefined, confirmed via a minimal
	// isolated repro to throw "Cannot read property 'cloneNode' of
	// undefined" the moment real cloning code touches it.
	Content *Node

	// classCache/classCacheRaw memoize Classes()'s split of Attr["class"],
	// keyed by the exact string it was split from — a scripted className/
	// classList write goes through Attr["class"] directly with no dedicated
	// setter to hook, so the cache self-invalidates by comparing the raw
	// string on every call instead of relying on an explicit invalidation
	// call site. Selector matching calls Classes() on every candidate rule
	// for every element (O(rules x elements) on a class-heavy page), so a
	// class list stayed a top CPU cost (strings.Fields, repeated per call)
	// until this was measured with pprof against tailwindcss.com.
	classCacheRaw string
	classCache    []string
}

// Attribute returns the value of the named attribute (lowercased name) and
// whether it was present.
func (n *Node) Attribute(name string) (string, bool) {
	if n.Attr == nil {
		return "", false
	}
	v, ok := n.Attr[name]
	return v, ok
}

// Classes returns the whitespace-split class list of an element.
func (n *Node) Classes() []string {
	c, ok := n.Attribute("class")
	if !ok {
		return nil
	}
	if c == n.classCacheRaw && n.classCache != nil {
		return n.classCache
	}
	n.classCache = strings.Fields(c)
	n.classCacheRaw = c
	return n.classCache
}

// ID returns the element's id attribute (empty if absent).
func (n *Node) ID() string {
	id, _ := n.Attribute("id")
	return id
}

// Parse turns an HTML document into an owned Node tree. The returned node is a
// Document whose single element child is <html>.
func Parse(htmlSrc string) (*Node, error) {
	root, err := html.Parse(strings.NewReader(htmlSrc))
	if err != nil {
		return nil, err
	}
	doc := &Node{Type: Document, Quirks: !hasDoctype(root)}
	convertChildren(root, doc)
	attachDeclarativeShadowRoots(doc)
	return doc, nil
}

// hasDoctype reports whether the parsed tree's top-level children include a
// `<!DOCTYPE ...>` — see Node.Quirks for what this scoped check does and does
// not cover.
func hasDoctype(root *html.Node) bool {
	for c := root.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.DoctypeNode {
			return true
		}
	}
	return false
}

// convertChildren walks h's children, converting element, text and comment
// nodes and dropping everything else (doctype, processing instructions — no
// real page's rendering depends on either surviving into the DOM). A
// <template>'s own children are parsed into its Content fragment instead of
// its Children, per spec (see Node.Content's doc comment) — this is the ONE
// place besides NewElement a template can originate from, so both must agree.
func convertChildren(h *html.Node, parent *Node) {
	for c := h.FirstChild; c != nil; c = c.NextSibling {
		switch c.Type {
		case html.CommentNode:
			parent.Children = append(parent.Children, &Node{
				Type:   Comment,
				Text:   c.Data,
				Parent: parent,
			})
		case html.ElementNode:
			tag := strings.ToLower(c.Data)
			el := &Node{
				Type:   Element,
				Tag:    tag,
				Attr:   map[string]string{},
				Parent: parent,
			}
			for _, a := range c.Attr {
				el.Attr[strings.ToLower(a.Key)] = a.Val
			}
			parent.Children = append(parent.Children, el)
			if tag == "template" {
				el.Content = &Node{Type: Element, Tag: "#fragment", Attr: map[string]string{}}
				convertChildren(c, el.Content)
			} else {
				convertChildren(c, el)
			}
		case html.TextNode:
			// Preserve the raw text; whitespace handling is a layout concern.
			parent.Children = append(parent.Children, &Node{
				Type:   Text,
				Text:   c.Data,
				Parent: parent,
			})
		}
	}
}

// Find returns the first element in document order with the given tag, or nil.
func Find(n *Node, tag string) *Node {
	if n.Type == Element && n.Tag == tag {
		return n
	}
	for _, c := range n.Children {
		if got := Find(c, tag); got != nil {
			return got
		}
	}
	return nil
}

// Title returns the document's <title> text, trimmed, or "".
func Title(root *Node) string {
	t := Find(root, "title")
	if t == nil {
		return ""
	}
	var sb strings.Builder
	for _, c := range t.Children {
		if c.Type == Text {
			sb.WriteString(c.Text)
		}
	}
	return strings.TrimSpace(sb.String())
}
