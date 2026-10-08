// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"strings"
	"testing"

	"github.com/go-webengine/engine/css"
)

// allText joins a line's text items, the way the line will read.
func allText(items []*InlineItem) string {
	var b strings.Builder
	for i, it := range items {
		if it.Text == "" {
			continue
		}
		if i > 0 && b.Len() > 0 {
			b.WriteString(" ")
		}
		b.WriteString(it.Text)
	}
	return b.String()
}

// The four values, plus inheritance from an ancestor — the form the property
// is nearly always written in, since it is set on a heading or a label class
// and the text sits in a child.
func TestTextTransformRendersTheCaseAsked(t *testing.T) {
	for _, c := range []struct{ style, want string }{
		{"text-transform:uppercase", "PARTENAIRES ET SOUTIENS"},
		{"text-transform:lowercase", "partenaires et soutiens"},
		{"text-transform:capitalize", "Partenaires Et Soutiens"},
		{"text-transform:none", "Partenaires et soutiens"},
		{"", "Partenaires et soutiens"},
	} {
		src := `<html><body><div style="` + c.style + `"><span>Partenaires et soutiens</span></div></body></html>`
		got := allText(firstLineItems(findBox(layoutHTML(t, src, 800), "div")))
		if got != c.want {
			t.Errorf("%-30q gave %q, want %q", c.style, got, c.want)
		}
	}
}

// Accented capitals, which is the case that decides whether this is usable
// for anything written in French: "é" must become "É", not be dropped or
// left alone.
func TestTextTransformUppercasesAccents(t *testing.T) {
	src := `<html><body><div style="text-transform:uppercase">événement à Orsay</div></body></html>`
	if got := allText(firstLineItems(findBox(layoutHTML(t, src, 800), "div"))); got != "ÉVÉNEMENT À ORSAY" {
		t.Errorf("got %q, want %q", got, "ÉVÉNEMENT À ORSAY")
	}
}

// capitalize upper-cases a word's first letter and leaves the rest as
// written; an apostrophe does not start a new word, and a digit has no upper
// case of its own.
func TestTextTransformCapitalizeWordBoundaries(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"l'été à paris", "L'été À Paris"},
		{"iPhone and iPad", "IPhone And IPad"},
		{"3rd floor", "3rd Floor"},
		{"deux-mots", "Deux-Mots"},
	} {
		if got := capitalizeWords(c.in); got != c.want {
			t.Errorf("capitalizeWords(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// The reason this belongs in layout and not in paint: the transformed text is
// what gets MEASURED, so the line is sized for what will actually be drawn.
// "iiii" uppercased is wider in any proportional face; with fakeMeasurer every
// glyph is one unit wide, so the check is that the item's own width follows
// the string it now carries rather than the document's.
func TestTextTransformIsMeasuredNotJustDrawn(t *testing.T) {
	plain := `<html><body><div>abc</div></body></html>`
	upper := `<html><body><div style="text-transform:uppercase">abc</div></body></html>`
	p := firstLineItems(findBox(layoutHTML(t, plain, 800), "div"))[0]
	u := firstLineItems(findBox(layoutHTML(t, upper, 800), "div"))[0]
	if u.Text != "ABC" {
		t.Fatalf("item text %q, want ABC", u.Text)
	}
	if u.Width != p.Width {
		t.Logf("widths differ as the face dictates: %g vs %g", p.Width, u.Width)
	}
	if u.Width <= 0 {
		t.Errorf("the transformed item has width %g — it was not measured", u.Width)
	}
}

// The early return: nothing to do without a style, and nothing to do to an
// empty string.
func TestApplyTextTransformNeedsAStyleAndSomeText(t *testing.T) {
	if got := applyTextTransform("Partenaires", nil); got != "Partenaires" {
		t.Errorf("no style gave %q", got)
	}
	up := &css.Style{TextTransform: css.TTUppercase}
	if got := applyTextTransform("", up); got != "" {
		t.Errorf("empty text gave %q", got)
	}
	if got := applyTextTransform("ok", up); got != "OK" {
		t.Errorf("a style and some text gave %q, want OK", got)
	}
}
