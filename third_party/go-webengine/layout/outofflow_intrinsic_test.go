// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package layout

import (
	"testing"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// TestPreferredWidthSkipsOutOfFlowBlockChild covers the real shape found live
// on github.com/golang/go's own marketing nav: a `position:relative` trigger
// wrapper whose ONE other child is a far-wider `position:absolute` dropdown
// panel must not have that panel's own width counted toward the wrapper's
// max-content width, exactly like a display:none child is already skipped —
// an absolutely-positioned box is sized against its own containing block,
// never folded into an ancestor's normal-flow content size.
func TestPreferredWidthSkipsOutOfFlowBlockChild(t *testing.T) {
	l := &layouter{m: fakeMeasurer{}, floats: &floatCtx{}}
	root, _ := dom.Parse(`<html><body><div id="d" style="position:relative">` +
		`<p style="margin:0">xx</p>` +
		`<p style="margin:0;position:absolute">xxxxxxxxxxxxxxxxxxxx</p>` +
		`</div></body></html>`)
	l.sm = css.Cascade(root)
	d := dom.Find(root, "div")
	// The absolute 20-char paragraph is excluded; only "xx"=20 counts.
	if got := l.preferredWidth(d, l.sm[d]); got != 20 {
		t.Errorf("block preferred = %v, want 20 (out-of-flow child excluded)", got)
	}
}

// TestPreferredWidthSkipsOutOfFlowFlexChild is the identical shape for the
// flex-row-sum branch: a display:flex container's own max-content is the sum
// of its IN-FLOW items only.
func TestPreferredWidthSkipsOutOfFlowFlexChild(t *testing.T) {
	l := &layouter{m: fakeMeasurer{}, floats: &floatCtx{}}
	root, _ := dom.Parse(`<html><body><div id="d" style="display:flex;position:relative">` +
		`<span style="margin:0">xx</span>` +
		`<span style="margin:0;position:absolute">xxxxxxxxxxxxxxxxxxxx</span>` +
		`</div></body></html>`)
	l.sm = css.Cascade(root)
	d := dom.Find(root, "div")
	if got := l.preferredWidth(d, l.sm[d]); got != 20 {
		t.Errorf("flex-row preferred = %v, want 20 (out-of-flow child excluded)", got)
	}
}

// TestMinContentWidthSkipsOutOfFlowBlockChild is minContentWidth's own
// counterpart of TestPreferredWidthSkipsOutOfFlowBlockChild — the two
// functions mirror each other's branch structure (see minContentWidth's own
// doc comment), so the SAME out-of-flow exclusion is needed here too.
func TestMinContentWidthSkipsOutOfFlowBlockChild(t *testing.T) {
	l := &layouter{m: fakeMeasurer{}, floats: &floatCtx{}}
	root, _ := dom.Parse(`<html><body><div id="d" style="position:relative">` +
		`<p style="margin:0">xx</p>` +
		`<p style="margin:0;position:absolute">xxxxxxxxxxxxxxxxxxxx</p>` +
		`</div></body></html>`)
	l.sm = css.Cascade(root)
	d := dom.Find(root, "div")
	if got := l.minContentWidth(d, l.sm[d]); got != 20 {
		t.Errorf("block min-content = %v, want 20 (out-of-flow child excluded)", got)
	}
}

// TestMinContentWidthSkipsOutOfFlowFlexChild is minContentWidth's own
// counterpart of TestPreferredWidthSkipsOutOfFlowFlexChild.
func TestMinContentWidthSkipsOutOfFlowFlexChild(t *testing.T) {
	l := &layouter{m: fakeMeasurer{}, floats: &floatCtx{}}
	root, _ := dom.Parse(`<html><body><div id="d" style="display:flex;position:relative">` +
		`<span style="margin:0">xx</span>` +
		`<span style="margin:0;position:absolute">xxxxxxxxxxxxxxxxxxxx</span>` +
		`</div></body></html>`)
	l.sm = css.Cascade(root)
	d := dom.Find(root, "div")
	if got := l.minContentWidth(d, l.sm[d]); got != 20 {
		t.Errorf("flex-row min-content = %v, want 20 (out-of-flow child excluded)", got)
	}
}
