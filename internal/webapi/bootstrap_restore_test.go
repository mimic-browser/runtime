package webapi

import (
	"strings"
	"testing"

	"github.com/dop251/goja"
)

// handwrittenSurfaceSection deliberately uses syntax-insensitive markers.
// Authored JavaScript is formatted independently, so tests must not depend on
// whitespace around operators when extracting production closures.
func handwrittenSurfaceSection(t *testing.T, startMarker, endMarker string) string {
	t.Helper()
	start := strings.Index(handwrittenSurface, startMarker)
	if start < 0 {
		t.Fatalf("start marker %q missing", startMarker)
	}
	end := strings.Index(handwrittenSurface[start:], endMarker)
	if end < 0 {
		t.Fatalf("end marker %q missing", endMarker)
	}
	return handwrittenSurface[start : start+end]
}

// Exercise the production restore closure independently of the browser host.
// Full snapshot deserialization and cross-frame behavior belong to browser tests.
func TestBootstrapRestoreRebindsState(t *testing.T) {
	section := handwrittenSurfaceSection(t, "globalThis.__mimicRestoreBootstrap", "globalThis.__mimicEvalSourceResolver")
	runtime := goja.New()
	_, err := runtime.RunString(`
 const window=globalThis,document={},documentPolicy={},permissionsPolicySlots=new WeakMap();
 const originalPolicy={clauses:new Map([['old','()']]),origin:'old'};
 permissionsPolicySlots.set(documentPolicy,originalPolicy);
 let host,hostToken=1,bridgeRealmID="old-realm",intlEnvironment={},security={},windowRelations,windowTop,windowParent,frameElementCache={},uaData={};
 const remoteWindowCache=new Map(),remoteDocumentCache=new Map(),crossRealmCache=new Map(),crossRealmSymbols=new Map(),crossRealmSymbolReferences=new Map(),localCrossRealmSymbols=new Map(),elementWrappers=new Map(),documentWrappers=new Map(),tracedAccesses=new Map();
 const caches=[remoteWindowCache,remoteDocumentCache,crossRealmCache,crossRealmSymbols,crossRealmSymbolReferences,localCrossRealmSymbols,elementWrappers,documentWrappers,tracedAccesses];
 const setTraceActive=enabled=>{tracedAccesses.clear()};
 for(const cache of caches)cache.set('stale',{});
 let nextCrossRealmSymbolID=4,root=7;
 const bootstrapRestoreHooks=[()=>{root=host.documentRootID()}];
 const callback=()=>root;
 const bootstrapCallbacks=[['installFrameReferenceBridge',[callback,callback,callback]],['setDOMQueryCallback',[callback]],['registerFormSnapshot',[callback]],['registerShadowSnapshot',[callback,callback,callback]]];
 const calls=[];
 class PermissionsPolicy {constructor(token,data){if(token!==2)throw Error('token');permissionsPolicySlots.set(this,{clauses:new Map([['fresh','*']]),origin:data.origin})}}
 const remoteWindow=id=>({id});
 const freshHost={token:()=>2,selfRealmID:()=>"new-realm",intlEnvironment:()=>({locale:'en',timeZone:'UTC'}),documentSecurity:()=>({secureContext:true}),permissionsPolicy:()=>({origin:'https://new.test'}),windowRelations:()=>({self:9,top:10,parent:9}),documentRootID:()=>99,ready:()=>calls.push('ready')};
 for(const [name,args] of bootstrapCallbacks)freshHost[name]=(...actual)=>{if(actual.length!==args.length||actual.some((v,i)=>v!==args[i]))throw Error('callback identity');calls.push(name)};
 ` + section + `
 __mimicRestoreBootstrap(freshHost);
 if(host!==freshHost||hostToken!==2||bridgeRealmID!=="new-realm"||intlEnvironment.locale!=='en'||!security.secureContext)throw Error('host state');
 if(permissionsPolicySlots.get(documentPolicy)!==originalPolicy||originalPolicy.origin!=='https://new.test'||originalPolicy.clauses.has('old'))throw Error('canonical policy');
 // Restoring a bootstrap must not create native WindowProxy references before
 // its profile/host has been rebound. Topology IDs are eager; objects are lazy.
 if(windowRelations.self!==9||windowRelations.top!==10||windowRelations.parent!==9||windowTop!==undefined||windowParent!==undefined||root!==99)throw Error('topology/root');
 if(caches.some(cache=>cache.size)||nextCrossRealmSymbolID!==0||frameElementCache!==undefined||uaData!==undefined)throw Error('stale cache');
 if(calls.join(',')!=='installFrameReferenceBridge,setDOMQueryCallback,registerFormSnapshot,registerShadowSnapshot,ready')throw Error('registration order');
 `)
	if err != nil {
		t.Fatal(err)
	}
}

func TestBootstrapSourceParsesWithRestore(t *testing.T) {
	if _, err := goja.Compile("bootstrap", Surface("", nil), false); err != nil {
		t.Fatal(err)
	}
}

// Expected descriptor and assignment behavior was measured in frozen Chrome
// 152.0.7977.83, including parent replacing itself with a data property.
func TestBootstrapWindowTopologyDescriptors(t *testing.T) {
	section := handwrittenSurfaceSection(t, "const getWindowTop", "window.__receiveFrameMessage")
	runtime := goja.New()
	_, err := runtime.RunString(`const window=globalThis;let active=true;const host={documentActive:()=>active};let windowTop=window,windowParent=window;` + nativeFunctionsSurface + section + `
 Object.defineProperty(window,'top',{configurable:false});
 const parentDescriptor=Object.getOwnPropertyDescriptor(window,'parent'),topDescriptor=Object.getOwnPropertyDescriptor(window,'top');
 if(parentDescriptor.get.name!=='get parent'||parentDescriptor.get.length!==0||parentDescriptor.set.name!=='set parent'||parentDescriptor.set.length!==1||!parentDescriptor.enumerable||!parentDescriptor.configurable)throw Error('parent descriptor');
 if(topDescriptor.get.name!=='get top'||topDescriptor.get.length!==0||topDescriptor.set!==undefined||!topDescriptor.enumerable||topDescriptor.configurable)throw Error('top descriptor');
 if(String(parentDescriptor.get)!=='function get parent() { [native code] }'||String(parentDescriptor.set)!=='function set parent() { [native code] }')throw Error('native accessors');
 const nextParent={};windowParent=nextParent;windowTop=nextParent;
 if(parent!==nextParent||top!==nextParent)throw Error('restored cells');
 active=false;if(parent!==null||top!==null)throw Error('inactive relations');active=true;
 if(!Reflect.set(window,'parent',17))throw Error('parent write');
 const replaced=Object.getOwnPropertyDescriptor(window,'parent');
 if(replaced.value!==17||!replaced.writable||!replaced.enumerable||!replaced.configurable||replaced.get!==undefined)throw Error('replaceable parent');
 (function(){'use strict';window.parent=18})();
 if(!Reflect.deleteProperty(window,'parent')||typeof parent!=='undefined')throw Error('parent delete');
 if(Reflect.set(window,'top',17)||Reflect.deleteProperty(window,'top'))throw Error('unforgeable top');
 let threw=false;try{(function(){'use strict';window.top=17})()}catch(error){threw=error instanceof TypeError}if(!threw)throw Error('top strict assignment');
 `)
	if err != nil {
		t.Fatal(err)
	}
}
