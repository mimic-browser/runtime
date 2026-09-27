//go:build (windows || linux) && amd64

package v8

import (
	"context"
	"testing"
)

func TestConnectedRealmsRetainOwnerAndIsolateGlobals(t *testing.T) {
	for _, mode := range []string{"ordinary", "snapshot"} {
		t.Run(mode, func(t *testing.T) {
			var root *adapter
			if mode == "ordinary" {
				root = (Factory{}).New().(*adapter)
			} else {
				seed, err := (Factory{}).BuildBootstrapSnapshot(context.Background(), `globalThis.seedMarker={value:42};`)
				if err != nil {
					t.Fatal(err)
				}
				defer seed.Close()
				runtime, err := seed.NewRuntime()
				if err != nil {
					t.Fatal(err)
				}
				root = runtime.(*adapter)
			}
			defer root.Close()
			childRuntime, restored, err := root.NewRealmRuntime(mode == "snapshot")
			if err != nil || restored != (mode == "snapshot") {
				t.Fatalf("connected restore: %v %v", restored, err)
			}
			child := childRuntime.(*adapter)
			defer child.Close()
			if child.owner != root.owner {
				t.Fatal("connected realm moved to another native owner")
			}
			if _, err := root.Eval(context.Background(), `Array.prototype.parentMutation=true;globalThis.parentMarker=7`, "parent.js"); err != nil {
				t.Fatal(err)
			}
			value, err := child.Eval(context.Background(), `Array.prototype.parentMutation===undefined&&typeof parentMarker==='undefined'`, "child.js")
			if err != nil || value.Export() != true {
				t.Fatalf("realm state isolation: %v %v", value, err)
			}
			child.ReleaseValue(value)
			if err := root.Close(); err != nil {
				t.Fatal(err)
			}
			value, err = child.Eval(context.Background(), `6*7`, "after-parent-close.js")
			if err != nil || value.String() != "42" {
				t.Fatalf("child outlived root: %v %v", value, err)
			}
			child.ReleaseValue(value)
			if err := child.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-root.owner.done:
			default:
				t.Fatal("last connected realm retained the native owner")
			}
		})
	}
}

func TestConnectedSnapshotBareRealmDoesNotInheritPlatformSeed(t *testing.T) {
	snapshot, err := (Factory{}).BuildBootstrapSnapshot(context.Background(), `globalThis.seedMarker=42;`)
	if err != nil {
		t.Fatal(err)
	}
	defer snapshot.Close()
	runtime, err := snapshot.NewRuntime()
	if err != nil {
		t.Fatal(err)
	}
	root := runtime.(*adapter)
	defer root.Close()
	childRuntime, restored, err := root.NewRealmRuntime(false)
	if err != nil || restored {
		t.Fatalf("bare connected realm: %v %v", restored, err)
	}
	child := childRuntime.(*adapter)
	defer child.Close()
	value, err := child.Eval(context.Background(), `typeof seedMarker`, "bare.js")
	if err != nil || value.String() != "undefined" {
		t.Fatalf("platform seed leaked into bare realm: %v %v", value, err)
	}
	child.ReleaseValue(value)
}

func TestConnectedRealmMicrotaskRetirement(t *testing.T) {
	root := (Factory{}).New().(*adapter)
	defer root.Close()
	runtime, _, err := root.NewRealmRuntime(false)
	if err != nil {
		t.Fatal(err)
	}
	child := runtime.(*adapter)
	defer child.Close()
	value, err := child.Eval(context.Background(), `
globalThis.events = [];
Promise.resolve().then(() => events.push(1));
true;
`, "child.js")
	if err != nil {
		t.Fatal(err)
	}
	child.ReleaseValue(value)
	if err := child.DeactivatePromiseJobs(); err != nil {
		t.Fatal(err)
	}
	if err := root.MicrotaskCheckpoint(); err != nil {
		t.Fatal(err)
	}
	value, err = child.Eval(context.Background(), `events.length`, "retained.js")
	if err != nil || value.String() != "0" {
		t.Fatalf("retired jobs: %v %v", value, err)
	}
	child.ReleaseValue(value)
	value, err = child.Eval(context.Background(), `
Promise.resolve().then(() => events.push(2));
true;
`, "retained-new-jobs.js")
	if err != nil {
		t.Fatal(err)
	}
	child.ReleaseValue(value)
	value, err = root.Eval(context.Background(), `
globalThis.activeDone = false;
Promise.resolve().then(() => (activeDone = true));
true;
`, "active.js")
	if err != nil {
		t.Fatal(err)
	}
	root.ReleaseValue(value)
	if err := root.MicrotaskCheckpoint(); err != nil {
		t.Fatal(err)
	}
	value, err = child.Eval(context.Background(), `events.length`, "retained-check.js")
	if err != nil || value.String() != "0" {
		t.Fatalf("new retired jobs: %v %v", value, err)
	}
	child.ReleaseValue(value)
	value, err = root.Eval(context.Background(), `activeDone`, "active-check.js")
	if err != nil || value.Export() != true {
		t.Fatalf("active sibling jobs: %v %v", value, err)
	}
	root.ReleaseValue(value)
	if err := child.DeactivatePromiseJobs(); err != nil {
		t.Fatal(err)
	}
}
