// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import "testing"

// TestCheckboxHackGolden verifies the FINAL rendered result of the CSS
// checkbox-hack: a menu hidden by default (display:none) that only a `:checked`
// sibling combinator reveals. With no user interaction a toggle is checked iff
// it carries the `checked` attribute, so a default-unchecked toggle keeps its
// menu HIDDEN — exactly the state Chrome paints for MediaWiki's collapsed
// Vector-2022 dropdowns. Solid-colour blocks only → font-independent bytes.
//
// Row layout (body margin 0, blocks stack):
//   A hdr   y  0..20  green  (0,150,0)   ; A menu HIDDEN (unchecked)
//   B hdr   y 20..40  olive  (150,150,0) ; B menu SHOWN  y 40..70 red (220,40,40)
//   C hdr   y 70..90  teal   (0,150,150) ; C inv  HIDDEN (:not(:checked) matches)
func TestCheckboxHackGolden(t *testing.T) {
	img := renderFixture(t, "checkbox_hack.html", 200, 130)

	// Row A: header paints; its red menu is hidden (unchecked toggle).
	assertPixel(t, img, 10, 10, 0, 150, 0, "A header (green)")
	// Directly below A's header is B's header — proving A's menu reserved NO
	// space and did NOT paint (else this would be red at y=30).
	assertPixel(t, img, 10, 30, 150, 150, 0, "B header (olive) — A menu stayed hidden")

	// Row B: the `:checked` toggle reveals its red menu.
	assertPixel(t, img, 10, 55, 220, 40, 40, "B menu shown (:checked reveals)")

	// Row C: header paints directly after B's menu (proving B's menu is exactly
	// 30px tall, ending at y=70), and C's blue .inv is hidden by
	// `.tog:not(:checked) ~ .inv { display:none }` since the toggle is unchecked.
	assertPixel(t, img, 10, 80, 0, 150, 150, "C header (teal)")
	assertPixel(t, img, 10, 100, 255, 255, 255, "C .inv hidden (:not(:checked)) — white below")

	checkGolden(t, img, "checkbox_hack.png")
}
