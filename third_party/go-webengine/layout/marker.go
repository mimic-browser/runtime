// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"strconv"
	"strings"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// attachMarker builds and attaches the list-item marker for an already-laid-out
// box. ordinal is the item's number within its list (used only by decimal
// markers). The marker is positioned in the indent to the LEFT of the box's
// content (list-style-position: outside), vertically aligned to the first line.
//
// list-style-position: inside is parsed and cascaded but rendered here as
// outside (a documented deferral); it therefore takes the same geometry.
func (l *layouter) attachMarker(box *Box, st *css.Style, ordinal int) {
	if st.ListStyleType == css.ListNone {
		return
	}
	em := st.FontSize
	ascent, _ := l.lineMetricsFor(st)
	// The marker aligns to the first line of the item's content; an empty item
	// falls back to the top of its content box.
	lineTop := box.ContentY
	if line := firstLineBox(box); line != nil {
		lineTop = line.Y
	}
	// Gap between the marker and the content edge (a fraction of the font size).
	gap := 0.5 * em

	m := &Marker{Type: st.ListStyleType, Style: st}
	if st.ListStyleType == css.ListDecimal {
		m.Text = strconv.Itoa(ordinal) + "."
		m.W = l.m.Measure(m.Text, st.FontFamily, st.FontSize, st.FontWeight, st.Italic)
		m.Ascent = ascent
		m.X = box.ContentX - gap - m.W // right-aligned: right edge at contentX-gap
		m.Y = lineTop
	} else {
		// disc / circle / square: a square glyph box ~0.35em on a side, vertically
		// centred near the middle of the first line's lowercase text.
		size := 0.35 * em
		centerY := lineTop + ascent - 0.32*em
		m.W, m.H = size, size
		m.X = box.ContentX - gap - size
		m.Y = centerY - size/2
	}
	box.Marker = m
}

// firstLineBox returns the first inline line box in a subtree (the box's own
// lines, else the first line found by descending its children in order), or nil
// when the subtree holds no inline content.
func firstLineBox(box *Box) *LineBox {
	if len(box.Lines) > 0 {
		return box.Lines[0]
	}
	for _, ch := range box.Children {
		if lb := firstLineBox(ch); lb != nil {
			return lb
		}
	}
	return nil
}

// listStart returns the starting ordinal for a list container's items: an
// <ol start="N"> seeds N, everything else starts at 1.
func listStart(node *dom.Node) int {
	if node.Type == dom.Element && node.Tag == "ol" {
		if v, ok := attrInt(node, "start"); ok {
			return v
		}
	}
	return 1
}

// attrInt parses an integer-valued attribute, reporting whether it was present
// and well-formed.
func attrInt(n *dom.Node, name string) (int, bool) {
	v, ok := n.Attribute(name)
	if !ok {
		return 0, false
	}
	i, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0, false
	}
	return i, true
}
