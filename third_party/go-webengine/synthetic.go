// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

// This file is the actual "drive a login form" API: Focus/Blur/Type/Click
// synthesize the DOM events a real keystroke or mouse click produces,
// dispatched through the LiveDocument's persistent js.Session (so a
// stateful script — a controlled React input, say — sees exactly the
// sequence a real browser would deliver), each followed by a resettle so
// the returned frame reflects whatever the interaction changed. Built on
// js.Session.Dispatch (the event-firing seam) and ElementAt (elements.go,
// the click-coordinate-to-node resolver) landed in earlier phases.
package engine

import (
	"context"
	"image"

	"github.com/go-webengine/engine/dom"
	"github.com/go-webengine/engine/js"
)

// Focus moves input focus to n: blurs whatever was focused before (firing
// blur/focusout and, if its value changed since ITS OWN focus, change —
// same rule Blur uses), then fires focus/focusin on n and records n's
// current value as the baseline for its own eventual blur. A page relies on
// focus to know which control a subsequent Type lands in — this models
// that explicitly rather than an implicit "last node touched" guess.
func (d *LiveDocument) Focus(ctx context.Context, n *dom.Node) (*image.RGBA, *RenderInfo, error) {
	return d.Interact(ctx, func() { d.focus(n) })
}

// focus is Focus's mutation, factored out so Type and Click can ensure
// focus within their OWN Interact call instead of nesting one.
func (d *LiveDocument) focus(n *dom.Node) {
	if d.focused == n {
		return
	}
	if d.focused != nil {
		d.blur()
	}
	d.focused = n
	d.focusedValue = n.Attr["value"]
	d.sess.Dispatch(n, "focus", js.EventInit{})
	d.sess.Dispatch(n, "focusin", js.EventInit{Bubbles: true})
}

// Blur removes focus from whatever is currently focused (a no-op if
// nothing is). Firing it explicitly matters for a form that validates or
// submits-on-blur, and for the "did this field's value change" signal
// change carries.
func (d *LiveDocument) Blur(ctx context.Context) (*image.RGBA, *RenderInfo, error) {
	return d.Interact(ctx, func() { d.blur() })
}

// blur is Blur's mutation, and Focus's own step when moving focus away from
// a previously-focused node.
func (d *LiveDocument) blur() {
	if d.focused == nil {
		return
	}
	n := d.focused
	d.focused = nil
	d.sess.Dispatch(n, "blur", js.EventInit{})
	d.sess.Dispatch(n, "focusout", js.EventInit{Bubbles: true})
	if n.Attr["value"] != d.focusedValue {
		d.sess.Dispatch(n, "change", js.EventInit{Bubbles: true})
	}
}

// Type synthesizes typing text into n, one rune at a time: keydown, then —
// unless a listener called preventDefault() on it (a real page's
// character-filtering validator, say) — n's value gains that character and
// "input" fires, then keyup always fires. Firing one full keydown/input/
// keyup triple per character (not one bulk value assignment) is what lets a
// controlled React/Vue onChange see every keystroke, exactly as a real user
// typing would trigger it. Ensures n is focused first (moving focus there,
// with the normal blur/change fallout on whatever was focused before, if
// anything) — matching real usage: you cannot type into an unfocused field.
func (d *LiveDocument) Type(ctx context.Context, n *dom.Node, text string) (img *image.RGBA, info *RenderInfo, err error) {
	for _, r := range text {
		key := string(r)
		img, info, err = d.Interact(ctx, func() {
			d.focus(n)
			prevented := d.sess.Dispatch(n, "keydown", js.EventInit{Bubbles: true, Cancelable: true, Key: key})
			if !prevented {
				if n.Attr == nil {
					n.Attr = map[string]string{}
				}
				n.Attr["value"] += key
				d.sess.Dispatch(n, "input", js.EventInit{Bubbles: true, Data: key, InputType: "insertText"})
			}
			d.sess.Dispatch(n, "keyup", js.EventInit{Bubbles: true, Cancelable: true, Key: key})
		})
		if err != nil {
			return
		}
	}
	return
}

// Backspace removes the last character of n's value (a no-op if already
// empty), firing keydown("Backspace")→[if not prevented]delete+input→keyup
// — the same event shape Type's per-character typing uses, since a real
// browser fires exactly this sequence for a Backspace keystroke too.
// Ensures n is focused first, same as Type.
func (d *LiveDocument) Backspace(ctx context.Context, n *dom.Node) (*image.RGBA, *RenderInfo, error) {
	return d.Interact(ctx, func() {
		d.focus(n)
		prevented := d.sess.Dispatch(n, "keydown", js.EventInit{Bubbles: true, Cancelable: true, Key: "Backspace"})
		if !prevented {
			if v := []rune(n.Attr["value"]); len(v) > 0 {
				n.Attr["value"] = string(v[:len(v)-1])
				d.sess.Dispatch(n, "input", js.EventInit{Bubbles: true, InputType: "deleteContentBackward"})
			}
		}
		d.sess.Dispatch(n, "keyup", js.EventInit{Bubbles: true, Cancelable: true, Key: "Backspace"})
	})
}

// KeyDown fires a bare keydown+keyup pair for a NAMED key (e.g. "Enter",
// "Tab", "Escape") that, unlike a printable character or Backspace, this
// package does not itself interpret — it never mutates n's value or moves
// focus on the caller's behalf. It exists for the common real case of a
// login form whose OWN script listens for Enter on keydown to submit
// itself (dispatchEvent already lets that handler run and call
// preventDefault — see the reported bool); a host wanting real Tab-order
// traversal or an implicit Enter-submits-the-enclosing-form fallback
// implements that itself, on top of this, since neither is modeled here.
func (d *LiveDocument) KeyDown(ctx context.Context, n *dom.Node, key string) (defaultPrevented bool, img *image.RGBA, info *RenderInfo, err error) {
	img, info, err = d.Interact(ctx, func() {
		defaultPrevented = d.sess.Dispatch(n, "keydown", js.EventInit{Bubbles: true, Cancelable: true, Key: key})
		d.sess.Dispatch(n, "keyup", js.EventInit{Bubbles: true, Cancelable: true, Key: key})
	})
	return
}

// Click synthesizes a real user click on n: mousedown, mouseup, then click
// — the sequence a page's onclick/addEventListener('click', …) expects,
// bubbling per real semantics (so a handler on a container, not just the
// clicked element itself, sees it too). Also moves focus to n first for a
// focusable control (an input, say), matching a real browser: clicking a
// field focuses it before the click event itself fires. It reports whether
// a listener called preventDefault() on the click, so a caller driving a
// submit button (a later phase) can tell whether the page's own script
// handled it (a fetch()-based login, say) rather than letting native
// form submission happen too.
func (d *LiveDocument) Click(ctx context.Context, n *dom.Node) (defaultPrevented bool, img *image.RGBA, info *RenderInfo, err error) {
	img, info, err = d.Interact(ctx, func() {
		d.focus(n)
		d.sess.Dispatch(n, "mousedown", js.EventInit{Bubbles: true, Cancelable: true})
		d.sess.Dispatch(n, "mouseup", js.EventInit{Bubbles: true, Cancelable: true})
		defaultPrevented = d.sess.Dispatch(n, "click", js.EventInit{Bubbles: true, Cancelable: true})
	})
	return
}
