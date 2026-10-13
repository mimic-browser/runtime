package browser

import (
	"context"
	"testing"
	"time"
)

func TestLazyAudioMaterializationPreservesObservableIdentity(t *testing.T) {
	serialBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		value, err := page.Evaluate(ctx, `(()=>{
 const names=['AudioBuffer','OfflineAudioContext','AudioContext','GainNode'];
 const constructors=new Map(names.map(name=>[name,globalThis[name]]));
 const prototypes=new Map(names.map(name=>[name,globalThis[name].prototype]));
 const method=BaseAudioContext.prototype.createGain;
 const getter=Object.getOwnPropertyDescriptor(AudioBuffer.prototype,'length').get;
 const descriptors=names.map(name=>Object.getOwnPropertyDescriptor(globalThis,name));
 const methodDescriptor=Object.getOwnPropertyDescriptor(BaseAudioContext.prototype,'createGain');
 const getterDescriptor=Object.getOwnPropertyDescriptor(AudioBuffer.prototype,'length');
 const sources=[AudioBuffer,OfflineAudioContext,method,getter].map(fn=>Function.prototype.toString.call(fn));
 const context=new OfflineAudioContext(1,8,8000);
 const gain=context.createGain();
 return names.every((name,index)=>globalThis[name]===constructors.get(name)&&globalThis[name].prototype===prototypes.get(name)&&Object.getOwnPropertyDescriptor(globalThis,name).value===descriptors[index].value)
   && BaseAudioContext.prototype.createGain===method
   && Object.getOwnPropertyDescriptor(AudioBuffer.prototype,'length').get===getter
   && Object.getOwnPropertyDescriptor(BaseAudioContext.prototype,'createGain').enumerable===methodDescriptor.enumerable
   && Object.getOwnPropertyDescriptor(BaseAudioContext.prototype,'createGain').configurable===methodDescriptor.configurable
   && Object.getOwnPropertyDescriptor(AudioBuffer.prototype,'length').enumerable===getterDescriptor.enumerable
   && Object.getOwnPropertyDescriptor(AudioBuffer.prototype,'length').configurable===getterDescriptor.configurable
   && [AudioBuffer,OfflineAudioContext,method,getter].map(fn=>Function.prototype.toString.call(fn)).every((source,index)=>source===sources[index])
   && gain instanceof GainNode && context instanceof OfflineAudioContext;
})()`)
		if err != nil || value != true {
			t.Fatalf("identity changed across lazy materialization: value=%v error=%v", value, err)
		}
	})
}

func TestLazyAudioMaterializationIgnoresDeletedSiblingGlobal(t *testing.T) {
	serialBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		value, err := page.Evaluate(ctx, `(()=>{
 const Offline=OfflineAudioContext;
 delete globalThis.AudioBuffer;
 const context=new Offline(1,8,8000);
 return context.length===8 && !('AudioBuffer' in globalThis);
})()`)
		if err != nil || value != true {
			t.Fatalf("author global mutation affected lazy audio materialization: value=%v error=%v", value, err)
		}
	})
}

func TestLazyWebGLMaterializationPreservesObservableIdentity(t *testing.T) {
	serialBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		value, err := page.Evaluate(ctx, `(()=>{
 const ctor=WebGLRenderingContext,proto=ctor.prototype,method=proto.getParameter;
 const getter=Object.getOwnPropertyDescriptor(WebGLActiveInfo.prototype,'name').get;
 const ctorDescriptor=Object.getOwnPropertyDescriptor(globalThis,'WebGLRenderingContext');
 const methodDescriptor=Object.getOwnPropertyDescriptor(proto,'getParameter');
 const sources=[ctor,method,getter].map(fn=>Function.prototype.toString.call(fn));
 const gl=document.createElement('canvas').getContext('webgl');
 return WebGLRenderingContext===ctor&&WebGLRenderingContext.prototype===proto
   && proto.getParameter===method
   && Object.getOwnPropertyDescriptor(WebGLActiveInfo.prototype,'name').get===getter
   && Object.getOwnPropertyDescriptor(globalThis,'WebGLRenderingContext').value===ctorDescriptor.value
   && Object.getOwnPropertyDescriptor(proto,'getParameter').enumerable===methodDescriptor.enumerable
   && Object.getOwnPropertyDescriptor(proto,'getParameter').configurable===methodDescriptor.configurable
   && [ctor,method,getter].map(fn=>Function.prototype.toString.call(fn)).every((source,index)=>source===sources[index])
   && gl instanceof WebGLRenderingContext;
})()`)
		if err != nil || value != true {
			t.Fatalf("identity changed across lazy materialization: value=%v error=%v", value, err)
		}
	})
}
