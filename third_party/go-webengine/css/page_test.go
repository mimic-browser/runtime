// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"math"
	"reflect"
	"testing"

	"github.com/go-webengine/engine/dom"
)

func TestParsePageRulesSelectors(t *testing.T) {
	src := `
	/* a comment */
	@import url(x.css);
	@page { margin: 1in }
	@page :first { margin-top: 30mm }
	@page:LEFT { margin-left: 2in }
	@page cover { size: A5 }
	@page :left, :right { margin-bottom: 5mm }
	@pages { margin: 9in }
	p { margin: 0 }
	@font-face { font-family: x; src: url(y) }
	@page {
		margin: 15mm;
		@top-center { content: counter(page); font-size: 9pt }
		size: A5;
		@bottom-left { content: "x" }
	}
	`
	got := ParsePageRules(src, Media{})
	want := []PageRule{
		{"", []Declaration{{Property: "margin", Value: "1in"}}},
		{":first", []Declaration{{Property: "margin-top", Value: "30mm"}}},
		{":left", []Declaration{{Property: "margin-left", Value: "2in"}}},
		{"cover", []Declaration{{Property: "size", Value: "A5"}}},
		{":left", []Declaration{{Property: "margin-bottom", Value: "5mm"}}},
		{":right", []Declaration{{Property: "margin-bottom", Value: "5mm"}}},
		{"", []Declaration{{Property: "margin", Value: "15mm"}, {Property: "size", Value: "A5"}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParsePageRules =\n%+v\nwant\n%+v", got, want)
	}
	if got := ParsePageRules("@page { margin: 1in", Media{}); got != nil {
		t.Errorf("unterminated block: %+v, want nil", got)
	}
	if got := ParsePageRules("p", Media{}); got != nil {
		t.Errorf("no block: %+v, want nil", got)
	}
}

// TestParsePageRulesMedia: a @page inside @media applies under exactly the
// media its style rules would; @layer is unwrapped.
func TestParsePageRulesMedia(t *testing.T) {
	src := `
	@media print { @page { size: A4 } }
	@media screen { @page { size: A5 } }
	@layer base { @page :first { margin: 1in } }
	@media (min-width: 500px) { @page { margin: 2in } }
	`
	sels := func(rules []PageRule) []string {
		var out []string
		for _, r := range rules {
			out = append(out, r.Selector+"{"+r.Declarations[0].Property+":"+r.Declarations[0].Value+"}")
		}
		return out
	}
	cases := []struct {
		m    Media
		want []string
	}{
		{Media{Type: Print, Width: 1024}, []string{"{size:A4}", ":first{margin:1in}", "{margin:2in}"}},
		{Media{Width: 1024}, []string{"{size:A5}", ":first{margin:1in}", "{margin:2in}"}},
		{Media{Type: Print, Width: 300}, []string{"{size:A4}", ":first{margin:1in}"}},
	}
	for _, c := range cases {
		if got := sels(ParsePageRules(src, c.m)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v: %v want %v", c.m, got, c.want)
		}
	}
}

func TestStripNestedBlocks(t *testing.T) {
	cases := []struct{ in, want string }{
		{"margin: 1in; @top-center { content: counter(page) } margin-bottom: 2in", "margin: 1in; margin-bottom: 2in"},
		{"@top-center { x } margin-bottom: 2in", " margin-bottom: 2in"},
		{"margin: 1in; @top-center { content", "margin: 1in;"},
		{"margin: 1in", "margin: 1in"},
	}
	for _, c := range cases {
		if got := stripNestedBlocks(c.in); got != c.want {
			t.Errorf("stripNestedBlocks(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

func specNear(a, b PageSpec) bool {
	near := func(x, y float64) bool { return math.Abs(x-y) < 0.01 }
	if !near(a.Width, b.Width) || !near(a.Height, b.Height) || a.MarginSet != b.MarginSet {
		return false
	}
	for i := range a.Margin {
		if !near(a.Margin[i], b.Margin[i]) {
			return false
		}
	}
	return true
}

func TestResolvePage(t *testing.T) {
	const a4w, a4h = 793.7008, 1122.5197
	all := [4]bool{true, true, true, true}
	cases := []struct {
		name string
		css  string
		sel  string
		want PageSpec
	}{
		{"A5 + 15mm", `@page { size: A5; margin: 15mm }`, "",
			PageSpec{559.3701, 793.7008, [4]float64{56.6929, 56.6929, 56.6929, 56.6929}, all}},
		{"two lengths", `@page { size: 100mm 200mm }`, "", PageSpec{Width: 377.9528, Height: 755.9055}},
		{"letter landscape", `@page { size: letter landscape }`, "", PageSpec{Width: 1056, Height: 816}},
		{"landscape letter", `@page { size: landscape letter }`, "", PageSpec{Width: 1056, Height: 816}},
		{"A4 portrait", `@page { size: A4 portrait }`, "", PageSpec{Width: a4w, Height: a4h}},
		{"legal, ledger, JIS", `@page { size: legal } @page { size: jis-b4 } @page { size: LEDGER }`, "",
			PageSpec{Width: 1056, Height: 1632}},
		{"square", `@page { size: 10cm }`, "", PageSpec{Width: 377.9528, Height: 377.9528}},
		{"margin pair", `@page { margin: 1in 2in }`, "", PageSpec{Margin: [4]float64{96, 192, 96, 192}, MarginSet: all}},
		{"margin triple", `@page { margin: 1in 2in 3in }`, "", PageSpec{Margin: [4]float64{96, 192, 288, 192}, MarginSet: all}},
		{"margin quad", `@page { margin: 1in 2in 3in 4in }`, "", PageSpec{Margin: [4]float64{96, 192, 288, 384}, MarginSet: all}},
		{":first overrides only the top", `@page { margin: 1in } @page :first { margin-top: 30mm }`, ":first",
			PageSpec{Margin: [4]float64{113.3858, 96, 96, 96}, MarginSet: all}},
		{"the unqualified page ignores :first", `@page { margin: 1in } @page :first { margin-top: 30mm }`, "",
			PageSpec{Margin: [4]float64{96, 96, 96, 96}, MarginSet: all}},
		{"a selector with no rule inherits the unqualified page", `@page { margin: 1in } @page :first { margin-top: 30mm }`, ":left",
			PageSpec{Margin: [4]float64{96, 96, 96, 96}, MarginSet: all}},
		{"named page", `@page { size: A4 } @page cover { size: A5; margin-left: 1in }`, "cover",
			PageSpec{559.3701, 793.7008, [4]float64{0, 0, 0, 96}, [4]bool{false, false, false, true}}},
		{"pseudo-class case", `@page :First { size: A4 }`, ":first", PageSpec{Width: a4w, Height: a4h}},
		{"orientation alone is auto", `@page { size: A4 } @page :first { size: landscape }`, ":first", PageSpec{}},
		{"auto", `@page { size: A4 } @page { size: auto }`, "", PageSpec{}},
		{"later wins", `@page { size: A5 } @page { size: A4 }`, "", PageSpec{Width: a4w, Height: a4h}},
		{"!important is just a value", `@page { margin: 1in !important }`, "", PageSpec{Margin: [4]float64{96, 96, 96, 96}, MarginSet: all}},
		{"invalid sizes leave the earlier value",
			`@page { size: A4 } @page { size: A4 A5 } @page { size: portrait landscape } @page { size: foo }` +
				`@page { size: 10cm 20cm 30cm } @page { size: 10cm foo } @page { size: auto landscape } @page { size: A4 10cm }` +
				`@page { size: 50% } @page { size: 10cm auto }`,
			"", PageSpec{Width: a4w, Height: a4h}},
		{"invalid margins are ignored",
			`@page { margin: 10% } @page { margin: auto } @page { margin: 1in 2in 3in 4in 5in } @page { margin-top: 10% }` +
				`@page { margin-top: auto } @page { margin-left: 1in 2in }`,
			"", PageSpec{}},
		{"every unit", `@page { margin-top: 72pt; margin-right: 6pc; margin-bottom: 2.54cm; margin-left: 1em }` +
			`@page :first { margin-top: 2rem; margin-right: 40q; margin-bottom: 0; margin-left: 96px }`,
			":first", PageSpec{Margin: [4]float64{32, 37.7953, 0, 96}, MarginSet: all}},
		{"unknown page properties are ignored", `@page { page: cover; bleed: 6pt; marks: crop; color: red }`, "", PageSpec{}},
		{"no rules", ``, ":first", PageSpec{}},
	}
	for _, c := range cases {
		got := ResolvePage(ParsePageRules(c.css, Media{}), c.sel)
		if !specNear(got, c.want) {
			t.Errorf("%s: ResolvePage = %+v want %+v", c.name, got, c.want)
		}
	}
	if _, _, ok := parsePageSize(""); ok {
		t.Error("parsePageSize(\"\") = ok")
	}
}

// TestDocumentPage gathers @page from two <style> elements and one external
// sheet, external first: a later declaration wins, whichever sheet it is in.
func TestDocumentPage(t *testing.T) {
	root, err := dom.Parse(`<html><head>` +
		`<style>@page { margin-top: 2in } @page :first { margin-left: 3in }</style>` +
		`<link rel="stylesheet" href="x.css">` +
		`</head><body>` +
		`<style>@page { size: A5 } @media screen { @page { margin-bottom: 5in } }</style>` +
		`</body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	ext := []string{`@page { size: A4; margin: 1in } @media print { @page { margin-bottom: 0 } }`}
	all := [4]bool{true, true, true, true}
	cases := []struct {
		m    Media
		sel  string
		want PageSpec
	}{
		{Media{}, "", PageSpec{559.3701, 793.7008, [4]float64{192, 96, 480, 96}, all}},
		{Media{}, ":first", PageSpec{559.3701, 793.7008, [4]float64{192, 96, 480, 288}, all}},
		{Media{Type: Print}, "", PageSpec{559.3701, 793.7008, [4]float64{192, 96, 0, 96}, all}},
	}
	for _, c := range cases {
		if got := DocumentPage(root, ext, c.m, c.sel); !specNear(got, c.want) {
			t.Errorf("DocumentPage(%+v, %q) = %+v want %+v", c.m, c.sel, got, c.want)
		}
	}
	if got := DocumentPage(root, nil, Media{}, ""); !specNear(got, PageSpec{559.3701, 793.7008, [4]float64{192, 0, 480, 0}, [4]bool{true, false, true, false}}) {
		t.Errorf("DocumentPage without external sheets = %+v", got)
	}
}
