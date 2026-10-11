package webapi

import (
	"net/url"
	"testing"

	"github.com/dop251/goja"
)

func TestWorkerFetchSharedSemantics(t *testing.T) {
	runtime := goja.New()
	if err := runtime.Set("parseURL", func(input, base string) map[string]any {
		parsed, err := url.Parse(input)
		if err != nil {
			panic(runtime.NewTypeError("Invalid URL"))
		}
		if base != "" {
			root, err := url.Parse(base)
			if err != nil {
				panic(runtime.NewTypeError("Invalid base"))
			}
			parsed = root.ResolveReference(parsed)
		}
		if !parsed.IsAbs() {
			panic(runtime.NewTypeError("Invalid URL"))
		}
		return map[string]any{"href": parsed.String(), "username": "", "password": ""}
	}); err != nil {
		t.Fatal(err)
	}
	_, err := runtime.RunString(`const calls=[],canceled=[];
 const __workerHost={token:()=>1,nameJSON:()=>JSON.stringify('fetch-worker'),navigator:()=>({}),location:()=>({href:'https://example.test/dir/worker.js'}),isSecureContext:()=>true,
 intlEnvironment:()=>({locale:'en-US',timeZone:'UTC'}),
 performance:operation=>{if(operation==='worker')return true;throw Error('Unexpected Performance operation: '+operation)},performanceInstallBuffer:()=>{},
 urlParts:parseURL,abortFetch:id=>canceled.push(id),fetch:(...args)=>{calls.push(args);return Promise.resolve({status:200,url:args[0],headers:{'content-type':'text/plain'},bodyBytes:[65,66]})}};`)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runtime.RunString(WorkerSurface(`globalThis.WorkerGlobalScope=class WorkerGlobalScope extends EventTarget{};globalThis.DedicatedWorkerGlobalScope=class DedicatedWorkerGlobalScope extends WorkerGlobalScope{};`, nil))
	if err != nil {
		t.Fatal(err)
	}
	value, err := runtime.RunString(`(async()=>{
 if(globalThis.name!=='fetch-worker')throw Error('worker creation name');
 const a=new AbortController(),b=new AbortController();let ae=0,be=0;
 a.signal.addEventListener('abort',()=>ae++,{once:true});b.signal.addEventListener('abort',()=>be++);
 const reason={custom:true};a.abort(reason);a.abort();
 if(ae!==1||be!==0||b.signal.aborted)throw Error('abort listener isolation');
 try{await fetch('/never',{signal:a.signal});throw Error('preabort resolved')}catch(e){if(e!==reason)throw e}
 if(calls.length)throw Error('preabort reached transport');
 const request=new Request('echo',{method:'POST',body:new URLSearchParams({x:'hello world'})});
 const response=await fetch(request),copy=response.clone();
 if(response.bodyUsed||request.url!=='https://example.test/dir/echo')throw Error('request base or initial body state');
 if(await response.text()!=='AB'||await copy.text()!=='AB'||!response.bodyUsed)throw Error('body clone consumption');
 try{response.headers.set('x','bad');throw Error('mutable response headers')}catch(e){if(!(e instanceof TypeError))throw e}
 const call=calls[0];if(call[1]!=='POST'||new TextDecoder().decode(new Uint8Array(call[3]))!=='x=hello+world'||call[5].credentials!=='same-origin')throw Error('transport contract');
 if(Response.redirect('next').headers.get('location')!=='https://example.test/dir/next')throw Error('redirect base');
 return true;
 })()`)
	if err != nil {
		t.Fatal(err)
	}
	promise, ok := value.Export().(*goja.Promise)
	if !ok || promise.State() != goja.PromiseStateFulfilled || !promise.Result().ToBoolean() {
		t.Fatalf("worker fetch result: %v", value)
	}
}
