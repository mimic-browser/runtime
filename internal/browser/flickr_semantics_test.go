//go:build (windows || linux) && amd64

package browser

import (
	"context"
	"testing"
)

func TestBorrowedDOMOperationsPreserveOwnerIdentity(t *testing.T) {
	parallelBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	historyEval(t, p, `(()=>{
 const iframe=document.createElement('iframe');document.body.append(iframe);
 const foreign=iframe.contentWindow,parent=document.createElement('div'),child=document.createElement('span');
 parent.id='borrowed-parent';parent.append(child);document.body.append(parent);
 const getter=Object.getOwnPropertyDescriptor(foreign.Node.prototype,'parentNode').get;
 const borrowed=getter.call(child);
 if(borrowed!==parent||child.parentNode!==parent||document.getElementById(parent.id)!==parent)return false;
 const descriptor=Object.getOwnPropertyDescriptor(foreign.Node.prototype,'parentNode');
 Object.defineProperty(foreign.Node.prototype,'parentNode',{...descriptor,get(){return 'replacement'}});
 if(getter.call(child)!==parent||Object.getOwnPropertyDescriptor(foreign.Node.prototype,'parentNode').get.call(child)!=='replacement')return false;
 Object.defineProperty(child,'parentNode',{get:()=>null});
 parent.insertBefore(document.createElement('script'),child);
 const created=foreign.Document.prototype.createElement.call(document,'div');
 const found=foreign.Document.prototype.getElementsByTagName.call(document,'head')[0];
 return parent.childNodes.length===2&&Object.getPrototypeOf(created)===HTMLDivElement.prototype&&found===document.head;
 })()`, true)
}

func TestCrossRealmEventDispatchUsesOriginalStateAndListeners(t *testing.T) {
	parallelBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	historyEval(t, p, `(()=>{
 const iframe=document.createElement('iframe');document.body.append(iframe);
 const foreign=iframe.contentWindow;
 for(const borrowed of [false,true]){
   const event=borrowed?new Event('probe',{cancelable:true}):new foreign.Event('probe',{cancelable:true});
   let seen=0;
   const listener=received=>{seen++;if(received!==event||received.target!==document.body)seen=-100;received.preventDefault()};
   document.body.addEventListener('probe',listener);
   const allowed=borrowed?foreign.EventTarget.prototype.dispatchEvent.call(document.body,event):document.body.dispatchEvent(event);
   document.body.removeEventListener('probe',listener);
   if(allowed||!event.defaultPrevented||seen!==1)return false;
 }
 return true;
 })()`, true)
}

func TestReflectedAttributesIgnorePublicOverrides(t *testing.T) {
	parallelBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	historyEval(t, p, `(()=>{
 const script=document.createElement('script'),probe=document.createElement('script'),anchor=document.createElement('a'),link=document.createElement('link');
 let calls=0;
 for(const element of [script,anchor,link]){
   element.setAttribute=()=>{calls++;throw Error('author override')};
   element.getAttribute=()=>{calls++;throw Error('author override')};
 }
 script.integrity='sha384-proof';script.async=true;script.defer=true;
 probe.type='text/javascript';probe.src='/proof.js';probe.integrity='sha384-proof';probe.async=true;probe.defer=true;
 anchor.type='application/pdf';link.type='text/css';
 document.body.lang='en';
 return calls===0&&script.async&&script.defer&&script.integrity==='sha384-proof'&&probe.getAttributeNames().join()==='type,src,integrity,async,defer'&&probe.attributes.length===5&&probe.attributes.getNamedItem('integrity').value==='sha384-proof'&&anchor.type==='application/pdf'&&link.type==='text/css'&&document.body.lang==='en';
 })()`, true)
}

func TestSpecialURLAndViewportFontSerialization(t *testing.T) {
	parallelBrowserTest(t)
	p := bootstrapSnapshotPage(t)
	historyEval(t, p, `(()=>{
 const url=new URL('https://example.test');
 if(url.href!=='https://example.test/'||url.pathname!=='/')return false;
 url.search='a=1';if(url.href!=='https://example.test/?a=1')return false;
 const root=document.documentElement;
 root.style.fontSize='calc(0.017733564013841074rem + 1.3840830449826986vw)';
 const computed=getComputedStyle(root).fontSize;
	const expected=0.0177336*16+1.38408*innerWidth/100;
	return Math.abs(parseFloat(computed)-expected)<0.0001&&root.style.fontSize==='calc(0.0177336rem + 1.38408vw)';
 })()`, true)
}

func TestShadowHostUpdatesFollowingSiblingFlow(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		got, err := p.Evaluate(context.Background(), `(()=>{
 document.body.style.cssText='margin:0';
 document.body.innerHTML='<div id="host"><div style="height:100px"></div></div><div id="following" style="height:10px"></div>';
 const host=document.getElementById('host'),following=document.getElementById('following');
 const before=following.getBoundingClientRect().top;
 const shadow=host.attachShadow({mode:'open'});
 shadow.innerHTML='<div style="height:20px"></div>';
 return JSON.stringify([before,following.getBoundingClientRect().top]);
 })()`)
		if err != nil || got != `[100,20]` {
			t.Fatalf("shadow flow: %v %v", got, err)
		}
	})
}
