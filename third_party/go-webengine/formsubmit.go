// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

// This file is the fallback path for a <form> whose submission ISN'T
// intercepted by the page's own JS: LiveDocument.Submit performs the real
// GET/POST a browser would, exactly like clicking a plain server-rendered
// form's submit button. A page that DOES handle submit with fetch()/XHR
// (the common SPA login shape) never reaches this — Submit checks
// defaultPrevented first and gets out of the way.
package engine

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/js"
)

// Submit dispatches "submit" on form through the live session first (a real
// browser always does, even for a plain HTML form) and reports whether that
// navigated. If a listener called preventDefault() — the common SPA case,
// already reachable via Focus/Type/Click plus the engine's existing
// fetch()/XHR support — nothing further happens: navigated is false, next
// is nil, and d is still the current page. Otherwise Submit performs the
// real request per the form's action/method (enctype is always
// application/x-www-form-urlencoded/GET-querystring; multipart file upload
// is out of scope — not a shape a login form takes), parses the response as
// a new page, and returns it as a freshly Open'd *LiveDocument — d itself is
// Closed, matching how a real navigation ends the previous page's JS. The
// caller must switch to using next once navigated is true.
func (d *LiveDocument) Submit(ctx context.Context, form *dom.Node) (next *LiveDocument, navigated bool, err error) {
	if d.closed {
		return nil, false, ErrClosed
	}
	prevented := d.sess.Dispatch(form, "submit", js.EventInit{Bubbles: true, Cancelable: true})
	// A submit that reached a script listener still needs a resettle+frame
	// (the handler may have mutated the DOM, e.g. showing a spinner) even
	// when native submission does not follow.
	if prevented {
		d.resettle(ctx)
		return nil, false, nil
	}

	target := formAction(d.doc.URL, form)
	method := strings.ToUpper(strings.TrimSpace(form.Attr["method"]))
	data := formData(form)

	var resp *Document
	if method == "POST" {
		resp, err = d.e.postForm(ctx, target, data)
	} else {
		resp, err = d.e.Fetch(ctx, appendQuery(target, data))
	}
	if err != nil {
		return nil, false, err
	}

	// OpenDocument never actually fails (it has no fallible step of its own
	// once resp already exists) — its error return exists for a future
	// phase, not today, so nothing here pretends otherwise.
	next, _ = d.e.OpenDocument(ctx, resp, d.viewport)
	d.Close()
	return next, true, nil
}

// formAction resolves the form's action against pageURL, defaulting to
// pageURL itself when action is absent or empty — a form with no action
// attribute submits back to the page it's on, per spec.
func formAction(pageURL string, form *dom.Node) string {
	raw := strings.TrimSpace(form.Attr["action"])
	if raw == "" {
		return pageURL
	}
	if resolved, ok := resolveURL(pageURL, raw); ok {
		return resolved
	}
	return pageURL
}

// formData gathers name/value pairs from form's control descendants, in
// document order: input/textarea/select with a non-empty name, skipping
// disabled controls and unchecked checkboxes/radios. This is what a real
// browser submits — button/submit/reset/image inputs are excluded (only the
// one actually clicked would be included, and Submit is not tied to a
// specific button).
func formData(form *dom.Node) url.Values {
	vals := url.Values{}
	var walk func(n *dom.Node)
	walk = func(n *dom.Node) {
		for _, c := range n.Children {
			if c.Type == dom.Element {
				controlValue(c, vals)
			}
			walk(c)
		}
	}
	walk(form)
	return vals
}

// controlValue adds c's contribution to vals if it is a submittable, named,
// enabled, "checked if applicable" control, reporting whether it did.
func controlValue(c *dom.Node, vals url.Values) bool {
	name := strings.TrimSpace(c.Attr["name"])
	if name == "" {
		return false
	}
	if _, disabled := c.Attribute("disabled"); disabled {
		return false
	}
	switch c.Tag {
	case "input":
		typ := strings.ToLower(c.Attr["type"])
		switch typ {
		case "checkbox", "radio":
			if _, checked := c.Attribute("checked"); !checked {
				return false
			}
			v := c.Attr["value"]
			if v == "" {
				v = "on"
			}
			vals.Add(name, v)
		case "button", "submit", "reset", "image", "file":
			return false
		default:
			vals.Add(name, c.Attr["value"])
		}
		return true
	case "textarea":
		// A real browser's textarea.value defaults to its text content until
		// something SETS the value; this engine's generic value accessor
		// (js/dom.go) does not special-case textarea, so an untouched one
		// carries no "value" attribute at all — fall back to the text.
		if v, ok := c.Attribute("value"); ok {
			vals.Add(name, v)
		} else {
			vals.Add(name, dom.TextContent(c))
		}
		return true
	case "select":
		if v, ok := selectedOptionValue(c); ok {
			vals.Add(name, v)
		}
		return true
	}
	return false
}

// selectedOptionValue returns the submitted value of sel's selected <option>
// (its value attribute, or its text content if that attribute is absent —
// same fallback a real browser applies), or false if sel has no options.
func selectedOptionValue(sel *dom.Node) (string, bool) {
	var opts []*dom.Node
	var walk func(n *dom.Node)
	walk = func(n *dom.Node) {
		for _, c := range n.Children {
			if c.Type == dom.Element && c.Tag == "option" {
				opts = append(opts, c)
			}
			walk(c)
		}
	}
	walk(sel)
	if len(opts) == 0 {
		return "", false
	}
	chosen := opts[0]
	for _, o := range opts {
		if _, ok := o.Attribute("selected"); ok {
			chosen = o
			break
		}
	}
	if v, ok := chosen.Attribute("value"); ok {
		return v, true
	}
	return dom.TextContent(chosen), true
}

// appendQuery appends data as target's query string (a GET form submission),
// replacing any query target already had — matching real browser behavior
// (a GET form's own action query string, if any, is discarded).
func appendQuery(target string, data url.Values) string {
	u, err := url.Parse(target)
	if err != nil {
		return target
	}
	u.RawQuery = data.Encode()
	return u.String()
}

// postForm performs a real application/x-www-form-urlencoded POST and parses
// the response the same way Fetch parses a GET response.
func (e *Engine) postForm(ctx context.Context, target string, data url.Values) (*Document, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if e.UserAgent != "" {
		req.Header.Set("User-Agent", e.UserAgent)
	}
	resp, err := e.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	// Same 32MB bound doGet applies to a normal page fetch (engine.go).
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	ctype := resp.Header.Get("Content-Type")
	utf8, err := decodeCharset(body, ctype)
	if err != nil {
		utf8 = body
	}
	root, err := dom.Parse(string(utf8))
	if err != nil {
		return nil, err
	}
	finalURL := target
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	return &Document{URL: finalURL, Title: dom.Title(root), Root: root, HTML: string(utf8)}, nil
}
