//go:build (windows || linux) && amd64

package browser

import (
	"os"
	"testing"
)

// Generated WebIDL fallback operations keep their captured descriptor shape
// when V8 materializes the function on the first reflective or ordinary read.
func TestGeneratedLazyOperationsPreserveSurface(t *testing.T) {
	probe := `(()=>{
  const owner=AnalyserNode.prototype;
  const before=Reflect.ownKeys(owner);
  const first=Object.getOwnPropertyDescriptor(owner,'getFloatFrequencyData');
  const second=Object.getOwnPropertyDescriptor(owner,'getFloatFrequencyData');
  if(!first||first.value!==second.value||first.value!==owner.getFloatFrequencyData)return 'identity';
  if(first.value.name!=='getFloatFrequencyData'||first.value.length!==1)return 'metadata';
  if(first.enumerable!==true||first.configurable!==true||first.writable!==true)return 'descriptor';
  if(Function.prototype.toString.call(first.value)!=='function getFloatFrequencyData() { [native code] }')return 'source';
  if(JSON.stringify(before)!==JSON.stringify(Reflect.ownKeys(owner)))return 'key order';
  const replacement=function replacement(){};
  owner.getFloatFrequencyData=replacement;
  if(owner.getFloatFrequencyData!==replacement)return 'assignment';
  delete owner.getFloatFrequencyData;
  if(Object.hasOwn(owner,'getFloatFrequencyData'))return 'deletion';
  return 'ok';
})()`
	page := bootstrapSnapshotPage(t)
	if value := bootstrapSnapshotEvaluate(t, page, probe); value != "ok" {
		t.Fatalf("ordinary generated operation: %v", value)
	}
	bootstrapSnapshotWarm(t, page)
	restored, err := page.ctx.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	if value := bootstrapSnapshotEvaluate(t, restored, probe); value != "ok" {
		t.Fatalf("restored generated operation: %v", value)
	}
	if os.Getenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT") != "1" && !restored.Top.Realm.bootstrapRestored {
		t.Fatal("generated operation probe did not use a restored realm")
	}
}

func TestGeneratedLazyOperationFreezeBeforeFirstRead(t *testing.T) {
	page := bootstrapSnapshotPage(t)
	probe := `(()=>{
  const owner=BiquadFilterNode.prototype;
  const before=Reflect.ownKeys(owner);
  Object.freeze(owner);
  const descriptor=Object.getOwnPropertyDescriptor(owner,'getFrequencyResponse');
  if(!descriptor||typeof descriptor.value!=='function')return 'function';
  if(descriptor.value!==owner.getFrequencyResponse||descriptor.value.length!==3)return 'identity';
  if(descriptor.enumerable!==true||descriptor.configurable!==false||descriptor.writable!==false)return 'descriptor';
  if(JSON.stringify(before)!==JSON.stringify(Reflect.ownKeys(owner)))return 'key order';
  return 'ok';
})()`
	if value := bootstrapSnapshotEvaluate(t, page, probe); value != "ok" {
		t.Fatalf("freeze before generated operation read: %v", value)
	}
	bootstrapSnapshotWarm(t, page)
	restored, err := page.ctx.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	if value := bootstrapSnapshotEvaluate(t, restored, probe); value != "ok" {
		t.Fatalf("restored freeze before generated operation read: %v", value)
	}
}

func TestGeneratedLazyOperationMutationBeforeFirstRead(t *testing.T) {
	page := bootstrapSnapshotPage(t)
	probe := `(()=>{
  const owner=AnalyserNode.prototype;
  const replacement=function replacement(){};
  owner.getByteFrequencyData=replacement;
  if(owner.getByteFrequencyData!==replacement)return 'assignment';
  if(!delete owner.getFloatTimeDomainData||Object.hasOwn(owner,'getFloatTimeDomainData'))return 'deletion';
  Object.defineProperty(owner,'getByteTimeDomainData',{value:replacement,writable:false,enumerable:false,configurable:false});
  const descriptor=Object.getOwnPropertyDescriptor(owner,'getByteTimeDomainData');
  if(descriptor.value!==replacement||descriptor.writable||descriptor.enumerable||descriptor.configurable)return 'redefinition';
  return 'ok';
})()`
	if value := bootstrapSnapshotEvaluate(t, page, probe); value != "ok" {
		t.Fatalf("mutation before generated operation read: %v", value)
	}
	bootstrapSnapshotWarm(t, page)
	restored, err := page.ctx.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	if value := bootstrapSnapshotEvaluate(t, restored, probe); value != "ok" {
		t.Fatalf("restored mutation before generated operation read: %v", value)
	}
}

func TestGeneratedConstructorNativeParityAfterSnapshotRestore(t *testing.T) {
	probe := `(()=>{
  const ctor=CDATASection;
  if(ctor.name!=='CDATASection'||ctor.length!==0)return 'metadata';
  if(ctor.prototype.constructor!==ctor)return 'constructor identity';
  if(Object.getPrototypeOf(ctor)!==Text)return 'interface inheritance';
  if(Object.getPrototypeOf(ctor.prototype)!==Text.prototype)return 'prototype inheritance';
  if(Function.prototype.toString.call(ctor)!=='function CDATASection() { [native code] }')return 'source';
  for(const invoke of [()=>ctor(),()=>new ctor()]){
    try{invoke();return 'accepted unsupported constructor'}catch(error){if(!(error instanceof TypeError))return error.name}
  }
  return 'ok';
})()`
	page := bootstrapSnapshotPage(t)
	if value := bootstrapSnapshotEvaluate(t, page, probe); value != "ok" {
		t.Fatalf("ordinary generated constructor: %v", value)
	}
	bootstrapSnapshotWarm(t, page)
	restored, err := page.ctx.NewPage()
	if err != nil {
		t.Fatal(err)
	}
	if value := bootstrapSnapshotEvaluate(t, restored, probe); value != "ok" {
		t.Fatalf("restored generated constructor: %v", value)
	}
	if os.Getenv("MIMIC_DISABLE_BOOTSTRAP_SNAPSHOT") != "1" && !restored.Top.Realm.bootstrapRestored {
		t.Fatal("generated constructor probe did not use a restored realm")
	}
}
