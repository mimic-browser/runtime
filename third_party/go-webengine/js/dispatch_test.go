// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package js

import (
	"testing"
	"time"

	"github.com/go-webengine/engine/dom"
)

// TestSessionDispatchReachesJSListener is the load-bearing proof for the
// Go-facing seam: a listener a page's OWN script registered must fire when
// something OUTSIDE this package (a host synthesizing a real user
// interaction) calls Session.Dispatch, exactly as if the page had called
// element.dispatchEvent() itself.
func TestSessionDispatchReachesJSListener(t *testing.T) {
	const src = `<html><body>
		<div id="parent"><input id="field"></div>
		<script>
			document.getElementById('field').addEventListener('keydown', function(e){
				console.log('keydown key=' + e.key);
			});
			document.getElementById('field').addEventListener('input', function(e){
				console.log('input data=' + e.data + ' type=' + e.inputType);
			});
			document.getElementById('parent').addEventListener('click', function(e){
				console.log('delegated click target=' + e.target.id);
			});
		</script>
	</body></html>`
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	field := dom.Find(root, "input")

	var logs []string
	sess := Begin(root, Options{PageURL: "https://demo.test/", Timeout: 3 * time.Second,
		Log: func(l string) { logs = append(logs, l) }})
	defer sess.Close()
	sess.RunInitial()

	sess.Dispatch(field, "keydown", EventInit{Bubbles: true, Cancelable: true, Key: "a"})
	sess.Dispatch(field, "input", EventInit{Bubbles: true, Data: "a", InputType: "insertText"})
	// Delegated: the listener is on #parent, the event targets #field.
	sess.Dispatch(field, "click", EventInit{Bubbles: true, Cancelable: true})

	mustHaveJS(t, logs, "keydown key=a", "input data=a type=insertText", "delegated click target=field")
}

// TestSessionDispatchReportsPreventDefault is what a native-form-submission
// fallback (a later phase) needs: knowing whether a script intercepted the
// event, without the caller having to inspect the event object itself.
func TestSessionDispatchReportsPreventDefault(t *testing.T) {
	const src = `<html><body><form id="f"></form>
		<script>
			document.getElementById('f').addEventListener('submit', function(e){ e.preventDefault(); });
		</script>
	</body></html>`
	root, err := dom.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	form := dom.Find(root, "form")

	sess := Begin(root, Options{PageURL: "https://demo.test/", Timeout: 3 * time.Second})
	defer sess.Close()
	sess.RunInitial()

	if prevented := sess.Dispatch(form, "submit", EventInit{Bubbles: true, Cancelable: true}); !prevented {
		t.Fatal("Dispatch: want defaultPrevented=true, the listener called preventDefault()")
	}
}

// TestSessionDispatchNoListenersIsSafe covers dispatching to a node with no
// registered listeners at all (a plain <button> nobody wired up) — must not
// panic, and must report defaultPrevented=false.
func TestSessionDispatchNoListenersIsSafe(t *testing.T) {
	root, err := dom.Parse(`<html><body><button id="b">Go</button></body></html>`)
	if err != nil {
		t.Fatal(err)
	}
	btn := dom.Find(root, "button")

	sess := Begin(root, Options{PageURL: "https://demo.test/", Timeout: 3 * time.Second})
	defer sess.Close()
	sess.RunInitial()

	if prevented := sess.Dispatch(btn, "click", EventInit{Bubbles: true}); prevented {
		t.Fatal("Dispatch with no listeners: want defaultPrevented=false")
	}
}
