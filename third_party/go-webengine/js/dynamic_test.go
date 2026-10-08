// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package js

import (
	"strings"
	"testing"
	"time"

	"github.com/dop251/goja"

	"github.com/go-webengine/engine/dom"
)

// fakeMetrics is a test Metrics: fixed rects and computed values keyed by node /
// property.
type fakeMetrics struct {
	rect map[*dom.Node][4]float64
	comp map[string]string
}

func (m *fakeMetrics) Rect(n *dom.Node) (x, y, w, h float64, ok bool) {
	if r, ok := m.rect[n]; ok {
		return r[0], r[1], r[2], r[3], true
	}
	return 0, 0, 0, 0, false
}

func (m *fakeMetrics) Computed(n *dom.Node, prop string) (string, bool) {
	if m.comp == nil {
		return "", false
	}
	v, ok := m.comp[prop]
	return v, ok
}

// TestSessionGeometryReadback proves the DOM binding returns REAL geometry from
// the installed Metrics (getBoundingClientRect / offset* / client* / scroll* /
// getComputedStyle), and zeros for an unlaid-out node.
func TestSessionGeometryReadback(t *testing.T) {
	src := `<html class="client-nojs"><head></head><body>` +
		`<div id="d"><span id="s">hi</span></div><b id="x"></b>` +
		`<script>
			var d=document.getElementById('d');
			var s=document.getElementById('s');
			var x=document.getElementById('x');
			var r=d.getBoundingClientRect();
			console.log('rect='+r.x+','+r.y+','+r.width+','+r.height+','+r.right+','+r.bottom+','+r.left+','+r.top);
			console.log('off='+d.offsetWidth+','+d.offsetHeight+','+d.clientWidth+','+d.clientHeight+','+d.scrollWidth+','+d.scrollHeight);
			console.log('soff='+s.offsetTop+','+s.offsetLeft);
			console.log('xoff='+x.offsetTop+','+x.offsetLeft+',ow='+x.offsetWidth);
			console.log('xrect='+x.getBoundingClientRect().width);
			console.log('rects='+d.getClientRects().length+',x='+x.getClientRects().length);
			var cs=window.getComputedStyle(d);
			console.log('disp='+cs.getPropertyValue('display')+',w='+cs.width);
			console.log('unknown=['+cs.getPropertyValue('no-such-prop')+']');
			console.log('pri=['+cs.getPropertyPriority('display')+'],txt=['+cs.cssText+']');
			cs.setProperty('display','none'); cs.removeProperty('display'); // read-only no-ops
			cs.foo='ignored';
			console.log('has='+('display' in cs)+',keys='+Object.keys(cs).length);
			delete cs.foo;
			console.log('dispAfter='+cs.getPropertyValue('display'));
		</script></body></html>`

	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	d := dom.Find(root, "div")
	s := dom.Find(root, "span")
	m := &fakeMetrics{
		rect: map[*dom.Node][4]float64{
			d: {10, 20, 200, 40},
			s: {12, 25, 50, 18},
		},
		comp: map[string]string{"display": "block", "width": "186px"},
	}

	var logs []string
	sess := Begin(root, Options{PageURL: "https://demo.test/", Timeout: 3 * time.Second,
		Log: func(l string) { logs = append(logs, l) }})
	sess.SetMetrics(m)
	sess.RunInitial()
	sess.Close()
	sess.Close() // idempotent

	mustHaveJS(t, logs,
		"rect=10,20,200,40,210,60,10,20",
		"off=200,40,200,40,200,40",
		"soff=5,2",       // span relative to its offsetParent (the div)
		"xoff=0,0,ow=0",  // unlaid-out node: zeros, no ancestor rect
		"xrect=0",        // zero DOMRect
		"rects=1,x=0",    // d has a rect; x has none
		"disp=block,w=186px",
		"unknown=[]",              // unknown computed property → ""
		"pri=[],txt=[]",           // priority + cssText inert
		"has=true,keys=0",         // Has true, Keys empty (read-only view)
		"dispAfter=block",         // setProperty/removeProperty were no-ops
	)
}

// TestSessionRunPending proves a <script> injected by a running script is picked
// up and executed by the next RunPending, in the same runtime — and only once.
func TestSessionRunPending(t *testing.T) {
	src := `<html><head></head><body><script>
		var s=document.createElement('script');
		s.textContent='window.__v=(window.__v||0)+1; console.log("inj v="+window.__v);';
		document.body.appendChild(s);
	</script></body></html>`
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	var logs []string
	sess := Begin(root, Options{PageURL: "https://demo.test/", Timeout: 3 * time.Second,
		Log: func(l string) { logs = append(logs, l) }})
	defer sess.Close()
	sess.RunInitial()
	if !sess.RunPending() {
		t.Fatal("RunPending should have run the injected script")
	}
	if sess.RunPending() {
		t.Fatal("RunPending should not re-run an already-executed script")
	}
	mustHaveJS(t, logs, "inj v=1")
	if strings.Count(strings.Join(logs, "\n"), "inj v=") != 1 {
		t.Fatalf("injected script ran more than once: %v", logs)
	}
}

// TestSessionInjectedScriptOnloadAndOnerrorFire covers the standard webpack/
// Turbopack chunk-loading idiom directly: document.createElement('script');
// s.onload=fn; s.src=...; parent.appendChild(s) — for both outcomes (the
// script fetches and runs; the fetch fails). Before this, no element had an
// onload/onerror PROPERTY at all (a plain, silently-ignored assignment), and
// runScripts never dispatched "load"/"error" on a <script> after handling
// it, so neither outcome was ever observable to the page's own code — a
// dynamic loader's promise could never settle either way.
func TestSessionInjectedScriptOnloadAndOnerrorFire(t *testing.T) {
	src := `<html><head></head><body><script>
		var ok=document.createElement('script');
		ok.onload=function(){ console.log('ok-onload'); };
		ok.onerror=function(){ console.log('ok-onerror (should not fire)'); };
		ok.textContent='console.log("ok-ran");';
		document.body.appendChild(ok);

		var bad=document.createElement('script');
		bad.onload=function(){ console.log('bad-onload (should not fire)'); };
		bad.onerror=function(){ console.log('bad-onerror'); };
		bad.src='https://demo.test/does-not-exist.js';
		document.body.appendChild(bad);
	</script></body></html>`
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	var logs []string
	sess := Begin(root, Options{PageURL: "https://demo.test/", Timeout: 3 * time.Second,
		Log: func(l string) { logs = append(logs, l) }})
	defer sess.Close()
	sess.RunInitial()
	sess.RunPending()
	mustHaveJS(t, logs, "ok-ran", "ok-onload", "bad-onerror")
	joined := strings.Join(logs, "\n")
	if strings.Contains(joined, "should not fire") {
		t.Fatalf("a handler fired for the wrong outcome: %v", logs)
	}
}

// TestSessionRunPendingReportsPendingScriptAppendedDuringDrainTimers covers a
// narrower case than TestSessionRunPending above: a <script> that appears in
// the DOM not because THIS call's own runScripts found and ran a script that
// appended it, but because a timer job — already sitting in the queue from
// an earlier phase, exactly like webpack/Turbopack's own chunk-loading
// "give up after N ms" fallback surviving to fire later — is what drainTimers
// fires DURING this same RunPending call. Before this was fixed, RunPending
// decided its "ran" return value from runScripts alone, BEFORE drainTimers
// (called right after) had a chance to inject anything — so a script
// appended this way sat in the DOM, correctly never re-executed, but
// invisible to the caller: dynamic.go's settle loop reads a false "ran" as
// "nothing left to do" and stops iterating, so this <script> was NEVER RUN
// AT ALL. Confirmed live on react.dev's own webpack chunk-loading bundle via
// direct instrumentation (see FIDELITY.md): a dynamically-created
// <script src="…f809.js"> was correctly appended to <head> with the right
// src, but its fetch was never even attempted.
func TestSessionRunPendingReportsPendingScriptAppendedDuringDrainTimers(t *testing.T) {
	root, err := dom.Parse(`<html><head></head><body></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	var logs []string
	sess := Begin(root, Options{PageURL: "https://demo.test/", Timeout: 3 * time.Second,
		Log: func(l string) { logs = append(logs, l) }})
	defer sess.Close()
	sess.RunInitial()

	// Simulate a timer job that only fires NOW, as part of THIS RunPending's
	// own drainTimers step, by enqueuing it directly rather than via a
	// script runScripts would itself discover and run first (which would
	// set "ran" true from that alone, never exercising the bug).
	fnVal, err := sess.b.vm.RunString(`(function(){
		var s = document.createElement('script');
		s.textContent = 'window.__v=(window.__v||0)+1; console.log("inj v="+window.__v);';
		document.body.appendChild(s);
	})`)
	if err != nil {
		t.Fatal(err)
	}
	fn, ok := goja.AssertFunction(fnVal)
	if !ok {
		t.Fatal("expected a callable function value")
	}
	sess.b.jobs = append(sess.b.jobs, timerJob{fn: fn})

	if !sess.RunPending() {
		t.Fatal("RunPending should report true: drainTimers just appended a script that is now waiting to run")
	}
	// The appended script is deliberately NOT run inline within the same
	// call (see RunPending's own doc comment on why, and
	// TestSettleFixpointCap for what breaks if it were) — a follow-up call,
	// matching how dynamic.go's settle loop actually behaves on ran==true,
	// is what runs it.
	sess.RunPending()
	mustHaveJS(t, logs, "inj v=1")
}

// TestMarkJSEnabled covers the exported pre-cascade signal helper.
func TestMarkJSEnabled(t *testing.T) {
	MarkJSEnabled(nil) // no panic
	root, _ := dom.Parse(`<html class="client-nojs"><body></body></html>`)
	MarkJSEnabled(root)
	html := dom.Find(root, "html")
	if html.Attr["class"] != "client-js" {
		t.Fatalf("MarkJSEnabled: class = %q, want client-js", html.Attr["class"])
	}
}

// TestSessionNoMetricsZeros confirms that without a Metrics source the geometry
// APIs still report zeros / inline styles (the legacy Run behaviour).
func TestSessionNoMetricsZeros(t *testing.T) {
	root, _ := dom.Parse(`<html><body><div id="d" style="color:teal"></div><script>
		var d=document.getElementById('d');
		console.log('r='+d.getBoundingClientRect().width+',ow='+d.offsetWidth+',rects='+d.getClientRects().length);
		console.log('c='+window.getComputedStyle(d).getPropertyValue('color'));
	</script></body></html>`)
	var logs []string
	sess := Begin(root, Options{Timeout: time.Second, Log: func(l string) { logs = append(logs, l) }})
	defer sess.Close()
	sess.RunInitial()
	mustHaveJS(t, logs, "r=0,ow=0,rects=0", "c=teal")
}

func mustHaveJS(t *testing.T, logs []string, subs ...string) {
	t.Helper()
	joined := strings.Join(logs, "\n")
	for _, s := range subs {
		if !strings.Contains(joined, s) {
			t.Errorf("missing log %q in:\n%s", s, joined)
		}
	}
}
