// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import "testing"

// TestUADeclarationsAllBranches exercises every arm of the user-agent
// stylesheet switch so its defaults are all covered and sane.
func TestUADeclarationsAllBranches(t *testing.T) {
	blockish := []string{"html", "body", "div", "section", "article", "header",
		"footer", "nav", "main", "aside", "figure", "form", "hr",
		"blockquote", "pre", "address", "fieldset", "figcaption", "p",
		"h1", "h2", "h3", "h4", "h5", "h6", "ul", "ol", "dd", "dt", "dl",
		"caption"}
	for _, tag := range blockish {
		decls := uaDeclarations(tag, false)
		if !hasDisplay(decls, "block") {
			t.Errorf("%s: expected display:block, got %v", tag, decls)
		}
	}
	// Table-family default displays.
	tableDisplays := map[string]string{
		"table": "table", "tr": "table-row", "td": "table-cell",
		"th": "table-cell", "thead": "table-row-group", "tbody": "table-row-group",
		"tfoot": "table-row-group",
	}
	for tag, want := range tableDisplays {
		if !hasDisplay(uaDeclarations(tag, false), want) {
			t.Errorf("%s: expected display:%s", tag, want)
		}
	}
	if !hasDecl(uaDeclarations("th", false), "font-weight", "bold") {
		t.Error("th should be bold")
	}
	if !hasDecl(uaDeclarations("td", false), "padding", "1px") {
		t.Error("td padding")
	}
	if !hasDecl(uaDeclarations("dd", false), "margin-left", "40px") {
		t.Error("dd margin-left")
	}
	if !hasDecl(uaDeclarations("figure", false), "margin", "16px 40px") {
		t.Error("figure margin")
	}
	inlineish := []string{"span", "label", "abbr", "sup", "sub", "mark", "u", "s",
		"del", "ins", "time", "q", "img", "button", "input", "select", "textarea"}
	for _, tag := range inlineish {
		if !hasDisplay(uaDeclarations(tag, false), "inline") {
			t.Errorf("%s: expected display:inline", tag)
		}
	}
	hidden := []string{"head", "title", "style", "script", "meta", "link", "base", "noscript", "template", "option"}
	for _, tag := range hidden {
		if !hasDisplay(uaDeclarations(tag, false), "none") {
			t.Errorf("%s: expected display:none", tag)
		}
	}
	// Text-styling tags.
	if !hasDecl(uaDeclarations("a", false), "color", "#0000ee") {
		t.Error("a: expected link colour")
	}
	for _, tag := range []string{"strong", "b"} {
		if !hasDecl(uaDeclarations(tag, false), "font-weight", "bold") {
			t.Errorf("%s: expected bold", tag)
		}
	}
	for _, tag := range []string{"code", "kbd", "samp", "tt"} {
		if !hasDecl(uaDeclarations(tag, false), "font-family", "monospace") {
			t.Errorf("%s: expected monospace", tag)
		}
	}
	if !hasDecl(uaDeclarations("small", false), "font-size", "13px") {
		t.Error("small font-size")
	}
	// Italic tags default to font-style:italic.
	for _, tag := range []string{"em", "i", "cite", "var", "dfn"} {
		if !hasDecl(uaDeclarations(tag, false), "font-style", "italic") {
			t.Errorf("%s: expected font-style:italic", tag)
		}
	}
	// Unknown tags have no default declarations.
	if uaDeclarations("unknowntag", false) != nil {
		t.Error("unknowntag: expected nil declarations")
	}
	// Special block extras.
	if !hasDecl(uaDeclarations("body", false), "margin", "8px") {
		t.Error("body margin")
	}
	if !hasDecl(uaDeclarations("blockquote", false), "margin", "16px 40px") {
		t.Error("blockquote margin")
	}
	if !hasDecl(uaDeclarations("pre", false), "white-space", "pre") {
		t.Error("pre white-space")
	}
	if !hasDecl(uaDeclarations("ul", false), "padding-left", "40px") {
		t.Error("ul padding")
	}
	// List defaults: <li> is a list-item; <ul> discs, <ol> decimals.
	if !hasDisplay(uaDeclarations("li", false), "list-item") {
		t.Error("li: expected display:list-item")
	}
	if !hasDecl(uaDeclarations("ul", false), "list-style-type", "disc") {
		t.Error("ul: expected list-style-type:disc")
	}
	if !hasDecl(uaDeclarations("ol", false), "list-style-type", "decimal") {
		t.Error("ol: expected list-style-type:decimal")
	}
	if !hasDecl(uaDeclarations("ol", false), "padding-left", "40px") {
		t.Error("ol padding")
	}
}

func hasDisplay(decls []Declaration, v string) bool {
	return hasDecl(decls, "display", v)
}

func hasDecl(decls []Declaration, prop, val string) bool {
	for _, d := range decls {
		if d.Property == prop && d.Value == val {
			return true
		}
	}
	return false
}

func TestHexUppercase(t *testing.T) {
	if c, ok := parseColor("#ABCDEF"); !ok || c != (Color{0xAB, 0xCD, 0xEF, 255}) {
		t.Errorf("uppercase hex = %v %v", c, ok)
	}
}
