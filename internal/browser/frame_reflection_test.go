package browser

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestFrameReflectionPreservesDescriptorsAndKeys(t *testing.T) {
	serialBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		value, err := page.Evaluate(ctx, `(()=>{
 const frame=document.createElement('iframe');document.body.appendChild(frame);const child=frame.contentWindow;
 const ctor=child.HTMLScriptElement,prototype=ctor.prototype;
 const descriptor=Object.getOwnPropertyDescriptor(prototype,'src'),again=Object.getOwnPropertyDescriptor(prototype,'src');
 if(!descriptor||!descriptor.enumerable||!descriptor.configurable||descriptor.get!==again.get||descriptor.set!==again.set)return 'src descriptor';
 const script=frame.contentDocument.createElement('script');descriptor.set.call(script,'https://example.test/a.js');
 if(descriptor.get.call(script)!=='https://example.test/a.js')return 'receiver';
 const constructorDescriptor=Object.getOwnPropertyDescriptor(ctor,'prototype');
 if(constructorDescriptor.configurable||constructorDescriptor.writable||constructorDescriptor.value!==prototype)return 'constructor prototype';
 if(Object.getOwnPropertyDescriptor(descriptor.get,'prototype')!==undefined||Reflect.ownKeys(descriptor.get).includes('caller'))return 'getter shape';
 const tag=Object.getOwnPropertyDescriptor(prototype,Symbol.toStringTag);
 if(!tag||tag.value!=='HTMLScriptElement'||tag.writable||tag.enumerable||!tag.configurable||prototype[Symbol.toStringTag]!=='HTMLScriptElement')return 'symbol descriptor';
 if(!Reflect.ownKeys(prototype).includes(Symbol.toStringTag)||!Object.keys(prototype).includes('src'))return 'keys';
 if(!('src' in prototype)||!('toString' in prototype)||'definitelyMissingProperty' in prototype)return 'has';
 const object=child.eval('(()=>{const object={visible:1};Object.defineProperty(object,"fixed",{value:7,writable:true,configurable:false});object[Symbol.for("shared")]=2;const unique=Symbol("unique");object[unique]=3;Object.defineProperty(object,"symbolValue",{value:unique});return object})()');
 const symbols=Object.getOwnPropertySymbols(object),unique=symbols.find(x=>x!==Symbol.for('shared'));
 if(Reflect.set(object,'symbolValue',99)!==false)return 'readonly reflect set';
 let readonlyRejected=false;try{(()=>{'use strict';object.symbolValue=99})()}catch(error){readonlyRejected=error instanceof TypeError}
 if(!readonlyRejected||object.symbolValue!==unique)return 'readonly strict set';
 if(symbols.length!==2||object[Symbol.for('shared')]!==2||object[unique]!==3||Object.getOwnPropertySymbols(object)[1]!==unique)return 'symbol identity';
 if(Object.getOwnPropertyDescriptor(object,'symbolValue').value!==unique||object.symbolValue!==unique)return 'symbol value';
 if(Object.getOwnPropertyDescriptor(object,unique).value!==3||Object.getOwnPropertyDescriptor(object,Symbol('unique'))!==undefined)return 'symbol lookup';
 const localA=Symbol('same'),localB=Symbol('same');object[localA]=11;object[localB]=12;
 if(object[localA]!==11||object[localB]!==12||Object.getOwnPropertyDescriptor(object,localA).value!==11||!Reflect.ownKeys(object).includes(localA)||!Reflect.ownKeys(object).includes(localB))return 'local symbols';
 if(Object.getOwnPropertyDescriptor(object,'fixed').value!==7)return 'fixed initial';
 object.fixed=9;if(Object.getOwnPropertyDescriptor(object,'fixed').value!==9||object.fixed!==9)return 'fixed update';
 if(Object.defineProperty(object,'visible',{value:99})!==object||child.eval('(object=>object.visible)')(object)!==99)return 'define owner';
 if(!Reflect.deleteProperty(object,'visible')||child.eval('(object=>"visible" in object)')(object))return 'delete owner';
 Object.defineProperty(object,'visible',{value:1,writable:true,enumerable:true,configurable:true});
 for(const operation of [()=>Object.setPrototypeOf(object,null)]){
 let rejected=false;try{operation()}catch(error){rejected=error.name==='NotSupportedError'}if(!rejected)return 'unsupported mutation accepted';
 }
 if(object.visible!==1||!Object.isExtensible(object)||!Object.keys(object).includes('visible'))return 'rejected mutation changed source';
 if(Object.preventExtensions(object)!==object||Object.isExtensible(object)||child.eval('(object=>Object.isExtensible(object))')(object))return 'preventExtensions owner';
 if(Reflect.set(object,'newProperty',3)||Reflect.defineProperty(object,'newProperty',{value:3}))return 'preventExtensions mutation';
 if(!Reflect.deleteProperty(object,'visible')||Reflect.ownKeys(object).includes('visible'))return 'nonextensible delete';
 const hostile=child.eval('(()=>{globalThis.conversions=0;const object={toString(){conversions++;throw Error("conversion")}};Object.defineProperty(object,"explosive",{get(){conversions++;throw Error("getter")},enumerable:true,configurable:true});return object})()');
 if(!Reflect.ownKeys(hostile).includes('explosive')||typeof Object.getOwnPropertyDescriptor(hostile,'explosive').get!=='function'||child.eval('conversions')!==0)return 'reflection invoked user code';
 return true;
 })()`)
		if err != nil || value != true {
			t.Fatalf("remote reflection: %v, %v", value, err)
		}
	})
}

func TestFrameNonconfigurableAccessorDescriptorBoundary(t *testing.T) {
	serialBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		value, err := page.Evaluate(ctx, `(()=>{
 const frame=document.createElement('iframe');document.body.appendChild(frame);const child=frame.contentWindow;
 const object=child.eval('globalThis.reads=0;globalThis.getter=()=>{reads++;return 7};globalThis.object={};Object.defineProperty(object,"value",{get:getter,enumerable:true,configurable:false});object');
 let outcome;try{const descriptor=Object.getOwnPropertyDescriptor(object,'value');
 if(descriptor.configurable||!descriptor.enumerable||descriptor.set!==undefined||descriptor.get!==child.eval('getter')||descriptor.get!==Object.getOwnPropertyDescriptor(object,'value').get)return 'wrong descriptor';outcome='supported';
 }catch(error){if(error.name!=='NotSupportedError')throw error;outcome='unsupported'}
 if(child.eval('reads===0&&Object.getOwnPropertyDescriptor(object,"value").get===getter&&!Object.getOwnPropertyDescriptor(object,"value").configurable')!==true)return 'source changed';
 return outcome;
 })()`)
		expected := "supported"
		if strings.HasSuffix(t.Name(), "/goja") {
			expected = "unsupported"
		}
		if err != nil || value != expected {
			t.Fatalf("accessor boundary: %v, %v; expected %s", value, err, expected)
		}
	})
}

func TestFrameIntegrityTracksOwnerMutations(t *testing.T) {
	serialBrowserTest(t)
	historyTestPages(t, func(t *testing.T, page *Page) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		value, err := page.Evaluate(ctx, `(() => {
  const frame = document.createElement('iframe');
  document.body.appendChild(frame);
  const child = frame.contentWindow;
  const values = child.eval(`+"`"+`(() => {
    const object = { mutable: 1, nested: {} };
    globalThis.integrityValues = [object, [1, 2], function callable() {}];
    return integrityValues;
  })()`+"`"+`);
  const object = values[0], nested = object.nested;
  child.eval('integrityValues[0].mutable = 2; Object.freeze(integrityValues[0]); Object.freeze(integrityValues[1]); Object.freeze(integrityValues[2]);');
  for (const value of [object, values[1], values[2]]) {
    if (Object.isExtensible(value) || !Object.isFrozen(value)) return 'owner freeze';
    if (Object.freeze(value) !== value) return 'repeat identity';
  }
  if (object.mutable !== 2 || object.nested !== nested) return 'live data identity';
  if (Reflect.set(object, 'mutable', 3) || Reflect.deleteProperty(object, 'mutable')) return 'frozen mutation';
  const sealed = child.eval('globalThis.integritySealed = { value: 1 }; Object.seal(integritySealed); integritySealed');
  if (!Object.isSealed(sealed) || Object.isFrozen(sealed)) return 'owner seal';
  child.eval('integritySealed.value = 4');
  if (sealed.value !== 4 || Object.getOwnPropertyDescriptor(sealed, 'value').value !== 4) return 'sealed live value';
  const open = child.eval('globalThis.integrityOpen = { value: 1 }; Object.preventExtensions(integrityOpen); integrityOpen');
  if (Object.isExtensible(open)) return 'owner nonextensible';
  child.eval('delete integrityOpen.value');
  if (Reflect.ownKeys(open).length || Object.getOwnPropertyDescriptor(open, 'value')) return 'owner deletion';
  return true;
})()`)
		if err != nil || value != true {
			t.Fatalf("remote integrity ownership: %v, %v", value, err)
		}
	})
}
