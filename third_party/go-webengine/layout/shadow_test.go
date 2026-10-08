// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"reflect"
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// layoutShadowHTML lays out src with an EMPTY StyleMap, deliberately
// bypassing css.Cascade (which is itself shadow-DOM-aware — see
// css/shadowdom.go — but that is package css's own concern, exercised by its
// own tests). Every node here has a nil style, which collapses the whole
// document to one inline formatting context (see contents() /
// hasBlockLevelChild(), both of which treat a nil *css.Style as "not
// block-level"): exactly what these tests want, since they are checking slot
// PROJECTION in isolation — which text ends up in the flattened rendered
// content, and in what order — not box geometry or real computed styles.
// firstLineItems(box) below therefore reads the line items directly off the
// root box. Tests that need real display:flex/grid/table dispatch (below)
// use layoutHTML (real css.Cascade) instead, the same as every other
// flex/grid/table test in this package.
func layoutShadowHTML(t *testing.T, src string) *Box {
	t.Helper()
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	box, _ := LayoutDocument(root, css.StyleMap{}, 1024, fakeMeasurer{}, nil)
	return box
}

func TestSlotDefaultDistributionAndUnmatchedSlotAttrDropped(t *testing.T) {
	// The declarative shadow root's default <slot> falls back to "Fallback"
	// only when nothing is assigned to it — here the host has an assigned
	// text node ("Loose") and an assigned element (<b>A</b>), so "Fallback"
	// must NOT appear. <i slot="missing"> names a slot that does not exist in
	// this shadow tree, so per spec it renders NOTHING — not the default
	// slot, not anywhere.
	box := layoutShadowHTML(t, `<my-elem>`+
		`<template shadowrootmode="open"><div><slot>Fallback</slot></div></template>`+
		`Loose <b>A</b><i slot="missing">Unrendered</i>`+
		`</my-elem>`)
	got := texts(firstLineItems(box))
	want := []string{"Loose", "A"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("texts = %v, want %v", got, want)
	}
}

func TestSlotNamedDistributionFollowsShadowTreeOrder(t *testing.T) {
	// The shadow tree places <slot name="b"> BEFORE <slot name="a">, so the
	// rendered order must follow the SHADOW tree's slot order, not the light
	// DOM's source order (slot="a" is written first in the light DOM below).
	box := layoutShadowHTML(t, `<my-elem>`+
		`<template shadowrootmode="open"><div><slot name="b"></slot><slot name="a"></slot></div></template>`+
		`<span slot="a">First</span><span slot="b">Second</span>`+
		`</my-elem>`)
	got := texts(firstLineItems(box))
	want := []string{"Second", "First"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("texts = %v, want %v (shadow tree order, not light DOM order)", got, want)
	}
}

func TestSlotFallbackContentWhenNothingAssigned(t *testing.T) {
	box := layoutShadowHTML(t, `<my-elem>`+
		`<template shadowrootmode="open"><div><slot>Fallback</slot></div></template>`+
		`</my-elem>`)
	got := texts(firstLineItems(box))
	want := []string{"Fallback"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("texts = %v, want %v", got, want)
	}
}

func TestSlotOutsideShadowTreeRendersOwnContent(t *testing.T) {
	// A <slot> with no enclosing shadow root (ShadowHost nil) is just a plain,
	// unrecognised element: it shows its own children, like any other unknown
	// tag — assignedSlotNodes returns nil for it (exercised directly below),
	// and renderedChildren falls back to its own Children.
	box := layoutShadowHTML(t, `<body><slot>Plain</slot></body>`)
	got := texts(firstLineItems(box))
	want := []string{"Plain"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("texts = %v, want %v", got, want)
	}
}

func TestAssignedSlotNodesNilWithoutShadowHost(t *testing.T) {
	slot := dom.NewElement("slot")
	if got := assignedSlotNodes(slot); got != nil {
		t.Errorf("assignedSlotNodes(no ShadowHost) = %v, want nil", got)
	}
}

func TestRenderedChildrenPlainElementPassthrough(t *testing.T) {
	root, err := dom.Parse(`<body><div><span>a</span><span>b</span></div></body>`)
	if err != nil {
		t.Fatal(err)
	}
	div := dom.Find(root, "div")
	got := (&layouter{}).renderedChildren(div)
	if !reflect.DeepEqual(got, div.Children) {
		t.Errorf("renderedChildren(plain div) = %v, want div.Children (%v)", got, div.Children)
	}
}

func TestSlotWithoutNameReceivesUnnamedLightChildrenOnly(t *testing.T) {
	// A named-slot child ("named") must NOT be picked up by the default
	// (unnamed) slot even though the default slot is otherwise happy to take
	// any child with no "slot" attribute.
	box := layoutShadowHTML(t, `<my-elem>`+
		`<template shadowrootmode="open"><div><slot></slot></div></template>`+
		`<b>unnamed</b><i slot="x">named</i>`+
		`</my-elem>`)
	got := texts(firstLineItems(box))
	want := []string{"unnamed"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("texts = %v, want %v", got, want)
	}
}

// The following exercise renderedChildren's use in the flex/grid/table
// container algorithms (flexItems, gridItems, collectRows/makeRow) and in
// preferredWidth's flex-row and block max-content scans — separate code
// paths from contents()'s default block/inline dispatch that also walk a
// node's children directly and so needed the SAME substitution. Unlike the
// tests above, these use real css.Cascade (layoutHTML, shared with every
// other flex/grid/table test in this package) since flex/grid/table dispatch
// is driven by a real computed `display` value, which an empty StyleMap
// cannot provide.

func TestSlotDistributionInsideFlexContainer(t *testing.T) {
	// The flex container's only DIRECT child in the flat tree is the <slot>
	// itself (default display:inline, like any unstyled element — this
	// engine has no UA default for "slot" — so flexbox blockifies exactly
	// ONE box, matching real Shadow DOM: a <slot> is a real, styleable
	// participant, not erased from the tree; that's what makes
	// `:host(...) slot{display:none}` able to hide it at all). Both
	// projected spans render as ITS inline content, not as two independent
	// flex items — proving flexItems reached the shadow tree (via
	// renderedChildren) rather than either ignoring it or over-flattening
	// past the slot boundary.
	src := `<html><body style="margin:0"><my-elem style="display:flex">` +
		`<template shadowrootmode="open"><slot></slot></template>` +
		`<span>A</span><span>B</span>` +
		`</my-elem></body></html>`
	host := findBox(layoutHTML(t, src, 300), "my-elem")
	if host == nil {
		t.Fatal("no my-elem box")
	}
	if len(host.Children) != 1 {
		t.Fatalf("flex container should have exactly 1 flex-item box (the <slot>), got %d", len(host.Children))
	}
	got := texts(firstLineItems(host.Children[0]))
	want := []string{"A", "B"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("slot flex-item's content = %v, want %v", got, want)
	}
}

func TestSlotDistributionInsideGridContainer(t *testing.T) {
	// Same reasoning as the flex case above: one grid item (the <slot>),
	// both spans rendered as its own inline content.
	src := `<html><body style="margin:0"><my-elem style="display:grid;grid-template-columns:100px">` +
		`<template shadowrootmode="open"><slot></slot></template>` +
		`<span>A</span><span>B</span>` +
		`</my-elem></body></html>`
	host := findBox(layoutHTML(t, src, 300), "my-elem")
	if host == nil {
		t.Fatal("no my-elem box")
	}
	if len(host.Children) != 1 {
		t.Fatalf("grid container should have exactly 1 grid-item box (the <slot>), got %d", len(host.Children))
	}
	got := texts(firstLineItems(host.Children[0]))
	want := []string{"A", "B"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("slot grid-item's content = %v, want %v", got, want)
	}
}

func TestSlotDistributionInsideTableRowGroup(t *testing.T) {
	// The row GROUP itself is the shadow host here, projecting its light-DOM
	// <tr> children into its shadow tree's default slot — collectRows must
	// follow renderedChildren one level into the row group too, not just at
	// the table's own top level.
	src := `<html><body style="margin:0"><table><my-tbody>` +
		`<template shadowrootmode="open"><slot></slot></template>` +
		`<tr><td>A</td></tr><tr><td>B</td></tr>` +
		`</my-tbody></table></body></html>`
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	// The row group's display must be set explicitly: table-row-group is not
	// this engine's UA default for an unknown custom-element tag.
	tbody := dom.Find(root, "my-tbody")
	tbody.Attr["style"] = "display:table-row-group"
	sm := css.Cascade(root)
	box, _ := LayoutDocument(root, sm, 300, fakeMeasurer{}, nil)
	table := findBox(box, "table")
	if table == nil {
		t.Fatal("no table box")
	}
	if len(table.Children) != 2 {
		t.Fatalf("table should have 2 row boxes from the row group's projected slot content, got %d", len(table.Children))
	}
}
