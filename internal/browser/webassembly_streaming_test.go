//go:build (windows || linux) && amd64

package browser

import "testing"

func TestWebAssemblyStreamingMatchesChrome152(t *testing.T) {
	page := bootstrapSnapshotPage(t)
	probe := `(async()=>{try{
  const names=Object.getOwnPropertyNames(WebAssembly);
  const expected=['compile','validate','instantiate','Module','Instance','Table','Memory','Global','Tag','JSTag','Exception','CompileError','LinkError','RuntimeError','compileStreaming','instantiateStreaming','Suspending','promising','SuspendError'];
  if(JSON.stringify(names)!==JSON.stringify(expected))return 'names:'+JSON.stringify(names);
  for(const name of ['compileStreaming','instantiateStreaming']){
    const descriptor=Object.getOwnPropertyDescriptor(WebAssembly,name);
    if(!descriptor?.writable||!descriptor?.enumerable||!descriptor?.configurable||descriptor.value.length!==1||descriptor.value.name!==name||Function.prototype.toString.call(descriptor.value)!=='function '+name+'() { [native code] }')return 'descriptor:'+name;
  }
  const bytes=new Uint8Array([0,97,115,109,1,0,0,0]);
  const response=()=>new Response(bytes,{headers:{'Content-Type':'application/wasm'}});
  if(!(response() instanceof Response))return 'response-brand';
  const module=await WebAssembly.compileStreaming(response());
  if(!(module instanceof WebAssembly.Module))return 'compile';
  const result=await WebAssembly.instantiateStreaming(Promise.resolve(response()));
  if(!(result.module instanceof WebAssembly.Module)||!(result.instance instanceof WebAssembly.Instance))return 'instantiate';
  try{await WebAssembly.compileStreaming(new Response(bytes));return 'mime-accepted'}catch(error){if(error.name!=='TypeError'||error.message!=="Failed to execute 'compile' on 'WebAssembly': Incorrect response MIME type. Expected 'application/wasm'.")return 'mime-error:'+error}
  try{await WebAssembly.compileStreaming(1);return 'type-accepted'}catch(error){if(error.name!=='TypeError'||error.message!=="Failed to execute 'compile' on 'WebAssembly': An argument must be provided, which must be a Response or Promise<Response> object")return 'type-error:'+error}
	return 'ok';
}catch(error){return 'unexpected:'+error.name+':'+error.message}
})()`
	if value := bootstrapSnapshotEvaluate(t, page, probe); value != "ok" {
		t.Fatalf("cold realm: %v", value)
	}
	bootstrapSnapshotWarm(t, page)
	restored, err := page.ctx.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	if value := bootstrapSnapshotEvaluate(t, restored, probe); value != "ok" {
		t.Fatalf("restored realm: %v", value)
	}
}
