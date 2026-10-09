package browser

import (
	"context"
	"testing"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

// This independent fixture was measured against frozen Chrome 152. In
// particular, inert document descendants are connected despite no Window,
// and removal preserves ownership while insertion adopts without cloning.
func TestInertHTMLDocumentOwnershipChrome152(t *testing.T) {
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
	value, err := p.Evaluate(context.Background(), `(()=>{
 const d=document.implementation.createHTMLDocument('  A \n B '),empty=document.implementation.createHTMLDocument();
 d.body.innerHTML='<form></form><form></form><div id="local"><span>text</span></div>';
 const n=d.getElementById('local'),child=n.firstChild;
 const checks=[d!==document,d instanceof Document,n.ownerDocument===d,child.ownerDocument===d,d.documentElement.parentNode===d,n.getRootNode()===d,n.isConnected,d.defaultView===null,d.URL==='about:blank',d.title==='A B',d.compatMode==='CSS1Compat',d.doctype.nodeType===10,empty.head.childNodes.length===0,d.body.children.length===3,d.implementation===d.implementation];
 checks.push(document.implementation.createHTMLDocument(undefined).head.childNodes.length===0,d.contentType==='text/html',d.characterSet==='UTF-8',d.doctype instanceof DocumentType,d.doctype.name==='html',d.textContent===null);
 const tags=d.body.getElementsByTagName('form');checks.push(tags.length===2,tags[2]===undefined,tags.item(2)===null);d.body.appendChild(d.createElement('form'));checks.push(tags.length===3);
 n.remove();checks.push(n.ownerDocument===d,!n.isConnected);
 const copy=n.cloneNode(true);checks.push(copy.ownerDocument===d,copy.firstChild.ownerDocument===d);
 document.body.appendChild(n);checks.push(n.ownerDocument===document,child.ownerDocument===document,n.firstChild===child);
 const imported=d.importNode(n,true);checks.push(imported!==n,imported.ownerDocument===d,imported.firstChild.ownerDocument===d);
 d.body.appendChild(imported);imported.textContent='replacement';checks.push(imported.firstChild.ownerDocument===d);
 let hierarchy='';try{d.appendChild(d.createElement('div'))}catch(e){hierarchy=e.name}checks.push(hierarchy==='HierarchyRequestError');
 return checks.every(Boolean)?'ok':JSON.stringify(checks);
})()`)
	if err != nil || value != "ok" {
		t.Fatalf("inert document semantics: %v %v", value, err)
	}
}

// Topology, ownership and attribute projections may share an internal pure-read
// observation, but ordinary script can mutate between any two public getters.
// Keep this as a single callback so a retained public snapshot cannot pass by
// relying on the next protocol command to invalidate it.
func TestDOMReadMutationReparentAndAdoption(t *testing.T) {
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
	value, err := p.Evaluate(context.Background(), `(()=>{
 const first=document.createElement('section'),second=document.createElement('section'),node=document.createElement('span');
 document.body.append(first,second);first.append(node);node.setAttribute('data-state','before');
 const before=node.parentNode===first&&node.ownerDocument===document&&node.getAttribute('data-state')==='before';
 second.append(node);node.setAttribute('data-state','after');
 const moved=node.parentNode===second&&node.ownerDocument===document&&node.getAttribute('data-state')==='after';
 const inert=document.implementation.createHTMLDocument('owner');inert.body.append(node);
 const adopted=node.parentNode===inert.body&&node.ownerDocument===inert&&node.getAttribute('data-state')==='after';
 document.body.append(node);
 return before&&moved&&adopted&&node.parentNode===document.body&&node.ownerDocument===document;
})()`)
	if err != nil || value != true {
		t.Fatalf("DOM read/mutate/read semantics: %v %v", value, err)
	}
}

func TestCanonicalDOMReadPublicationAcrossWorldsAndTeardown(t *testing.T) {
	parallelBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	d := NewDebugger(p)
	defer d.Close()
	debuggerEval(t, d, `document.body.innerHTML='<section id="box"><span data-state="before">old</span></section>'`, DebuggerOptions{})
	owner := p.Top.Realm
	if owner.domSharedRevision == nil || owner.domRevisionSubscription == nil {
		t.Fatal("V8 realm did not install canonical revision publication")
	}
	world, err := p.IsolatedWorld(context.Background(), p.Top.ID, "canonical-reads")
	if err != nil {
		t.Fatal(err)
	}
	read := func(source string, want any) {
		t.Helper()
		result, err := d.Evaluate(context.Background(), p.Top.ID, world, source, DebuggerOptions{ReturnByValue: true})
		if err != nil || result["exceptionDetails"] != nil || result["result"].(map[string]any)["value"] != want {
			t.Fatalf("canonical read: %#v %v, want %v", result, err, want)
		}
	}
	read(`globalThis.box=document.getElementById('box');globalThis.node=box.firstChild;globalThis.list=box.childNodes;node.getAttribute('data-state')==='before'&&node.parentNode===box&&node.ownerDocument===document&&node.isConnected&&list.length===1&&list[0]===node`, true)
	debuggerEval(t, d, `const node=document.querySelector('span');node.setAttribute('data-state','after');node.setAttribute('data-extra','new');node.firstChild.data='\ud800updated';document.getElementById('box').append(document.createElement('i'));`, DebuggerOptions{})
	read(`node.getAttribute('data-state')==='after'&&node.getAttributeNames().includes('data-extra')&&node.firstChild.data==='\ud800updated'&&list.length===2&&list[0]===node`, true)
	debuggerEval(t, d, `document.querySelector('span').remove()`, DebuggerOptions{})
	read(`node.parentNode===null&&!node.isConnected&&node.ownerDocument===document&&list.length===1`, true)
	debuggerEval(t, d, `document.getElementById('box').prepend(node)`, DebuggerOptions{})
	read(`node.parentNode===box&&node.isConnected&&list.length===2&&list[0]===node`, true)
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if owner.domSharedRevision != nil || owner.domRevisionSubscription != nil {
		t.Fatal("Page teardown retained the revision publisher")
	}
	// A retained canonical node arena may outlive its execution realm.
	owner.document.InvalidateObservations()
}

func TestCanonicalStyleAttributeRowsAcrossWorlds(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		d := NewDebugger(p)
		defer d.Close()
		debuggerEval(t, d, `
document.head.innerHTML = '<style>#probe:dir(ltr) { color: red } #probe[data-state="before"] { width: 10px } #probe[data-state="after"] { width: 20px }</style>';
document.body.innerHTML = '<div id="probe" data-state="before" data-empty=""></div>';
document.getElementById('probe').setAttribute('__proto__', 'literal');
`, DebuggerOptions{})
		world, err := p.IsolatedWorld(context.Background(), p.Top.ID, "attribute-row")
		if err != nil {
			t.Fatal(err)
		}
		read := func(want string) {
			t.Helper()
			result, err := d.Evaluate(context.Background(), p.Top.ID, world, `(() => {
  const element = document.getElementById('probe');
  const width = getComputedStyle(element).width;
  return JSON.stringify([
    width,
    element.getAttribute('data-state'),
    element.getAttribute('data-empty'),
    element.getAttribute('__proto__'),
    element.getAttribute('constructor'),
    element.getAttributeNames().includes('__proto__'),
  ]);
})()`, DebuggerOptions{ReturnByValue: true})
			if err != nil || result["exceptionDetails"] != nil || result["result"].(map[string]any)["value"] != want {
				t.Fatalf("canonical attribute row: %#v %v; want %s", result, err, want)
			}
		}
		read(`["10px","before","","literal",null,true]`)
		debuggerEval(t, d, `
const element = document.getElementById('probe');
element.setAttribute('data-state', 'after');
element.removeAttribute('__proto__');
`, DebuggerOptions{})
		read(`["20px","after","",null,null,false]`)
	})
}

func TestXMLDocumentFactoryChrome152(t *testing.T) {
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
	value, err := p.Evaluate(context.Background(), `(()=>{
 const impl=document.implementation,d=impl.createDocument('http://www.w3.org/1999/xhtml','html',null),body=d.createElement('body');
 d.documentElement.appendChild(body);body.appendChild(d.createElement('MiXeD'));
 const x=impl.createDocument('urn:test','p:Root',null),child=x.createElement('Case');x.documentElement.appendChild(child);
 const checks=[d instanceof XMLDocument,d instanceof Document,d.contentType==='application/xhtml+xml',d.body===body,d.head===null,d.defaultView===null,d.URL==='about:blank',d.readyState==='complete',body.firstChild.localName==='MiXeD',body.ownerDocument===d,x.contentType==='application/xml',x.documentElement.localName==='Root',x.documentElement.prefix==='p',x.documentElement.nodeName==='p:Root',child.localName==='Case',child.namespaceURI===null,child.ownerDocument===x,child.isConnected,impl.createDocument(null,'',null).documentElement===null];
 for(const [args,want] of [[[],'TypeError'],[[null,'a:b'],'NamespaceError'],[['urn:x','a b'],'InvalidCharacterError']]){try{impl.createDocument(...args);checks.push(false)}catch(e){checks.push(e.name===want)}}
 document.body.appendChild(child);checks.push(child.ownerDocument===document,child===document.body.lastChild);
 return checks.every(Boolean)?'ok':JSON.stringify(checks);
})()`)
	if err != nil || value != "ok" {
		t.Fatalf("XML factory: %v %v", value, err)
	}
}

func TestDialogModalSelectorStateChrome152(t *testing.T) {
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
	value, err := p.Evaluate(context.Background(), `(()=>{
 const d=document.createElement('dialog'),out=[d.matches(':modal')];try{d.showModal()}catch(e){out.push(e.name)}
 document.body.append(d);d.show();out.push(d.open,d.matches(':modal'));d.close();d.showModal();out.push(d.open,d.matches(':modal'));
 d.removeAttribute('open');out.push(d.matches(':modal'));d.remove();out.push(d.matches(':modal'));document.body.append(d);out.push(d.matches(':modal'));
 return JSON.stringify(out);
})()`)
	if err != nil || value != `[false,"InvalidStateError",true,false,true,true,true,false,false]` {
		t.Fatalf("modal state: %v %v", value, err)
	}
}

func TestGetElementByIDLiteralOrderAndRootBoundaries(t *testing.T) {
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
	value, err := p.Evaluate(context.Background(), `(()=>{
 const id='a:b [c], d',a=document.createElement('i'),b=document.createElement('b');a.id=b.id=id;document.body.append(a,b);
 const checks=[document.getElementById(id)===a,document.getElementById('')===null];
 document.body.insertBefore(b,a);checks.push(document.getElementById(id)===b);b.remove();checks.push(document.getElementById(id)===a);
 const d=document.implementation.createHTMLDocument();d.body.append(b);checks.push(d.getElementById(id)===b,document.getElementById(id)===a);
 const f=document.createDocumentFragment();f.append(a);checks.push(f.getElementById(id)===a,document.getElementById(id)===null);
 const t=document.createElement('template');t.innerHTML='<i id="hidden"></i>';document.body.append(t);checks.push(document.getElementById('hidden')===null,t.content.getElementById('hidden')!==null);
 return checks.every(Boolean)?'ok':JSON.stringify(checks);
})()`)
	if err != nil || value != "ok" {
		t.Fatalf("ID lookup: %v %v", value, err)
	}
}
