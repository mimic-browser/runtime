// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"strings"
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// renderInline reconstructs the visible text of an inline run: a single space is
// written wherever an item carries a SpaceBefore, "|" for a forced break, and
// "#" for a replaced element (image). This makes CSS whitespace collapsing —
// including collapsing ACROSS inline-element boundaries — directly assertable.
func renderInline(items []*InlineItem) string {
	var b strings.Builder
	for _, it := range items {
		if it.LineBreak {
			b.WriteString("|")
			continue
		}
		if it.SpaceBefore > 0 {
			b.WriteByte(' ')
		}
		if it.Image != nil {
			b.WriteString("#")
			continue
		}
		b.WriteString(it.Text)
	}
	return b.String()
}

func TestInlineWhitespaceCollapsing(t *testing.T) {
	cases := []struct {
		name string
		body string // inner HTML of a <p>
		want string
	}{
		// Leading + internal + trailing whitespace all collapse; no leading indent.
		{"collapse", "  hi   there  ", "hi there"},
		// No whitespace around an inline element → runs join with no space.
		{"tight-boundary", "a<b>b</b>c", "abc"},
		// Whitespace on either side of an inline element → exactly one space.
		{"spaced-boundary", "a <b>b</b> c", "a b c"},
		// Punctuation immediately after an inline element keeps no space before it
		// (the pre-fix bug produced "y ,"), while the following word gets one.
		{"punct-after-inline", "x <b>y</b>, z", "x y, z"},
		// A whitespace-only text node between two inline elements carries one space.
		{"ws-only-node", "<b>a</b> <b>b</b>", "a b"},
		// A forced break resets the line: the word after it takes no leading space
		// when the source has none around the <br>.
		{"break-tight", "x<br>y", "x|y"},
		// A replaced element (image) participates in whitespace collapsing like a
		// word: one space before it (source had one), one after.
		{"image", `x <img width="10" height="10"> y`, "x # y"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, err := dom.Parse("<html><body><p id=\"p\">" + c.body + "</p></body></html>")
			if err != nil {
				t.Fatal(err)
			}
			sm := css.Cascade(root)
			l := &layouter{sm: sm, m: fakeMeasurer{}, floats: &floatCtx{}}
			p := dom.Find(root, "p")
			items := l.collectInline(p, sm[p], false, 0)
			if got := renderInline(items); got != c.want {
				t.Errorf("collapse(%q) = %q, want %q", c.body, got, c.want)
			}
		})
	}
}
