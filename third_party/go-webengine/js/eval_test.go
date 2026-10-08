// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package js

import (
	"strings"
	"testing"
	"time"

	"github.com/go-webengine/engine/dom"
)

func newTestSession(t *testing.T, src string) *Session {
	t.Helper()
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	sess := Begin(root, Options{PageURL: "https://demo.test/", Timeout: 3 * time.Second})
	t.Cleanup(sess.Close)
	sess.RunInitial()
	return sess
}

func TestEvalReturnsAPlainGoValue(t *testing.T) {
	sess := newTestSession(t, `<html><body></body></html>`)
	v, err := sess.Eval("1 + 2")
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	n, ok := v.(int64)
	if !ok || n != 3 {
		t.Fatalf("Eval(1+2) = %#v (%T), want int64(3)", v, v)
	}
}

func TestEvalSeesPageState(t *testing.T) {
	sess := newTestSession(t, `<html><body>
		<script>window.__answer = 6 * 7;</script>
	</body></html>`)
	v, err := sess.Eval("window.__answer")
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if v != int64(42) {
		t.Fatalf("Eval(window.__answer) = %#v, want 42 — a value the page's OWN script set, not this call's", v)
	}
}

// TestEvalReadsBackTestharnessShapedResults proves the actual intended use:
// reading a testharness.js-style completion summary back out of the page,
// the way a WPT conformance harness needs to.
func TestEvalReadsBackTestharnessShapedResults(t *testing.T) {
	sess := newTestSession(t, `<html><body>
		<script>
			window.__wptResults = {status: 0, tests: [
				{name: "test 1", status: 0},
				{name: "test 2", status: 1}
			]};
		</script>
	</body></html>`)
	v, err := sess.Eval("JSON.stringify(window.__wptResults)")
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	got, ok := v.(string)
	if !ok {
		t.Fatalf("Eval result = %#v (%T), want a JSON string", v, v)
	}
	for _, want := range []string{`"status":0`, `"name":"test 1"`, `"name":"test 2"`, `"status":1`} {
		if !strings.Contains(got, want) {
			t.Errorf("JSON %q does not contain %q", got, want)
		}
	}
}

func TestEvalPropagatesAThrownJSError(t *testing.T) {
	sess := newTestSession(t, `<html><body></body></html>`)
	_, err := sess.Eval("throw new Error('boom')")
	if err == nil {
		t.Fatal("Eval of a throwing expression: want an error, got nil")
	}
}

func TestEvalOnAClosedSessionErrors(t *testing.T) {
	sess := newTestSession(t, `<html><body></body></html>`)
	sess.Close()
	if _, err := sess.Eval("1"); err == nil {
		t.Fatal("Eval after Close: want an error, got nil")
	}
}
