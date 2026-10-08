// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package js

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dop251/goja"
	"github.com/go-webengine/engine/dom"
)

const testURL = "https://ex.com/a/b?x=1&y=2#frag"

// runJS parses htmlSrc, runs its scripts, and returns the mutated tree, the
// captured console/diagnostic lines, and the Result.
func runJS(t *testing.T, htmlSrc string) (*dom.Node, []string, Result) {
	t.Helper()
	root, err := dom.Parse(htmlSrc)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var logs []string
	res := Run(root, Options{
		PageURL:   testURL,
		UserAgent: "TestUA/1.0",
		Client:    http.DefaultClient,
		Timeout:   3 * time.Second,
		Log:       func(s string) { logs = append(logs, s) },
	})
	return root, logs, res
}

// script wraps a script body in a minimal JS-disabled document.
func page(body string) string {
	return `<html class="client-nojs"><head><title>T</title></head><body>` +
		`<div id="d" class="foo" data-existing="v"></div>` +
		`<input name="nm">` +
		`<script>` + body + `</script></body></html>`
}

// has reports whether any log line contains sub.
func has(logs []string, sub string) bool {
	for _, l := range logs {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

func mustHave(t *testing.T, logs []string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if !has(logs, s) {
			t.Errorf("missing log %q in:\n%s", s, strings.Join(logs, "\n"))
		}
	}
}

func TestClientJSSignalAndCounts(t *testing.T) {
	root, logs, res := runJS(t, page(`console.log('ran')`))
	html := dom.Find(root, "html")
	if html.Attr["class"] != "client-js" {
		t.Fatalf("client-js signal not set: %q", html.Attr["class"])
	}
	if res.ScriptsRun != 1 || res.ScriptsFailed != 0 {
		t.Fatalf("counts: %+v", res)
	}
	mustHave(t, logs, "log: ran")
}

func TestRunDefaultsAndNoHTML(t *testing.T) {
	// A Document with no <html> exercises the setClientJS nil branch and Run's
	// zero-value option defaults (ctx nil, timeout 0, viewport 0).
	root := &dom.Node{Type: dom.Document}
	res := Run(root, Options{})
	if res.ScriptsRun != 0 {
		t.Fatalf("expected no scripts, got %+v", res)
	}
}

func TestRunNilLog(t *testing.T) {
	// Log nil must not panic even when scripts log / fail.
	root, _ := dom.Parse(page(`console.log('x'); notAfunc();`))
	Run(root, Options{PageURL: testURL})
}

func TestClassList(t *testing.T) {
	root, logs, _ := runJS(t, page(`
		var d=document.getElementById('d');
		d.classList.add('c','a','foo');
		d.classList.remove('a','missing');
		console.log('has-foo='+d.classList.contains('foo'));
		console.log('toggle1='+d.classList.toggle('t'));
		console.log('toggle2='+d.classList.toggle('t'));
		console.log('forceOff='+d.classList.toggle('x', false));
		console.log('forceOn='+d.classList.toggle('y', true));
		console.log('replace='+d.classList.replace('foo','z'));
		console.log('replaceNo='+d.classList.replace('nope','q'));
		console.log('item0='+d.classList.item(0));
		console.log('itemOOB='+d.classList.item(99));
		console.log('len='+d.classList.length);
		console.log('val='+d.classList.value);
		d.classList.value='p q';
	`))
	mustHave(t, logs,
		"has-foo=true", "toggle1=true", "toggle2=false",
		"forceOff=false", "forceOn=true", "replace=true", "replaceNo=false",
		"itemOOB=null", "len=", "val=")
	d := dom.Find(root, "div")
	if d.Attr["class"] != "p q" {
		t.Fatalf("classList.value set: %q", d.Attr["class"])
	}
}

func TestStyle(t *testing.T) {
	root, logs, _ := runJS(t, page(`
		var d=document.getElementById('d');
		d.style.color='red';
		d.style.backgroundColor='blue';
		d.style.setProperty('margin','1px');
		console.log('color='+d.style.color);
		console.log('gp='+d.style.getPropertyValue('margin'));
		console.log('rm='+d.style.removeProperty('color'));
		console.log('hasBg='+('backgroundColor' in d.style));
		console.log('hasNo='+('padding' in d.style));
		console.log('keys='+Object.keys(d.style).length);
		d.style.cssText='top: 0';
		console.log('css='+d.style.cssText);
		delete d.style.top;
		console.log('afterDelete='+d.style.cssText);
		d.style.setProperty('','x');
	`))
	mustHave(t, logs, "color=red", "gp=1px", "rm=red", "hasBg=true", "hasNo=false", "css=top: 0")
	d := dom.Find(root, "div")
	if strings.Contains(d.Attr["style"], "top") {
		t.Fatalf("style delete failed: %q", d.Attr["style"])
	}
}

func TestGetComputedStyle(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var d=document.getElementById('d');
		d.style.color='green';
		console.log('gcs='+window.getComputedStyle(d).getPropertyValue('color'));
		console.log('gcsNull='+(window.getComputedStyle(null).nodeType));
	`))
	mustHave(t, logs, "gcs=green", "gcsNull=undefined")
}

func TestElementTreeAndManipulation(t *testing.T) {
	root, logs, _ := runJS(t, page(`
		var d=document.getElementById('d');
		console.log('meta='+d.tagName+d.nodeName+d.localName+d.nodeType);
		console.log('parent='+d.parentNode.tagName+d.parentElement.tagName);
		var s=document.createElement('span');
		d.appendChild(s);
		var t=document.createTextNode('txt');
		d.appendChild(t);
		console.log('counts='+d.children.length+','+d.childNodes.length+','+d.childElementCount);
		console.log('hasChildren='+d.hasChildNodes()+' hasNone='+document.createElement('i').hasChildNodes());
		console.log('first='+d.firstChild.nodeType+' fe='+d.firstElementChild.tagName+' last='+d.lastChild.nodeType+' le='+d.lastElementChild.tagName);
		console.log('nsib='+(s.nextElementSibling)+' psib='+(s.previousElementSibling));
		console.log('nextSib='+s.nextSibling.nodeType+' prevSib='+(t.previousSibling.tagName));
		d.append('bare', document.createElement('em'));
		d.prepend(document.createElement('p'));
		var deep=d.cloneNode(true), shallow=d.cloneNode(false);
		console.log('clone='+deep.children.length+','+shallow.children.length);
		console.log('contains='+d.contains(s)+','+document.body.contains(d)+','+d.contains(null)+','+d.contains({}));
		var e1=document.createElement('p'); e1.setAttribute('id','x'); e1.appendChild(document.createTextNode('hi'));
		var e2=document.createElement('p'); e2.setAttribute('id','x'); e2.appendChild(document.createTextNode('hi'));
		var e3=document.createElement('p'); e3.setAttribute('id','y'); e3.appendChild(document.createTextNode('hi'));
		var e4=document.createElement('span'); e4.appendChild(document.createTextNode('hi'));
		console.log('eqSelf='+e1.isEqualNode(e1)+' eqStructural='+e1.isEqualNode(e2)+' eqDiffAttr='+e1.isEqualNode(e3)+' eqDiffTag='+e1.isEqualNode(e4)+' eqNull='+e1.isEqualNode(null));
		d.insertBefore(document.createElement('q'), d.firstChild);
		console.log('afterInsert='+d.firstElementChild.tagName);
		d.replaceChild(document.createElement('r'), d.firstElementChild);
		console.log('afterReplace='+d.firstElementChild.tagName);
		var lc=d.lastChild; d.removeChild(lc);
		s.remove();
		console.log('conn='+d.isConnected+','+document.createElement('z').isConnected);
	`))
	mustHave(t, logs, "parent=BODYBODY", "nsib=null", "afterInsert=Q", "afterReplace=R", "conn=true,false",
		"eqSelf=true eqStructural=true eqDiffAttr=false eqDiffTag=false eqNull=false",
		"hasChildren=true hasNone=false")
	if dom.Find(root, "div") == nil {
		t.Fatal("div vanished")
	}
}

func TestAttributes(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var d=document.getElementById('d');
		console.log('hasAttrs0='+d.hasAttributes()+' names0='+d.getAttributeNames().join(','));
		d.setAttribute('data-x','1');
		console.log('ga='+d.getAttribute('data-x')+' has='+d.hasAttribute('data-x')+' miss='+d.getAttribute('nope'));
		d.removeAttribute('data-x');
		console.log('ta='+d.toggleAttribute('hidden')+' ta2='+d.toggleAttribute('hidden'));
		d.hidden=true; console.log('hid='+d.hidden); d.hidden=false; console.log('hid2='+d.hidden);
		d.id='newid'; console.log('id='+d.id);
		d.className='x y'; console.log('cn='+d.className);
		d.value='v'; console.log('val='+d.value);
		d.checked=true; console.log('chk='+d.checked); d.checked=false; console.log('chk2='+d.checked);
		d.href='h'; d.src='s'; d.title='ti';
		console.log('props='+d.href+d.src+d.title);
		var bare=document.createElement('span');
		console.log('hasAttrsBare='+bare.hasAttributes()+' namesBare='+bare.getAttributeNames().join(','));
		var scr=document.createElement('script');
		console.log('type0='+JSON.stringify(scr.type));
		scr.type='module';
		console.log('type1='+scr.type+' attr1='+scr.getAttribute('type'));
	`))
	mustHave(t, logs, "hasAttrs0=true names0=class,data-existing,id",
		"ga=1 has=true miss=null", "ta=true ta2=false",
		"type0=\"\"", "type1=module attr1=module",
		"hid=true", "hid2=false", "id=newid", "cn=x y", "val=v", "chk=true", "chk2=false", "props=hsti",
		"hasAttrsBare=false namesBare=")
}

func TestSelectOptions(t *testing.T) {
	_, logs, _ := runJS(t, `<html><body><select id="s">
		<option value="a">A</option>
		<option value="b">B</option>
		<option value="c" selected>C</option>
	</select><script>
		var s = document.getElementById('s');
		var opts = s.getElementsByTagName('option');
		console.log('initial=' + s.selectedIndex);
		console.log('sel0=' + opts[0].selected + ' sel2=' + opts[2].selected);
		s.selectedIndex = 0;
		console.log('after=' + s.selectedIndex);
		console.log('sel0b=' + opts[0].selected + ' sel2b=' + opts[2].selected);
		opts[1].selected = true;
		console.log('viaOption=' + s.selectedIndex);
	</script></body></html>`)
	mustHave(t, logs,
		"initial=2", "sel0=false sel2=true",
		"after=0", "sel0b=true sel2b=false",
		"viaOption=1")
}

func TestSelectDefaultsToFirstOptionWithNoneMarked(t *testing.T) {
	_, logs, _ := runJS(t, `<html><body><select id="s">
		<option value="a">A</option>
		<option value="b">B</option>
	</select><script>
		console.log('idx=' + document.getElementById('s').selectedIndex);
	</script></body></html>`)
	mustHave(t, logs, "idx=0")
}

func TestSelectWithNoOptionsReportsNegativeOne(t *testing.T) {
	_, logs, _ := runJS(t, `<html><body><select id="s"></select><script>
		console.log('idx=' + document.getElementById('s').selectedIndex);
	</script></body></html>`)
	mustHave(t, logs, "idx=-1")
}

// TestOptionSelectedOutsideSelect covers an <option> with no owning <select>
// (malformed markup, or one detached by a script) — setting .selected must
// still just set the attribute, not panic walking a nil ancestor chain.
func TestOptionSelectedOutsideSelect(t *testing.T) {
	_, logs, _ := runJS(t, `<html><body><option id="o">O</option><script>
		var o = document.getElementById('o');
		o.selected = true;
		console.log('sel=' + o.selected);
	</script></body></html>`)
	mustHave(t, logs, "sel=true")
}

func TestTextAndHTML(t *testing.T) {
	root, logs, _ := runJS(t, page(`
		var d=document.getElementById('d');
		d.innerHTML='<b>bold</b>';
		console.log('ih='+d.innerHTML+' tc='+d.textContent+' oh='+d.outerHTML);
		d.textContent='plain'; console.log('tc2='+d.textContent);
		d.innerText='itxt'; console.log('it='+d.innerText+' itc='+d.textContent);
	`))
	mustHave(t, logs, "ih=<b>bold</b> tc=bold", "tc2=plain", "it=itxt itc=itxt")
	if got := dom.TextContent(dom.Find(root, "div")); got != "itxt" {
		t.Fatalf("final text %q", got)
	}
}

// TestGetRootNode confirms Node.getRootNode() — entirely missing before
// this fix — returns the topmost ancestor (document, for a connected node;
// itself, for a detached one), the DOM standard's own "root" (§4.4).
func TestGetRootNode(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		console.log('plain='+(document.getElementById('d').getRootNode() === document));
		var detached = document.createElement('div');
		console.log('detached='+(detached.getRootNode() === detached));
	`))
	mustHave(t, logs, "plain=true", "detached=true")
}

// TestGetRootNodeComposedBridgesShadowTrees confirms getRootNode's own
// composed:true branch (DOM Standard §4.4's "shadow-including root") walks
// OUT of a shadow tree via ShadowHost all the way to the true top-level
// document, even through NESTED shadow trees (a shadow host that is itself
// inside another shadow tree) — while plain getRootNode() (composed
// omitted/false) stops at the node's own shadow tree's top, per spec.
//
// Exercised directly at the Go level rather than via a JS script: this
// engine has no way yet for a script to obtain a node reference from INSIDE
// a shadow tree in the first place (no element.shadowRoot, and
// getElementById correctly never crosses a shadow boundary per spec), so
// the composed:true bridging this fix adds is not yet JS-reachable — real
// and spec-correct, not dead code, just ahead of the API surface that would
// let a script exercise it.
func TestGetRootNodeComposedBridgesShadowTrees(t *testing.T) {
	document := &dom.Node{Type: dom.Element, Tag: "html"}
	outerHost := &dom.Node{Type: dom.Element, Tag: "outer-widget", Parent: document}
	innerHost := &dom.Node{Type: dom.Element, Tag: "inner-widget", ShadowHost: outerHost}
	leaf := &dom.Node{Type: dom.Element, Tag: "span", ShadowHost: innerHost}

	if got := getRootNode(leaf, false); got != leaf {
		t.Fatalf("getRootNode(leaf, false) = %v, want leaf itself (its own shadow tree's top)", got)
	}
	if got := getRootNode(leaf, true); got != document {
		t.Fatalf("getRootNode(leaf, true) = %v, want the true top-level document, bridging through both nested shadow hosts", got)
	}
}

// TestCompareDocumentPosition confirms Node.compareDocumentPosition —
// entirely missing before this fix — against the DOM standard's own
// documented bitmask values (§4.4): 0 for the same node; CONTAINS|PRECEDING
// (10) / CONTAINED_BY|FOLLOWING (20) for an ancestor/descendant pair in
// either direction; plain PRECEDING (2) / FOLLOWING (4) for unrelated nodes
// compared by tree order; and the DISCONNECTED bit (1) set for a detached
// node with no common root.
func TestCompareDocumentPosition(t *testing.T) {
	_, logs, _ := runJS(t, `<html class="client-nojs"><body>`+
		`<div id="parent"><span id="a">A</span><span id="b">B</span></div>`+
		`<div id="other">O</div>`+
		`<script>
			var parent = document.getElementById('parent');
			var a = document.getElementById('a');
			var b = document.getElementById('b');
			var other = document.getElementById('other');
			var detached = document.createElement('div');
			console.log('same='+a.compareDocumentPosition(a));
			console.log('parentToA='+parent.compareDocumentPosition(a));
			console.log('aToParent='+a.compareDocumentPosition(parent));
			console.log('aToB='+a.compareDocumentPosition(b));
			console.log('bToA='+b.compareDocumentPosition(a));
			console.log('aToOther='+a.compareDocumentPosition(other));
			console.log('disconnectedBit='+(a.compareDocumentPosition(detached) & 1));
		</script></body></html>`)
	mustHave(t, logs,
		"same=0",
		"parentToA=20", // CONTAINED_BY|FOLLOWING
		"aToParent=10", // CONTAINS|PRECEDING
		"aToB=4",       // FOLLOWING
		"bToA=2",       // PRECEDING
		"aToOther=4",   // FOLLOWING (other comes after a's whole subtree)
		"disconnectedBit=1")
}

func TestInsertAdjacentHTML(t *testing.T) {
	root, _, _ := runJS(t, page(`
		var d=document.getElementById('d');
		d.insertAdjacentHTML('beforeend','<i>be</i>');
		d.insertAdjacentHTML('afterbegin','<u>ab</u>');
		d.insertAdjacentHTML('beforebegin','<b>bb</b>');
		d.insertAdjacentHTML('afterend','<s>ae</s>');
	`))
	if dom.Find(root, "i") == nil || dom.Find(root, "u") == nil ||
		dom.Find(root, "b") == nil || dom.Find(root, "s") == nil {
		t.Fatal("insertAdjacentHTML positions not all applied")
	}
	// beforebegin/afterend land as siblings of div, under body.
	body := dom.Find(root, "body")
	var order []string
	for _, c := range body.Children {
		if c.Type == dom.Element {
			order = append(order, c.Tag)
		}
	}
	if order[0] != "b" {
		t.Fatalf("beforebegin not first sibling: %v", order)
	}
}

// TestInsertAdjacentElementAndText confirms insertAdjacentElement/
// insertAdjacentText — entirely missing before this fix, so calling either
// threw "TypeError: ... is not a function" on any real page using them —
// place an existing element/a new text node at all four DOM standard
// positions (§4.9's own "insert adjacent" algorithm), and that
// insertAdjacentElement returns the inserted element per spec.
func TestInsertAdjacentElementAndText(t *testing.T) {
	root, logs, _ := runJS(t, page(`
		var d=document.getElementById('d');
		var span=document.createElement('span'); span.id='moved';
		var ret=d.insertAdjacentElement('beforeend', span);
		console.log('ret='+(ret===span));
		d.insertAdjacentText('afterbegin','AB');
		d.insertAdjacentText('beforebegin','BB');
		d.insertAdjacentText('afterend','AE');
	`))
	mustHave(t, logs, "ret=true")
	if dom.Find(root, "span") == nil {
		t.Fatal("insertAdjacentElement did not insert the element")
	}
	d := dom.Find(root, "div")
	if d == nil || len(d.Children) == 0 || d.Children[0].Type != dom.Text || d.Children[0].Text != "AB" {
		t.Fatalf("insertAdjacentText afterbegin: %v", d)
	}
	body := dom.Find(root, "body")
	var texts []string
	for _, c := range body.Children {
		if c.Type == dom.Text && (c.Text == "BB" || c.Text == "AE") {
			texts = append(texts, c.Text)
		}
	}
	if len(texts) != 2 || texts[0] != "BB" {
		t.Fatalf("insertAdjacentText beforebegin/afterend under body: %v", texts)
	}
}

// TestChildNodeConvenienceMethods confirms before/after/replaceWith/
// replaceChildren — the ChildNode/ParentNode mixin convenience methods,
// entirely missing before this fix (the same class of gap as
// insertAdjacentElement/insertAdjacentText, round 125) — insert/replace in
// the DOM standard's own documented order for the common case (no argument
// is already a sibling of the target, the nuance its own
// "viablePreviousSibling"/"viableNextSibling ... not in nodes" step exists
// for and this engine deliberately does not model).
func TestChildNodeConvenienceMethods(t *testing.T) {
	_, logs, _ := runJS(t, `<html class="client-nojs"><body>`+
		`<div id="wrap"><span id="x">X</span></div>`+
		`<script>
			var x = document.getElementById('x');
			x.before('B1', 'B2');
			x.after('A1');
			console.log('ba='+document.getElementById('wrap').innerHTML);

			var repl = document.getElementById('x');
			var newEl = document.createElement('b'); newEl.textContent = 'NEW';
			repl.replaceWith(newEl, 'tail');
			console.log('rw='+document.getElementById('wrap').innerHTML);

			var wrap2 = document.createElement('div');
			wrap2.innerHTML = '<p>old1</p><p>old2</p>';
			wrap2.replaceChildren(document.createElement('i'), 'newtext');
			console.log('rc='+wrap2.innerHTML);

			var noParent = document.createElement('span');
			noParent.before('ignored'); noParent.after('ignored'); noParent.replaceWith('ignored');
			console.log('noParentOk=true');
		</script></body></html>`)
	mustHave(t, logs,
		`ba=B1B2<span id="x">X</span>A1`,
		`rw=B1B2<b>NEW</b>tailA1`,
		`rc=<i></i>newtext`,
		"noParentOk=true")
}

func TestQueries(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		console.log('qs='+document.querySelector('.foo').tagName);
		console.log('qsnull='+document.querySelector('.nope'));
		console.log('qsa='+document.querySelectorAll('div').length);
		console.log('byTag='+document.getElementsByTagName('div').length+' all='+(document.getElementsByTagName('*').length>0));
		console.log('byCls='+document.getElementsByClassName('foo').length+' clsEmpty='+document.getElementsByClassName('').length);
		console.log('byName='+document.getElementsByName('nm').length);
		console.log('byId='+document.getElementById('d').tagName+' idMiss='+document.getElementById('zz'));
		var d=document.getElementById('d');
		console.log('closest='+d.closest('body').tagName+' closestNone='+d.closest('.zzz'));
		console.log('matches='+d.matches('#d')+','+d.matches('.zzz'));
		console.log('elqs='+document.body.querySelector('#d').tagName+' elqsNull='+(document.body.querySelector('.zzz')));
		console.log('emptySel='+d.querySelectorAll('').length);
	`))
	mustHave(t, logs, "qs=DIV", "qsnull=null", "byTag=1 all=true",
		"clsEmpty=0", "byName=1", "byId=DIV idMiss=null",
		"closest=BODY closestNone=null", "matches=true,false", "elqs=DIV", "emptySel=0")
}

func TestTextNodeWrapper(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var t=document.createTextNode('hi');
		console.log('t='+t.textContent+t.nodeValue+t.data+t.nodeType+t.nodeName);
		t.data='a'; console.log('d='+t.data);
		t.nodeValue='b'; console.log('nv='+t.nodeValue);
		t.textContent='c'; console.log('tc='+t.textContent);
		document.body.appendChild(t);
		console.log('parent='+t.parentNode.tagName+t.parentElement.tagName+' nsib='+t.nextSibling);
	`))
	mustHave(t, logs, "t=hihihi3#text", "d=a", "nv=b", "tc=c", "parent=BODYBODY nsib=null")
}

func TestDocumentMisc(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		console.log('ns='+document.createElementNS('svg','rect').tagName);
		console.log('cmt='+document.createComment('c').nodeType);
		console.log('frag='+document.createDocumentFragment().tagName);
		document.write('x'); document.writeln('y'); document.open(); document.close();
		console.log('active='+document.activeElement.tagName+' scroll='+document.scrollingElement.tagName+' cur='+(document.currentScript instanceof HTMLScriptElement));
		console.log('meta='+document.characterSet+' '+document.compatMode+' '+document.hidden+' '+document.visibilityState+' '+document.readyState+' '+document.nodeType);
		document.cookie='a=1; path=/';
		document.cookie='b=2';
		document.cookie='';
		document.cookie=';;';
		console.log('cookie='+document.cookie);
	`))
	// page()'s own fixture (js_test.go's own helper) ships no <!DOCTYPE>, so
	// it is genuinely in quirks mode (see dom.Node.Quirks) — compatMode
	// correctly reports "BackCompat" here, not "CSS1Compat" (a value this
	// test previously pinned as correct when compatMode was hardcoded and
	// never actually read the page's own quirks-mode status).
	mustHave(t, logs, "ns=RECT", "cmt=8", "frag=#FRAGMENT", "active=BODY scroll=HTML cur=true",
		"meta=UTF-8 BackCompat false visible complete 9", "cookie=a=1; b=2")
}

// TestDocumentCompatModeStandards confirms compatMode reports "CSS1Compat"
// for a page that DOES declare <!DOCTYPE html> (standards mode) — the
// opposite branch from TestDocumentMisc's own quirks-mode fixture.
func TestDocumentCompatModeStandards(t *testing.T) {
	_, logs, _ := runJS(t, `<!DOCTYPE html><html><body><script>`+
		`console.log('compatMode='+document.compatMode);`+
		`</script></body></html>`)
	mustHave(t, logs, "compatMode=CSS1Compat")
}

// TestDocumentURL confirms document.URL/documentURI — both entirely missing
// before this fix — return the page's own URL (DOM Standard §3.5: "this's
// URL, serialized"), the same value window.location.href already exposes.
func TestDocumentURL(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		console.log('url='+document.URL);
		console.log('same='+(document.URL === document.documentURI));
		console.log('matchesLocation='+(document.URL === location.href));
	`))
	mustHave(t, logs, "url="+testURL, "same=true", "matchesLocation=true")
}

// TestElementLangDirTabIndex confirms lang/dir/tabIndex — all entirely
// missing before this fix — as a fresh audit pass over the HTMLElement
// interface (HTML Standard §3.2.6 for lang/dir, §6.6.3 for tabIndex).
// lang/dir are plain reflected attributes; tabIndex's getter is NOT: a
// present, integer-parseable tabindex attribute wins, otherwise the default
// is 0 for natively-interactive elements (an <a> here) and -1 for anything
// else (a plain <div>), matching the spec's own documented algorithm
// exactly rather than a plain reflection.
func TestElementLangDirTabIndex(t *testing.T) {
	_, logs, _ := runJS(t, `<html class="client-nojs"><body>`+
		`<div id="d1" lang="fr" dir="rtl">D1</div>`+
		`<a id="anchor" href="#">A</a>`+
		`<div id="d2" tabindex="5">D2</div>`+
		`<div id="d3" tabindex="abc">D3</div>`+
		`<script>
			var d1 = document.getElementById('d1');
			console.log('lang='+d1.lang+' dir='+d1.dir);
			console.log('anchorTab='+document.getElementById('anchor').tabIndex);
			console.log('divTab='+d1.tabIndex);
			console.log('explicitTab='+document.getElementById('d2').tabIndex);
			console.log('badTab='+document.getElementById('d3').tabIndex);
			d1.tabIndex = 3;
			console.log('setTab='+d1.getAttribute('tabindex'));
		</script></body></html>`)
	mustHave(t, logs,
		"lang=fr dir=rtl",
		"anchorTab=0",  // natively interactive: default 0
		"divTab=-1",    // not interactive, no attribute: default -1
		"explicitTab=5",
		"badTab=-1", // unparseable tabindex falls back to the element's default
		"setTab=3")
}

// TestElementAccessKeyAndSpellcheck covers three GlobalEventHandlers-adjacent
// HTMLElement members found entirely missing while auditing the same section
// as lang/dir/tabIndex above: accessKey (a plain reflection, real corpus
// trigger: en.wikipedia.org's own sidebar sets accesskey="j"/"r"/etc.),
// accessKeyLabel (always "", a disclosed simplification within real
// browsers' own already-inconsistent behaviour), and spellcheck — NOT a
// plain boolean like `disabled`, but an enumerated content attribute with
// "true"/""/"false" keywords (real corpus trigger: pkg.go.dev's own main.js
// sets `codeBlock.spellcheck = false` on a dynamically-created example
// block).
func TestElementAccessKeyAndSpellcheck(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var d = document.getElementById('d');
		d.accessKey = 'k';
		console.log('accessKey='+d.accessKey+' attr='+d.getAttribute('accesskey'));
		console.log('accessKeyLabel='+JSON.stringify(d.accessKeyLabel));

		console.log('spellcheckDefault='+d.spellcheck);
		d.spellcheck = false;
		console.log('spellcheckOff='+d.spellcheck+' attr='+d.getAttribute('spellcheck'));
		d.spellcheck = true;
		console.log('spellcheckOn='+d.spellcheck+' attr='+d.getAttribute('spellcheck'));
	`))
	mustHave(t, logs,
		"accessKey=k attr=k",
		`accessKeyLabel=""`,
		"spellcheckDefault=true",
		"spellcheckOff=false attr=false",
		"spellcheckOn=true attr=true")
}

// TestDetailsOpenReflection confirms HTMLDetailsElement.open — entirely
// missing before this fix — is a plain boolean reflection of the "open"
// content attribute, the same shape as checked/hidden. This is the property
// the CSS engine's own UA stylesheet already keys off
// (`details:not([open]) > :not(summary) { display: none }`, css/ua.go), so a
// script that sets `details.open = true` previously created a dead JS
// property with no effect on the actual rendered content at all — confirmed
// live on pkg.go.dev's own frontend.js/main.js (a version-switcher dropdown
// closing on Escape, an example block expanding itself, and a URL-fragment
// auto-expand on hashchange).
func TestDetailsOpenReflection(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var d = document.getElementById('d');
		console.log('defaultOpen='+d.open);
		d.open = true;
		console.log('afterSet='+d.open+' attr='+d.getAttribute('open'));
		d.open = false;
		console.log('afterUnset='+d.open+' attr='+d.getAttribute('open'));
	`))
	mustHave(t, logs,
		"defaultOpen=false",
		"afterSet=true attr=",
		"afterUnset=false attr=null")
}

// TestElementDisabledReflection confirms `disabled` — entirely missing
// despite already being a real, wired CSS selector (css/selector.go's own
// isDisabled/c.Disabled, backing `button:disabled` author rules) — is a
// plain presence-based boolean reflection, the same shape as
// checked/open/hidden. Real corpus usage confirmed on caniuse.com's own
// bundle.js, which toggles a <link>/<style> element's own `disabled`
// (alongside `media`) to switch an alternate stylesheet on and off.
func TestElementDisabledReflection(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var d = document.getElementById('d');
		console.log('defaultDisabled='+d.disabled);
		d.disabled = true;
		console.log('afterSet='+d.disabled+' attr='+d.getAttribute('disabled'));
		d.disabled = false;
		console.log('afterUnset='+d.disabled+' attr='+d.getAttribute('disabled'));
	`))
	mustHave(t, logs,
		"defaultDisabled=false",
		"afterSet=true attr=",
		"afterUnset=false attr=null")
}

// TestScrollTopLeftReadWhatYouWrote confirms scrollTop/scrollLeft — hardcoded
// to always read 0 with no setter at all, so a script-set value silently did
// nothing — now at least round-trip a script-set value, even though this
// engine has no real scroll/clip model to make the value affect layout or
// paint. Real corpus usage confirmed on pkg.go.dev's own frontend.js, which
// both sets and reads scrollTop back to decide whether to scroll a
// dropdown/search-results container so its active item stays visible.
func TestScrollTopLeftReadWhatYouWrote(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var d = document.getElementById('d');
		console.log('defaultTop='+d.scrollTop+' defaultLeft='+d.scrollLeft);
		d.scrollTop = 42;
		d.scrollLeft = 7;
		console.log('afterSet top='+d.scrollTop+' left='+d.scrollLeft);
	`))
	mustHave(t, logs,
		"defaultTop=0 defaultLeft=0",
		"afterSet top=42 left=7")
}

// TestIndeterminateAccessor confirms `.indeterminate` — entirely missing
// before this fix — round-trips a script-set value. Unlike checked/disabled,
// it has NO backing content attribute (HTML Standard §4.10.5.1.19): setting
// it must NOT touch getAttribute('indeterminate') at all, only the runtime
// state the accessor (and the ":indeterminate" CSS pseudo-class,
// css/selector_test.go) reads. Real corpus usage confirmed on github.com's
// own behaviors.js, which sets it on page load for its "select all"
// bulk-action tri-state checkboxes.
func TestIndeterminateAccessor(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var d = document.getElementById('d');
		console.log('default='+d.indeterminate);
		d.indeterminate = true;
		console.log('afterSet='+d.indeterminate+' attr='+d.getAttribute('indeterminate'));
		d.indeterminate = false;
		console.log('afterUnset='+d.indeterminate);
	`))
	mustHave(t, logs,
		"default=false",
		"afterSet=true attr=null",
		"afterUnset=false")
}

// TestConstraintValidationAPI confirms setCustomValidity/validity/
// validationMessage/checkValidity — entirely missing before this fix — the
// Constraint Validation API subset this engine actually models: a CUSTOM
// validity message is the only thing that can ever make an element invalid
// (no native required/pattern/min/max/step checking is modelled at all, a
// disclosed scope boundary), and checkValidity() fires a cancelable
// "invalid" event when invalid, per spec. Real corpus usage confirmed on
// github.com's own behaviors.js form-validation UI.
func TestConstraintValidationAPI(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var d = document.getElementById('d');
		console.log('defaultValid='+d.validity.valid+' customError='+d.validity.customError+' msg='+JSON.stringify(d.validationMessage));
		console.log('defaultCheck='+d.checkValidity());

		var invalidFired = false;
		d.addEventListener('invalid', function(e){ invalidFired = true; console.log('invalidCancelable='+e.cancelable); });
		d.setCustomValidity('nope');
		console.log('afterSet valid='+d.validity.valid+' customError='+d.validity.customError+' msg='+d.validationMessage);
		console.log('afterSetCheck='+d.checkValidity()+' fired='+invalidFired);

		d.setCustomValidity('');
		console.log('afterClear valid='+d.validity.valid+' customError='+d.validity.customError);
	`))
	mustHave(t, logs,
		`defaultValid=true customError=false msg=""`,
		"defaultCheck=true",
		"afterSet valid=false customError=true msg=nope",
		"afterSetCheck=false fired=true",
		"invalidCancelable=true",
		"afterClear valid=true customError=false")
}

// TestMediaElementCurrentTimeAndPlayPause confirms currentTime/play()/
// pause()/paused — entirely missing before this fix — round-trip a
// script-set value and stay internally consistent, even though this engine
// does no real media decoding/playback (a static renderer has no timeline
// to advance). Real corpus usage confirmed on archive.org's own
// details-av.js: clicking a transcript entry finds the real <video>
// element, seeks it (`videoEl.currentTime = ...`), and resumes playback
// (`videoEl.play()`).
func TestMediaElementCurrentTimeAndPlayPause(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var d = document.getElementById('d');
		console.log('defaultTime='+d.currentTime+' defaultPaused='+d.paused);
		d.currentTime = 12.5;
		console.log('afterSeek='+d.currentTime);
		var p = d.play();
		console.log('afterPlay paused='+d.paused+' isPromise='+(p instanceof Promise));
		d.pause();
		console.log('afterPause paused='+d.paused);
	`))
	mustHave(t, logs,
		"defaultTime=0 defaultPaused=true",
		"afterSeek=12.5",
		"afterPlay paused=false isPromise=true",
		"afterPause paused=true")
}

// TestAddEventListenerOnce confirms addEventListener's `{once: true}` option
// — entirely ignored before this fix (the third argument was never read at
// all) — per the DOM standard's own "inner invoke" algorithm (§2.9): a once
// listener fires exactly once across repeated dispatches, a regular listener
// keeps firing every time, and — the subtle case a naive "remove after
// invoke" implementation gets wrong — a once listener that re-dispatches its
// own event type from inside itself does NOT re-trigger itself, because the
// spec removes it BEFORE running its callback, not after. Real, live usage:
// caniuse.com's own ad-network bundle.js calls exactly
// addEventListener("featureRendered", cb, {once:true}).
func TestAddEventListenerOnce(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var count = 0;
		var d = document.getElementById('d');
		d.addEventListener('click', function(){ count++; }, {once:true});
		d.dispatchEvent(new Event('click'));
		d.dispatchEvent(new Event('click'));
		d.dispatchEvent(new Event('click'));
		console.log('count='+count);

		var normal = 0;
		d.addEventListener('foo', function(){ normal++; });
		d.dispatchEvent(new Event('foo'));
		d.dispatchEvent(new Event('foo'));
		console.log('normal='+normal);

		var reentrant = 0;
		d.addEventListener('bar', function(){
			reentrant++;
			if (reentrant < 5) d.dispatchEvent(new Event('bar'));
		}, {once:true});
		d.dispatchEvent(new Event('bar'));
		console.log('reentrant='+reentrant);
	`))
	mustHave(t, logs, "count=1", "normal=2", "reentrant=1")
}

// TestOnclickOnsubmitHandlers covers the "el.onX = fn" IDL-attribute idiom
// for the two GlobalEventHandlers members this session found actually used
// live (out of the full ~60-member mixin, deliberately not wired
// speculatively): pkg.go.dev's own main.js sets `el.onclick = fn` on its
// tree-nav "expand/collapse all" control, and caniuse.com's own bundle.js
// sets `form.onsubmit = fn` to intercept its search form. Also checks that a
// later assignment REPLACES rather than stacks (a real browser's "el.onclick
// = a; el.onclick = b" behaves like one property, not two listeners), unlike
// addEventListener's own additive semantics.
func TestOnclickOnsubmitHandlers(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var d = document.getElementById('d');
		var clicks = 0;
		d.onclick = function(){ clicks++; };
		d.dispatchEvent(new Event('click'));
		d.dispatchEvent(new Event('click'));
		console.log('clicks='+clicks);

		var which = '';
		d.onclick = function(){ which += 'a'; };
		d.onclick = function(){ which += 'b'; };
		d.dispatchEvent(new Event('click'));
		console.log('which='+which);

		var submitted = false;
		d.onsubmit = function(e){ submitted = true; e.preventDefault(); };
		var ev = new Event('submit', {cancelable:true});
		d.dispatchEvent(ev);
		console.log('submitted='+submitted+' defaultPrevented='+ev.defaultPrevented);
	`))
	mustHave(t, logs, "clicks=2", "which=b", "submitted=true defaultPrevented=true")
}

// TestDocumentCurrentScriptIdentifiesTheRightScriptAndClearsAfterwards covers
// document.currentScript beyond "is it non-null": it must identify THIS
// specific <script> among several (not just any script on the page), and
// must go back to null once synchronous execution ends — during a queued
// timer callback, exactly as a real browser's spec-mandated behaviour. A
// real bundler's own base-URL detection (Webpack/Turbopack's common
// `document.currentScript.src` idiom, confirmed load-bearing live on
// tailwindcss.com — see js/dom.go's own doc comment) reads this once per
// script and would silently miscompute for every later chunk if either of
// these held only sometimes.
func TestDocumentCurrentScriptIdentifiesTheRightScriptAndClearsAfterwards(t *testing.T) {
	_, logs, _ := runJS(t, `<html><body>
		<script data-id="first">console.log('first='+document.currentScript.getAttribute('data-id'));</script>
		<script data-id="second">
			console.log('second='+document.currentScript.getAttribute('data-id'));
			setTimeout(function() { console.log('timer='+document.currentScript); }, 0);
		</script>
	</body></html>`)
	mustHave(t, logs, "first=first", "second=second", "timer=null")
}

func TestDocumentTitleAndCookie(t *testing.T) {
	// Existing <title> is updated in place.
	root, logs, _ := runJS(t, page(`
		console.log('title='+document.title);
		document.title='New';
		console.log('title2='+document.title);
	`))
	mustHave(t, logs, "title=T", "title2=New")
	if dom.Title(root) != "New" {
		t.Fatalf("title not updated: %q", dom.Title(root))
	}
}

func TestSetTitleCreatesTitle(t *testing.T) {
	// No <title> present -> setTitle creates one under <head>.
	root, _, _ := runJS(t, `<html><head></head><body><script>document.title='Made'</script></body></html>`)
	if dom.Title(root) != "Made" {
		t.Fatalf("title not created: %q", dom.Title(root))
	}
}

func TestSetTitleNoHead(t *testing.T) {
	// A document with <html>/<body> but no <head>: setTitle must not panic and
	// must not create a title (nowhere to put it).
	root := &dom.Node{Type: dom.Document}
	html := dom.NewElement("html")
	dom.AppendChild(root, html)
	body := dom.NewElement("body")
	dom.AppendChild(html, body)
	sc := dom.NewElement("script")
	dom.AppendChild(body, sc)
	dom.AppendChild(sc, dom.NewText(`document.title='x'`))
	res := Run(root, Options{PageURL: testURL})
	if res.ScriptsRun != 1 {
		t.Fatalf("script did not run: %+v", res)
	}
	if dom.Find(root, "title") != nil {
		t.Fatal("title should not be created without a head")
	}
}

func TestWindowStubs(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		window.scrollTo(0,0); window.scroll(); window.scrollBy(); window.print();
		window.moveTo(); window.resizeTo(); window.stop(); window.focus(); window.blur(); window.close();
		console.log('confirm='+window.confirm('?')+' prompt='+window.prompt('?')+' open='+window.open('u')+' alert='+window.alert('!'));
		console.log('sel='+window.getSelection());
		console.log('sc='+window.structuredClone({a:7}).a);
		console.log('fetch='+(typeof fetch));
		var mm=matchMedia('(min-width: 100px)');
		mm.addListener(function(){}); mm.removeListener(function(){});
		mm.addEventListener('x',function(){}); mm.removeEventListener('x',function(){}); mm.dispatchEvent({});
		console.log('mm='+mm.matches+' '+mm.media);
		console.log('dark='+matchMedia('(prefers-color-scheme: dark)').matches+' light='+matchMedia('(prefers-color-scheme: light)').matches+' narrow='+matchMedia('(min-width: 5000px)').matches);
		console.log('motion='+matchMedia('(prefers-reduced-motion: reduce)').matches+' nopref='+matchMedia('(prefers-reduced-motion: no-preference)').matches);
		console.log('aliases='+(window===self)+(window===globalThis)+(window===top)+(window===parent)+(window===frames));
		console.log('dims='+window.innerWidth+','+window.innerHeight+','+window.outerWidth+','+window.outerHeight+','+window.devicePixelRatio+','+window.scrollX+','+window.length+','+window.closed+',['+window.name+']');
	`))
	mustHave(t, logs, "confirm=false prompt=null open=null alert=undefined",
		"sel=null", "sc=7", "fetch=function", "mm=true (min-width: 100px)",
		"dark=true light=false narrow=false",
		"motion=false nopref=true",
		"aliases=truetruetruetruetrue", "dims=1024,768,1024,768,1,0,0,false,[]")
}

func TestCustomElementsRegistry(t *testing.T) {
	// A bare reference to `customElements` (no method call needed) previously
	// threw an uncaught ReferenceError, since the global didn't exist at all —
	// confirmed live on caniuse.com, whose bundled script references it early
	// and, with nothing stopping the throw, never reaches the REST of that
	// same script file, including an unrelated `classList.remove("no-js")`
	// call that several nav links and controls depend on being CSS-unhidden
	// (engine#130). define/get/upgrade are harmless no-ops (this engine has
	// no custom-element upgrade machinery — a real, already-documented gap,
	// not attempted here) and whenDefined resolves immediately, since nothing
	// is ever "defined" to legitimately keep it pending on.
	_, logs, _ := runJS(t, page(`
		console.log('type='+(typeof customElements));
		console.log('define='+customElements.define('x-foo', function(){}));
		console.log('get='+customElements.get('x-foo'));
		console.log('upgrade='+customElements.upgrade(document.body));
		customElements.whenDefined('x-foo').then(function(v){ console.log('whenDefined='+v); });
	`))
	mustHave(t, logs, "type=object", "define=undefined", "get=undefined",
		"upgrade=undefined", "whenDefined=undefined")
}

func TestDocumentImplementationCreateHTMLDocument(t *testing.T) {
	// document.implementation was entirely absent, so jQuery's own bootstrap
	// (`E.implementation.createHTMLDocument("").body.innerHTML=...`, a
	// feature-detection check it runs unconditionally at load) threw
	// "Cannot read property 'createHTMLDocument' of undefined or null" —
	// which aborted jQuery's whole script before it finished assigning the
	// global `$`, so every OTHER script on the page that expects `$` failed
	// too with a plain ReferenceError (confirmed live on go.dev/blog,
	// engine#132). The detached document createHTMLDocument returns is
	// backed by real elements (not a plain object), so `.body.innerHTML=`
	// parses real child nodes exactly like it would on the main document —
	// the same real-vs-plain-object distinction jQuery's own feature check
	// (does the browser correctly parse two sibling forms?) depends on.
	_, logs, _ := runJS(t, page(`
		var d = document.implementation.createHTMLDocument("ignored title");
		console.log('head='+d.head.tagName+' body='+d.body.tagName+' de='+d.documentElement.tagName);
		d.body.innerHTML = '<form></form><form></form>';
		console.log('parsed='+d.body.childNodes.length+' tag0='+d.body.childNodes[0].tagName);
		var base = d.createElement('base');
		base.href = 'https://example.com/';
		d.head.appendChild(base);
		console.log('base='+d.head.firstChild.tagName+' href='+d.head.firstChild.href);
	`))
	mustHave(t, logs, "head=HEAD body=BODY de=HTML", "parsed=2 tag0=FORM",
		"base=BASE href=https://example.com/")
}

func TestTreeWalkerNextNode(t *testing.T) {
	// document.createTreeWalker returned a bare empty object, so calling
	// nextNode() on it threw "Object has no member 'nextNode'" — confirmed
	// live on caniuse.com, where lit-html (used by its own bundle for
	// web-component templates) builds a walker exactly this way
	// (`document.createTreeWalker(document, 129)`, then repeatedly calls
	// nextNode() over a cloned template to find bound attributes) and
	// aborted its OWN template-compilation code entirely — every web
	// component built on lit-html was broken, not just one narrow feature
	// (engine#134). whatToShow=1 is NodeFilter.SHOW_ELEMENT: the walk below
	// must skip the text nodes entirely and visit only elements, in document
	// order (children before siblings), and stop (return null) once it runs
	// past the root's own subtree.
	_, logs, _ := runJS(t, page(`
		document.body.innerHTML = '<div id="a">x<span id="b">y</span></div><p id="c"></p>';
		var w = document.createTreeWalker(document.body, 1);
		var seen = [];
		var n;
		while ((n = w.nextNode()) !== null) { seen.push(n.id); }
		console.log('seen='+seen.join(','));
		console.log('current='+w.currentNode.id);
		console.log('afterEnd='+w.nextNode());
		w.currentNode = document.getElementById('a');
		console.log('reset='+w.currentNode.id);
	`))
	mustHave(t, logs, "seen=a,b,c", "current=c", "afterEnd=null", "reset=a")
}

func TestTemplateContent(t *testing.T) {
	// <template>.content was entirely unimplemented (undefined) — every
	// real browser exposes an inert DocumentFragment holding what would
	// otherwise be a template's light-DOM children, and lit-html's own
	// template-cloning path reads exactly `el.content.cloneNode(true)`
	// (see js.newTreeWalker's own doc comment, which already documents
	// `this.el.content` as the real expression caniuse.com's lit-html-based
	// web components read). With no .content at all, real cloning code
	// throws "Cannot read property 'cloneNode' of undefined" the moment it
	// touches it. Confirmed live via a minimal isolated repro before this
	// fix; the two assertions below mirror it plus the innerHTML round trip.
	_, logs, _ := runJS(t, page(`
		var tpl = document.createElement('template');
		console.log('contentIn='+('content' in tpl));
		tpl.innerHTML = '<b class="x">hi</b>';
		console.log('ownChildren='+tpl.children.length);
		console.log('contentChildren='+tpl.content.children.length);
		var clone = tpl.content.cloneNode(true);
		document.getElementById('d').appendChild(clone);
		console.log('target='+document.getElementById('d').innerHTML);
		console.log('tplInnerHTML='+tpl.innerHTML);
	`))
	mustHave(t, logs, "contentIn=true", "ownChildren=0", "contentChildren=1",
		`target=<b class="x">hi</b>`, `tplInnerHTML=<b class="x">hi</b>`)
}

// TestTemplateElementCloneCarriesContent confirms cloning the <template>
// ELEMENT itself (not the far more common `tpl.content.cloneNode(true)`
// idiom TestTemplateContent above exercises, which never touches this code
// path since .content is itself a plain fragment) gives the clone a real
// content fragment rather than leaving it undefined — per the HTML
// Standard's own "cloning steps for template elements" (§4.12.3): a clone
// always gets a fresh content fragment, and that fragment's children are
// only cloned from the original when the clone is deep (subtree). Found by
// reading that spec text directly (this session's own "bibliography
// before" discipline) against cloneNode's generic, template-unaware
// implementation, not by measurement.
func TestTemplateElementCloneCarriesContent(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var tpl = document.createElement('template');
		tpl.innerHTML = '<b>hi</b>';
		var deep = tpl.cloneNode(true);
		var shallow = tpl.cloneNode(false);
		console.log('deepUndefined='+(deep.content===undefined||deep.content===null));
		console.log('deepChildren='+deep.content.children.length);
		console.log('shallowUndefined='+(shallow.content===undefined||shallow.content===null));
		console.log('shallowChildren='+shallow.content.children.length);
	`))
	mustHave(t, logs, "deepUndefined=false", "deepChildren=1", "shallowUndefined=false", "shallowChildren=0")
}

func TestNavigator(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		console.log('nav='+navigator.userAgent+'|'+navigator.language+'|'+navigator.languages.length+'|'+navigator.onLine+'|'+navigator.javaEnabled()+'|'+navigator.sendBeacon('u')+'|'+navigator.hardwareConcurrency+'|'+navigator.cookieEnabled+'|'+navigator.appName);
	`))
	mustHave(t, logs, "nav=TestUA/1.0|en-US|2|true|false|false|1|true|Netscape")
}

func TestHistoryScreenPerformance(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		history.pushState({},'','/x'); history.replaceState({},'',''); history.back(); history.forward(); history.go(-1);
		console.log('hist='+history.length+','+history.state+','+history.scrollRestoration);
		console.log('screen='+screen.width+','+screen.height+','+screen.availWidth+','+screen.colorDepth+','+screen.pixelDepth);
		console.log('perf='+performance.now()+','+performance.getEntriesByType('x').length+','+performance.getEntries().length+','+performance.getEntriesByName('n').length+','+performance.timeOrigin);
		performance.mark('m'); performance.measure('x'); performance.clearMarks(); performance.clearMeasures();
	`))
	mustHave(t, logs, "hist=1,null,auto", "screen=1024,768,1024,24,24", "perf=0,0,0,0,0")
}

func TestConsoleLevels(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		console.log('a'); console.warn('b'); console.error('c'); console.info('d'); console.debug('e'); console.trace('f');
		console.group(); console.groupEnd(); console.groupCollapsed(); console.table(); console.dir();
		console.assert(); console.count(); console.time(); console.timeEnd(); console.clear();
	`))
	mustHave(t, logs, "log: a", "warn: b", "error: c", "info: d", "debug: e", "trace: f")
}

func TestTimers(t *testing.T) {
	_, logs, res := runJS(t, page(`
		setTimeout(function(){console.log('t0')},0);
		setInterval(function(){console.log('int')},10);
		requestAnimationFrame(function(){console.log('raf')});
		requestIdleCallback(function(){console.log('idle')});
		queueMicrotask(function(){console.log('mt')});
		setImmediate(function(){console.log('imm')});
		clearTimeout(1); clearInterval(2); cancelAnimationFrame(3); cancelIdleCallback(4);
		setTimeout('notafunc', 0);
		console.log('id='+(setTimeout(function(){},0)>0));
	`))
	mustHave(t, logs, "t0", "int", "raf", "idle", "mt", "imm", "id=true")
	if res.TimersRun < 6 {
		t.Fatalf("expected >=6 timers drained, got %d", res.TimersRun)
	}
}

// TestTimerDelayOrdering confirms setTimeout honours its own requested delay
// as a real ordering signal, not just enqueue order. Real page code commonly
// races a near-immediate retry/continuation against a much later safety-net
// timeout (confirmed live on react.dev's own chunk-loading machinery); a
// naive FIFO drain can let the later-delay job "win" a race it should always
// lose purely because it happened to be enqueued first.
func TestTimerDelayOrdering(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		setTimeout(function(){console.log('slow')}, 30000);
		setTimeout(function(){console.log('fast')}, 0);
		setTimeout(function(){console.log('mid')}, 10);
	`))
	mustHave(t, logs, "fast", "mid", "slow")
	fast, mid, slow := -1, -1, -1
	for i, l := range logs {
		switch l {
		case "log: fast":
			fast = i
		case "log: mid":
			mid = i
		case "log: slow":
			slow = i
		}
	}
	if !(fast < mid && mid < slow) {
		t.Fatalf("expected delay-ascending order fast<mid<slow, got positions fast=%d mid=%d slow=%d (logs=%v)", fast, mid, slow, logs)
	}
}

// TestTimerNegativeDelayClampsToZero confirms a negative delay (real browsers
// clamp it, never treating it as "before everything else") doesn't invert
// ordering against a genuine zero-delay job.
func TestTimerNegativeDelayClampsToZero(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		setTimeout(function(){console.log('a')}, 0);
		setTimeout(function(){console.log('b')}, -50);
	`))
	mustHave(t, logs, "a", "b")
	var order []string
	for _, l := range logs {
		if l == "log: a" || l == "log: b" {
			order = append(order, l)
		}
	}
	want := "log: a log: b"
	got := strings.Join(order, " ")
	if got != want {
		t.Fatalf("expected negative delay clamped to 0, keeping FIFO order %q, got %q", want, got)
	}
}

// TestTimerDelayTiesAreFIFO confirms equal (or default-zero) delays still
// resolve in enqueue order — the pre-existing, already-tested behaviour for
// every timer kind that doesn't pass an explicit delay (rAF, queueMicrotask,
// ...) must stay unchanged by delay-based ordering.
func TestTimerDelayTiesAreFIFO(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		setTimeout(function(){console.log('a')}, 5);
		setTimeout(function(){console.log('b')}, 5);
		setTimeout(function(){console.log('c')}, 5);
	`))
	mustHave(t, logs, "a", "b", "c")
	var order []string
	for _, l := range logs {
		if l == "log: a" || l == "log: b" || l == "log: c" {
			order = append(order, l)
		}
	}
	want := "log: a log: b log: c"
	got := strings.Join(order, " ")
	if got != want {
		t.Fatalf("expected FIFO order among equal delays %q, got %q", want, got)
	}
}

func TestConstructors(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var e=new Event('x',{bubbles:true,cancelable:true});
		console.log('ev='+e.type+e.bubbles+e.cancelable);
		e.preventDefault(); console.log('dp='+e.defaultPrevented);
		e.stopPropagation(); e.stopImmediatePropagation();
		console.log('cp='+e.composedPath().length+' ts='+e.timeStamp+' it='+e.isTrusted);
		var ce=new CustomEvent('c',{detail:{n:5}}); console.log('ce='+ce.detail.n);
		var ce2=new CustomEvent('c'); console.log('ce2='+ce2.detail);
		var e2=new Event('y'); console.log('noinit='+e2.bubbles);
		var mo=new MutationObserver(function(){}); mo.observe(document.body,{childList:true}); mo.unobserve(document.body); mo.disconnect();
		console.log('mo='+mo.takeRecords().length);
		new IntersectionObserver(function(){}).observe(document.body);
		new ResizeObserver(function(){}).disconnect();
		new PerformanceObserver(function(){}).disconnect();
		var u=new URL('/p?q=1','https://h.com:8080');
		console.log('url='+u.pathname+'|'+u.host+'|'+u.protocol+'|'+u.search+'|'+u.origin+'|'+u.port+'|'+u.hash+'|'+u.toString());
		var u2=new URL('https://a.com/x'); console.log('url2='+u2.href);
		var u3=new URL(String.fromCharCode(0)); console.log('url3='+(u3.href===''));
		var u4=new URL(String.fromCharCode(0),'https://h.com'); console.log('url4='+(u4.href===''));
		console.log('xhrtype='+(typeof XMLHttpRequest)+' fetchtype='+(typeof fetch));
		var img=new Image();
		console.log('node='+Node.ELEMENT_NODE+Node.TEXT_NODE+Node.DOCUMENT_NODE);
	`))
	mustHave(t, logs, "ev=xtruetrue", "dp=true", "cp=0 ts=0 it=false", "ce=5", "ce2=null",
		"noinit=false", "mo=0",
		"url=/p|h.com:8080|https:|?q=1|https://h.com:8080|8080|",
		"url2=https://a.com/x", "url3=true", "url4=true", "xhrtype=function fetchtype=function", "node=139")
}

func TestLocationFields(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		console.log('loc='+location.protocol+'|'+location.host+'|'+location.hostname+'|'+location.port+'|'+location.pathname+'|'+location.search+'|'+location.hash+'|'+location.origin);
		location.assign('x'); location.replace('y'); location.reload();
		console.log('href='+location.href+' str='+location.toString());
	`))
	// assign/replace resolve their target against the current href and update the
	// location (and toString, which reflects the binder's PageURL): 'x' resolves
	// against https://ex.com/a/b to https://ex.com/a/x, then 'y' to /a/y.
	mustHave(t, logs, "loc=https:|ex.com|ex.com||/a/b|?x=1&y=2|#frag|https://ex.com",
		"href=https://ex.com/a/y str=https://ex.com/a/y")
}

func TestEmptyLocation(t *testing.T) {
	// PageURL "" exercises withColon/withPrefix/pathOr/origin empty branches.
	root, _ := dom.Parse(page(`
		console.log('loc='+location.protocol+'|'+location.pathname+'|'+location.search+'|'+location.hash+'|'+location.origin);
	`))
	var logs []string
	Run(root, Options{PageURL: "", Log: func(s string) { logs = append(logs, s) }})
	mustHave(t, logs, "loc=|/|||null")
}

func TestBadLocationURL(t *testing.T) {
	// An unparseable PageURL exercises newLocation's parse-error fallback.
	root, _ := dom.Parse(page(`console.log('p='+location.pathname)`))
	var logs []string
	Run(root, Options{PageURL: "ht\ttp://bad url/\x7f", Log: func(s string) { logs = append(logs, s) }})
	// Any result is fine as long as it did not panic; assert a log came through.
	if len(logs) == 0 {
		t.Fatal("expected a log even with a bad URL")
	}
}

func TestStorage(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		localStorage.setItem('k','v'); localStorage.setItem('k','v2');
		console.log('g='+localStorage.getItem('k')+' miss='+localStorage.getItem('no')+' len='+localStorage.length+' key0='+localStorage.key(0)+' keyOOB='+localStorage.key(9));
		localStorage.removeItem('k'); localStorage.removeItem('gone');
		console.log('afterRm='+localStorage.length);
		localStorage.setItem('a','1'); localStorage.clear();
		console.log('cleared='+localStorage.length);
		sessionStorage.setItem('s','1');
		console.log('sess='+sessionStorage.getItem('s')+' localSep='+localStorage.getItem('s'));
	`))
	mustHave(t, logs, "g=v2 miss=null len=1 key0=k keyOOB=null", "afterRm=0", "cleared=0", "sess=1 localSep=null")
}

func TestDataset(t *testing.T) {
	root, logs, _ := runJS(t, page(`
		var d=document.getElementById('d');
		d.dataset.fooBar='1';
		console.log('attr='+d.getAttribute('data-foo-bar')+' get='+d.dataset.fooBar+' has='+('fooBar' in d.dataset)+' existing='+d.dataset.existing);
		console.log('keys='+Object.keys(d.dataset).length);
		delete d.dataset.fooBar;
		console.log('has2='+('fooBar' in d.dataset));
	`))
	mustHave(t, logs, "attr=1 get=1 has=true existing=v", "has2=false")
	if _, ok := dom.Find(root, "div").Attr["data-foo-bar"]; ok {
		t.Fatal("dataset delete failed")
	}
}

func TestEvents(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		document.addEventListener('DOMContentLoaded',function(){console.log('dcl:'+this.tagName)});
		window.addEventListener('load',function(){console.log('load')});
		window.addEventListener('pageshow',function(){console.log('pageshow')});
		var d=document.getElementById('d');
		d.addEventListener('click',function(){console.log('click:'+this.id)});
		d.addEventListener('click',function(){console.log('click2')});
		d.addEventListener('click',function(){console.log('dup')});  // distinct fn, kept
		d.click();
		d.dispatchEvent(new Event('click'));
		var h={handleEvent:function(){console.log('he')}};
		d.addEventListener('foo',h);
		d.addEventListener('foo',h);  // dedup
		d.dispatchEvent(new Event('foo'));
		d.removeEventListener('foo',h);
		d.removeEventListener('missingType', h);
		d.dispatchEvent(new Event('foo'));
		d.addEventListener('bad', 42);  // non-callable
		d.dispatchEvent(new Event('bad'));
		d.addEventListener('z');  // undefined handler ignored
		d.dispatchEvent('stringEvent');  // eventType from string, no listeners
		document.dispatchEvent(new Event('nolisteners'));
	`))
	mustHave(t, logs, "dcl:undefined", "load", "pageshow", "click:d", "click2", "he")
	// he must appear exactly once (dedup + removal).
	n := 0
	for _, l := range logs {
		if strings.Contains(l, "log: he") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("handleEvent fired %d times, want 1", n)
	}
}

func TestEventBubbling(t *testing.T) {
	// Three independent subtrees (not one reused tree) so each scenario's
	// listeners can't accumulate onto another's nodes.
	const src = `<html><body>
		<div id="a-grandparent"><div id="a-parent"><div id="a-child"></div></div></div>
		<div id="b-grandparent"><div id="b-parent"><div id="b-child"></div></div></div>
		<div id="c-parent"><div id="c-child"></div></div>
		<script>
			var log = [];
			document.getElementById('a-grandparent').addEventListener('click', function(){ log.push('grandparent'); });
			document.getElementById('a-parent').addEventListener('click', function(){ log.push('parent'); });
			document.getElementById('a-child').addEventListener('click', function(){ log.push('child'); });
			document.getElementById('a-child').dispatchEvent(new Event('click', {bubbles: true}));
			console.log('bubbled=' + log.join(','));

			log = [];
			document.getElementById('b-grandparent').addEventListener('click', function(){ log.push('grandparent'); });
			document.getElementById('b-parent').addEventListener('click', function(e){ log.push('parent'); e.stopPropagation(); });
			document.getElementById('b-child').addEventListener('click', function(){ log.push('child'); });
			document.getElementById('b-child').dispatchEvent(new Event('click', {bubbles: true}));
			console.log('stopped=' + log.join(','));

			log = [];
			document.getElementById('c-parent').addEventListener('click', function(){ log.push('parent'); });
			document.getElementById('c-child').addEventListener('click', function(){ log.push('child'); });
			document.getElementById('c-child').dispatchEvent(new Event('click', {bubbles: false}));
			console.log('nonbubbling=' + log.join(','));
		</script>
	</body></html>`
	_, logs, _ := runJS(t, src)
	mustHave(t, logs,
		"bubbled=child,parent,grandparent",
		// child's own (target-phase) listener still fires, parent's fires
		// and calls stopPropagation — grandparent's must NOT.
		"stopped=child,parent",
		// bubbles:false must not even reach parent.
		"nonbubbling=child")
}

// TestStopPropagationVsStopImmediate covers the real distinction: plain
// stopPropagation still lets LATER listeners on the SAME node run (only
// ancestors are skipped); stopImmediatePropagation does not.
func TestStopPropagationVsStopImmediate(t *testing.T) {
	const src = `<html><body>
		<div id="a-parent"><div id="a-child"></div></div>
		<div id="b-parent"><div id="b-child"></div></div>
		<script>
			var log = [];
			document.getElementById('a-child').addEventListener('click', function(e){ log.push('first'); e.stopPropagation(); });
			document.getElementById('a-child').addEventListener('click', function(){ log.push('second'); });
			document.getElementById('a-parent').addEventListener('click', function(){ log.push('parent'); });
			document.getElementById('a-child').dispatchEvent(new Event('click', {bubbles: true}));
			console.log('stopProp=' + log.join(','));

			log = [];
			document.getElementById('b-child').addEventListener('click', function(e){ log.push('first'); e.stopImmediatePropagation(); });
			document.getElementById('b-child').addEventListener('click', function(){ log.push('second'); });
			document.getElementById('b-parent').addEventListener('click', function(){ log.push('parent'); });
			document.getElementById('b-child').dispatchEvent(new Event('click', {bubbles: true}));
			console.log('stopImmediate=' + log.join(','));
		</script>
	</body></html>`
	_, logs, _ := runJS(t, src)
	mustHave(t, logs,
		"stopProp=first,second", // second same-node listener still ran; parent did not
		"stopImmediate=first")   // second same-node listener did NOT run
}

func TestEventCurrentTargetTracksBubblePhase(t *testing.T) {
	const src = `<html><body>
		<div id="parent"><div id="child"></div></div>
		<script>
			document.getElementById('parent').addEventListener('click', function(e){
				console.log('ct=' + e.currentTarget.id + ' target=' + e.target.id);
			});
			document.getElementById('child').dispatchEvent(new Event('click', {bubbles: true}));
		</script>
	</body></html>`
	_, logs, _ := runJS(t, src)
	mustHave(t, logs, "ct=parent target=child")
}

func TestScriptErrorContained(t *testing.T) {
	root, logs, res := runJS(t, `<html><head><title>T</title></head><body>
		<script>throw new Error('boom')</script>
		<script>this is not valid js @@@</script>
		<script>document.body.setAttribute('data-ran','yes')</script>
	</body></html>`)
	if res.ScriptsRun != 1 || res.ScriptsFailed != 2 {
		t.Fatalf("containment counts: %+v", res)
	}
	if dom.Find(root, "body").Attr["data-ran"] != "yes" {
		t.Fatal("later script did not run after earlier errors")
	}
	mustHave(t, logs, "boom")
}

func TestScriptTypeFiltering(t *testing.T) {
	_, _, res := runJS(t, `<html><body>
		<script type="module">export const x=1</script>
		<script type="application/json">{"a":1}</script>
		<script type="text/javascript">1</script>
		<script>2</script>
		<script src=""></script>
	</body></html>`)
	// Only the two classic scripts run; module/json are skipped; empty-src yields
	// no source.
	if res.ScriptsRun != 2 {
		t.Fatalf("expected 2 classic scripts, got %+v", res)
	}
}

func TestTimeout(t *testing.T) {
	root, _ := dom.Parse(`<html><body><script>while(true){}</script><script>window.__x=1</script></body></html>`)
	start := time.Now()
	res := Run(root, Options{PageURL: testURL, Timeout: 40 * time.Millisecond})
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("timeout not enforced, took %v", el)
	}
	// The infinite script fails; the second is skipped because the budget expired.
	if res.ScriptsFailed < 1 {
		t.Fatalf("expected a failed script, got %+v", res)
	}
	if res.ScriptsRun != 0 {
		t.Fatalf("second script should have been skipped by budget: %+v", res)
	}
}

func TestDrainTimerExpiry(t *testing.T) {
	// A script schedules a timer, then burns past the budget so the drain pass
	// sees an expired deadline and skips the queued callback.
	root, _ := dom.Parse(`<html><body><script>
		setTimeout(function(){ globalThis.__timerRan = true; }, 0);
		var t = Date.now(); while (Date.now() - t < 500) {}
	</script></body></html>`)
	var logs []string
	res := Run(root, Options{
		PageURL: testURL,
		Timeout: 30 * time.Millisecond,
		Log:     func(s string) { logs = append(logs, s) },
	})
	if res.TimersRun != 0 {
		t.Fatalf("expired drain should run no timers, got %d", res.TimersRun)
	}
}

func TestMaxScripts(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("<html><body>")
	for i := 0; i < maxScripts+10; i++ {
		sb.WriteString("<script>1</script>")
	}
	sb.WriteString("</body></html>")
	root, _ := dom.Parse(sb.String())
	res := Run(root, Options{PageURL: testURL})
	if res.ScriptsRun != maxScripts {
		t.Fatalf("script cap not enforced: %+v", res)
	}
}

func TestMaxTimerJobs(t *testing.T) {
	// A self-rescheduling timer would loop forever; the job cap stops it.
	root, _ := dom.Parse(`<html><body><script>
		function f(){ setTimeout(f, 0); }
		f();
	</script></body></html>`)
	res := Run(root, Options{PageURL: testURL, Timeout: 5 * time.Second})
	if res.TimersRun != maxTimerJobs {
		t.Fatalf("timer job cap not enforced: %d", res.TimersRun)
	}
}

func TestExternalScriptFetch(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok.js":
			hits++
			w.Write([]byte(`document.body.setAttribute('data-fetched','1')`))
		case "/404.js":
			w.WriteHeader(404)
		default:
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()

	htmlSrc := `<html><body>` +
		`<script src="` + srv.URL + `/ok.js"></script>` +
		`<script src="` + srv.URL + `/404.js"></script>` +
		`<script src="data:text/js,1"></script>` +
		`</body></html>`
	root, err := dom.Parse(htmlSrc)
	if err != nil {
		t.Fatal(err)
	}
	res := Run(root, Options{PageURL: srv.URL + "/", UserAgent: "UA", Client: http.DefaultClient, Timeout: 3 * time.Second})
	if hits != 1 {
		t.Fatalf("expected 1 successful fetch, got %d", hits)
	}
	if dom.Find(root, "body").Attr["data-fetched"] != "1" {
		t.Fatal("fetched script did not run")
	}
	// ok runs; 404 fails to fetch (no source, not counted); data: skipped.
	if res.ScriptsRun != 1 {
		t.Fatalf("run count: %+v", res)
	}
}

func TestFetchScriptNoClient(t *testing.T) {
	b := &binder{opt: Options{PageURL: testURL}}
	if _, ok := b.fetchScript("https://x/y.js"); ok {
		t.Fatal("no client should fail")
	}
}

func TestFetchScriptBadURL(t *testing.T) {
	b := &binder{opt: Options{PageURL: "://::", Client: http.DefaultClient, Ctx: context.Background()}}
	if _, ok := b.fetchScript(""); ok {
		t.Fatal("empty src should fail")
	}
	if _, ok := b.fetchScript("relative.js"); ok {
		// base "://::" is unparseable -> resolve fails.
		t.Fatal("unresolvable src should fail")
	}
}

func TestResolveURL(t *testing.T) {
	if _, ok := resolveURL("https://a.com/", "  "); ok {
		t.Fatal("blank ref should fail")
	}
	if _, ok := resolveURL("://bad", "x"); ok {
		t.Fatal("bad base should fail")
	}
	if _, ok := resolveURL("https://a.com/", "ht\ttp://x"); ok {
		t.Fatal("bad ref should fail")
	}
	got, ok := resolveURL("https://a.com/dir/", "../x")
	if !ok || got != "https://a.com/x" {
		t.Fatalf("resolve: %q %v", got, ok)
	}
}

func TestPureHelpers(t *testing.T) {
	if camelToKebab("backgroundColor") != "background-color" {
		t.Fatal("camelToKebab")
	}
	if withColon("") != "" || withColon("https") != "https:" {
		t.Fatal("withColon")
	}
	if withPrefix("?", "") != "" || withPrefix("?", "a") != "?a" {
		t.Fatal("withPrefix")
	}
	if pathOr("") != "/" || pathOr("/x") != "/x" {
		t.Fatal("pathOr")
	}
	if mergeCookie("", "a=1; path=/") != "a=1" {
		t.Fatal("mergeCookie first")
	}
	if mergeCookie("a=1", "b=2") != "a=1; b=2" {
		t.Fatal("mergeCookie append")
	}
	if mergeCookie("a=1", "   ") != "a=1" {
		t.Fatal("mergeCookie blank")
	}
	if serializeInlineStyle([][2]string{{"a", "1"}, {"b", "2"}}) != "a: 1; b: 2" {
		t.Fatal("serializeInlineStyle")
	}
	if len(parseInlineStyle("a:1; ; b; :x; c:2")) != 2 {
		t.Fatalf("parseInlineStyle: %v", parseInlineStyle("a:1; ; b; :x; c:2"))
	}
}

func TestNodeHelpersDirect(t *testing.T) {
	// siblings/nextSibling/prevSibling for a detached node return nil.
	n := dom.NewElement("div")
	if siblings(n) != nil || nextSibling(n) != nil || prevSibling(n) != nil {
		t.Fatal("detached siblings should be nil")
	}
	if firstChild(n) != nil || lastChild(n) != nil || firstElementChild(n) != nil || lastElementChild(n) != nil {
		t.Fatal("empty node children should be nil")
	}
	if nextElementSibling(n) != nil || prevElementSibling(n) != nil {
		t.Fatal("detached element siblings should be nil")
	}
	if contains(n, nil) {
		t.Fatal("contains nil should be false")
	}
	if hasAllClasses(n, nil) {
		t.Fatal("hasAllClasses empty want should be false")
	}
}

func TestGeometryAndDetachedInsert(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var d=document.getElementById('d');
		console.log('rect='+d.getBoundingClientRect().width+' rects='+d.getClientRects().length);
		d.focus(); d.blur(); d.scrollIntoView();
		console.log('geom='+d.offsetWidth+d.clientHeight+d.scrollTop+d.offsetTop+d.scrollWidth+d.clientWidth+d.scrollLeft+d.offsetLeft+d.offsetHeight+d.scrollHeight);
		console.log('op='+d.offsetParent.tagName);
		var det=document.createElement('div');
		det.insertAdjacentHTML('beforebegin','<b>x</b>');
		det.insertAdjacentHTML('afterend','<i>y</i>');
		det.insertAdjacentHTML('beforeend','<u>z</u>');
		console.log('detChildren='+det.children.length);
	`))
	mustHave(t, logs, "rect=0 rects=0", "geom=0000000000", "op=BODY", "detChildren=1")
}

func TestWindowAndDocumentEventRemoval(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		function w(){console.log('win-evt')}
		window.addEventListener('custom', w);
		window.removeEventListener('custom', w);
		window.dispatchEvent(new Event('custom'));   // no handler now
		function dh(){console.log('doc-evt')}
		document.addEventListener('c', dh);
		document.removeEventListener('c', dh);
		document.removeEventListener('never', dh);   // no such type
		document.dispatchEvent(new Event('c'));       // no handler now
		var d=document.getElementById('d');
		d.removeEventListener('none', function(){});  // no map at all
		console.log('done');
	`))
	if has(logs, "win-evt") || has(logs, "doc-evt") {
		t.Fatal("removed listeners should not fire")
	}
	mustHave(t, logs, "done")
}

func TestDatasetMissingAndCallbackThrow(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var d=document.getElementById('d');
		console.log('missing='+d.dataset.nope);
		setTimeout(function(){ throw new Error('cbboom'); });
		setTimeout(function(){ console.log('after-throw'); });
	`))
	mustHave(t, logs, "missing=undefined", "after-throw")
	// The throwing callback is contained and logged, later timers still run.
	if !has(logs, "cbboom") {
		t.Fatalf("callback error not logged: %v", logs)
	}
}

func TestFetchScriptConnectError(t *testing.T) {
	b := &binder{opt: Options{
		PageURL:   "http://127.0.0.1:1/",
		Client:    &http.Client{Timeout: 500 * time.Millisecond},
		Ctx:       context.Background(),
		UserAgent: "",
	}}
	if _, ok := b.fetchScript("http://127.0.0.1:1/x.js"); ok {
		t.Fatal("connect to closed port should fail")
	}
}

func TestSiblingEdgeHelpers(t *testing.T) {
	p := dom.NewElement("div")
	a := dom.NewElement("a")
	txt := dom.NewText("t")
	b2 := dom.NewElement("b")
	dom.AppendChild(p, a)
	dom.AppendChild(p, txt)
	dom.AppendChild(p, b2)
	// a's next element sibling is b (skipping the text node); b has none.
	if nextElementSibling(a) != b2 {
		t.Fatal("nextElementSibling should skip text")
	}
	if nextElementSibling(b2) != nil {
		t.Fatal("last element has no next element sibling")
	}
	if prevElementSibling(b2) != a {
		t.Fatal("prevElementSibling should skip text")
	}
	if prevElementSibling(a) != nil {
		t.Fatal("first element has no previous element sibling")
	}
	if nextSibling(b2) != nil || prevSibling(a) != nil {
		t.Fatal("edge siblings")
	}
}

// TestNilAttrBranches exercises the defensive nil-attribute paths that normal
// parsed trees never hit (their nodes always carry an Attr map).
func TestNilAttrBranches(t *testing.T) {
	b := &binder{vm: goja.New()}
	n := &dom.Node{Type: dom.Element, Tag: "x"} // Attr nil
	b.setAttr(n, "a", "1")
	if n.Attr["a"] != "1" {
		t.Fatal("setAttr nil-map branch")
	}
	b.removeAttr(&dom.Node{Type: dom.Element}, "a") // nil map, no panic

	writeClasses(&dom.Node{Type: dom.Element}, []string{"c"})

	n2 := &dom.Node{Type: dom.Element, Tag: "x"}
	setStyleProp(n2, "color", "red")
	if n2.Attr["style"] != "color: red" {
		t.Fatalf("setStyleProp nil-map: %q", n2.Attr["style"])
	}

	n3 := &dom.Node{Type: dom.Element, Tag: "x"}
	s := &styleDynObj{b: b, n: n3}
	s.Set("cssText", b.vm.ToValue("top: 0"))
	if n3.Attr["style"] != "top: 0" {
		t.Fatalf("styleDynObj.Set cssText nil-map: %q", n3.Attr["style"])
	}

	// Updating an existing property in place (the found-existing branch).
	setStyleProp(n2, "color", "blue")
	if styleProp(n2, "color") != "blue" {
		t.Fatalf("setStyleProp update: %q", n2.Attr["style"])
	}
	setStyleProp(n2, "color", "") // remove among (only) declaration
	if styleProp(n2, "color") != "" {
		t.Fatal("setStyleProp remove")
	}
}

func TestContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled: the watcher interrupts execution
	root, _ := dom.Parse(`<html><body><script>while(true){}</script></body></html>`)
	start := time.Now()
	res := Run(root, Options{PageURL: testURL, Ctx: ctx, Timeout: 5 * time.Second})
	if time.Since(start) > 3*time.Second {
		t.Fatal("cancelled context did not stop execution promptly")
	}
	if res.ScriptsFailed < 1 {
		t.Fatalf("expected interrupted script, got %+v", res)
	}
}

func TestEventTypeNil(t *testing.T) {
	if eventType(nil) != "" {
		t.Fatal("eventType(nil) should be empty")
	}
}

// TestMutationObserverChildList confirms a basic appendChild is delivered as
// a childList record with the added node — the previously-inert stub (see
// installConstructors' shared no-op "observer" closure, now MutationObserver's
// own real constructor) never called back at all, so this callback simply
// never ran before the fix.
func TestMutationObserverChildList(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var mo = new MutationObserver(function(records){
			console.log('count='+records.length);
			console.log('type='+records[0].type);
			console.log('added='+records[0].addedNodes.length);
			console.log('target='+records[0].target.id);
		});
		mo.observe(document.getElementById('d'), {childList: true});
		var span = document.createElement('span');
		document.getElementById('d').appendChild(span);
	`))
	mustHave(t, logs, "count=1", "type=childList", "added=1", "target=d")
}

// TestMutationObserverAttributes confirms setAttribute/removeAttribute are
// delivered as attribute records, and that attributeOldValue is honoured.
func TestMutationObserverAttributes(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var mo = new MutationObserver(function(records){
			records.forEach(function(r){
				console.log('type='+r.type+' name='+r.attributeName+' old='+r.oldValue);
			});
		});
		mo.observe(document.getElementById('d'), {attributes: true, attributeOldValue: true});
		document.getElementById('d').setAttribute('data-existing', 'new-value');
		document.getElementById('d').removeAttribute('class');
	`))
	mustHave(t, logs,
		"type=attributes name=data-existing old=v",
		"type=attributes name=class old=foo")
}

// TestMutationObserverSubtree confirms a mutation on a DESCENDANT of the
// observed target is only delivered when subtree is set.
func TestMutationObserverSubtree(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var child = document.createElement('span');
		child.id = 'child';
		document.getElementById('d').appendChild(child);

		var withoutSubtree = 0, withSubtree = 0;
		new MutationObserver(function(r){ withoutSubtree += r.length; })
			.observe(document.getElementById('d'), {attributes: true});
		new MutationObserver(function(r){ withSubtree += r.length; })
			.observe(document.getElementById('d'), {attributes: true, subtree: true});

		document.getElementById('child').setAttribute('data-x', '1');
		setTimeout(function(){
			console.log('without='+withoutSubtree+' with='+withSubtree);
		}, 0);
	`))
	mustHave(t, logs, "without=0 with=1")
}

// TestMutationObserverBatching confirms several synchronous mutations before
// the callback actually runs are delivered as ONE invocation with multiple
// records, not one invocation per mutation.
func TestMutationObserverBatching(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var calls = 0, total = 0;
		var mo = new MutationObserver(function(records){
			calls++; total += records.length;
		});
		mo.observe(document.getElementById('d'), {attributes: true});
		document.getElementById('d').setAttribute('data-a', '1');
		document.getElementById('d').setAttribute('data-b', '2');
		setTimeout(function(){ console.log('calls='+calls+' total='+total); }, 0);
	`))
	mustHave(t, logs, "calls=1 total=2")
}

// TestMutationObserverDisconnect confirms disconnect() before the pending
// callback runs suppresses it entirely.
func TestMutationObserverDisconnect(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var fired = false;
		var mo = new MutationObserver(function(){ fired = true; });
		mo.observe(document.getElementById('d'), {attributes: true});
		document.getElementById('d').setAttribute('data-a', '1');
		mo.disconnect();
		setTimeout(function(){ console.log('fired='+fired); }, 0);
	`))
	mustHave(t, logs, "fired=false")
}

// TestCommentNode confirms document.createComment produces a real, JS-visible
// Comment node (nodeType 8) — the previous binding (`dom.NewText("")`)
// silently discarded the comment's own data and reported nodeType 3 (text)
// instead of 8, matching this engine's own former "comments dropped
// entirely" gap in the parser (see dom.NodeType's own doc comment).
func TestCommentNode(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		document.getElementById('d').appendChild(document.createComment('hello'));
		var c = document.getElementById('d').lastChild;
		console.log('nodeType='+c.nodeType+' nodeName='+c.nodeName+' data='+c.data);
		c.data = 'bye';
		console.log('after='+c.textContent+' node='+c.nodeValue);
	`))
	mustHave(t, logs, "nodeType=8 nodeName=#comment data=hello", "after=bye node=bye")
}

// TestCommentNodePreservedFromHTML confirms a comment already present in the
// parsed HTML source (not JS-created) is a real Comment node reachable via
// firstChild, and that innerHTML round-trips it back out — this is the exact
// mechanism React's streaming-SSR hydration depends on: its `<!--$-->`/
// `<!--/$-->` Suspense-boundary markers must survive parsing as real comment
// nodes for its hydration walk to find them, confirmed live as react.dev's
// own "Minified React error #418" cascading into a full client-side crash
// before this fix (dom/dom.go's convertChildren previously dropped every
// comment during parsing).
func TestCommentNodePreservedFromHTML(t *testing.T) {
	_, logs, _ := runJS(t, `<html class="client-nojs"><body>`+
		`<div id="d"><!--marker--><span>x</span></div>`+
		`<script>
			var first = document.getElementById('d').firstChild;
			console.log('type='+first.nodeType+' data='+first.data);
			console.log('html='+document.getElementById('d').innerHTML);
		</script></body></html>`)
	mustHave(t, logs, "type=8 data=marker", "html=<!--marker--><span>x</span>")
}

// TestMutationObserverTakeRecords confirms takeRecords() returns and clears
// the pending queue synchronously, ahead of the scheduled callback.
func TestMutationObserverTakeRecords(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var mo = new MutationObserver(function(){});
		mo.observe(document.getElementById('d'), {attributes: true});
		document.getElementById('d').setAttribute('data-a', '1');
		var taken = mo.takeRecords();
		console.log('taken='+taken.length+' again='+mo.takeRecords().length);
	`))
	mustHave(t, logs, "taken=1 again=0")
}

// TestMutationObserverAttributeFilter confirms attributeFilter restricts
// delivered records to the named attributes — per the DOM standard's own
// "queue a mutation record" algorithm (§4.3.2), read directly rather than
// reasoned from general knowledge (this session's own "bibliography before"
// discipline): "options[attributeFilter] exists, and options[attributeFilter]
// does not contain name" excludes the record. Round 119 shipped
// MutationObserver without reading this text and silently ignored the option
// entirely.
func TestMutationObserverAttributeFilter(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var seen = [];
		var mo = new MutationObserver(function(records){
			records.forEach(function(r){ seen.push(r.attributeName); });
		});
		mo.observe(document.getElementById('d'), {attributes: true, attributeFilter: ['data-existing']});
		document.getElementById('d').setAttribute('data-existing', 'x');
		document.getElementById('d').setAttribute('class', 'y');
		setTimeout(function(){ console.log('count='+seen.length+' seen='+seen.join(',')); }, 0);
	`))
	mustHave(t, logs, "count=1 seen=data-existing")
}

// TestMutationObserverAttributeFilterImpliesAttributes confirms the DOM
// standard's own observe() steps (§4.3.1): "If either
// options[attributeOldValue] or options[attributeFilter] exists, and
// options[attributes] does not exist, then set options[attributes] to true"
// — the common real-world idiom of passing attributeFilter/attributeOldValue
// alone, with no explicit `attributes: true`, must still turn attribute
// observation on.
func TestMutationObserverAttributeFilterImpliesAttributes(t *testing.T) {
	_, logs, _ := runJS(t, page(`
		var fired = false;
		new MutationObserver(function(){ fired = true; })
			.observe(document.getElementById('d'), {attributeFilter: ['class']});
		document.getElementById('d').setAttribute('class', 'z');
		setTimeout(function(){ console.log('fired='+fired); }, 0);
	`))
	mustHave(t, logs, "fired=true")
}
