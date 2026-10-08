// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"testing"

	"github.com/go-webengine/engine/dom"
)

func findElementByID(n *dom.Node, id string) *dom.Node {
	if n.Type == dom.Element && n.ID() == id {
		return n
	}
	for _, child := range n.Children {
		if found := findElementByID(child, id); found != nil {
			return found
		}
	}
	return nil
}

func TestGeneratedTextUsesPseudoCascadeAndDocumentOrder(t *testing.T) {
	root, err := dom.Parse(`<html><head><style>
		span::before{content:"wrong"}
		.label::before{content:"[" / "";font-size:small;margin-right:4px}
		.label::after{content:"]"}
		.label::after{content:none!important}
	</style></head><body><span id="target" class="label">edit</span></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	target := findElementByID(root, "target")
	sm := Cascade(root)
	if len(target.Children) != 1 {
		t.Fatal("pseudo content entered the ordinary cascade")
	}
	AddGeneratedText(root, sm, nil, Media{Width: 800})
	if len(target.Children) != 2 || target.Children[0].Children[0].Text != "[" || target.Children[1].Text != "edit" {
		t.Fatalf("generated child order: %+v", target.Children)
	}
	if got := sm[target.Children[0]].FontSize; got < 13 || got > 14 {
		t.Fatalf("generated font size = %g", got)
	}
	if got := sm[target.Children[0]].Margin.Right; got != 4 {
		t.Fatalf("generated margin-right = %g", got)
	}
}

func TestGeneratedTextDoesNotChangeSelectorMatching(t *testing.T) {
	root, err := dom.Parse(`<html><head><style>
		div::before{content:"("}
		div > span:first-child::before{content:"["}
		span::after{content:"]"}
	</style></head><body><div id="outer"><span id="inner">x</span></div></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	sm := Cascade(root)
	AddGeneratedText(root, sm, nil, Media{Width: 800})
	outer, inner := findElementByID(root, "outer"), findElementByID(root, "inner")
	if got := outer.Children[0].Children[0].Text; got != "(" {
		t.Fatalf("outer ::before = %q", got)
	}
	if got := inner.Children[0].Children[0].Text; got != "[" {
		t.Fatalf("first-child ::before = %q", got)
	}
	if got := inner.Children[len(inner.Children)-1].Children[0].Text; got != "]" {
		t.Fatalf("inner ::after = %q", got)
	}
}

func TestLiteralGeneratedContent(t *testing.T) {
	for _, tc := range []struct {
		input, want string
		ok          bool
	}{
		{`'[' / ''`, "[", true},
		{`"a" 'b'`, "ab", true},
		{`'\5b '`, "[", true},
		{`attr(title)`, "", false},
		{`none`, "", false},
	} {
		got, ok := literalGeneratedContent(tc.input)
		if got != tc.want || ok != tc.ok {
			t.Errorf("content %q = (%q, %v), want (%q, %v)", tc.input, got, ok, tc.want, tc.ok)
		}
	}
}
