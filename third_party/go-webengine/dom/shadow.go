// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package dom

// ShadowRoot is a shadow tree attached to a host element: a separate node
// list, distinct from the host's own (light-DOM) Children, that cascade and
// layout render in the host's place. It models declarative Shadow DOM only
// (a <template shadowrootmode="open"|"closed"> consumed at parse time — see
// attachDeclarativeShadowRoots); there is no imperative attachShadow()/
// customElements.define() path. "open" and "closed" are treated identically
// for rendering (the distinction only affects JS .shadowRoot introspection,
// out of scope here), so the mode itself is not recorded.
type ShadowRoot struct {
	// Host is the element this shadow root is attached to.
	Host *Node
	// Children are the shadow tree's top-level nodes (what a real
	// DocumentFragment's children would be) — the host's declarative
	// <template>'s .content, hoisted out of the light DOM. Their Parent
	// pointers are nil (they are the root of a separate tree); descendants
	// further down keep the ordinary Parent chain within the shadow tree.
	Children []*Node
}

// firstElementChild returns n's first Element-type child, or nil. Unlike
// Find it does not recurse — declarative Shadow DOM requires the <template>
// to be the host's first child specifically (a simplification of the real
// parser rule, which only requires it to precede any other content; this
// engine parses the whole document up front rather than incrementally, so
// "first child" is the closest equivalent), not merely a descendant.
func firstElementChild(n *Node) *Node {
	for _, c := range n.Children {
		if c.Type == Element {
			return c
		}
	}
	return nil
}

// attachDeclarativeShadowRoots walks n's subtree converting every declarative
// shadow-DOM template — a <template shadowrootmode="open"|"closed"> that is
// the first element child of its parent — into an attached ShadowRoot on that
// parent, mirroring how a real browser's HTML parser attaches one as it
// parses (the <template> element itself never becomes part of the live tree;
// only its .content does, as the shadow root's children). It recurses into
// both the resulting shadow tree's content (a shadow tree may itself declare
// nested shadow roots) and the remaining light DOM, so this is a single
// bottom-up-agnostic pass over the whole document.
//
// This runs once, immediately after HTML parsing (see Parse) — not per
// render — matching real parser timing and keeping every later cascade/
// layout pass (the settle loop runs several) working from the same stable
// shadow structure without re-detecting it.
func attachDeclarativeShadowRoots(n *Node) {
	if n.Type == Element && n.Shadow == nil {
		if tmpl := firstElementChild(n); tmpl != nil && tmpl.Tag == "template" {
			if mode, ok := tmpl.Attribute("shadowrootmode"); ok && (mode == "open" || mode == "closed") {
				RemoveChild(n, tmpl)
				// The template's real content lives in its Content fragment
				// (see Node.Content's doc comment), never in tmpl.Children
				// directly — hoist FROM there, matching what this type's own
				// doc comment above already says ("the host's declarative
				// <template>'s .content, hoisted out of the light DOM").
				var content []*Node
				if tmpl.Content != nil {
					content = tmpl.Content.Children
					tmpl.Content.Children = nil
				}
				sr := &ShadowRoot{Host: n, Children: content}
				n.Shadow = sr
				for _, c := range sr.Children {
					c.Parent = nil
					markShadowHost(c, n)
				}
			}
		}
	}
	if n.Shadow != nil {
		for _, c := range n.Shadow.Children {
			attachDeclarativeShadowRoots(c)
		}
	}
	for _, c := range n.Children {
		attachDeclarativeShadowRoots(c)
	}
}

// markShadowHost records host as the shadow-tree owner of n and every
// descendant reachable through n.Children. It runs BEFORE any nested
// declarative shadow root within n's subtree has been hoisted out, so it
// necessarily (and harmlessly) also marks a not-yet-processed nested
// <template shadowrootmode> and its own content with the OUTER host; when
// attachDeclarativeShadowRoots's subsequent recursive call reaches that
// nested host, it hoists the nested template and re-marks that content with
// the nested (correct, innermost) host, overwriting the transient outer
// marking. The field this sets (Node.ShadowHost) is what a <slot> uses to
// find its host's light-DOM children for slot assignment (see
// layout.assignedSlotNodes) — a stored back-reference, rather than threading
// a "current host" parameter through every layout tree-walk function, since
// layout's call graph (block contents / inline collection / anonymous-run
// flushing) is not a single simple recursion the way CSS's cascade walk is.
func markShadowHost(n *Node, host *Node) {
	n.ShadowHost = host
	for _, c := range n.Children {
		markShadowHost(c, host)
	}
}
