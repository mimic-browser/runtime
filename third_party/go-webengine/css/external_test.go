// Copyright (c) the go-webengine/engine authors
//
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"reflect"
	"testing"

	"github.com/go-webengine/engine/dom"
)

func mustParse(t *testing.T, src string) *dom.Node {
	t.Helper()
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return root
}

func TestStylesheetLinks(t *testing.T) {
	root := mustParse(t, `<html><head>
		<link rel="stylesheet" href="a.css">
		<link rel="stylesheet" href="  b.css  " media="screen and (min-width: 640px)">
		<link rel="STYLESHEET ALTERNATE" href="alt.css">
		<link rel="stylesheet">
		<link rel="stylesheet" href="">
		<link rel="icon" href="favicon.ico">
		<link rel="preload stylesheet" href="c.css">
	</head><body></body></html>`)
	got := StylesheetLinks(root)
	want := []LinkRef{
		{Href: "a.css", Media: ""},
		{Href: "b.css", Media: "screen and (min-width: 640px)"},
		{Href: "c.css", Media: ""},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("StylesheetLinks =\n  %#v\nwant\n  %#v", got, want)
	}
}

func TestStylesheetLinksNone(t *testing.T) {
	if got := StylesheetLinks(mustParse(t, `<html><body><p>hi</p></body></html>`)); got != nil {
		t.Fatalf("want nil, got %#v", got)
	}
}

func TestMediaApplies(t *testing.T) {
	cases := []struct {
		media string
		vw    float64
		want  bool
	}{
		{"", 1024, true},
		{"all", 1024, true},
		{"screen", 1024, true},
		{"print", 1024, false},
		{"screen and (min-width: 640px)", 1024, true},
		{"screen and (min-width: 640px)", 500, false},
		{"(max-width: 800px)", 500, true},
		{"(max-width: 800px)", 1024, false},
		{"print, screen and (min-width: 200px)", 1024, true}, // second component matches
		{"print, (min-width: 2000px)", 1024, false},          // neither matches
	}
	for _, c := range cases {
		if got := MediaApplies(c.media, c.vw); got != c.want {
			t.Errorf("MediaApplies(%q, %v) = %v, want %v", c.media, c.vw, got, c.want)
		}
	}
}

func TestImportURLs(t *testing.T) {
	cases := []struct {
		sheet      string
		wantURLs   []string
		wantMedias []string
	}{
		{`@import "base.css"; @import url(theme.css) screen and (min-width: 600px);
		  body{color:red}@import "ignored-after-rule.css";`,
			[]string{"base.css", "theme.css"},
			[]string{"", "screen and (min-width: 600px)"}},
		{`@charset "utf-8"; /* c */ @import url('x.css');`,
			[]string{"x.css"}, []string{""}},
		{`  @import url( "spaced.css" ) ;`, []string{"spaced.css"}, []string{""}},
		{`body{color:red}`, nil, nil},
		{`@import ;`, nil, nil},            // no clause
		{`@import url(unterminated`, nil, nil}, // no semicolon
		{`@import url(;`, nil, nil},         // no closing paren before ;
		{`@import badtoken.css;`, nil, nil}, // neither url() nor quoted
		{`@import "";`, nil, nil},           // empty url
		{`/* unterminated comment`, nil, nil},
		{`@charset "utf-8"`, nil, nil}, // charset without semicolon
	}
	for _, c := range cases {
		gotURLs, gotMedias := ImportURLs(c.sheet)
		if !reflect.DeepEqual(gotURLs, c.wantURLs) || !reflect.DeepEqual(gotMedias, c.wantMedias) {
			t.Errorf("ImportURLs(%q) = %#v / %#v, want %#v / %#v",
				c.sheet, gotURLs, gotMedias, c.wantURLs, c.wantMedias)
		}
	}
}

func TestImportURLsSingleQuoteUnterminated(t *testing.T) {
	if u, _ := ImportURLs(`@import 'x.css;`); u != nil {
		t.Fatalf("want nil for unterminated quote, got %#v", u)
	}
}
