package browser

import (
	"context"
	"strings"
	"testing"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

// These observable results were checked against frozen Chrome 152.0.7977.82.
func TestConstructedStylesheetRulesAndAdoption(t *testing.T) {
	parallelBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Evaluate(context.Background(), `(()=>{
const sheet=new CSSStyleSheet();sheet.replaceSync('/* comment */ a { color: red; --x: "a;b" } @media (min-width: 1px) { b { display: none } } @import "ignored.css";');
const list=sheet.cssRules,rule=list[0];
if(list.length!==2||rule.cssText!=='a { color: red; --x: "a;b"; }'||list[1].cssRules[0].style.display!=='none')return 'parsed rules';
rule.style.color='blue';if(rule.cssText!=='a { color: blue; --x: "a;b"; }')return 'declaration edit';
if(sheet.insertRule('p { color: green }',1)!==1||list.length!==3)return 'insertion';
let errors=[];for(const fn of [()=>sheet.insertRule('a{}b{}'),()=>sheet.deleteRule(99)])try{fn()}catch(e){errors.push(e.name)}
const host=document.createElement('div');host.id='adopted-host';document.body.append(host);const root=host.attachShadow({mode:'open'});root.innerHTML='<span>styled</span>';
root.adoptedStyleSheets=[sheet];const adoption=root.adoptedStyleSheets;
try{root.adoptedStyleSheets=[{}]}catch(e){errors.push(e.name)}
if(errors.join()!=='SyntaxError,IndexSizeError,TypeError')return errors.join();
document.adoptedStyleSheets.push(sheet);
sheet.replaceSync('span { color: purple }');
if(list!==sheet.cssRules||adoption!==root.adoptedStyleSheets||root.adoptedStyleSheets[0]!==sheet||list[0].cssText!=='span { color: purple; }')return 'identity or replacement';
return sheet instanceof CSSStyleSheet&&sheet instanceof StyleSheet&&root.querySelectorAll('style').length===0;
})()`)
	if err != nil || got != true {
		t.Fatalf("constructed sheets: %v %v", got, err)
	}
	snapshot, err := p.CaptureSnapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	html := string(snapshot.Files["index.html"])
	if strings.Count(html, "span { color: purple; }") != 2 {
		t.Fatalf("document and shadow adoption missing from export: %s", html)
	}
	got, err = p.Evaluate(context.Background(), `document.querySelectorAll('style').length===0&&document.querySelector('#adopted-host').shadowRoot.querySelectorAll('style').length===0`)
	if err != nil || got != true {
		t.Fatalf("snapshot mutated live DOM: %v %v", got, err)
	}
}

func TestStyleSheetDropsUnsupportedVendorKeyframes(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	value, err := p.Evaluate(context.Background(), `(()=>{
		const style=document.createElement('style');
		style.textContent='*{color:transparent}body,html{margin:0}fieldset{padding:0}button:focus,input:focus{outline:0}:focus{border:none}textarea{resize:none}.no-selection{user-select:none}@-webkit-keyframes pop{from{opacity:0}to{opacity:1}}@-moz-keyframes pop{from{opacity:0}to{opacity:1}}@-o-keyframes pop{from{opacity:0}to{opacity:1}}@keyframes pop{from{opacity:0}to{opacity:1}}';
		document.head.append(style);
		const text=Array.from(style.sheet.cssRules,rule=>rule.cssText).join('\n');
		return JSON.stringify([style.sheet.cssRules.length,text.includes('@-webkit-keyframes'),text.includes('@keyframes'),text.includes('@-moz-keyframes'),text.includes('@-o-keyframes')]);
	})()`)
	if err != nil || value != `[9,true,true,false,false]` {
		t.Fatalf("vendor keyframe CSSOM recovery: %v, %v", value, err)
	}
}

func TestTreeWalkerCanonicalTemplatesAndFiltering(t *testing.T) {
	parallelBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Evaluate(context.Background(), `(()=>{
const template=document.createElement('template');template.innerHTML='<section id="skip"><i id="a"></i><b id="reject"><u id="hidden"></u></b><i id="b"></i></section><!--marker--><p id="c"></p>';
const root=template.content, filter=n=>n.id==='skip'?NodeFilter.FILTER_SKIP:n.id==='reject'?NodeFilter.FILTER_REJECT:NodeFilter.FILTER_ACCEPT;
const walker=document.createTreeWalker(root,NodeFilter.SHOW_ELEMENT|NodeFilter.SHOW_COMMENT,filter),seen=[];
for(let n;(n=walker.nextNode());)seen.push(n.id||n.data);
if(seen.join()!=='a,b,marker,c'||walker.root!==root||walker.currentNode!==root.querySelector('#c'))return 'forward:'+seen;
if(walker.previousNode().data!=='marker'||walker.previousNode().id!=='b'||walker.previousSibling().id!=='a')return 'backward';
walker.currentNode=root;if(walker.firstChild().id!=='a'||walker.nextSibling().id!=='b')return 'skipped parent';
walker.currentNode=root;const p=document.createElement('p');p.id='new';root.append(p);if(walker.lastChild()!==p)return 'live mutation';
const calls=[];const counted=document.createTreeWalker(root,NodeFilter.SHOW_ELEMENT,n=>{calls.push(n.id);return n.id==='skip'?NodeFilter.FILTER_SKIP:NodeFilter.FILTER_ACCEPT});
if(counted.nextNode().id!=='a'||calls.join()!=='skip,a')return 'duplicate filter:'+calls;
const documentWalker=document.createTreeWalker(document,NodeFilter.SHOW_ELEMENT);if(documentWalker.nextNode()!==document.documentElement)return 'document root';
let recursive;recursive=document.createTreeWalker(root,NodeFilter.SHOW_ALL,()=>recursive.nextNode());let error='';try{recursive.nextNode()}catch(e){error=e.name}
return error==='InvalidStateError'&&template.childNodes.length===0;
})()`)
	if err != nil || got != true {
		t.Fatalf("tree walker: %v %v", got, err)
	}
}

func TestAttributeOrderSurvivesTemplateParsingAndClone(t *testing.T) {
	parallelBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Evaluate(context.Background(), `(()=>{
const template=document.createElement('template');template.innerHTML='<p z="one" style="color: red" a="two"></p>';
const node=template.content.firstChild;if(node.getAttributeNames().join()!=='z,style,a')return 'parser';
node.setAttribute('style','color: blue');node.removeAttribute('z');node.setAttribute('z','three');
const clone=node.cloneNode(true),container=document.createElement('div');container.append(clone);return node.getAttributeNames().join()==='style,a,z'&&Array.from(node.attributes,a=>a.name).join()==='style,a,z'&&clone.getAttributeNames().join()==='style,a,z'&&container.innerHTML==='<p style="color: blue" a="two" z="three"></p>';
})()`)
	if err != nil || got != true {
		t.Fatalf("attribute order: %v %v", got, err)
	}
}

func TestComputedStyleIgnoresUnsupportedStylesheetSelectors(t *testing.T) {
	parallelBrowserTest(t)
	b, err := New(v8engine.Factory{}, chrome152.New())
	if err != nil {
		t.Fatal(err)
	}
	c := b.NewContext()
	defer c.Close()
	p, err := c.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	got, err := p.Evaluate(context.Background(), `(()=>{
const style=document.createElement('style');style.textContent='p::-moz-selection { margin-top: 99px; } p:unknown-pseudo { margin-top: 88px; } p { margin-top: 12px; }';document.head.append(style);
const node=document.createElement('p');document.body.append(node);
const computed=getComputedStyle(node).getPropertyValue('margin-top');
let errors=[];for(const query of [()=>node.matches('p:unknown-pseudo'),()=>document.querySelector('p:unknown-pseudo')])try{query()}catch(error){errors.push(error.name)}
return computed==='12px'&&errors.join()==='SyntaxError,SyntaxError';
})()`)
	if err != nil || got != true {
		t.Fatalf("stylesheet selector boundary: %v %v", got, err)
	}
}
