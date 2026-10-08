// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"
	"fmt"
	"hash/fnv"
	"image"
	"strconv"
	"time"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/js"
	"github.com/go-webengine/engine/layout"
	"github.com/go-webengine/engine/paint"
)

// maxSettlePasses bounds how many times the settle loop re-runs
// cascade→layout→inject-scripts before the final paint. Real progressive
// enhancement (a class toggle, a nav collapse, a ResourceLoader chain) settles
// in one or two rounds; the cap guarantees termination regardless.
const maxSettlePasses = 3

// renderPass is the mutable output of one cascade+layout, threaded through the
// settle loop and consumed by the final paint.
type renderPass struct {
	sheets  []string
	sm      css.StyleMap
	box     *layout.Box
	height  float64
	imgSize map[*dom.Node][2]float64
	imgs    map[*dom.Node]image.Image
	bgImgs  map[string]image.Image
}

// layoutMetrics implements js.Metrics: it answers getBoundingClientRect /
// offset* / getComputedStyle from the real laid-out box tree + computed styles
// of one layout pass. Replaced each pass via Session.SetMetrics.
type layoutMetrics struct {
	idx      map[*dom.Node]layout.Rect
	sm       css.StyleMap
	vpW, vpH int
}

func newLayoutMetrics(box *layout.Box, sm css.StyleMap, vpW, vpH int) *layoutMetrics {
	return &layoutMetrics{idx: layout.BuildIndex(box), sm: sm, vpW: vpW, vpH: vpH}
}

// Rect returns n's used border-box rect (document coords).
func (m *layoutMetrics) Rect(n *dom.Node) (x, y, w, h float64, ok bool) {
	r, ok := m.idx[n]
	if !ok {
		return 0, 0, 0, 0, false
	}
	return r.X, r.Y, r.W, r.H, true
}

// Computed resolves the used value of a geometry-relevant property. Unknown
// properties report ok=false (getComputedStyle then returns "").
func (m *layoutMetrics) Computed(n *dom.Node, prop string) (string, bool) {
	st := m.sm[n]
	r, hasRect := m.idx[n]
	switch prop {
	case "width":
		if hasRect && st != nil {
			return pxStr(contentAxis(r.W, st.Padding.Left+st.Padding.Right, st.Border.Widths().Left+st.Border.Widths().Right)), true
		}
		if hasRect {
			return pxStr(r.W), true
		}
	case "height":
		if hasRect && st != nil {
			return pxStr(contentAxis(r.H, st.Padding.Top+st.Padding.Bottom, st.Border.Widths().Top+st.Border.Widths().Bottom)), true
		}
		if hasRect {
			return pxStr(r.H), true
		}
	}
	if st == nil {
		return "", false
	}
	switch prop {
	case "display":
		if st.Display == css.DisplayNone || !hasRect {
			return "none", true
		}
		return displayString(st.Display), true
	case "position":
		return positionString(st.Position), true
	case "margin-top":
		return pxStr(st.Margin.Top), true
	case "margin-right":
		return pxStr(st.Margin.Right), true
	case "margin-bottom":
		return pxStr(st.Margin.Bottom), true
	case "margin-left":
		return pxStr(st.Margin.Left), true
	case "padding-top":
		return pxStr(st.Padding.Top), true
	case "padding-right":
		return pxStr(st.Padding.Right), true
	case "padding-bottom":
		return pxStr(st.Padding.Bottom), true
	case "padding-left":
		return pxStr(st.Padding.Left), true
	case "font-size":
		return pxStr(st.FontSize), true
	case "font-weight":
		return strconv.Itoa(st.FontWeight), true
	case "font-style":
		if st.Italic {
			return "italic", true
		}
		return "normal", true
	case "line-height":
		if lh, ok := st.LineHeight.Resolve(st.FontSize); ok {
			return pxStr(lh), true
		}
		return "normal", true
	case "color":
		return colorString(st.Color), true
	case "background-color":
		return colorString(st.Background), true
	case "text-align":
		return textAlignString(st.TextAlign), true
	case "box-sizing":
		if st.BoxSizing == css.BorderBox {
			return "border-box", true
		}
		return "content-box", true
	case "opacity":
		if st.HasOpacity {
			return strconv.FormatFloat(st.Opacity, 'f', -1, 64), true
		}
		return "1", true
	case "z-index":
		if st.ZIndexAuto {
			return "auto", true
		}
		return strconv.Itoa(st.ZIndex), true
	case "visibility":
		switch st.Visibility {
		case css.VisibilityHidden:
			return "hidden", true
		case css.VisibilityCollapse:
			return "collapse", true
		default:
			return "visible", true
		}
	case "top":
		return lengthUsed(st.Top), true
	case "right":
		return lengthUsed(st.Right), true
	case "bottom":
		return lengthUsed(st.Bottom), true
	case "left":
		return lengthUsed(st.Left), true
	}
	return "", false
}

// contentAxis subtracts padding+border from a border-box extent, clamped at 0.
func contentAxis(border, padding, borderW float64) float64 {
	v := border - padding - borderW
	if v < 0 {
		return 0
	}
	return v
}

func pxStr(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) + "px" }

func lengthUsed(l css.Length) string {
	if l.Auto {
		return "auto"
	}
	if l.IsPercent {
		return strconv.FormatFloat(l.Percent*100, 'f', -1, 64) + "%"
	}
	return pxStr(l.Px)
}

func colorString(c css.Color) string {
	if c.A == 0 {
		return "rgba(0, 0, 0, 0)"
	}
	if c.A == 255 {
		return "rgb(" + strconv.Itoa(int(c.R)) + ", " + strconv.Itoa(int(c.G)) + ", " + strconv.Itoa(int(c.B)) + ")"
	}
	a := strconv.FormatFloat(float64(c.A)/255, 'f', -1, 64)
	return "rgba(" + strconv.Itoa(int(c.R)) + ", " + strconv.Itoa(int(c.G)) + ", " + strconv.Itoa(int(c.B)) + ", " + a + ")"
}

func displayString(d css.Display) string {
	switch d {
	case css.DisplayBlock:
		return "block"
	case css.DisplayNone:
		return "none"
	case css.DisplayInlineBlock:
		return "inline-block"
	case css.DisplayFlex:
		return "flex"
	case css.DisplayGrid:
		return "grid"
	case css.DisplayTable:
		return "table"
	case css.DisplayTableRow:
		return "table-row"
	case css.DisplayTableCell:
		return "table-cell"
	case css.DisplayTableRowGroup:
		return "table-row-group"
	}
	return "inline"
}

func positionString(p css.Position) string {
	switch p {
	case css.PositionRelative:
		return "relative"
	case css.PositionAbsolute:
		return "absolute"
	case css.PositionFixed:
		return "fixed"
	case css.PositionSticky:
		return "sticky"
	}
	return "static"
}

func textAlignString(a css.TextAlign) string {
	switch a {
	case css.AlignCenter:
		return "center"
	case css.AlignRight:
		return "right"
	}
	return "left"
}

// domSignature is a structural+attribute+text digest of the document, used to
// detect whether a script pass changed anything the cascade/layout depends on.
// It is stable given the deterministic (sorted-attribute) serialization, so an
// unchanged signature across a pass means the geometry is unchanged too (layout
// is a pure function of DOM + styles + viewport) — the settle loop's fixpoint.
func domSignature(root *dom.Node) uint64 {
	h := fnv.New64a()
	if el := dom.Find(root, "html"); el != nil {
		_, _ = h.Write([]byte(dom.OuterHTML(el)))
	}
	return h.Sum64()
}

// settle runs the layout↔JS feedback loop: it starts a JS Session against the
// already-laid-out DOM, feeds it the real geometry, runs the page scripts, then
// iterates cascade→layout→inject-scripts to a bounded fixpoint, updating rp in
// place. It is deterministic and always terminates (pass cap + time budget).
// onStage (when non-nil, from a progressive render) is called with "settle"
// after each pass that re-laid-out the document; the caller dedups against the
// previous emitted frame's geometry.
//
// settle returns the live Session rather than closing it: a one-shot render
// (renderCoreStaged) closes it immediately after, byte-identical to before;
// Engine.Open keeps it alive instead, across separate later interactions, via
// LiveDocument (interactive.go) — the only reason the return value exists.
func (e *Engine) settle(ctx context.Context, doc *Document, vpW, vpH int, fonts *paint.Fonts, rp *renderPass, initialLayout time.Duration, onStage func(stage string, rp *renderPass)) *js.Session {
	sess := js.Begin(doc.Root, e.jsOptions(ctx, doc, vpW, vpH))

	sess.SetMetrics(newLayoutMetrics(rp.box, rp.sm, vpW, vpH))
	// Signature of the DOM the INITIAL layout was computed from (client-js already
	// applied), captured before the scripts run so a mutation is detectable.
	sig := domSignature(doc.Root)
	sess.RunInitialScripts()

	// ES modules run after classic scripts (they are deferred). goja cannot run a
	// module graph natively, so transpile the page's <script type="module"> graph
	// to one classic IIFE with esbuild, inject it as a classic script, and run it
	// through the same pass. Strictly gated: a page with no module scripts skips
	// this entirely. Best-effort — a bundle failure leaves the page as-is.
	//
	// Deliberately BEFORE DispatchLifecycle below, not after: a classic script
	// above may have just created the page's <script type="module"> element(s)
	// itself (a hand-rolled ResourceLoader, e.g. pkg.go.dev's own loadScript()
	// helper) whose own top-level code registers a window "load" listener —
	// see DispatchLifecycle's own doc comment for why that listener must get a
	// chance to register before "load" fires, not after.
	if !e.DisableJS {
		if bundled, mstats, ok := e.bundleModuleScripts(ctx, doc); ok {
			e.jslog(fmt.Sprintf("modules: bundled %d entr(y/ies) -> %d modules, %d->%d bytes in %s",
				mstats.entries, mstats.fetched, mstats.bytesIn, mstats.bytesOut, mstats.wallClock))
			injectBundledScript(doc.Root, bundled)
		} else if mstats.entries > 0 {
			e.jslog(fmt.Sprintf("modules: %d module script(s) not bundled (fetched=%d, errors=%d, first=%q)",
				mstats.entries, mstats.fetched, mstats.errors, mstats.firstErr))
		}
	}
	// Unconditional, not just after a successful module bundle: a classic
	// script above may equally well have appended a plain, non-module <script>
	// of its own (the same hand-rolled-ResourceLoader idiom, e.g. pkg.go.dev's
	// loadScript() also loads a plain dialog-polyfill.js this way) — it too
	// needs to run before "load" fires, for the same reason.
	sess.RunPending()
	sess.DispatchLifecycle()

	deadline, hasDeadline := ctx.Deadline()
	layoutDur := initialLayout
	overBudget := func() bool { return hasDeadline && time.Until(deadline) < 2*layoutDur }
	prevLinks := linkKey(doc.Root)
	relaidOut := false

	for pass := 0; pass < maxSettlePasses; pass++ {
		newSig := domSignature(doc.Root)
		if newSig == sig {
			break // scripts changed nothing layout-relevant: initial layout stands
		}
		// Bound the added cost: never start a re-layout that risks blowing a
		// caller's deadline (a very expensive page renders its pre-settle layout
		// rather than timing out).
		if overBudget() {
			e.jslog("settle: re-layout skipped to respect deadline")
			break
		}
		sig = newSig

		// Re-collect the cascade from the MUTATED DOM: injected <style> text is
		// picked up by the cascade's own <style> walk; injected <link> stylesheets
		// are refetched only when the link set actually changed (bounded network).
		if k := linkKey(doc.Root); k != prevLinks {
			rp.sheets = e.fetchExternalSheets(ctx, doc, float64(vpW))
			prevLinks = k
		}
		newSm := css.CascadeVW(doc.Root, float64(vpW), rp.sheets)
		start := time.Now()
		newBox, newHeight := layout.LayoutDocumentViewport(doc.Root, newSm, float64(vpW), float64(vpH), fonts, rp.imgSize)
		// A script's DOM/class mutation can change which elements are query
		// containers, or their sizes, so @container queries need the same
		// bounded re-resolution here that the initial (pre-settle) layout gets
		// in renderCoreStaged.
		newSm, newBox, newHeight = layoutWithContainers(doc.Root, rp.sheets, float64(vpW), float64(vpH), fonts, rp.imgSize, newSm, newBox, newHeight)
		layoutDur = time.Since(start)

		// Never let a script pass erase an already-good render: a client-side
		// router that fails to load its own error route (observed on react.dev)
		// can unmount the whole app tree while still leaving the DOM "changed"
		// (so the signature check above does not save us). If the page had real
		// content before this pass and has essentially none after it, the pass is
		// almost certainly a broken script re-render, not an intentional wipe —
		// keep the last good layout and stop, rather than settling on blank.
		if !renderedEmpty(rp.box, rp.height) && renderedEmpty(newBox, newHeight) {
			// Keeping the prior GEOMETRY is right (see above), but discarding this
			// pass wholesale also throws away any harmless, unrelated style-only
			// mutation that happened in the SAME script batch as whatever emptied
			// the render — observed live on react.dev: its dark-mode toggle script
			// (an ordinary matchMedia('(prefers-color-scheme: dark)') + classList.
			// add('dark') on <html>, and nothing to do with hydration) runs in the
			// same pass as a client-side router that fails to mount and empties the
			// tree. Both mutations land in one signature check; rejecting the pass
			// rejected the good class-toggle style change along with the bad
			// content wipe, so the page silently stayed in its pre-JS light theme
			// forever even though <html class="dark"> was correctly present in the
			// final DOM. Node identity survives a script pass (mutation is in
			// place, not a replacement), so re-skinning the PRESERVED box tree with
			// the freshly cascaded styles is safe: geometry (X/Y/W/H/children)
			// stays exactly as before, only each box's Style pointer is swapped to
			// its up-to-date computed value.
			e.jslog("settle: script pass emptied an already-rendered page; keeping prior layout, re-skinning with its styles")
			reskin(rp.box, newSm)
			rp.sm = newSm
			break
		}
		rp.sm, rp.box, rp.height = newSm, newBox, newHeight
		relaidOut = true

		// Progressive: emit an intermediate frame for this re-laid-out pass (the
		// interruptible-reflow analogue). The caller dedups a pass whose geometry
		// did not visibly change from the previous emitted frame.
		if onStage != nil {
			onStage("settle", rp)
		}

		sess.SetMetrics(newLayoutMetrics(rp.box, rp.sm, vpW, vpH))
		// Run any <script> injected since the last pass (a ResourceLoader chain).
		// If none ran, a layout change alone triggers no further JS, so the state
		// just computed is final.
		if !sess.RunPending() {
			break
		}
	}

	// Final resource refresh: the settled DOM/CSS can reference theme-specific
	// <img>/background bitmaps (e.g. a dark-theme set the initial light layout
	// never fetched) or newly-injected images. When a script actually changed the
	// layout, reload the images/backgrounds against the settled styles and lay out
	// once more so the final paint matches — budget-gated so an expensive page is
	// never pushed over its deadline.
	if relaidOut && !overBudget() {
		rp.imgSize, rp.imgs = e.loadImages(ctx, doc, rp.sm, vpW)
		rp.bgImgs = e.loadBackgroundImages(ctx, doc, rp.sm)
		rp.box, rp.height = layout.LayoutDocumentViewport(doc.Root, rp.sm, float64(vpW), float64(vpH), fonts, rp.imgSize)
		rp.sm, rp.box, rp.height = layoutWithContainers(doc.Root, rp.sheets, float64(vpW), float64(vpH), fonts, rp.imgSize, rp.sm, rp.box, rp.height)
	}
	return sess
}

// reskin walks a PRESERVED box tree (geometry already final and not to be
// touched) and swaps each box's, marker's, and inline text run's Style to its
// up-to-date computed value from sm, leaving X/Y/W/H/Children/Lines/Items
// themselves untouched. It is the settle loop's way of adopting a rejected
// pass's style-only changes (see the renderedEmpty guard above) without
// adopting that pass's broken geometry. Inline text color (and other
// per-InlineItem style) is carried on InlineItem.Style rather than the
// enclosing box's Style, so line items need the same treatment as boxes for a
// dark-mode class toggle to actually recolor rendered text. A node with no
// entry in sm (e.g. an anonymous box, or a synthetic item with no originating
// element) keeps its current Style.
func reskin(box *layout.Box, sm css.StyleMap) {
	if box == nil {
		return
	}
	if box.Node != nil {
		if st, ok := sm[box.Node]; ok {
			box.Style = st
			if box.Marker != nil {
				box.Marker.Style = st
			}
		}
	}
	for _, line := range box.Lines {
		for _, item := range line.Items {
			if item.Node == nil {
				continue
			}
			if st, ok := sm[item.Node]; ok {
				item.Style = st
			}
		}
	}
	for _, child := range box.Children {
		reskin(child, sm)
	}
}

// linkKey is a digest of the document's stylesheet <link> hrefs (in order), used
// to decide whether external sheets must be refetched after a script pass.
func linkKey(root *dom.Node) uint64 {
	h := fnv.New64a()
	for _, ln := range css.StylesheetLinks(root) {
		_, _ = h.Write([]byte(ln.Href))
		_, _ = h.Write([]byte{0})
	}
	return h.Sum64()
}

// jsOptions builds the Options for a Session from the engine config.
func (e *Engine) jsOptions(ctx context.Context, doc *Document, vpW, vpH int) js.Options {
	return js.Options{
		PageURL:        doc.URL,
		UserAgent:      e.UserAgent,
		Client:         e.Client,
		Ctx:            ctx,
		Timeout:        e.JSTimeout,
		ViewportWidth:  vpW,
		ViewportHeight: vpH,
		Log:            e.JSLog,
	}
}

// jslog routes an engine diagnostic to JSLog if set.
func (e *Engine) jslog(msg string) {
	if e.JSLog != nil {
		e.JSLog("engine: " + msg)
	}
}
