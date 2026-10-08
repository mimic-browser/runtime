// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import "testing"

func TestParseColor(t *testing.T) {
	cases := []struct {
		in   string
		want Color
		ok   bool
	}{
		{"red", Color{255, 0, 0, 255}, true},
		{"  White ", Color{255, 255, 255, 255}, true},
		{"transparent", Transparent, true},
		{"#0f0", Color{0, 255, 0, 255}, true},
		{"#0000ee", Color{0, 0, 238, 255}, true},
		{"rgb(1,2,3)", Color{1, 2, 3, 255}, true},
		{"rgba(255, 0, 0, 0.5)", Color{255, 0, 0, 128}, true},
		{"rgb(300, -5, 10)", Color{255, 0, 10, 255}, true},
		{"#12", Color{}, false},
		{"#gggggg", Color{}, false},
		{"#gg0", Color{}, false},
		{"rgb(1,2)", Color{}, false},
		{"rgb(a,b,c)", Color{}, false},
		{"rgb(1,2,x)", Color{}, false},
		{"rgba(1,2,3,z)", Color{}, false},
		{"rgb 1 2 3", Color{}, false},
		{"nosuchcolor", Color{}, false},
	}
	for _, c := range cases {
		got, ok := parseColor(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("parseColor(%q) = %v,%v want %v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestParseLength(t *testing.T) {
	cases := []struct {
		in    string
		emRef float64
		want  Length
		ok    bool
	}{
		{"auto", 16, Length{Auto: true}, true},
		{"0", 16, Length{Px: 0}, true},
		{"12px", 16, Length{Px: 12}, true},
		{"2em", 16, Length{Px: 32}, true},
		{"50%", 16, Length{Percent: 0.5, IsPercent: true}, true},
		{"10", 16, Length{}, false},
		{"xpx", 16, Length{}, false},
		{"xem", 16, Length{}, false},
		{"x%", 16, Length{}, false},
	}
	for _, c := range cases {
		got, ok := parseLength(c.in, c.emRef)
		if ok != c.ok || got != c.want {
			t.Errorf("parseLength(%q,%v) = %v,%v want %v,%v", c.in, c.emRef, got, ok, c.want, c.ok)
		}
	}
}

func TestLengthResolve(t *testing.T) {
	if got := (Length{Px: 20}).Resolve(100); got != 20 {
		t.Errorf("px resolve = %v", got)
	}
	if got := (Length{Percent: 0.25, IsPercent: true}).Resolve(400); got != 100 {
		t.Errorf("percent resolve = %v", got)
	}
}

func TestParseEdges(t *testing.T) {
	cases := []struct {
		in   string
		want Edges
		ok   bool
	}{
		{"5px", Edges{5, 5, 5, 5}, true},
		{"1px 2px", Edges{1, 2, 1, 2}, true},
		{"1px 2px 3px", Edges{1, 2, 3, 2}, true},
		{"1px 2px 3px 4px", Edges{1, 2, 3, 4}, true},
		{"auto 4px", Edges{0, 4, 0, 4}, true},   // auto collapses to 0
		{"5% 4px", Edges{0, 4, 0, 4}, true},     // percent collapses to 0
		{"1px 2px 3px 4px 5px", Edges{}, false}, // too many
		{"nope", Edges{}, false},
	}
	for _, c := range cases {
		got, ok := parseEdges(c.in, 16)
		if ok != c.ok || got != c.want {
			t.Errorf("parseEdges(%q) = %v,%v want %v,%v", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestParseFontFamily(t *testing.T) {
	cases := map[string]FontFamily{
		"monospace":                    Mono,
		`"Courier New", monospace`:     Mono,
		"serif":                        Serif,
		"Georgia, 'Times New Roman'":   Serif,
		"sans-serif":                   Sans,
		"Arial, Helvetica, sans-serif": Sans,
		"Inter, system-ui, sans-serif": Sans,
		"cursive":                      Sans, // unknown generic → Sans fallback
	}
	for in, want := range cases {
		// The BUCKET is what this case table is about, and it is unchanged by
		// named families: a page resolves to the same generic it always did.
		if got := parseFontFamily(in); got.Generic != want.Generic {
			t.Errorf("parseFontFamily(%q) bucket = %v want %v", in, got.Generic, want.Generic)
		}
	}
}

// The names a declaration asks for are kept, in order and lowercased, with the
// generic keywords left out — that list is what an @font-face rule is matched
// against. An alias the bucket heuristic recognises ("helvetica") stays in it:
// it names a face this engine has not got, and a page could @font-face it.
func TestParseFontFamilyKeepsTheNames(t *testing.T) {
	cases := map[string]string{
		`'IBM Plex Sans', 'Helvetica Neue', Helvetica, Arial, sans-serif`: "ibm plex sans,helvetica neue,helvetica,arial",
		`Spectral, 'Iowan Old Style', Palatino, Georgia, serif`:          "spectral,iowan old style,palatino,georgia",
		`"IBM Plex Mono", Menlo, monospace`:                              "ibm plex mono,menlo",
		"sans-serif":                                                     "",
		"serif, sans-serif":                                              "",
		`  Spectral  `:                                                   "spectral",
	}
	for in, want := range cases {
		if got := parseFontFamily(in); got.Names != want {
			t.Errorf("parseFontFamily(%q).Names = %q want %q", in, got.Names, want)
		}
	}
	// NamedFamilies splits that list back out, and gives nothing for a
	// generic-only declaration rather than one empty name.
	if got := parseFontFamily(`Spectral, Georgia, serif`).NamedFamilies(); len(got) != 2 || got[0] != "spectral" || got[1] != "georgia" {
		t.Errorf("NamedFamilies = %q", got)
	}
	if got := parseFontFamily("serif").NamedFamilies(); got != nil {
		t.Errorf("NamedFamilies on a generic-only declaration = %q, want nil", got)
	}
}

func TestStyleBoldAndFirstToken(t *testing.T) {
	if (&Style{FontWeight: 700}).Bold() != true {
		t.Error("700 should be bold")
	}
	if (&Style{FontWeight: 400}).Bold() != false {
		t.Error("400 should not be bold")
	}
	if firstToken("  red 1px ") != "red" {
		t.Error("firstToken")
	}
	if firstToken("   ") != "" {
		t.Error("firstToken empty")
	}
}

func TestAtoiClamp(t *testing.T) {
	if n, ok := atoiClamp("700"); !ok || n != 700 {
		t.Errorf("atoiClamp 700 = %v,%v", n, ok)
	}
	if _, ok := atoiClamp("7a"); ok {
		t.Error("7a should fail")
	}
	if _, ok := atoiClamp(""); ok {
		t.Error("empty should fail")
	}
}

func TestParseLengthViewportAndRem(t *testing.T) {
	cases := []struct {
		in        string
		wantPx    float64
		wantPct   float64
		isPercent bool
	}{
		{"60vw", 0, 0.6, true},
		{"15vh", 0, 0.15, true},
		{"2rem", 32, 0, false},
		{"1.5em", 24, 0, false}, // em unaffected by rem case (emRef 16)
	}
	for _, c := range cases {
		l, ok := parseLength(c.in, 16)
		if !ok {
			t.Errorf("parseLength(%q) failed", c.in)
			continue
		}
		if c.isPercent {
			if !l.IsPercent || l.Percent != c.wantPct {
				t.Errorf("%q = %+v want pct %v", c.in, l, c.wantPct)
			}
		} else if l.Px != c.wantPx {
			t.Errorf("%q = %+v want px %v", c.in, l, c.wantPx)
		}
	}
	if _, ok := parseLength("xvw", 16); ok {
		t.Error("xvw should fail")
	}
	if _, ok := parseLength("xrem", 16); ok {
		t.Error("xrem should fail")
	}
}

func TestLineHeightResolve(t *testing.T) {
	cases := []struct {
		name     string
		lh       LineHeight
		fontSize float64
		wantPx   float64
		wantOK   bool
	}{
		{"normal", LineHeight{Normal: true}, 16, 0, false},
		{"factor", LineHeight{Factor: 1.5}, 20, 30, true},
		{"factor larger font", LineHeight{Factor: 1.5}, 32, 48, true},
		{"factor with zero font-size", LineHeight{Factor: 1.5}, 0, 0, false},
		{"fixed px", LineHeight{Px: 24}, 16, 24, true},
		{"zero px is normal-ish", LineHeight{Px: 0}, 16, 0, false},
		{"negative px rejected", LineHeight{Px: -5}, 16, 0, false},
	}
	for _, c := range cases {
		px, ok := c.lh.Resolve(c.fontSize)
		if px != c.wantPx || ok != c.wantOK {
			t.Errorf("%s: Resolve(%v) = %v,%v want %v,%v",
				c.name, c.fontSize, px, ok, c.wantPx, c.wantOK)
		}
	}
}

// TestParseClipRect covers the legacy `clip: rect(...)` property — found
// load-bearing live on pkg.go.dev, whose own "skip to main content" link
// uses clip:rect(0 0 0 0) (the CSS2-era screen-reader-only idiom) rather than
// the modern width:1px/height:1px/overflow:hidden pattern this engine
// already handled. Both the space-separated form used there and CSS2's
// originally-required comma-separated form must parse to the same rect.
func TestParseClipRect(t *testing.T) {
	want := Edges{Top: 0, Right: 0, Bottom: 0, Left: 0}
	if r, ok := parseClipRect("rect(0 0 0 0)", 16); !ok || r != want {
		t.Errorf("rect(0 0 0 0) = %v,%v want %v,true", r, ok, want)
	}
	if r, ok := parseClipRect("rect(0, 0, 0, 0)", 16); !ok || r != want {
		t.Errorf("rect(0, 0, 0, 0) = %v,%v want %v,true", r, ok, want)
	}
	if r, ok := parseClipRect("rect(1px, 2px, 3px, 4px)", 16); !ok ||
		r != (Edges{Top: 1, Right: 2, Bottom: 3, Left: 4}) {
		t.Errorf("rect(1px,2px,3px,4px) = %v,%v", r, ok)
	}
	// A bare "auto" edge (a real, spec-legal value this simplified parser
	// does not resolve) must not be guessed at — the whole value is left
	// unrecognised, same as any other clip value outside the common case.
	if _, ok := parseClipRect("rect(auto, 0, 0, 0)", 16); ok {
		t.Error("rect() with an auto edge should not parse")
	}
	if _, ok := parseClipRect("rect(0, 0, 0)", 16); ok {
		t.Error("rect() with the wrong number of edges should not parse")
	}
	if _, ok := parseClipRect("auto", 16); ok {
		t.Error(`clip: auto (the initial value, no rect() at all) should not parse`)
	}
}
