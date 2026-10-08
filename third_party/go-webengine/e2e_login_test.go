// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"
	"image"
	"testing"

	"github.com/go-webengine/engine/dom"
)

// TestEndToEndLoginByCoordinate is the capstone proof for the whole
// interactive-form-input effort (F0-F5, plus the form-control layout/paint
// work that turned out to be a prerequisite): a login flow driven by
// NOTHING but pixel coordinates — the way a real consumer actually drives
// one (a host translating a user's click/tap into a point, exactly what
// go-aiquota/tray's embedded onboarding window and browserproxy's own
// click forwarding both do), never a direct *dom.Node reference. It
// exercises the full chain in one place: Open renders the page and the
// element index (F2) is what resolves each point to the right control —
// which requires the control to actually HAVE a box (layout.go's
// isFormControlTag branch; without it #email/#password/#submit are
// invisible and unresolvable, which is exactly what this test caught
// before that existed); Focus/Type/Click (F3) dispatch through the
// persistent session (F0) so the page's own JS state survives every one of
// the many separate calls this makes; the final click's handler flips a
// JS-side "authenticated" flag that only a correct email+password pair
// sets, and that flag — not just the DOM's raw attribute values — is what
// the test checks.
func TestEndToEndLoginByCoordinate(t *testing.T) {
	const src = `<html><body>
		<div style="padding:20px">
			<label>Email</label><br>
			<input id="email" style="width:200px;height:24px"><br><br>
			<label>Password</label><br>
			<input id="password" type="password" style="width:200px;height:24px"><br><br>
			<button id="submit" style="width:100px;height:30px">Log in</button>
		</div>
		<div id="result">pending</div>
		<script>
			var authenticated = false;
			document.getElementById('submit').addEventListener('click', function(e){
				e.preventDefault();
				var email = document.getElementById('email').value;
				var password = document.getElementById('password').value;
				authenticated = (email === 'agent@go-aiquota.test' && password === 'correct-horse-battery-staple');
				document.getElementById('result').textContent = authenticated ? 'authenticated' : 'rejected';
			});
		</script>
	</body></html>`

	live := openFixture(t, New(), src, image.Rect(0, 0, 400, 400))
	ctx := context.Background()

	// Locate each control the way a real host would: read its rect off the
	// CURRENT element index, not by holding onto the *dom.Node identity a
	// prior lookup happened to return.
	pointOf := func(id string) image.Point {
		n := findByID(live.Document().Root, id)
		if n == nil {
			t.Fatalf("fixture element #%s not found", id)
		}
		r, ok := live.Elements()[n]
		if !ok {
			t.Fatalf("#%s missing from the element index", id)
		}
		return center(r)
	}

	emailPt := pointOf("email")
	passwordPt := pointOf("password")
	submitPt := pointOf("submit")

	resolve := func(pt image.Point) *dom.Node {
		n, ok := live.ElementAt(pt)
		if !ok {
			t.Fatalf("ElementAt(%v): no element resolved", pt)
		}
		return n
	}

	emailField := resolve(emailPt)
	if emailField.Attr["id"] != "email" {
		t.Fatalf("ElementAt(email point) resolved to #%s, want #email", emailField.Attr["id"])
	}
	if _, _, err := live.Focus(ctx, emailField); err != nil {
		t.Fatalf("Focus(email): %v", err)
	}
	if _, _, err := live.Type(ctx, emailField, "agent@go-aiquota.test"); err != nil {
		t.Fatalf("Type(email): %v", err)
	}

	passwordField := resolve(passwordPt)
	if passwordField.Attr["id"] != "password" {
		t.Fatalf("ElementAt(password point) resolved to #%s, want #password", passwordField.Attr["id"])
	}
	if _, _, err := live.Focus(ctx, passwordField); err != nil {
		t.Fatalf("Focus(password): %v", err)
	}
	if _, _, err := live.Type(ctx, passwordField, "correct-horse-battery-staple"); err != nil {
		t.Fatalf("Type(password): %v", err)
	}

	submitBtn := resolve(submitPt)
	if submitBtn.Attr["id"] != "submit" {
		t.Fatalf("ElementAt(submit point) resolved to #%s, want #submit", submitBtn.Attr["id"])
	}
	prevented, img, _, err := live.Click(ctx, submitBtn)
	if err != nil {
		t.Fatalf("Click(submit): %v", err)
	}
	if !prevented {
		t.Fatal("Click(submit): want defaultPrevented=true")
	}
	if img == nil {
		t.Fatal("Click(submit): want a non-nil frame")
	}

	result := findByID(live.Document().Root, "result")
	if got := dom.TextContent(result); got != "authenticated" {
		t.Fatalf("final result = %q, want %q — a coordinate-only-driven login did not reach the page's JS correctly", got, "authenticated")
	}
}

// TestEndToEndLoginByCoordinate_WrongPassword is the negative control: the
// SAME coordinate-driven flow with a wrong password must NOT authenticate
// — proof this isn't a test that would pass no matter what was typed.
func TestEndToEndLoginByCoordinate_WrongPassword(t *testing.T) {
	const src = `<html><body>
		<input id="email" style="width:200px;height:24px">
		<input id="password" type="password" style="width:200px;height:24px">
		<button id="submit" style="width:100px;height:30px">Log in</button>
		<div id="result">pending</div>
		<script>
			document.getElementById('submit').addEventListener('click', function(e){
				e.preventDefault();
				var ok = document.getElementById('email').value === 'agent@go-aiquota.test' &&
					document.getElementById('password').value === 'correct-horse-battery-staple';
				document.getElementById('result').textContent = ok ? 'authenticated' : 'rejected';
			});
		</script>
	</body></html>`
	live := openFixture(t, New(), src, image.Rect(0, 0, 400, 200))
	ctx := context.Background()

	email := findByID(live.Document().Root, "email")
	password := findByID(live.Document().Root, "password")
	submit := findByID(live.Document().Root, "submit")

	live.Focus(ctx, email)
	live.Type(ctx, email, "agent@go-aiquota.test")
	live.Focus(ctx, password)
	live.Type(ctx, password, "wrong-password")
	live.Click(ctx, submit)

	if got := dom.TextContent(findByID(live.Document().Root, "result")); got != "rejected" {
		t.Fatalf("result with a wrong password = %q, want %q", got, "rejected")
	}
}
