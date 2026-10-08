// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package dom

import "testing"

func TestNewElementAndText(t *testing.T) {
	el := NewElement("DIV")
	if el.Type != Element || el.Tag != "div" || el.Attr == nil {
		t.Fatalf("NewElement: %+v", el)
	}
	tx := NewText("hi")
	if tx.Type != Text || tx.Text != "hi" {
		t.Fatalf("NewText: %+v", tx)
	}
}

func TestNewElementTemplateHasContent(t *testing.T) {
	tpl := NewElement("template")
	if tpl.Content == nil {
		t.Fatal("NewElement(\"template\") has no Content fragment")
	}
	if len(tpl.Content.Children) != 0 {
		t.Fatalf("fresh template's Content already has children: %v", tpl.Content.Children)
	}
	// A non-template element gets no Content at all.
	if div := NewElement("div"); div.Content != nil {
		t.Error("NewElement(\"div\") should not get a Content fragment")
	}
}

func TestSetInnerHTMLOnTemplateWritesContent(t *testing.T) {
	tpl := NewElement("template")
	AppendChild(tpl.Content, NewText("stale")) // pre-existing content, must be replaced
	if err := SetInnerHTML(tpl, `<b>hi</b>`); err != nil {
		t.Fatal(err)
	}
	if len(tpl.Children) != 0 {
		t.Fatalf("template's own Children = %v, want none", tpl.Children)
	}
	if len(tpl.Content.Children) != 1 || tpl.Content.Children[0].Tag != "b" {
		t.Fatalf("template content = %v", tpl.Content.Children)
	}
	if got := InnerHTML(tpl); got != "<b>hi</b>" {
		t.Fatalf("InnerHTML(tpl) = %q", got)
	}
	if got := OuterHTML(tpl); got != "<template><b>hi</b></template>" {
		t.Fatalf("OuterHTML(tpl) = %q", got)
	}
}

func TestAppendChildDetaches(t *testing.T) {
	p1 := NewElement("div")
	p2 := NewElement("section")
	c := NewElement("span")
	AppendChild(p1, c)
	if c.Parent != p1 || len(p1.Children) != 1 {
		t.Fatal("append to p1 failed")
	}
	// Re-appending to p2 detaches from p1.
	AppendChild(p2, c)
	if c.Parent != p2 || len(p2.Children) != 1 || len(p1.Children) != 0 {
		t.Fatalf("re-append did not detach: p1=%d p2=%d", len(p1.Children), len(p2.Children))
	}
	// No-ops.
	AppendChild(nil, c)
	AppendChild(p1, nil)
	AppendChild(p1, p1)
}

func TestRemoveChild(t *testing.T) {
	p := NewElement("div")
	a, b := NewElement("a"), NewElement("b")
	AppendChild(p, a)
	AppendChild(p, b)
	RemoveChild(p, a)
	if a.Parent != nil || len(p.Children) != 1 || p.Children[0] != b {
		t.Fatal("remove a failed")
	}
	// Removing a non-child and nils are no-ops.
	RemoveChild(p, a)
	RemoveChild(nil, b)
	RemoveChild(p, nil)
}

func TestInsertBefore(t *testing.T) {
	p := NewElement("div")
	a, b, c := NewElement("a"), NewElement("b"), NewElement("c")
	AppendChild(p, a)
	AppendChild(p, b)
	InsertBefore(p, c, b) // a, c, b
	if len(p.Children) != 3 || p.Children[1] != c {
		t.Fatalf("insertBefore order wrong: %v", tags(p))
	}
	// ref nil -> append.
	d := NewElement("d")
	InsertBefore(p, d, nil)
	if p.Children[len(p.Children)-1] != d {
		t.Fatal("insertBefore nil ref should append")
	}
	// ref not a child -> append.
	e := NewElement("e")
	InsertBefore(p, e, NewElement("x"))
	if p.Children[len(p.Children)-1] != e {
		t.Fatal("insertBefore unknown ref should append")
	}
	// Detaches from old parent.
	other := NewElement("o")
	AppendChild(other, a)
	InsertBefore(p, a, d)
	if a.Parent != p || len(other.Children) != 0 {
		t.Fatal("insertBefore did not detach")
	}
	// No-ops.
	InsertBefore(nil, a, nil)
	InsertBefore(p, nil, nil)
	InsertBefore(p, p, nil)
}

// TestAppendChildUnwrapsFragment covers the real shape found live on
// caniuse.com's own "Browser scores" widget: appending a DocumentFragment
// (document.createDocumentFragment(), represented here as a plain Element
// with the synthetic tag "#fragment") must move its OWN children into the
// target directly, per spec, never insert the fragment node itself — a
// site's JS populating a fragment with real content and then appending the
// fragment to a real container ended up with that content nested inside a
// literal, unstyled `<#fragment>` element instead of becoming the
// container's own direct children, breaking any CSS relying on that
// (`>`-combinator rules, a flex container's "only direct children are flex
// items" rule).
func TestAppendChildUnwrapsFragment(t *testing.T) {
	p := NewElement("div")
	frag := NewElement("#fragment")
	a, b := NewElement("a"), NewElement("b")
	AppendChild(frag, a)
	AppendChild(frag, b)

	AppendChild(p, frag)

	if got := tags(p); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("p's children = %v, want [a b] (fragment unwrapped, not inserted itself)", got)
	}
	if a.Parent != p || b.Parent != p {
		t.Error("fragment's own children were not reparented to p")
	}
	if len(frag.Children) != 0 {
		t.Errorf("fragment still holds %d children after being appended, want 0 (emptied per spec)", len(frag.Children))
	}
	if frag.Parent != nil {
		t.Error("the fragment itself must never gain a parent — it is never part of the tree")
	}

	// An existing sibling stays in place, with the fragment's children
	// inserted after it in document order (a plain AppendChild appends).
	p2 := NewElement("div")
	c := NewElement("c")
	AppendChild(p2, c)
	frag2 := NewElement("#fragment")
	AppendChild(frag2, NewElement("d"))
	AppendChild(frag2, NewElement("e"))
	AppendChild(p2, frag2)
	if got := tags(p2); len(got) != 3 || got[0] != "c" || got[1] != "d" || got[2] != "e" {
		t.Fatalf("p2's children = %v, want [c d e]", got)
	}
}

// TestInsertBeforeUnwrapsFragment is InsertBefore's own counterpart of
// TestAppendChildUnwrapsFragment: a fragment's children are inserted in
// order immediately before ref, preserving their relative order.
func TestInsertBeforeUnwrapsFragment(t *testing.T) {
	p := NewElement("div")
	a, z := NewElement("a"), NewElement("z")
	AppendChild(p, a)
	AppendChild(p, z)

	frag := NewElement("#fragment")
	AppendChild(frag, NewElement("b"))
	AppendChild(frag, NewElement("c"))
	InsertBefore(p, frag, z)

	if got := tags(p); len(got) != 4 || got[0] != "a" || got[1] != "b" || got[2] != "c" || got[3] != "z" {
		t.Fatalf("p's children = %v, want [a b c z]", got)
	}
	if len(frag.Children) != 0 || frag.Parent != nil {
		t.Error("the fragment must end up empty and detached, never itself inserted")
	}
}

func TestTextContentAndSet(t *testing.T) {
	root, _ := Parse(`<div>a<span>b<em>c</em></span>d</div>`)
	div := Find(root, "div")
	if got := TextContent(div); got != "abcd" {
		t.Fatalf("TextContent=%q", got)
	}
	SetTextContent(div, "zzz")
	if len(div.Children) != 1 || div.Children[0].Type != Text || div.Children[0].Text != "zzz" {
		t.Fatalf("SetTextContent: %v", div.Children)
	}
	SetTextContent(div, "")
	if len(div.Children) != 0 {
		t.Fatal("SetTextContent empty should clear children")
	}
}

func TestInnerAndOuterHTML(t *testing.T) {
	el := NewElement("div")
	el.Attr["id"] = "x"
	el.Attr["class"] = "y"
	AppendChild(el, NewText("a & b"))
	child := NewElement("br")
	AppendChild(el, child)
	if got := InnerHTML(el); got != "a &amp; b<br>" {
		t.Fatalf("InnerHTML=%q", got)
	}
	// Attribute order is sorted (class before id).
	if got := OuterHTML(el); got != `<div class="y" id="x">a &amp; b<br></div>` {
		t.Fatalf("OuterHTML=%q", got)
	}
}

func TestSetInnerHTML(t *testing.T) {
	el := NewElement("div")
	AppendChild(el, NewText("old"))
	if err := SetInnerHTML(el, `<p class="c">hi</p><span>x</span>`); err != nil {
		t.Fatal(err)
	}
	if len(el.Children) != 2 || el.Children[0].Tag != "p" || el.Children[0].Parent != el {
		t.Fatalf("SetInnerHTML children: %v", tags(el))
	}
	if TextContent(el) != "hix" {
		t.Fatalf("text after SetInnerHTML=%q", TextContent(el))
	}
}

func TestParseFragment(t *testing.T) {
	nodes, err := ParseFragment(`<li>1</li><li>2</li>text`)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 3 {
		t.Fatalf("want 3 fragment nodes, got %d", len(nodes))
	}
	for _, n := range nodes {
		if n.Parent != nil {
			t.Fatal("fragment nodes must be detached")
		}
	}
}

func TestSerializeTextNode(t *testing.T) {
	// A bare text node serializes (and escapes) directly.
	if got := OuterHTML(NewText("<b>")); got != "&lt;b&gt;" {
		t.Fatalf("text serialize=%q", got)
	}
}

// TestNewCommentAndSerialize confirms a comment node round-trips through
// OuterHTML/InnerHTML — needed for React's own streaming-SSR hydration
// markers (`<!--$-->`/`<!--/$-->`) to survive a re-serialize (e.g. via
// innerHTML) the same way they survive the initial parse.
func TestNewCommentAndSerialize(t *testing.T) {
	c := NewComment("$")
	if c.Type != Comment || c.Text != "$" {
		t.Fatalf("NewComment = %+v", c)
	}
	if got := OuterHTML(c); got != "<!--$-->" {
		t.Fatalf("comment OuterHTML=%q", got)
	}
	el := NewElement("div")
	AppendChild(el, NewComment("a"))
	AppendChild(el, NewText("b"))
	if got := InnerHTML(el); got != "<!--a-->b" {
		t.Fatalf("InnerHTML with comment=%q", got)
	}
	// A comment's data contributes nothing to textContent (spec: only Text
	// descendants count for an Element's text content).
	if got := TextContent(el); got != "b" {
		t.Fatalf("TextContent should skip comment data, got %q", got)
	}
}

func tags(n *Node) []string {
	var out []string
	for _, c := range n.Children {
		if c.Type == Element {
			out = append(out, c.Tag)
		} else {
			out = append(out, "#text:"+c.Text)
		}
	}
	return out
}
