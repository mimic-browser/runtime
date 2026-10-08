// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import "testing"

func TestResolveLightDarkPicksDarkBranch(t *testing.T) {
	// This engine has no live appearance signal, so it picks the same fixed
	// answer as window.go's matchMedia('prefers-color-scheme') — dark — to
	// match the bench suite's real (dark-appearance) reference Chromium.
	got, ok := resolveLightDark("light-dark(white, black)")
	if !ok || got != "black" {
		t.Fatalf("resolveLightDark = %q,%v want %q,true", got, ok, "black")
	}
}

func TestResolveLightDarkNoOccurrence(t *testing.T) {
	got, ok := resolveLightDark("1px solid red")
	if !ok || got != "1px solid red" {
		t.Fatalf("resolveLightDark no-op = %q,%v", got, ok)
	}
}

func TestResolveLightDarkMultipleAndNestedCommas(t *testing.T) {
	// Two calls in one value, plain text trailing the last call (exercising the
	// "no further match" loop exit), and a branch value that itself contains a
	// top-level comma (e.g. rgb(...)) which must not be mistaken for the
	// separator.
	got, ok := resolveLightDark("light-dark(rgb(1,2,3), rgb(4,5,6)) light-dark(a, b) !important")
	if !ok || got != "rgb(4,5,6) b !important" {
		t.Fatalf("resolveLightDark multi = %q,%v", got, ok)
	}
}

func TestResolveLightDarkMalformed(t *testing.T) {
	if _, ok := resolveLightDark("light-dark(onlyone)"); ok {
		t.Error("missing second argument should be invalid")
	}
	if _, ok := resolveLightDark("light-dark(a, b"); ok {
		t.Error("unterminated light-dark( should be invalid")
	}
}

func TestResolveLightDarkIdentifierBoundary(t *testing.T) {
	// "light-dark(" preceded by an identifier char is not a real function
	// token (mirrors findVarFunc's own boundary guard).
	if got := findLightDarkFunc("mylight-dark(a,b)", 0); got != -1 {
		t.Errorf("mylight-dark( matched at %d", got)
	}
}

// TestCascadeLightDarkThroughVar covers the real end-to-end shape seen live on
// developer.mozilla.org: a custom property's raw value is itself wrapped in
// light-dark(), reached only after var() substitution unwraps it — the
// light-dark() resolution in resolveDeclValue must run on the FINAL
// substituted text, not just on a literal declaration.
func TestCascadeLightDarkThroughVar(t *testing.T) {
	st := styleOf(t, `<html><body>`+
		`<p style="--bg:light-dark(white,black);background-color:var(--bg)">x</p>`+
		`</body></html>`, "p")
	if st.Background != (Color{0, 0, 0, 255}) {
		t.Errorf("background = %v, want black (dark branch)", st.Background)
	}
}
