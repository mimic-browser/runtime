//go:build (windows || linux) && amd64

package gov8_test

import (
	"github.com/maclof/gov8"
	"runtime"
	"testing"
)

func TestHTMLDDA(t *testing.T) {
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
	checkNativeNumberABI(t, iso, scope, ctx)
	gov8.CheckContextCloseReleasesDirectFunctionCallbacks(t, iso)
	ot, err := iso.NewObjectTemplate(scope)
	if err != nil {
		t.Fatal(err)
	}
	if err = ot.MarkAsUndetectable(); err != nil {
		t.Fatal(err)
	}
	if err = ot.SetCallAsFunctionHandler(func(cs *gov8.CallbackScope, args gov8.FunctionCallbackArguments, rv gov8.ReturnValue) {
		rv.SetInt32(42)
	}, gov8.Value{}); err != nil {
		t.Fatal(err)
	}
	obj, ok, err := ot.NewInstance(scope, ctx)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	advSeed(t, scope, ctx, "all", obj.Value)
	for _, expr := range []string{`typeof all === "undefined"`, `!all`, `all == null`, `all == undefined`, `all !== undefined`, `all !== null`, `all() === 42`, `Object(all) === all`, `Object.prototype.toString.call(all) === "[object Object]"`} {
		if got := advEvalText(t, scope, ctx, expr); got != "true" {
			t.Errorf("%s: %s", expr, got)
		}
	}
}

func advNewIso(t *testing.T) *gov8.Isolate {
	t.Helper()
	iso, err := gov8.NewIsolate()
	if err != nil {
		t.Fatalf("NewIsolate: %v", err)
	}
	return iso
}

func advNewCtx(t *testing.T, iso *gov8.Isolate) (*gov8.Scope, *gov8.Context) {
	t.Helper()
	scope, err := iso.NewScope()
	if err != nil {
		t.Fatalf("NewScope: %v", err)
	}
	ctx, err := iso.NewContext()
	if err != nil {
		t.Fatalf("NewContext: %v", err)
	}
	return scope, ctx
}

func advSeed(t *testing.T, scope *gov8.Scope, ctx *gov8.Context, name string, v gov8.Value) {
	t.Helper()
	global, err := ctx.GlobalObject(scope)
	if err != nil {
		t.Fatalf("GlobalObject: %v", err)
	}
	if _, err := global.SetByName(scope, ctx, name, v); err != nil {
		t.Fatalf("SetByName %s: %v", name, err)
	}
}

func advEval(t *testing.T, scope *gov8.Scope, ctx *gov8.Context, src string) gov8.Value {
	t.Helper()
	script, err := ctx.Compile(scope, src, nil)
	if err != nil {
		t.Fatalf("Compile %q: %v", src, err)
	}
	defer func() { _ = script.Close() }()
	v, err := script.Run(scope, nil)
	if err != nil {
		t.Fatalf("Run %q: %v", src, err)
	}
	return v
}

func advEvalText(t *testing.T, scope *gov8.Scope, ctx *gov8.Context, src string) string {
	t.Helper()
	v := advEval(t, scope, ctx, src)
	txt, err := v.ToString(ctx)
	if err != nil {
		t.Fatalf("ToString %q: %v", src, err)
	}
	return txt
}
