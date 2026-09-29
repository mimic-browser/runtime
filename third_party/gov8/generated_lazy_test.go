//go:build (windows || linux) && amd64

package gov8_test

import (
	"runtime"
	"testing"

	"github.com/maclof/gov8"
)

func TestGeneratedLazyInstaller(t *testing.T) {
	if isolatePlatformLifecycle(t) {
		return
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := gov8.Initialize(); err != nil {
		t.Fatal(err)
	}
	defer gov8.Shutdown()
	iso := advNewIso(t)
	defer iso.Close()
	scope, ctx := advNewCtx(t, iso)
	defer scope.Close()
	defer ctx.Close()
	installer, err := iso.NewGeneratedLazyInstaller(scope, ctx)
	if err != nil {
		t.Fatal(err)
	}
	advSeed(t, scope, ctx, "installLazy", installer)
	for _, expression := range []string{
		`var calls=0; installLazy((id,holder,name)=>{calls++; return function generated(){return id+this.base}}); true`,
		`var target={base:7}; installLazy(target,"work",41,0)`,
		`Reflect.ownKeys(target).includes("work") && calls===0`,
		`Object.getOwnPropertyDescriptor(target,"work").value.call(target)===48 && calls===1`,
		`target.work === target.work && target.work.call(target)===48 && calls===1`,
		`Object.getOwnPropertyDescriptor(target,"work").value === target.work`,
		`var assigned={}; installLazy(assigned,"work",1,0) && (assigned.work=99)===99 && assigned.work===99 && calls===1`,
		`var removed={}; installLazy(removed,"work",2,0) && delete removed.work && !("work" in removed) && calls===1`,
		`var redefined={}; installLazy(redefined,"work",3,0) && Object.defineProperty(redefined,"work",{value:81}).work===81 && calls===2`,
		`var sealed={base:0}; installLazy(sealed,"work",4,0) && Object.seal(sealed)===sealed && sealed.work()===4 && !Object.getOwnPropertyDescriptor(sealed,"work").configurable`,
		`var frozen={base:0}; installLazy(frozen,"work",5,0) && Object.freeze(frozen)===frozen && frozen.work()===5 && !Object.getOwnPropertyDescriptor(frozen,"work").writable`,
	} {
		if got := advEvalText(t, scope, ctx, expression); got != "true" {
			t.Fatalf("%s: got %s", expression, got)
		}
	}
	references, err := gov8.GeneratedLazyReferences()
	if err != nil {
		t.Fatal(err)
	}
	if len(references) != 2 {
		t.Fatal("expected two snapshot callback references")
	}
	template, err := iso.NewObjectTemplate(scope)
	if err != nil {
		t.Fatal(err)
	}
	if err := template.SetGeneratedLazyDataProperty("templated", 62, gov8.AttrNone); err != nil {
		t.Fatal(err)
	}
	object, ok, err := template.NewInstance(scope, ctx)
	if err != nil || !ok {
		t.Fatalf("new templated object: %v, %v", ok, err)
	}
	advSeed(t, scope, ctx, "templatedObject", object.Value)
	if got := advEvalText(t, scope, ctx, `Reflect.ownKeys(templatedObject).includes('templated') && templatedObject.templated.call({base:0})===62`); got != "true" {
		t.Fatal(got)
	}
	checkGeneratedConstructorFactory(t, iso)
	checkGeneratedLazySnapshot(t)
}

func checkGeneratedConstructorFactory(t *testing.T, iso *gov8.Isolate) {
	t.Helper()
	scope, ctx := advNewCtx(t, iso)
	defer scope.Close()
	defer ctx.Close()
	factory, err := iso.NewGeneratedConstructorFactory(scope, ctx)
	if err != nil {
		t.Fatal(err)
	}
	advSeed(t, scope, ctx, "constructorFactory", factory)
	for _, expression := range []string{
		`var invocations=[]; constructorFactory((id,receiver,args,target)=>{invocations.push({id,receiver,args,target}); return target ? undefined : args.length}); true`,
		`var C=constructorFactory("Example",19); C.name==="Example" && C.length===0 && Object.hasOwn(C,"prototype")`,
		`C(1,2)===2 && invocations[0].id===19 && invocations[0].target===undefined`,
		`var instance=new C(7); instance instanceof C && invocations[1].receiver===instance && invocations[1].target===C && invocations[1].args[0]===7`,
		`var D=class extends C {}; var child=new D(); child instanceof D && invocations[2].target===D`,
	} {
		if got := advEvalText(t, scope, ctx, expression); got != "true" {
			t.Fatalf("%s: got %s; invocations %s", expression, got, advEvalText(t, scope, ctx, `JSON.stringify(invocations.map(x=>({id:x.id,receiver:typeof x.receiver,target:typeof x.target,args:x.args})))`))
		}
	}
}

func checkGeneratedLazySnapshot(t *testing.T) {
	t.Helper()
	lazyReferences, err := gov8.GeneratedLazyReferences()
	if err != nil {
		t.Fatal(err)
	}
	constructorReferences, err := gov8.GeneratedConstructorReferences()
	if err != nil {
		t.Fatal(err)
	}
	references := append(lazyReferences, constructorReferences...)
	creator, err := gov8.NewSnapshotCreatorWithExternalReferences(references)
	if err != nil {
		t.Fatal(err)
	}
	isolate := creator.Isolate()
	scope, err := isolate.NewScope()
	if err != nil {
		t.Fatal(err)
	}
	bare, err := isolate.NewContext()
	if err != nil {
		t.Fatal(err)
	}
	if err := creator.SetDefaultContext(bare); err != nil {
		t.Fatal(err)
	}
	context, err := isolate.NewContext()
	if err != nil {
		t.Fatal(err)
	}
	installer, err := isolate.NewGeneratedLazyInstaller(scope, context)
	if err != nil {
		t.Fatal(err)
	}
	constructors, err := isolate.NewGeneratedConstructorFactory(scope, context)
	if err != nil {
		t.Fatal(err)
	}
	advSeed(t, scope, context, "installLazy", installer)
	advSeed(t, scope, context, "constructorFactory", constructors)
	if got := advEvalText(t, scope, context, `var calls=0; installLazy((id)=>{calls++; return function(){return id}}); constructorFactory((id)=>{throw new TypeError('Illegal constructor')}); var target={}; installLazy(target,'value',27,0); var C=constructorFactory('Example',29); true`); got != "true" {
		t.Fatal(got)
	}
	index, err := creator.AddContext(context)
	if err != nil {
		t.Fatal(err)
	}
	_ = context.Close()
	_ = bare.Close()
	_ = scope.Close()
	blob, err := creator.CreateBlob(gov8.FunctionCodeKeep)
	if err != nil {
		t.Fatal(err)
	}
	defer blob.Release()
	params := gov8.NewCreateParams().SetExternalReferences(references)
	restored, err := gov8.NewIsolateFromSnapshotWithParams(blob, params)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	restoredScope, err := restored.NewScope()
	if err != nil {
		t.Fatal(err)
	}
	defer restoredScope.Close()
	restoredContext, ok, err := restoredScope.ContextFromSnapshot(index)
	if err != nil || !ok {
		t.Fatalf("restore context: %v, %v", ok, err)
	}
	defer restoredContext.Close()
	if got := advEvalText(t, restoredScope, restoredContext, `Reflect.ownKeys(target).includes('value') && calls===0 && target.value()===27 && target.value===target.value && calls===1 && C.name==='Example' && C.length===0 && (()=>{try{new C();return false}catch(error){return error instanceof TypeError && error.message==='Illegal constructor'}})()`); got != "true" {
		t.Fatal(got)
	}
}
