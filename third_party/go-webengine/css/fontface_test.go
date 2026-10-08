// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import "testing"

// The shape Google Fonts actually serves — the case that matters, since a
// <link> to fonts.googleapis.com is how most pages ask for a typeface. Served
// without a browser User-Agent it gives TrueType, which is what this engine
// can decode.
const googleFontsSheet = `/* latin */
@font-face {
  font-family: 'IBM Plex Sans';
  font-style: normal;
  font-weight: 400;
  font-stretch: normal;
  font-display: swap;
  src: url(https://fonts.gstatic.com/s/ibmplexsans/v23/zYXGKVElMYYaJe8b.ttf) format('truetype');
}
@font-face {
  font-family: 'IBM Plex Sans';
  font-style: italic;
  font-weight: 600;
  src: url(https://fonts.gstatic.com/s/ibmplexsans/v23/italic600.ttf) format('truetype');
}
@font-face {
  font-family: 'Spectral';
  font-style: normal;
  font-weight: bold;
  src: url(https://fonts.gstatic.com/s/spectral/v13/bold.ttf) format('truetype');
}`

func TestParseFontFacesGoogleFontsSheet(t *testing.T) {
	faces := ParseFontFaces(googleFontsSheet, Media{Width: 1024})
	if len(faces) != 3 {
		t.Fatalf("got %d faces, want 3: %+v", len(faces), faces)
	}
	want := []FontFace{
		{Family: "ibm plex sans", Weight: 400, Italic: false},
		{Family: "ibm plex sans", Weight: 600, Italic: true},
		{Family: "spectral", Weight: 700, Italic: false},
	}
	for i, w := range want {
		g := faces[i]
		if g.Family != w.Family || g.Weight != w.Weight || g.Italic != w.Italic {
			t.Errorf("face %d = {%q %d %v}, want {%q %d %v}", i, g.Family, g.Weight, g.Italic, w.Family, w.Weight, w.Italic)
		}
		if len(g.Srcs) != 1 || g.Srcs[0].Format != "truetype" {
			t.Errorf("face %d srcs = %+v", i, g.Srcs)
		}
	}
	if faces[0].Srcs[0].URL != "https://fonts.gstatic.com/s/ibmplexsans/v23/zYXGKVElMYYaJe8b.ttf" {
		t.Errorf("url = %q", faces[0].Srcs[0].URL)
	}
}

// src entries keep their order and their format hints, a local() entry is
// dropped for want of a system font database, and a hint's suffix is cut down
// to the base format that decides decodability.
func TestParseFontFacesSrcList(t *testing.T) {
	src := `@font-face {
	  font-family: Custom;
	  src: local('Custom Regular'),
	       url("a.woff2") format("woff2-variations"),
	       url(b.woff) format('woff'),
	       url(c.ttf);
	}`
	faces := ParseFontFaces(src, Media{})
	if len(faces) != 1 {
		t.Fatalf("got %d faces", len(faces))
	}
	got := faces[0].Srcs
	want := []FontSrc{{"a.woff2", "woff2"}, {"b.woff", "woff"}, {"c.ttf", ""}}
	if len(got) != len(want) {
		t.Fatalf("srcs = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("src %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if faces[0].Weight != 400 || faces[0].Italic {
		t.Errorf("a rule naming no weight or style is 400 upright, got %d italic=%v", faces[0].Weight, faces[0].Italic)
	}
}

// A rule with nothing to load, or no family to load it for, is not a face.
func TestParseFontFacesDropsUnusable(t *testing.T) {
	for _, src := range []string{
		`@font-face { font-family: Custom; }`,                  // no src
		`@font-face { src: url(a.ttf) format('truetype'); }`,   // no family
		`@font-face { font-family: Custom; src: local('C'); }`, // nothing fetchable
		`@font-face { font-family: Custom; src: url(); }`,      // empty url
		`@font-face { font-family: Custom; src: url(a.ttf`,     // unterminated block
	} {
		if faces := ParseFontFaces(src, Media{}); len(faces) != 0 {
			t.Errorf("%q gave %+v, want none", src, faces)
		}
	}
}

// A variable font's weight RANGE collapses to its first value: only a static
// instance can be loaded, so that is what the face claims to be.
func TestParseFontFacesWeightForms(t *testing.T) {
	for _, c := range []struct {
		decl string
		want int
	}{
		{"font-weight: 100 900;", 100},
		{"font-weight: normal;", 400},
		{"font-weight: bold;", 700},
		{"font-weight: 550;", 550},
		{"font-weight: lighter;", 400}, // not a number or a keyword we read: the default stands
		{"", 400},
	} {
		src := `@font-face { font-family: C; ` + c.decl + ` src: url(a.ttf); }`
		faces := ParseFontFaces(src, Media{})
		if len(faces) != 1 {
			t.Fatalf("%q gave %d faces", c.decl, len(faces))
		}
		if faces[0].Weight != c.want {
			t.Errorf("%q gave weight %d, want %d", c.decl, faces[0].Weight, c.want)
		}
	}
}

// Nesting: @media with a matching condition is entered and a non-matching one
// is not; @layer and a holding @supports are entered; a comment cannot hide a
// rule from this pass.
func TestParseFontFacesNesting(t *testing.T) {
	src := `
	@media print { @font-face { font-family: P; src: url(p.ttf); } }
	@media (min-width: 5000px) { @font-face { font-family: Wide; src: url(w.ttf); } }
	@layer base { @font-face { font-family: L; src: url(l.ttf); } }
	@supports (color: light-dark(red, blue)) { @font-face { font-family: S; src: url(s.ttf); } }
	@supports (display: subgrid) { @font-face { font-family: Unheld; src: url(u.ttf); } }
	/* @font-face { font-family: Commented; src: url(x.ttf); } */`
	faces := ParseFontFaces(src, Media{Type: Print, Width: 1024})
	var got []string
	for _, f := range faces {
		got = append(got, f.Family)
	}
	// "unheld" is absent on purpose: an @supports condition this engine does
	// not recognise drops its block wholesale, exactly as it does for rules.
	if len(got) != 3 || got[0] != "p" || got[1] != "l" || got[2] != "s" {
		t.Errorf("families = %q, want p, l, s", got)
	}
}

// The malformed and edge forms, each of which reaches a guard the well-formed
// sheets above never do.
func TestParseFontFacesMalformedForms(t *testing.T) {
	// A bare at-rule statement ending in ';' rides along in the prelude of
	// whatever block follows, so only the text after it names that block.
	if faces := ParseFontFaces(`@import url(other.css); @font-face { font-family: C; src: url(a.ttf); }`, Media{}); len(faces) != 1 {
		t.Errorf("an @import before the rule hid it: %+v", faces)
	}
	// An unterminated url() or format() yields no entry and no hint.
	if faces := ParseFontFaces(`@font-face { font-family: C; src: url(a.ttf; }`, Media{}); len(faces) != 0 {
		t.Errorf("an unterminated url() must not make a face: %+v", faces)
	}
	faces := ParseFontFaces(`@font-face { font-family: C; src: url(a.ttf) format('truetype; }`, Media{})
	if len(faces) != 1 || faces[0].Srcs[0].Format != "" {
		t.Errorf("an unterminated format() must leave the hint empty: %+v", faces)
	}
	// A weight past any real one is not a weight.
	over := ParseFontFaces(`@font-face { font-family: C; font-weight: 99999; src: url(a.ttf); }`, Media{})
	if len(over) != 1 || over[0].Weight != 400 {
		t.Errorf("an out-of-range weight should leave the default: %+v", over)
	}
	// And an empty value leaves it too.
	empty := ParseFontFaces(`@font-face { font-family: C; font-weight:   ; src: url(a.ttf); }`, Media{})
	if len(empty) != 1 || empty[0].Weight != 400 {
		t.Errorf("an empty weight should leave the default: %+v", empty)
	}
}

// atoiPositive's own rejections, which the sheet forms above cannot all reach:
// it is the guard that keeps a weight a number.
func TestAtoiPositive(t *testing.T) {
	for _, c := range []struct {
		in string
		n  int
		ok bool
	}{
		{"400", 400, true},
		{"1000", 1000, true},
		{"1001", 0, false},
		{"", 0, false},
		{"4x0", 0, false},
		{"-400", 0, false},
	} {
		if n, ok := atoiPositive(c.in); n != c.n || ok != c.ok {
			t.Errorf("atoiPositive(%q) = %d,%v want %d,%v", c.in, n, ok, c.n, c.ok)
		}
	}
}

// parseFontWeightKeyword with nothing to read — ParseDeclarations drops a
// declaration whose value is only whitespace, so no sheet form reaches this
// guard, and it is the one that keeps fields[0] safe to index.
func TestParseFontWeightKeywordEmpty(t *testing.T) {
	if _, ok := parseFontWeightKeyword(nil); ok {
		t.Error("no fields must not yield a weight")
	}
}

// An empty entry in a family list is skipped rather than named.
func TestParseFontFamilySkipsEmptyEntries(t *testing.T) {
	if got := parseFontFamily(`Spectral, , serif`).Names; got != "spectral" {
		t.Errorf("Names = %q, want %q", got, "spectral")
	}
	if got := parseFontFamily(`, ,`).Names; got != "" {
		t.Errorf("Names = %q, want empty", got)
	}
}
