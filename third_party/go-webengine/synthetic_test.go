// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"
	"testing"

	"image"

	"github.com/go-webengine/engine/dom"
)

// TestSyntheticLoginFlow is the decisive end-to-end proof: Focus, Type
// (character by character, across many separate Interact/resettle calls —
// LiveDocument's whole reason to exist), and Click drive a login form whose
// JS accumulates the typed text in its OWN closure variables (not just
// DOM/attribute state) and only "succeeds" once both fields match, exactly
// the shape a real login page's JS takes. If any keystroke's event were
// lost, or if a resettle silently reset the script's closures (the F0 bug
// class this whole design exists to avoid), the accumulated string would be
// wrong or short and this test would catch it.
func TestSyntheticLoginFlow(t *testing.T) {
	const src = `<html><body>
		<input id="email">
		<input id="password" type="password">
		<button id="submit">Log in</button>
		<div id="status">idle</div>
		<script>
			var email = '', password = '';
			document.getElementById('email').addEventListener('input', function(e){ email += e.data; });
			document.getElementById('password').addEventListener('input', function(e){ password += e.data; });
			document.getElementById('submit').addEventListener('click', function(e){
				e.preventDefault();
				var status = document.getElementById('status');
				if (email === 'user@test.com' && password === 'hunter2') {
					status.textContent = 'ok:' + email;
				} else {
					status.textContent = 'bad:' + email + '/' + password;
				}
			});
		</script>
	</body></html>`

	live := openFixture(t, New(), src, image.Rect(0, 0, 400, 300))
	root := live.Document().Root

	emailField := findByID(root, "email")
	passwordField := findByID(root, "password")
	submitBtn := findByID(root, "submit")

	if _, _, err := live.Focus(context.Background(), emailField); err != nil {
		t.Fatalf("Focus(email): %v", err)
	}
	if _, _, err := live.Type(context.Background(), emailField, "user@test.com"); err != nil {
		t.Fatalf("Type(email): %v", err)
	}
	if _, _, err := live.Focus(context.Background(), passwordField); err != nil {
		t.Fatalf("Focus(password): %v", err)
	}
	if _, _, err := live.Type(context.Background(), passwordField, "hunter2"); err != nil {
		t.Fatalf("Type(password): %v", err)
	}

	prevented, _, _, err := live.Click(context.Background(), submitBtn)
	if err != nil {
		t.Fatalf("Click(submit): %v", err)
	}
	if !prevented {
		t.Fatal("Click(submit): want defaultPrevented=true (the handler calls preventDefault())")
	}

	status := findByID(live.Document().Root, "status")
	if got := dom.TextContent(status); got != "ok:user@test.com" {
		t.Fatalf("final status = %q, want %q — typed text did not reach the page's JS closure state intact", got, "ok:user@test.com")
	}

	// The DOM-level value accessors must ALSO reflect what was typed (a
	// page that reads .value directly at submit time, the other common
	// pattern, must work too).
	if got := emailField.Attr["value"]; got != "user@test.com" {
		t.Fatalf("email field value = %q, want %q", got, "user@test.com")
	}
	if got := passwordField.Attr["value"]; got != "hunter2" {
		t.Fatalf("password field value = %q, want %q", got, "hunter2")
	}
}

// TestFocusFiresBlurAndChangeOnThePreviousField covers Focus's own
// side-effect on whatever was focused before: blur/focusout, and change
// only because the value actually differs from when it was focused.
func TestFocusFiresBlurAndChangeOnThePreviousField(t *testing.T) {
	const src = `<html><body>
		<input id="a"><input id="b">
		<div id="log"></div>
		<script>
			var log = [];
			var el = document.getElementById('log');
			document.getElementById('a').addEventListener('blur', function(){ log.push('blur'); el.textContent = log.join(','); });
			document.getElementById('a').addEventListener('focusout', function(){ log.push('focusout'); el.textContent = log.join(','); });
			document.getElementById('a').addEventListener('change', function(){ log.push('change'); el.textContent = log.join(','); });
		</script>
	</body></html>`
	live := openFixture(t, New(), src, image.Rect(0, 0, 400, 300))
	root := live.Document().Root
	a, b := findByID(root, "a"), findByID(root, "b")

	ctx := context.Background()
	if _, _, err := live.Focus(ctx, a); err != nil {
		t.Fatalf("Focus(a): %v", err)
	}
	if _, _, err := live.Type(ctx, a, "x"); err != nil {
		t.Fatalf("Type(a): %v", err)
	}
	// Moving focus to b must fire blur/focusout/change on a (value changed).
	if _, _, err := live.Focus(ctx, b); err != nil {
		t.Fatalf("Focus(b): %v", err)
	}

	if got := a.Attr["value"]; got != "x" {
		t.Fatalf("a.value = %q, want %q", got, "x")
	}
	if got := dom.TextContent(findByID(live.Document().Root, "log")); got != "blur,focusout,change" {
		t.Fatalf("blur/focusout/change log = %q, want %q", got, "blur,focusout,change")
	}
}

// TestBlurWithNoFocusIsNoop guards the trivial case (nothing focused yet)
// used implicitly whenever the very first Focus of a session runs.
func TestBlurWithNoFocusIsNoop(t *testing.T) {
	live := openFixture(t, New(), `<html><body><input id="a"></body></html>`, image.Rect(0, 0, 200, 200))
	if _, _, err := live.Blur(context.Background()); err != nil {
		t.Fatalf("Blur with nothing focused: %v", err)
	}
}

// TestTypeOnAttributelessNode covers a node with no Attr map at all (dom.Node's
// zero value, as opposed to one the HTML parser gave an empty-but-non-nil
// map) — Type must allocate it rather than panic on a nil-map write.
func TestTypeOnAttributelessNode(t *testing.T) {
	live := openFixture(t, New(), `<html><body><div id="host"></div></body></html>`, image.Rect(0, 0, 200, 200))
	host := findByID(live.Document().Root, "host")
	bare := &dom.Node{Type: dom.Element, Tag: "input"} // Attr is nil
	dom.AppendChild(host, bare)

	if _, _, err := live.Type(context.Background(), bare, "x"); err != nil {
		t.Fatalf("Type on an attributeless node: %v", err)
	}
	if got := bare.Attr["value"]; got != "x" {
		t.Fatalf("value = %q, want %q", got, "x")
	}
}

// TestTypeAfterCloseReturnsErrClosed covers Type's mid-loop error return —
// a caller typing multiple characters must stop cleanly if the document
// closes partway (e.g. the host window closing mid-flow).
func TestTypeAfterCloseReturnsErrClosed(t *testing.T) {
	live := openFixture(t, New(), `<html><body><input id="a"></body></html>`, image.Rect(0, 0, 200, 200))
	a := findByID(live.Document().Root, "a")
	live.Close()

	if _, _, err := live.Type(context.Background(), a, "x"); err != ErrClosed {
		t.Fatalf("Type after Close: err = %v, want ErrClosed", err)
	}
}

// TestBackspaceRemovesLastCharacter is the basic real-editing proof: type a
// typo, fix it with Backspace, confirm the corrected value — the flow a
// login window's user actually performs, not just forward-only typing.
func TestBackspaceRemovesLastCharacter(t *testing.T) {
	live := openFixture(t, New(), `<html><body><input id="a"></body></html>`, image.Rect(0, 0, 200, 200))
	a := findByID(live.Document().Root, "a")
	ctx := context.Background()

	if _, _, err := live.Type(ctx, a, "helloo"); err != nil {
		t.Fatalf("Type: %v", err)
	}
	if _, _, err := live.Backspace(ctx, a); err != nil {
		t.Fatalf("Backspace: %v", err)
	}
	if got := a.Attr["value"]; got != "hello" {
		t.Fatalf("value after Backspace = %q, want %q", got, "hello")
	}
}

// TestBackspaceOnEmptyValueIsNoop covers the boundary: Backspace on an
// already-empty field must not panic or produce a negative-length slice.
func TestBackspaceOnEmptyValueIsNoop(t *testing.T) {
	live := openFixture(t, New(), `<html><body><input id="a"></body></html>`, image.Rect(0, 0, 200, 200))
	a := findByID(live.Document().Root, "a")
	if _, _, err := live.Backspace(context.Background(), a); err != nil {
		t.Fatalf("Backspace on empty: %v", err)
	}
	if got := a.Attr["value"]; got != "" {
		t.Fatalf("value = %q, want empty", got)
	}
}

// TestBackspaceHonorsPreventDefault covers the same character-filtering
// pattern Type's own test suite covers for typing: a script that calls
// preventDefault() on keydown blocks the deletion too.
func TestBackspaceHonorsPreventDefault(t *testing.T) {
	const src = `<html><body><input id="a" value="locked">
		<script>
			document.getElementById('a').addEventListener('keydown', function(e){ e.preventDefault(); });
		</script>
	</body></html>`
	live := openFixture(t, New(), src, image.Rect(0, 0, 200, 200))
	a := findByID(live.Document().Root, "a")
	if _, _, err := live.Backspace(context.Background(), a); err != nil {
		t.Fatalf("Backspace: %v", err)
	}
	if got := a.Attr["value"]; got != "locked" {
		t.Fatalf("value = %q, want unchanged %q (keydown was prevented)", got, "locked")
	}
}

// TestKeyDownReachesPageScriptAndReportsPrevented covers the Enter-to-submit
// pattern a real login form commonly uses: the page's own keydown listener
// sees the named key and can prevent its default.
func TestKeyDownReachesPageScriptAndReportsPrevented(t *testing.T) {
	const src = `<html><body><input id="a">
		<div id="log">none</div>
		<script>
			document.getElementById('a').addEventListener('keydown', function(e){
				if (e.key === 'Enter') {
					e.preventDefault();
					document.getElementById('log').textContent = 'submitted';
				}
			});
		</script>
	</body></html>`
	live := openFixture(t, New(), src, image.Rect(0, 0, 200, 200))
	a := findByID(live.Document().Root, "a")

	prevented, _, _, err := live.KeyDown(context.Background(), a, "Enter")
	if err != nil {
		t.Fatalf("KeyDown: %v", err)
	}
	if !prevented {
		t.Fatal("KeyDown(Enter): want defaultPrevented=true")
	}
	if got := dom.TextContent(findByID(live.Document().Root, "log")); got != "submitted" {
		t.Fatalf("log = %q, want %q", got, "submitted")
	}
}

// TestKeyDownAfterCloseReturnsErrClosed mirrors Type's own Close guard.
func TestKeyDownAfterCloseReturnsErrClosed(t *testing.T) {
	live := openFixture(t, New(), `<html><body><input id="a"></body></html>`, image.Rect(0, 0, 200, 200))
	a := findByID(live.Document().Root, "a")
	live.Close()
	if _, _, _, err := live.KeyDown(context.Background(), a, "Enter"); err != ErrClosed {
		t.Fatalf("KeyDown after Close: err = %v, want ErrClosed", err)
	}
}
