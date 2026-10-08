// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import "testing"

// TestModernCSSDemoGolden verifies, with exact painted-pixel invariants, the
// modern-CSS features on one offline fixture: the space-separated rgb()/hsl()
// colour syntax, border-radius (rounded fills + rounded uniform border), and
// Tailwind-style dark mode (an :is(.dark …) rule + an escaped-colon `dark:`
// variant class on a <html class="dark"> document). The fixture is solid-colour
// blocks only, so the bytes are font-independent and byte-golden stable.
func TestModernCSSDemoGolden(t *testing.T) {
	img := renderFixture(t, "modern_css_demo.html", 400, 300)

	// The dark page backdrop proves the whole chain works end to end: the JS is
	// disabled for fixtures, but <html class="dark"> is already in the markup, so
	// the escaped-colon `dark:bg-wash-dark` class on <body> must match the
	// :is(.dark .dark\:bg-wash-dark) rule, whose modern rgb(35 39 47 / 1) value
	// must parse. A single broken link in that chain leaves this pixel white.
	assertPixel(t, img, 10, 10, 0x23, 0x27, 0x2f, "dark backdrop")
	assertPixel(t, img, 390, 290, 0x23, 0x27, 0x2f, "dark backdrop far corner")

	// The rounded card: its centre is the teal fill; its extreme top-left corner
	// (40,40) is rounded away, so the dark backdrop shows through there.
	assertPixel(t, img, 200, 100, 0x08, 0x7e, 0xa4, "card centre (modern rgb)")
	assertPixel(t, img, 40, 40, 0x23, 0x27, 0x2f, "card corner rounded away")
	// A point well inside the straight top edge is filled.
	assertPixel(t, img, 200, 40, 0x08, 0x7e, 0xa4, "card top edge filled")

	// The pill (hsl green) fills its centre; its extreme corner is rounded away.
	assertPixel(t, img, 140, 224, 0x2e, 0xb8, 0x5c, "pill centre (hsl)")
	assertPixel(t, img, 40, 200, 0x23, 0x27, 0x2f, "pill corner rounded away")

	// The bordered rounded box: a mid top-edge pixel carries the orange border
	// ink; its extreme corner is rounded away (backdrop shows through).
	assertPixel(t, img, 315, 200, 0xe6, 0x78, 0x28, "outline top border ink")
	assertPixel(t, img, 270, 200, 0x23, 0x27, 0x2f, "outline corner rounded away")

	checkGolden(t, img, "modern_css_demo.png")
}
