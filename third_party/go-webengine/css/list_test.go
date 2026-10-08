// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"testing"

	"github.com/go-webengine/engine/dom"
)

// styleByClass cascades src and returns the computed style of the first element
// carrying the given class attribute.
func styleByClass(t *testing.T, src, class string) *Style {
	t.Helper()
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	sm := Cascade(root)
	var found *dom.Node
	var walk func(n *dom.Node)
	walk = func(n *dom.Node) {
		if found != nil {
			return
		}
		if n.Type == dom.Element {
			if c, ok := n.Attribute("class"); ok && c == class {
				found = n
				return
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	if found == nil {
		t.Fatalf("no element with class %q", class)
	}
	st := sm[found]
	if st == nil {
		t.Fatalf("no style for class %q", class)
	}
	return st
}

// TestUAListDefaults confirms the user-agent defaults flow through the cascade:
// a <ul> is a disc block with the 40px indent, an <ol> is a decimal block, and
// an <li> is a list-item that INHERITS its container's marker glyph.
func TestUAListDefaults(t *testing.T) {
	ul := styleOf(t, `<html><body><ul><li>x</li></ul></body></html>`, "ul")
	if ul.Display != DisplayBlock {
		t.Errorf("ul display = %v", ul.Display)
	}
	if ul.ListStyleType != ListDisc {
		t.Errorf("ul list-style-type = %v, want disc", ul.ListStyleType)
	}
	if ul.Padding.Left != 40 {
		t.Errorf("ul padding-left = %v", ul.Padding.Left)
	}
	if ul.ListItem {
		t.Error("ul should not itself be a list-item")
	}

	li := styleOf(t, `<html><body><ul><li>x</li></ul></body></html>`, "li")
	if !li.ListItem {
		t.Error("li should be a list-item")
	}
	if li.Display != DisplayBlock {
		t.Errorf("li display = %v", li.Display)
	}
	if li.ListStyleType != ListDisc {
		t.Errorf("li inherited list-style-type = %v, want disc", li.ListStyleType)
	}

	ol := styleOf(t, `<html><body><ol><li>x</li></ol></body></html>`, "ol")
	if ol.ListStyleType != ListDecimal {
		t.Errorf("ol list-style-type = %v, want decimal", ol.ListStyleType)
	}
	oli := styleOf(t, `<html><body><ol><li>x</li></ol></body></html>`, "li")
	if oli.ListStyleType != ListDecimal {
		t.Errorf("ol > li inherited list-style-type = %v, want decimal", oli.ListStyleType)
	}
}

// TestNestedUAAlternation confirms the depth-based UA descendant rules:
// depth 1 disc, depth 2 circle, depth 3-and-deeper square.
func TestNestedUAAlternation(t *testing.T) {
	src := `<html><body>
	  <ul class="d1"><li>a
	    <ul class="d2"><li>b
	      <ul class="d3"><li>c
	        <ul class="d4"><li>d</li></ul>
	      </li></ul>
	    </li></ul>
	  </li></ul>
	</body></html>`
	if got := styleByClass(t, src, "d1").ListStyleType; got != ListDisc {
		t.Errorf("depth 1 = %v, want disc", got)
	}
	if got := styleByClass(t, src, "d2").ListStyleType; got != ListCircle {
		t.Errorf("depth 2 = %v, want circle", got)
	}
	if got := styleByClass(t, src, "d3").ListStyleType; got != ListSquare {
		t.Errorf("depth 3 = %v, want square", got)
	}
	if got := styleByClass(t, src, "d4").ListStyleType; got != ListSquare {
		t.Errorf("depth 4 = %v, want square (stays square)", got)
	}
}

// TestListStyleTypeParsing exercises every list-style-type keyword and a bad one.
func TestListStyleTypeParsing(t *testing.T) {
	cases := map[string]ListStyleType{
		"disc": ListDisc, "circle": ListCircle, "square": ListSquare,
		"decimal": ListDecimal, "none": ListNone,
	}
	for kw, want := range cases {
		st := styleOf(t, `<html><body><ul style="list-style-type:`+kw+`"><li>x</li></ul></body></html>`, "ul")
		if st.ListStyleType != want {
			t.Errorf("list-style-type:%s = %v, want %v", kw, st.ListStyleType, want)
		}
	}
	// An unrecognised keyword leaves the UA default (disc) unchanged.
	st := styleOf(t, `<html><body><ul style="list-style-type:lower-roman"><li>x</li></ul></body></html>`, "ul")
	if st.ListStyleType != ListDisc {
		t.Errorf("unknown list-style-type changed value to %v", st.ListStyleType)
	}
	// parseListStyleType directly on a bad value.
	if _, ok := parseListStyleType("garbage"); ok {
		t.Error("parseListStyleType accepted garbage")
	}
}

// TestListStylePosition parses the position property (inside/outside/unknown).
func TestListStylePosition(t *testing.T) {
	in := styleOf(t, `<html><body><ul style="list-style-position:inside"><li>x</li></ul></body></html>`, "ul")
	if in.ListStylePosition != ListInside {
		t.Errorf("inside = %v", in.ListStylePosition)
	}
	out := styleOf(t, `<html><body><ul style="list-style-position:outside"><li>x</li></ul></body></html>`, "ul")
	if out.ListStylePosition != ListOutside {
		t.Errorf("outside = %v", out.ListStylePosition)
	}
	// Inherited to the item.
	inLi := styleOf(t, `<html><body><ul style="list-style-position:inside"><li>x</li></ul></body></html>`, "li")
	if inLi.ListStylePosition != ListInside {
		t.Errorf("li inherited position = %v", inLi.ListStylePosition)
	}
	// Unknown value is a no-op (keeps outside default).
	bad := styleOf(t, `<html><body><ul style="list-style-position:sideways"><li>x</li></ul></body></html>`, "ul")
	if bad.ListStylePosition != ListOutside {
		t.Errorf("unknown position changed value to %v", bad.ListStylePosition)
	}
}

// TestListStyleShorthand covers the list-style shorthand arms.
func TestListStyleShorthand(t *testing.T) {
	// type only.
	a := styleOf(t, `<html><body><ul style="list-style:square"><li>x</li></ul></body></html>`, "ul")
	if a.ListStyleType != ListSquare {
		t.Errorf("shorthand type = %v", a.ListStyleType)
	}
	// type + position (any order) + an ignored url() image token.
	b := styleOf(t, `<html><body><ul style="list-style:inside circle url(dot.png)"><li>x</li></ul></body></html>`, "ul")
	if b.ListStyleType != ListCircle || b.ListStylePosition != ListInside {
		t.Errorf("shorthand = type %v pos %v", b.ListStyleType, b.ListStylePosition)
	}
	// outside keyword + none type.
	c := styleOf(t, `<html><body><ul style="list-style:none outside"><li>x</li></ul></body></html>`, "ul")
	if c.ListStyleType != ListNone || c.ListStylePosition != ListOutside {
		t.Errorf("shorthand none outside = type %v pos %v", c.ListStyleType, c.ListStylePosition)
	}
}

// TestDisplayListItemToggle confirms display:list-item sets the flag and any
// other display value clears it (last declaration wins in the cascade).
func TestDisplayListItemToggle(t *testing.T) {
	on := styleOf(t, `<html><body><div style="display:list-item">x</div></body></html>`, "div")
	if !on.ListItem || on.Display != DisplayBlock {
		t.Errorf("display:list-item = ListItem %v display %v", on.ListItem, on.Display)
	}
	// A later display:block wins and clears the list-item flag.
	off := styleOf(t, `<html><body><li style="display:block">x</li></body></html>`, "li")
	if off.ListItem {
		t.Error("display:block should clear the list-item flag")
	}
}
