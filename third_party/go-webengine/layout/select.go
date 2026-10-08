// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"strings"

	"github.com/go-webengine/engine/dom"
)

// optionLabel is an <option>'s displayed text: its `label` attribute if
// present and non-empty, else its text content
// (https://html.spec.whatwg.org/multipage/form-elements.html#the-option-element).
// A close approximation of the spec's "text" IDL attribute, which additionally
// collapses runs of internal whitespace to a single space — not modelled
// here, consistent with this engine's existing plain-text handling elsewhere.
// Deliberately duplicated in paint/paint.go (its own optionLabel), matching
// this codebase's established convention for small DOM-attribute-reading
// helpers needed independently by both packages (see paint.controlLabel's own
// doc comment) rather than forcing a shared import between them.
func optionLabel(opt *dom.Node) string {
	if v, ok := opt.Attribute("label"); ok && v != "" {
		return v
	}
	return strings.TrimSpace(dom.TextContent(opt))
}

// selectOptionLabels collects every label reachable from a <select> — each
// <option>'s (through any nesting <optgroup>) plus each <optgroup>'s own
// `label`, which a real UA also shows as a (non-selectable) row — for sizing
// the control to its widest possible entry. This mirrors the common pattern
// real engines use (e.g. WebKit's RenderMenuList::updateOptionsWidth) so the
// box does not visually resize as a different option becomes selected.
func selectOptionLabels(sel *dom.Node) []string {
	var labels []string
	var walk func(n *dom.Node)
	walk = func(n *dom.Node) {
		for _, c := range n.Children {
			if c.Type != dom.Element {
				continue
			}
			switch c.Tag {
			case "option":
				labels = append(labels, optionLabel(c))
			case "optgroup":
				if v, ok := c.Attribute("label"); ok && v != "" {
					labels = append(labels, v)
				}
				walk(c)
			}
		}
	}
	walk(sel)
	return labels
}
