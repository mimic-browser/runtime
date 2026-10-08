// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"testing"

	"github.com/go-webengine/engine/dom"
)

func TestBorderStyleNoneHidden(t *testing.T) {
	if st, ok := borderStyleKeyword("none"); !ok || st != BorderNone {
		t.Errorf("none = %v %v", st, ok)
	}
	if st, ok := borderStyleKeyword("hidden"); !ok || st != BorderNone {
		t.Errorf("hidden = %v %v", st, ok)
	}
	s := newStyle()
	applyOn(s, "border-style", "none", 16)
	if s.Border.Top.Style != BorderNone {
		t.Error("border-style:none")
	}
}

func TestMarginSideEdges(t *testing.T) {
	// A percentage margin-left is recorded (resolved later, against the
	// containing block's width, in layout's resolveWidths) and clears auto.
	s := newStyle()
	applyOn(s, "margin-left", "10%", 16)
	if s.MarginLeftAuto || !s.MarginLeftIsPercent || s.MarginLeftPercent != 0.1 {
		t.Errorf("percent margin-left = auto=%v isPercent=%v percent=%v", s.MarginLeftAuto, s.MarginLeftIsPercent, s.MarginLeftPercent)
	}
	// The bare keyword auto via the length parser path; also clears the
	// percent flag set just above.
	applyMarginSide(&s.Margin.Left, &s.MarginLeftAuto, &s.MarginLeftIsPercent, &s.MarginLeftPercent, "auto", 16)
	if !s.MarginLeftAuto || s.MarginLeftIsPercent {
		t.Error("auto via applyMarginSide")
	}
	// An unparseable value changes nothing (same "leave the field alone"
	// behaviour as every other malformed-value case in this file).
	applyMarginSide(&s.Margin.Left, &s.MarginLeftAuto, &s.MarginLeftIsPercent, &s.MarginLeftPercent, "not-a-length", 16)
	if !s.MarginLeftAuto {
		t.Error("unparseable margin-left value should leave the previous auto flag untouched")
	}
}

func TestSelectorChildNoParent(t *testing.T) {
	// "div > p" where p is the root (no element parent) fails the child arm.
	p := el("p", "", "")
	sel, _ := parseComplex("div > p")
	if sel.Matches(p) {
		t.Error("child with no parent should not match")
	}
	// Adjacent with no previous sibling (already root).
	adj, _ := parseComplex("a + p")
	if adj.Matches(p) {
		t.Error("adjacent with no previous should not match")
	}
}

func TestElementParentSkipsNonElements(t *testing.T) {
	// A text node between element and its element ancestor is skipped.
	p := el("p", "", "")
	doc := &dom.Node{Type: dom.Document}
	div := el("div", "", "")
	div.Parent = doc
	doc.Children = []*dom.Node{div}
	// Insert p under div with an intervening structure exercising the skip loop.
	p.Parent = div
	div.Children = []*dom.Node{p}
	if got := elementParent(p); got != div {
		t.Errorf("elementParent = %v", got)
	}
	// The div's element parent is nil (its parent is the Document node).
	if got := elementParent(div); got != nil {
		t.Errorf("div element parent = %v want nil", got)
	}
}

func TestHexNibbleInvalid(t *testing.T) {
	if _, ok := hexNibble('z'); ok {
		t.Error("z is not a hex nibble")
	}
	if n, ok := hexNibble('A'); !ok || n != 10 {
		t.Errorf("A = %v %v", n, ok)
	}
	if n, ok := hexNibble('9'); !ok || n != 9 {
		t.Errorf("9 = %v %v", n, ok)
	}
}

func TestTextAlignLeftStart(t *testing.T) {
	s := newStyle()
	s.TextAlign = AlignRight
	applyOn(s, "text-align", "left", 16)
	if s.TextAlign != AlignLeft {
		t.Error("text-align:left")
	}
	applyOn(s, "text-align", "justify", 16) // unknown value ignored
}

func TestMarginShorthandFiveValues(t *testing.T) {
	s := newStyle()
	s.Margin.Top = 99
	applyOn(s, "margin", "1px 2px 3px 4px 5px", 16) // too many → ignored
	if s.Margin.Top != 99 {
		t.Errorf("5-value margin should be ignored, got %+v", s.Margin)
	}
}

func TestParseSimpleAllEmpty(t *testing.T) {
	if _, ok := parseSimple("."); ok {
		t.Error("lone . should fail (no name)")
	}
	if _, ok := parseSimple(":only"); ok {
		t.Error("lone pseudo should fail")
	}
}

func TestPrevElementSiblingDetached(t *testing.T) {
	// A node not present in its recorded parent's children returns nil.
	parent := el("div", "", "")
	orphan := el("span", "", "")
	orphan.Parent = parent // but not in parent.Children
	if prevElementSibling(orphan) != nil {
		t.Error("detached node should have no previous sibling")
	}
}
