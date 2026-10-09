package browser

import "testing"

func TestViewTransitionDOMUpdateLifecycle(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		historyEval(t, p, ` (async()=>{
 const order=[];
 const transition=document.startViewTransition({types:['one','one','two'],update:async()=>{order.push('callback');await Promise.resolve();document.body.dataset.updated='yes';order.push('microtask');}});
 order.push('returned');transition.skipTransition();
 const stable=transition.finished===transition.finished && transition.updateCallbackDone===transition.updateCallbackDone;
 const ready=await transition.ready.then(()=>'unexpected',error=>error.name);
 await transition.updateCallbackDone;order.push('updated');await transition.finished;order.push('finished');
 return [order.join(','),stable,document.body.dataset.updated,Array.from(transition.types).join(','),ready].join('|');
 })()`, `returned,callback,microtask,updated,finished|true|yes|one,two|NotSupportedError`)
		historyEval(t, p, `(async()=>{const original={marker:1};const transition=document.startViewTransition(()=>{throw original});const ready=transition.ready.catch(error=>error.name);const results=await Promise.all([transition.updateCallbackDone.catch(error=>error===original),transition.finished.catch(error=>error===original),ready]);return results.join('|')})()`, `true|true|NotSupportedError`)
	})
}

func TestBufferSourceUsesIntrinsicViewBounds(t *testing.T) {
	parallelBrowserTest(t)
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		historyEval(t, p, `(async()=>{const bytes=new Uint8Array([0,65,66,0]);const view=new DataView(bytes.buffer,1,2);Object.defineProperties(view,{byteOffset:{get(){throw Error('author offset')}},buffer:{get(){throw Error('author buffer')}},byteLength:{get(){throw Error('author length')}}});const text=new TextDecoder().decode(view);let rejected=0;for(const input of [null,1,'AB',{}]){try{new TextDecoder().decode(input)}catch(error){if(error instanceof TypeError)rejected++}}const digest=await crypto.subtle.digest('SHA-256',view);return [text,rejected,new Uint8Array(digest).length].join('|')})()`, `AB|4|32`)
	})
}
