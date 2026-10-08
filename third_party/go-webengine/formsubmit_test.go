// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"
	"image"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/go-webengine/engine/dom"
)

// TestSubmitPOST is the decisive end-to-end proof for the plain-form path
// (no JS submit handler at all): typed field values reach a real POST
// request and the resulting page replaces the LiveDocument.
func TestSubmitPOST(t *testing.T) {
	var gotMethod, gotEmail, gotPassword string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		_ = r.ParseForm()
		gotEmail = r.PostForm.Get("email")
		gotPassword = r.PostForm.Get("password")
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>Welcome</title></head><body>ok</body></html>`))
	}))
	defer srv.Close()

	live := openFixture(t, New(), `<html><body>
		<form id="f" method="POST" action="/login">
			<input name="email" value="user@test.com">
			<input name="password" type="password" value="hunter2">
		</form>
	</body></html>`, image.Rect(0, 0, 200, 200))
	live.doc.URL = srv.URL + "/"

	form := findByID(live.Document().Root, "f")
	next, navigated, err := live.Submit(context.Background(), form)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if !navigated {
		t.Fatal("Submit: want navigated=true (no JS intercepted the submit)")
	}
	if next == nil {
		t.Fatal("Submit: want a non-nil next LiveDocument")
	}
	t.Cleanup(next.Close)

	if gotMethod != http.MethodPost {
		t.Fatalf("server saw method %q, want POST", gotMethod)
	}
	if gotEmail != "user@test.com" || gotPassword != "hunter2" {
		t.Fatalf("server saw email=%q password=%q", gotEmail, gotPassword)
	}
	if _, _, err := live.Frame(); err != ErrClosed {
		t.Fatalf("old LiveDocument after Submit: Frame err = %v, want ErrClosed (it should be Closed)", err)
	}
	if next.Document().Title != "Welcome" {
		t.Fatalf("next.Document().Title = %q, want %q", next.Document().Title, "Welcome")
	}
}

// TestSubmitGETAppendsQuery covers the default method (GET) and that field
// values land in the query string, not a body.
func TestSubmitGETAppendsQuery(t *testing.T) {
	var gotMethod, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body>ok</body></html>`))
	}))
	defer srv.Close()

	live := openFixture(t, New(), `<html><body>
		<form id="f" action="/search"><input name="q" value="hello world"></form>
	</body></html>`, image.Rect(0, 0, 200, 200))
	live.doc.URL = srv.URL + "/"

	form := findByID(live.Document().Root, "f")
	next, navigated, err := live.Submit(context.Background(), form)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if !navigated || next == nil {
		t.Fatal("Submit: want a navigation")
	}
	t.Cleanup(next.Close)

	if gotMethod != http.MethodGet {
		t.Fatalf("server saw method %q, want GET", gotMethod)
	}
	if gotQuery != "q=hello+world" {
		t.Fatalf("server saw query %q, want %q", gotQuery, "q=hello+world")
	}
}

// TestSubmitSkippedWhenJSPreventsDefault covers the SPA case: a script's
// own submit listener wins, no request is made, and the CURRENT document
// stays open (not replaced).
func TestSubmitSkippedWhenJSPreventsDefault(t *testing.T) {
	requested := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = true
	}))
	defer srv.Close()

	live := openFixture(t, New(), `<html><body>
		<form id="f" action="/login" method="POST"><input name="x" value="1"></form>
		<script>
			document.getElementById('f').addEventListener('submit', function(e){ e.preventDefault(); });
		</script>
	</body></html>`, image.Rect(0, 0, 200, 200))
	live.doc.URL = srv.URL + "/"

	form := findByID(live.Document().Root, "f")
	next, navigated, err := live.Submit(context.Background(), form)
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if navigated || next != nil {
		t.Fatalf("Submit with preventDefault: navigated=%v next=%v, want false/nil", navigated, next)
	}
	if requested {
		t.Fatal("Submit with preventDefault made a real HTTP request")
	}
	// The original document must still be usable.
	if _, _, err := live.Frame(); err != nil {
		t.Fatalf("original LiveDocument after a prevented Submit: %v", err)
	}
}

// TestSubmitOnClosedDocument covers the trivial guard.
func TestSubmitOnClosedDocument(t *testing.T) {
	live := openFixture(t, New(), `<html><body><form id="f"></form></body></html>`, image.Rect(0, 0, 100, 100))
	form := findByID(live.Document().Root, "f")
	live.Close()
	if _, _, err := live.Submit(context.Background(), form); err != ErrClosed {
		t.Fatalf("Submit after Close: err = %v, want ErrClosed", err)
	}
}

func TestFormDataGathering(t *testing.T) {
	root, err := dom.Parse(`<html><body><form id="f">
		<input name="text1" value="hello">
		<input name="unchecked" type="checkbox" value="yes">
		<input name="checked1" type="checkbox" checked value="yes">
		<input name="checked2" type="checkbox" checked>
		<input name="radioA" type="radio" checked value="a">
		<input name="skip" type="submit" value="Go">
		<input name="disabled1" value="x" disabled>
		<input value="noname">
		<textarea name="bio">hello bio</textarea>
		<textarea name="bio2" value="explicit">ignored fallback text</textarea>
		<select name="color"><option value="r">Red</option><option value="g" selected>Green</option></select>
		<select name="nolabel"><option>Just Text</option></select>
		<select name="empty"></select>
		<button name="submit-btn" value="go">Submit</button>
	</form></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	form := findByID(root, "f")
	data := formData(form)

	cases := map[string]string{
		"text1":    "hello",
		"checked1": "yes",
		"checked2": "on",
		"radioA":   "a",
		"bio":      "hello bio",
		"bio2":     "explicit",
		"color":    "g",
		"nolabel":  "Just Text",
	}
	for k, want := range cases {
		if got := data.Get(k); got != want {
			t.Errorf("data[%q] = %q, want %q", k, got, want)
		}
	}
	for _, absent := range []string{"unchecked", "skip", "disabled1", "empty", "submit-btn"} {
		if data.Has(absent) {
			t.Errorf("data has %q = %q, want absent", absent, data.Get(absent))
		}
	}
}

func TestFormActionDefaultsToPageURL(t *testing.T) {
	root, err := dom.Parse(`<html><body><form id="f"></form></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	form := findByID(root, "f")
	if got := formAction("https://example.test/login", form); got != "https://example.test/login" {
		t.Fatalf("formAction with no action attr = %q, want the page URL", got)
	}
}

// TestFormActionFallsBackToPageURLOnUnresolvableAction covers formAction's
// own fallback when resolveURL rejects the action (malformed percent-escape
// — %zz is not valid hex — makes url.Parse fail).
func TestFormActionFallsBackToPageURLOnUnresolvableAction(t *testing.T) {
	root, err := dom.Parse(`<html><body><form id="f" action="%zz"></form></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	form := findByID(root, "f")
	if got := formAction("https://example.test/login", form); got != "https://example.test/login" {
		t.Fatalf("formAction with an unresolvable action = %q, want the page URL fallback", got)
	}
}

// TestAppendQueryOnUnparseableTarget covers appendQuery's own fallback: an
// unparseable target string is returned as-is rather than panicking.
func TestAppendQueryOnUnparseableTarget(t *testing.T) {
	bad := "http://[::1:not-a-valid-host"
	if got := appendQuery(bad, url.Values{"a": {"1"}}); got != bad {
		t.Fatalf("appendQuery(%q, ...) = %q, want the input returned unchanged", bad, got)
	}
}

// TestSubmitPOSTNetworkError covers Submit's own error path when the
// request itself fails (nothing listening at the target).
func TestSubmitPOSTNetworkError(t *testing.T) {
	live := openFixture(t, New(), `<html><body>
		<form id="f" method="POST" action="/login"><input name="x" value="1"></form>
	</body></html>`, image.Rect(0, 0, 100, 100))
	live.doc.URL = "http://127.0.0.1:1/" // nothing listens on port 1

	form := findByID(live.Document().Root, "f")
	if _, navigated, err := live.Submit(context.Background(), form); err == nil || navigated {
		t.Fatalf("Submit against an unreachable target: navigated=%v err=%v, want an error", navigated, err)
	}
}
