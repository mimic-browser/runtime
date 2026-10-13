package browser

import (
	"context"
	"testing"
)

func TestTextDecoderDecodeDescriptorMatchesChrome152(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	historyEval(t, p, `(()=>{const d=Object.getOwnPropertyDescriptor(TextDecoder.prototype,'decode');return [d.enumerable,d.configurable,d.writable,d.value.name,d.value.length,Function.prototype.toString.call(d.value)].join('|')})()`, `true|true|true|decode|0|function decode() { [native code] }`)
}

func TestTextDecoderStreamChunkBoundariesAndErrors(t *testing.T) {
	parallelBrowserTest(t)
	p := testPage(t)
	value, err := p.Evaluate(context.Background(), `(async()=>{
async function decode(chunks,options){const stream=new TextDecoderStream('utf-8',options),out=[];let error='';try{const reader=new ReadableStream({start(c){for(const chunk of chunks)c.enqueue(chunk);c.close()}}).pipeThrough(stream).getReader();while(true){const result=await reader.read();if(result.done)break;out.push(result.value)}}catch(e){error=e.name}return {out:out.join(''),error}}
const bytes=(...a)=>new Uint8Array(a);
return {
split:await decode([bytes(239),bytes(187,191,240,159),bytes(153,130,65)]),
bom:await decode([bytes(239,187,191,65)],{ignoreBOM:true}),
flush:await decode([bytes(226,130)]),
fatal:await decode([bytes(226,130)],{fatal:true}),
invalid:await decode(['not bytes']),
tag:Object.prototype.toString.call(new TextDecoderStream()),encoding:new TextDecoderStream().encoding
};})()`)
	if err != nil {
		t.Fatal(err)
	}
	m := value.(map[string]any)
	for name, want := range map[string]string{"split": "🙂A", "bom": "\ufeffA", "flush": "\ufffd"} {
		got := m[name].(map[string]any)
		if got["out"] != want || got["error"] != "" {
			t.Fatalf("%s: %#v", name, got)
		}
	}
	for _, name := range []string{"fatal", "invalid"} {
		if got := m[name].(map[string]any); got["error"] != "TypeError" {
			t.Fatalf("%s: %#v", name, got)
		}
	}
	if m["tag"] != "[object TextDecoderStream]" || m["encoding"] != "utf-8" {
		t.Fatalf("decoder stream shape: %#v", m)
	}
}
