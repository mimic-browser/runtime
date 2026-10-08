// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

// This file adds a persistent counterpart to the one-shot Render family: a
// LiveDocument keeps a page's JS runtime alive across separate, later calls
// instead of tearing it down at the end of one render. It exists for a host
// that synthesizes real user interaction (focus a field, type a character,
// click a button) spread out over time — a plain Render per interaction
// would discard the JS runtime's own state (anything a script holds that
// isn't reflected back into the DOM/attributes, e.g. a controlled input's
// component state) between each one, silently diverging from what a real
// browser would do. See dynamic.go's settle, whose Session this reuses
// without re-running the page's initial scripts.
package engine

import (
	"context"
	"errors"
	"image"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/js"
	"github.com/go-webengine/engine/layout"
	"github.com/go-webengine/engine/paint"
)

// ErrClosed is returned by a LiveDocument method called after Close.
var ErrClosed = errors.New("engine: LiveDocument is closed")

// LiveDocument is a page whose JS session stays open across separate calls.
// Not safe for concurrent use — like js.Session, one goroutine drives one
// LiveDocument at a time. Callers interact through Interact (this file) and
// higher-level methods added by later phases (hit-testing, synthetic
// focus/type/click); Close releases the session and heap watchdog and must
// be called exactly once, whether by an explicit navigation-away or by the
// host window closing.
type LiveDocument struct {
	e        *Engine
	doc      *Document
	rp       *renderPass
	sess     *js.Session
	fonts    *paint.Fonts
	viewport image.Rectangle
	vpW, vpH int
	stopHeap func()
	closed   bool
	// focused is the node synthetic.go's Focus most recently moved input
	// focus to (nil if none), and focusedValue is that node's value at the
	// moment it was focused — the baseline Blur compares against to decide
	// whether a real "change" event is due, matching a browser firing
	// change only when a focused control's value actually differs at blur,
	// not on every keystroke (that is what "input" is for).
	focused      *dom.Node
	focusedValue string
	// prevLinks is the stylesheet <link> set digest AS OF THE LAST SETTLED
	// FRAME — carried across Interact calls (unlike settle's own prevLinks,
	// which is scoped to one render). resettle compares against this to
	// decide whether an interaction's own mutation touched the link set,
	// so it must be read BEFORE fn() runs, never recomputed from the
	// already-mutated DOM inside the same call — see
	// TestLiveDocumentResettleRefetchesChangedStylesheets.
	prevLinks uint64
}

// Open fetches url and renders it exactly like Render, but keeps the page's
// JS session alive afterward instead of closing it. The caller must call
// Close when done with the page (a real navigation opens a fresh
// LiveDocument via another Open — this type is for staying ON one page
// across interactions, not for the page itself changing).
func (e *Engine) Open(ctx context.Context, rawurl string, viewport image.Rectangle) (*LiveDocument, error) {
	doc, err := e.Fetch(ctx, rawurl)
	if err != nil {
		return nil, err
	}
	return e.OpenDocument(ctx, doc, viewport)
}

// OpenDocument is Open for an already-fetched Document — the interactive
// counterpart to RenderDocument, and what fixture tests use.
func (e *Engine) OpenDocument(ctx context.Context, doc *Document, viewport image.Rectangle) (*LiveDocument, error) {
	fonts := paint.NewFonts()
	vpW, vpH := viewportSize(viewport)
	rp, sess, stopHeap := e.renderCoreStaged(ctx, doc, vpW, vpH, fonts, nil)
	return &LiveDocument{
		e: e, doc: doc, rp: rp, sess: sess, fonts: fonts,
		viewport: viewport, vpW: vpW, vpH: vpH, stopHeap: stopHeap,
		prevLinks: linkKey(doc.Root),
	}, nil
}

// Document returns the live DOM's root document, for a caller that needs to
// walk or hit-test the current tree (e.g. a later phase's element index).
// The returned *Document is the SAME one Open/OpenDocument was given — its
// Root mutates in place as the page's own scripts and any synthetic
// interaction change it.
func (d *LiveDocument) Document() *Document { return d.doc }

// Frame renders the document's CURRENT state to an image, exactly like
// Render's return value — call it after Open, and again after each
// Interact, to get what the user should now see.
func (d *LiveDocument) Frame() (*image.RGBA, *RenderInfo, error) {
	if d.closed {
		return nil, nil, ErrClosed
	}
	img := d.e.newCanvas(d.doc, d.rp, d.viewport, d.vpW)
	paint.PaintFull(img, d.rp.box, d.fonts, d.rp.imgs, d.rp.bgImgs)
	return img, renderInfo(d.doc, d.rp), nil
}

// Eval runs code in the document's live JS session and returns its value
// as a plain Go value (see js.Session.Eval) — for reading page-side JS
// state back into Go (a conformance harness reading testharness.js's own
// PASS/FAIL results, say), not for driving input (see Interact and
// synthetic.go's Focus/Type/Click for that). It does not re-settle
// layout/paint afterward; a caller whose eval mutated the DOM and wants a
// fresh Frame should follow it with Interact.
func (d *LiveDocument) Eval(code string) (interface{}, error) {
	if d.closed {
		return nil, ErrClosed
	}
	return d.sess.Eval(code)
}

// Elements returns the CURRENT general element hit-map (every element's used
// border-box rect, in the same full-page image pixel space Frame's image
// uses) — call it fresh after Open or any Interact, since a mutation can
// move or resize anything. ElementAt resolves a specific point against it.
func (d *LiveDocument) Elements() map[*dom.Node]layout.Rect {
	return ElementsFromBox(d.rp.box)
}

// ElementAt is ElementAt(d.Elements(), pt) — the convenience a caller
// translating one click coordinate reaches for.
func (d *LiveDocument) ElementAt(pt image.Point) (*dom.Node, bool) {
	return ElementAt(d.Elements(), pt)
}

// Interact runs fn — which mutates the live DOM directly, or (in a later
// phase) dispatches a synthetic event through the live Session — and then
// resettles: re-cascades and re-lays-out from whatever fn changed, WITHOUT
// re-running the page's initial scripts. That last part is the entire point
// of LiveDocument: the JS runtime's own state (closures, timers, anything a
// script holds that the DOM alone doesn't capture) survives across
// repeated calls, exactly as it would across two real keystrokes in a
// browser. It returns the resulting frame.
func (d *LiveDocument) Interact(ctx context.Context, fn func()) (*image.RGBA, *RenderInfo, error) {
	if d.closed {
		return nil, nil, ErrClosed
	}
	fn()
	d.resettle(ctx)
	return d.Frame()
}

// resettle is settle's per-pass mechanic (re-cascade, re-layout, re-run any
// newly-inserted <script>, reload images if the layout actually changed),
// reimplemented here as its own bounded loop rather than shared with
// dynamic.go's settle — settle's loop is driven by comparing DOM signatures
// across a SCRIPT's own passes and is deliberately left untouched; here the
// caller (Interact) already knows something changed, so there is no
// signature gate to evaluate first. Both call the same underlying pure
// functions (css.CascadeVW, layout.LayoutDocument, layoutWithContainers,
// renderedEmpty, reskin, newLayoutMetrics) settle does, so a page's
// rendering behavior is identical either way. Nothing in this pipeline can
// currently fail (no network/IO on the hot path beyond the image reload,
// which — like settle's own — never returns an error), so unlike Open/Frame
// this has no error return; one is added if a future phase (e.g. native
// form submission) introduces a real fallible step here.
func (d *LiveDocument) resettle(ctx context.Context) {
	relaidOut := false

	for pass := 0; pass < maxSettlePasses; pass++ {
		if k := linkKey(d.doc.Root); k != d.prevLinks {
			d.rp.sheets = d.e.fetchExternalSheets(ctx, d.doc, float64(d.vpW))
			d.prevLinks = k
		}
		newSm := css.CascadeVW(d.doc.Root, float64(d.vpW), d.rp.sheets)
		newBox, newHeight := layout.LayoutDocumentViewport(d.doc.Root, newSm, float64(d.vpW), float64(d.vpH), d.fonts, d.rp.imgSize)
		newSm, newBox, newHeight = layoutWithContainers(d.doc.Root, d.rp.sheets, float64(d.vpW), float64(d.vpH), d.fonts, d.rp.imgSize, newSm, newBox, newHeight)

		// Same wipe-guard settle applies: never let a pass erase an
		// already-good render (see dynamic.go's settle for the full
		// rationale — a broken script re-render observed live on react.dev).
		if !renderedEmpty(d.rp.box, d.rp.height) && renderedEmpty(newBox, newHeight) {
			reskin(d.rp.box, newSm)
			d.rp.sm = newSm
			break
		}
		d.rp.sm, d.rp.box, d.rp.height = newSm, newBox, newHeight
		relaidOut = true

		d.sess.SetMetrics(newLayoutMetrics(d.rp.box, d.rp.sm, d.vpW, d.vpH))
		// A newly-inserted <script> (rare from a plain interaction, but a
		// click handler could add one) gets the same chance settle gives it;
		// no new script means this mutation is fully reflected already.
		if !d.sess.RunPending() {
			break
		}
	}

	d.doc.Title = dom.Title(d.doc.Root)

	if relaidOut {
		d.rp.imgSize, d.rp.imgs = d.e.loadImages(ctx, d.doc, d.rp.sm, d.vpW)
		d.rp.bgImgs = d.e.loadBackgroundImages(ctx, d.doc, d.rp.sm)
		d.rp.box, d.rp.height = layout.LayoutDocumentViewport(d.doc.Root, d.rp.sm, float64(d.vpW), float64(d.vpH), d.fonts, d.rp.imgSize)
		d.rp.sm, d.rp.box, d.rp.height = layoutWithContainers(d.doc.Root, d.rp.sheets, float64(d.vpW), float64(d.vpH), d.fonts, d.rp.imgSize, d.rp.sm, d.rp.box, d.rp.height)
	}
}

// Close releases the JS session's watchdog goroutine and the heap-guard
// goroutine. Idempotent.
func (d *LiveDocument) Close() {
	if d.closed {
		return
	}
	d.closed = true
	if d.sess != nil {
		d.sess.Close()
	}
	if d.stopHeap != nil {
		d.stopHeap()
	}
}
