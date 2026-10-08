// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"testing"

	"github.com/go-webengine/engine/css"
)

// findAll collects every box for a given tag in document order.
func findAll(b *Box, tag string) []*Box {
	var out []*Box
	var walk func(*Box)
	walk = func(x *Box) {
		if x == nil {
			return
		}
		if x.Node != nil && x.Node.Tag == tag {
			out = append(out, x)
		}
		for _, c := range x.Children {
			walk(c)
		}
	}
	walk(b)
	return out
}

// TestDiscMarkerGeometry asserts the disc marker's PRECISE box relative to the
// item content box: it sits in the indent to the left of the content, vertically
// centred on the first line. fakeMeasurer gives em=16, ascent=8.
func TestDiscMarkerGeometry(t *testing.T) {
	root := layoutHTML(t, `<html><body style="margin:0"><ul><li>x</li></ul></body></html>`, 1024)
	li := findBox(root, "li")
	if li == nil || li.Marker == nil {
		t.Fatal("no li marker")
	}
	m := li.Marker
	if m.Type != css.ListDisc {
		t.Fatalf("marker type = %v, want disc", m.Type)
	}
	em := li.Style.FontSize
	ascent, _ := (&layouter{m: fakeMeasurer{}}).lineMetricsFor(li.Style)
	line := firstLineBox(li)
	if line == nil {
		t.Fatal("li has no first line")
	}
	size := 0.35 * em
	gap := 0.5 * em
	assertF(t, "disc.W", m.W, size)
	assertF(t, "disc.H", m.H, size)
	assertF(t, "disc.X", m.X, li.ContentX-gap-size)
	assertF(t, "disc.Y", m.Y, line.Y+ascent-0.32*em-size/2)

	// Bounds: the marker lies entirely left of the content and inside the list.
	ul := findBox(root, "ul")
	if m.X+m.W > li.ContentX {
		t.Errorf("marker right edge %v past content left %v", m.X+m.W, li.ContentX)
	}
	if m.X < ul.X {
		t.Errorf("marker left %v spills left of the ul box %v", m.X, ul.X)
	}
}

// TestDecimalMarkerCounter asserts ordered-list numbering, the right-aligned
// text geometry, and that the counter increments across items.
func TestDecimalMarkerCounter(t *testing.T) {
	root := layoutHTML(t, `<html><body style="margin:0"><ol><li>a</li><li>b</li></ol></body></html>`, 1024)
	lis := findAll(root, "li")
	if len(lis) != 2 {
		t.Fatalf("want 2 li, got %d", len(lis))
	}
	want := []string{"1.", "2."}
	for i, li := range lis {
		if li.Marker == nil || li.Marker.Type != css.ListDecimal {
			t.Fatalf("li[%d] marker = %+v", i, li.Marker)
		}
		if li.Marker.Text != want[i] {
			t.Errorf("li[%d] text = %q, want %q", i, li.Marker.Text, want[i])
		}
		m := li.Marker
		w := fakeMeasurer{}.Measure(m.Text, li.Style.FontFamily, li.Style.FontSize, li.Style.FontWeight, li.Style.Italic)
		gap := 0.5 * li.Style.FontSize
		line := firstLineBox(li)
		assertF(t, "dec.W", m.W, w)
		assertF(t, "dec.X", m.X, li.ContentX-gap-w)
		assertF(t, "dec.Y", m.Y, line.Y)
		assertF(t, "dec.Ascent", m.Ascent, 8)
	}
}

// TestOrderedListStartAndValue honours <ol start> and per-item <li value>, and
// ignores malformed values.
func TestOrderedListStartAndValue(t *testing.T) {
	root := layoutHTML(t, `<html><body><ol start="5"><li>a</li><li value="9">b</li><li>c</li></ol></body></html>`, 1024)
	got := markerTexts(findAll(root, "li"))
	want := []string{"5.", "9.", "10."}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("item %d marker = %q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}

	// Malformed start / value fall back to the running counter (1, then 2).
	root2 := layoutHTML(t, `<html><body><ol start="oops"><li>a</li><li value="x">b</li></ol></body></html>`, 1024)
	got2 := markerTexts(findAll(root2, "li"))
	if got2[0] != "1." || got2[1] != "2." {
		t.Errorf("malformed start/value = %v, want [1. 2.]", got2)
	}
}

func markerTexts(lis []*Box) []string {
	out := make([]string, len(lis))
	for i, li := range lis {
		if li.Marker != nil {
			out[i] = li.Marker.Text
		}
	}
	return out
}

// TestNestedListMarkerTypes confirms the depth-alternating glyph reaches the
// laid-out markers, and that each nested <ol> restarts its counter.
func TestNestedListMarkerTypes(t *testing.T) {
	root := layoutHTML(t, `<html><body>
	  <ul><li>a<ul><li>b</li></ul></li></ul></body></html>`, 1024)
	lis := findAll(root, "li")
	if len(lis) != 2 {
		t.Fatalf("want 2 li, got %d", len(lis))
	}
	if lis[0].Marker.Type != css.ListDisc {
		t.Errorf("outer li = %v, want disc", lis[0].Marker.Type)
	}
	if lis[1].Marker.Type != css.ListCircle {
		t.Errorf("inner li = %v, want circle", lis[1].Marker.Type)
	}

	// Nested <ol> restarts at 1.
	root2 := layoutHTML(t, `<html><body><ol><li>a<ol><li>x</li><li>y</li></ol></li><li>b</li></ol></body></html>`, 1024)
	got := markerTexts(findAll(root2, "li"))
	// Document order: outer#1, inner#1, inner#2, outer#2.
	want := []string{"1.", "1.", "2.", "2."}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("nested ol item %d = %q, want %q (all=%v)", i, got[i], want[i], got)
		}
	}
}

// TestListStyleNoneNoMarker: list-style-type:none attaches no marker at all.
func TestListStyleNoneNoMarker(t *testing.T) {
	root := layoutHTML(t, `<html><body><ul style="list-style-type:none"><li>x</li></ul></body></html>`, 1024)
	li := findBox(root, "li")
	if li.Marker != nil {
		t.Errorf("none should attach no marker, got %+v", li.Marker)
	}
}

// TestEmptyListItemMarker: an empty item still gets a marker, aligned to the top
// of its (empty) content box since there is no first line.
func TestEmptyListItemMarker(t *testing.T) {
	root := layoutHTML(t, `<html><body style="margin:0"><ul><li></li></ul></body></html>`, 1024)
	li := findBox(root, "li")
	if li.Marker == nil {
		t.Fatal("empty li lost its marker")
	}
	if firstLineBox(li) != nil {
		t.Fatal("empty li unexpectedly has a line")
	}
	em := li.Style.FontSize
	ascent, _ := (&layouter{m: fakeMeasurer{}}).lineMetricsFor(li.Style)
	// Falls back to ContentY as the line top.
	assertF(t, "empty.Y", li.Marker.Y, li.ContentY+ascent-0.32*em-(0.35*em)/2)
}

// TestMarkerFirstLineDescendsChildren: a marker on an item whose content is
// block children aligns to the first line found by descending those children,
// skipping an empty leading block.
func TestMarkerFirstLineDescendsChildren(t *testing.T) {
	root := layoutHTML(t, `<html><body style="margin:0"><ul><li><div></div><div>x</div></li></ul></body></html>`, 1024)
	li := findBox(root, "li")
	if li.Marker == nil {
		t.Fatal("no marker")
	}
	// The first line is inside the SECOND div (the first is empty).
	divs := findAll(li, "div")
	if len(divs) != 2 {
		t.Fatalf("want 2 divs, got %d", len(divs))
	}
	line := firstLineBox(divs[1])
	if line == nil {
		t.Fatal("second div has no line")
	}
	em := li.Style.FontSize
	ascent, _ := (&layouter{m: fakeMeasurer{}}).lineMetricsFor(li.Style)
	assertF(t, "descend.Y", li.Marker.Y, line.Y+ascent-0.32*em-(0.35*em)/2)
}
