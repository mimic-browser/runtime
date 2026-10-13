package browser

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	chrome152 "github.com/moreveal/mimic/chrome/152"
	v8engine "github.com/moreveal/mimic/internal/engine/v8"
)

func TestLazyWebAPIDomainsIgnoreAuthorEvalCSP(t *testing.T) {
	serialBrowserTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Security-Policy", "script-src 'self' 'unsafe-inline'; require-trusted-types-for 'script'; trusted-types default")
		_, _ = w.Write([]byte(`<body>lazy domain CSP</body>`))
	}))
	defer server.Close()
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
	ctx := context.Background()
	if err := p.Navigate(ctx, server.URL); err != nil {
		t.Fatal(err)
	}
	value, err := p.Evaluate(ctx, `(()=>{
  trustedTypes.createPolicy('default',{createScript:source=>source==='this'?source:''});
  const result={};
  try{eval('1+1');result.authorEval='allowed'}catch(error){result.authorEval=error.name}
  try{eval(trustedTypes.defaultPolicy.createScript('this'));result.trustedAuthorEval='allowed'}
  catch(error){result.trustedAuthorEval=error.name}
  const originalEval=globalThis.eval;
  let leaked=false;
  globalThis.eval=()=>{leaked=true};
  const webglWhileReplaced=new OffscreenCanvas(2,2).getContext('webgl');
  try{navigator.gpu;result.overrideError='allowed'}catch(error){result.overrideError=error.name}
  globalThis.eval=originalEval;
  result.overrideLeak=leaked;
  result.webglWhileReplaced=webglWhileReplaced==null?'null':'loaded';
  for(const [name,probe] of [
    ['webgpu',()=>navigator.gpu],
    ['webgl',()=>new OffscreenCanvas(2,2).getContext('webgl')],
    ['audio',()=>new OfflineAudioContext(1,128,44100)]
  ]){
    try{result[name]=probe()==null?'null':'loaded'}
    catch(error){result[name]=error.name+':'+error.message}
  }
  const NativeTypeError=TypeError,NativeRangeError=RangeError,NativeError=Error,NativeProxy=Proxy;
  const effects=[];
  const replace=(name,Native)=>globalThis[name]=new NativeProxy(Native,{
    apply(target,thisArg,args){effects.push(name+':apply');return Reflect.apply(target,thisArg,args)},
    construct(target,args,newTarget){effects.push(name+':construct');return Reflect.construct(target,args,newTarget)}
  });
  replace('TypeError',NativeTypeError);
  replace('RangeError',NativeRangeError);
  replace('Error',NativeError);
  globalThis.Proxy=function(){effects.push('Proxy:construct');throw NativeError('author Proxy called')};
  try{new OffscreenCanvas(1,1).getContext('webgl').getParameter()}
  catch(error){result.nativeTypeError=error instanceof NativeTypeError}
  finally{globalThis.TypeError=NativeTypeError;globalThis.RangeError=NativeRangeError;globalThis.Error=NativeError;globalThis.Proxy=NativeProxy}
  result.intrinsicEffects=effects;
  const intrinsicNames=['Object','Function','Array','Map','Set','WeakMap','WeakSet','Reflect','Promise','Date','Math','Number','String','RegExp','JSON','ArrayBuffer','DataView','Uint8Array','Float32Array'];
  const originals={};
  const pixels=new Uint8Array(4);
  for(const name of intrinsicNames){
    originals[name]=globalThis[name];
    globalThis[name]=new NativeProxy(globalThis[name],{
      apply(){effects.push(name+':apply');throw new NativeError('author '+name+' called')},
      construct(){effects.push(name+':construct');throw new NativeError('author '+name+' called')},
      get(){effects.push(name+':get');throw new NativeError('author '+name+' read')}
    });
  }
  let broadIntrinsicResult;
  try{
    const attributes=document.createElement('div').attributes;
    const headers=new Headers([['x-test','ok']]);
    new OffscreenCanvas(1,1).getContext('webgl').readPixels(0,0,1,1,6408,5121,pixels);
    broadIntrinsicResult=attributes.length===0&&headers.get('x-test')==='ok'&&pixels.length===4;
  }catch(error){broadIntrinsicResult=error.name+':'+error.message}
  finally{for(const name of intrinsicNames)globalThis[name]=originals[name]}
  result.broadIntrinsicResult=broadIntrinsicResult;
  result.broadIntrinsicEffects=effects.slice();
  try{eval('2+2');result.authorEvalAfter='allowed'}
  catch(error){result.authorEvalAfter=error.name}
  return result;
})()`)
	if err != nil {
		t.Fatal(err)
	}
	got := value.(map[string]any)
	if got["authorEval"] != "EvalError" || got["authorEvalAfter"] != "EvalError" || got["trustedAuthorEval"] != "EvalError" || got["overrideError"] != "allowed" || got["overrideLeak"] != false || got["webglWhileReplaced"] != "loaded" || got["webgpu"] != "loaded" || got["webgl"] != "loaded" || got["audio"] != "loaded" || got["nativeTypeError"] != true || len(got["intrinsicEffects"].([]any)) != 0 || got["broadIntrinsicResult"] != true || len(got["broadIntrinsicEffects"].([]any)) != 0 {
		t.Fatalf("lazy domains under author CSP = %#v", got)
	}
}
