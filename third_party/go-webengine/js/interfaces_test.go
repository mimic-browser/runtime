// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package js

import (
	"strings"
	"testing"

	"github.com/go-webengine/engine/dom"
)

// evalLog runs a script body inside the standard test page and returns the
// console output. Scripts report results via console.log("KEY=" + value).
func evalLog(t *testing.T, body string) []string {
	t.Helper()
	_, logs, _ := runJS(t, page(body))
	return logs
}

// wantLog asserts a "log: <line>" was produced.
func wantLog(t *testing.T, logs []string, line string) {
	t.Helper()
	if !has(logs, "log: "+line) {
		t.Errorf("missing console output %q; got: %v", line, logs)
	}
}

func TestInterfacesInstanceOf(t *testing.T) {
	logs := evalLog(t, `
		var d = document.createElement('div');
		console.log('divHTMLElement=' + (d instanceof HTMLElement));
		console.log('divElement=' + (d instanceof Element));
		console.log('divNode=' + (d instanceof Node));
		console.log('divEventTarget=' + (d instanceof EventTarget));
		console.log('divDivElement=' + (d instanceof HTMLDivElement));
		console.log('spanSpanElement=' + (document.createElement('span') instanceof HTMLSpanElement));
		console.log('textIsText=' + (document.createTextNode('x') instanceof Text));
		console.log('textIsCharData=' + (document.createTextNode('x') instanceof CharacterData));
		console.log('docIsDocument=' + (document instanceof Document));
		console.log('docIsNode=' + (document instanceof Node));
		console.log('bodyIsHTMLElement=' + (document.body instanceof HTMLElement));
	`)
	for _, k := range []string{
		"divHTMLElement=true", "divElement=true", "divNode=true", "divEventTarget=true",
		"divDivElement=true", "spanSpanElement=true", "textIsText=true", "textIsCharData=true",
		"docIsDocument=true", "docIsNode=true", "bodyIsHTMLElement=true",
	} {
		wantLog(t, logs, k)
	}
}

func TestInterfacesNodeConstants(t *testing.T) {
	logs := evalLog(t, `
		console.log('elemConst=' + Node.ELEMENT_NODE);
		console.log('textConst=' + Node.TEXT_NODE);
		console.log('instConst=' + document.body.ELEMENT_NODE);
	`)
	wantLog(t, logs, "elemConst=1")
	wantLog(t, logs, "textConst=3")
	wantLog(t, logs, "instConst=1")
}

func TestInterfacesSubclassHTMLElement(t *testing.T) {
	// core-js / React-DOM subclass HTMLElement and set .prototype members. The
	// subclass instance must be instanceof the whole chain, and prototype writes
	// must take effect.
	logs := evalLog(t, `
		class Widget extends HTMLElement {
			constructor() { super(); this.built = true; }
		}
		Widget.prototype.kind = 'widget';
		var w = new Widget();
		console.log('wWidget=' + (w instanceof Widget));
		console.log('wHTMLElement=' + (w instanceof HTMLElement));
		console.log('wNode=' + (w instanceof Node));
		console.log('wBuilt=' + (w.built === true));
		console.log('wKind=' + w.kind);
	`)
	for _, k := range []string{"wWidget=true", "wHTMLElement=true", "wNode=true", "wBuilt=true", "wKind=widget"} {
		wantLog(t, logs, k)
	}
}

func TestInterfacesSubclassMutatesDOM(t *testing.T) {
	// A subclass method that mutates the real document must write through to the
	// shared dom tree (the point of subclassable interfaces: they still drive the
	// same node model).
	root, logs, _ := runJS(t, page(`
		class Widget extends HTMLElement {
			render(txt) {
				var p = document.createElement('p');
				p.textContent = txt;
				document.body.appendChild(p);
			}
		}
		var w = new Widget();
		w.render('HELLO_FROM_SUBCLASS');
		console.log('done=1');
	`))
	wantLog(t, logs, "done=1")
	body := dom.Find(root, "body")
	if body == nil || !strings.Contains(dom.TextContent(body), "HELLO_FROM_SUBCLASS") {
		t.Fatalf("subclass DOM mutation did not write through; body=%q", dom.TextContent(body))
	}
}

func TestDOMException(t *testing.T) {
	logs := evalLog(t, `
		var e = new DOMException('boom', 'NotFoundError');
		console.log('name=' + e.name);
		console.log('message=' + e.message);
		console.log('code=' + e.code);
		console.log('isException=' + (e instanceof DOMException));
		console.log('str=' + e.toString());
		var d = new DOMException('m', 'SyntaxError');
		console.log('syntaxCode=' + d.code);
		var def = new DOMException('only-message');
		console.log('defName=' + def.name);
		console.log('defCode=' + def.code);
		console.log('protoExists=' + (typeof DOMException.prototype));
	`)
	for _, k := range []string{
		"name=NotFoundError", "message=boom", "code=8", "isException=true",
		"str=NotFoundError: boom", "syntaxCode=12", "defName=Error", "defCode=0",
		"protoExists=object",
	} {
		wantLog(t, logs, k)
	}
}

func TestEventConstructors(t *testing.T) {
	logs := evalLog(t, `
		var e = new Event('click', { bubbles: true, cancelable: true });
		console.log('type=' + e.type);
		console.log('bubbles=' + e.bubbles);
		console.log('isEvent=' + (e instanceof Event));
		var ce = new CustomEvent('ping', { detail: { n: 7 } });
		console.log('ceType=' + ce.type);
		console.log('ceDetail=' + ce.detail.n);
		console.log('ceIsEvent=' + (ce instanceof Event));
		var me = new MouseEvent('mousedown', { clientX: 10, clientY: 20, button: 1 });
		console.log('meX=' + me.clientX);
		console.log('meIsMouse=' + (me instanceof MouseEvent));
		console.log('meIsUI=' + (me instanceof UIEvent));
		console.log('meIsEvent=' + (me instanceof Event));
		var ke = new KeyboardEvent('keydown', { key: 'Enter', keyCode: 13 });
		console.log('keKey=' + ke.key);
		console.log('keCode=' + ke.keyCode);
	`)
	for _, k := range []string{
		"type=click", "bubbles=true", "isEvent=true", "ceType=ping", "ceDetail=7",
		"ceIsEvent=true", "meX=10", "meIsMouse=true", "meIsUI=true", "meIsEvent=true",
		"keKey=Enter", "keCode=13",
	} {
		wantLog(t, logs, k)
	}
}

func TestEventSubclassDispatch(t *testing.T) {
	// A subclassed Event dispatched through a real element must reach a listener,
	// carrying the subclass fields.
	root, logs, _ := runJS(t, page(`
		class MyEvent extends CustomEvent {}
		var el = document.getElementById('d');
		var got = null;
		el.addEventListener('boom', function(ev){ got = ev; });
		var e = new MyEvent('boom', { detail: 42 });
		console.log('isMy=' + (e instanceof MyEvent));
		console.log('isCustom=' + (e instanceof CustomEvent));
		el.dispatchEvent(e);
		console.log('gotDetail=' + (got && got.detail));
	`))
	_ = root
	for _, k := range []string{"isMy=true", "isCustom=true", "gotDetail=42"} {
		wantLog(t, logs, k)
	}
}

func TestOwnerDocument(t *testing.T) {
	logs := evalLog(t, `
		console.log('elemOwner=' + (document.body.ownerDocument === document));
		console.log('createdOwner=' + (document.createElement('div').ownerDocument === document));
		console.log('textOwner=' + (document.createTextNode('x').ownerDocument === document));
	`)
	for _, k := range []string{"elemOwner=true", "createdOwner=true", "textOwner=true"} {
		wantLog(t, logs, k)
	}
}

func TestAtobBtoa(t *testing.T) {
	logs := evalLog(t, `
		console.log('enc=' + btoa('hello'));
		console.log('dec=' + atob('aGVsbG8='));
		console.log('roundtrip=' + (atob(btoa('Round Trip!')) === 'Round Trip!'));
	`)
	wantLog(t, logs, "enc=aGVsbG8=")
	wantLog(t, logs, "dec=hello")
	wantLog(t, logs, "roundtrip=true")
}
