//go:build (windows || linux) && amd64

package v8

import (
	"context"
	"testing"
)

func TestHostPromiseIgnoresAuthorConstructor(t *testing.T) {
	runtime := (Factory{}).New().(*adapter)
	defer runtime.Close()
	ctx := context.Background()
	_, err := runtime.Eval(ctx, `globalThis.Promise=function AuthorPromise(){throw new Error('author constructor')}`, "author-promise.js")
	if err != nil {
		t.Fatal(err)
	}
	promise := runtime.NewPromise()
	if promise.Value == nil {
		t.Fatal("host Promise consulted author constructor")
	}
	if err := promise.Resolve("ok"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.MicrotaskCheckpoint(); err != nil {
		t.Fatal(err)
	}
	value, settled, err := runtime.Await(promise.Value)
	if err != nil || !settled || value.String() != "ok" {
		t.Fatalf("native host Promise settlement: %v %v %v", value, settled, err)
	}
	runtime.ReleaseValue(promise.Value)
	runtime.ReleaseValue(value)
}
