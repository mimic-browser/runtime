// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"testing"

	"github.com/go-webengine/engine/dom"
)

// TestMediaMatchesOnTypes pins the media-type half of a query: print vs
// screen, "all", "only", "not", a comma list, and a type this engine never
// renders for — each against both media, at a width that keeps every width
// feature true so only the type decides.
func TestMediaMatchesOnTypes(t *testing.T) {
	screen := Media{Width: 1024}
	print := Media{Type: Print, Width: 1024}
	cases := []struct {
		cond           string
		screen, print_ bool
	}{
		{"", true, true},
		{"all", true, true},
		{"screen", true, false},
		{"print", false, true},
		{"PRINT", false, true},
		{"only screen", true, false},
		{"only print", false, true},
		{"not screen", false, true},
		{"not print", true, false},
		{"not all", false, false},
		{"screen, print", true, true},
		{"speech", false, false},
		{"tv, print", false, true},
		{"screen and (min-width: 600px)", true, false},
		{"print and (min-width: 600px)", false, true},
		{"(min-width: 600px)", true, true},
		{"not all and (min-width: 600px)", false, false},
		{"not all and (min-width: 2000px)", true, true},
		{"screen and (min-width: 2000px), print", false, true},
		// a comma inside a feature value is not a list separator
		{"(min-width: min(600px, 700px))", true, true},
	}
	for _, c := range cases {
		if got := mediaMatchesOn(c.cond, screen); got != c.screen {
			t.Errorf("mediaMatchesOn(%q, screen) = %v, want %v", c.cond, got, c.screen)
		}
		if got := mediaMatchesOn(c.cond, print); got != c.print_ {
			t.Errorf("mediaMatchesOn(%q, print) = %v, want %v", c.cond, got, c.print_)
		}
	}
	// Width features still bind under print.
	if mediaMatchesOn("print and (min-width: 2000px)", print) {
		t.Error("a min-width the print viewport does not reach must not match")
	}
	// Case-insensitive type field, with stray space.
	if !(Media{Type: " Print ", Width: 1}).isPrint() || (Media{Type: "screen"}).isPrint() || (Media{}).isPrint() {
		t.Error("isPrint: case/space-insensitive print, and everything else screen")
	}
}

// TestMediaAppliesToLinkMedia is the <link media> / @import face of the same
// evaluation.
func TestMediaAppliesToLinkMedia(t *testing.T) {
	print := Media{Type: Print, Width: 800}
	if !MediaAppliesTo("print", print) || MediaAppliesTo("print", Media{Width: 800}) {
		t.Error(`<link media="print"> applies under print only`)
	}
	if MediaAppliesTo("screen", print) || !MediaAppliesTo("screen", Media{Width: 800}) {
		t.Error(`<link media="screen"> applies under screen only`)
	}
	if !MediaAppliesTo("", print) || !MediaAppliesTo("all", print) {
		t.Error("an absent or all media applies everywhere")
	}
	// The pre-Media entry point is screen.
	if MediaApplies("print", 800) || !MediaApplies("screen", 800) {
		t.Error("MediaApplies evaluates for screen")
	}
}

// TestCascadeMediaPrint: CascadeMedia is the Media-typed cascade entry —
// "@media print" rules apply under Print and "@media screen" ones do not.
func TestCascadeMediaPrint(t *testing.T) {
	root, err := dom.Parse(`<html><head><style>` +
		`@media print { p { color: red } } @media screen { p { color: blue } }` +
		`</style></head><body><p>x</p></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	p := dom.Find(root, "p")
	if got := CascadeMedia(root, Media{Type: Print, Width: 1024}, nil)[p].Color; got != (Color{255, 0, 0, 255}) {
		t.Errorf("print colour = %+v, want red", got)
	}
	if got := CascadeMedia(root, Media{Width: 1024}, nil)[p].Color; got != (Color{0, 0, 255, 255}) {
		t.Errorf("screen colour = %+v, want blue", got)
	}
}
