//go:build windows && amd64

package v8

import (
	"context"
	"runtime"
	"testing"
)

func TestBootstrapThreadPolicyRestoresOnEveryExit(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	before, err := readThreadPowerPolicy()
	if err != nil {
		t.Skip(err)
	}
	for _, source := range []string{"globalThis.value = 42", "throw new Error('seed failure')", "function {"} {
		blob, buildErr := buildBootstrapSnapshot(context.Background(), source)
		if source == "globalThis.value = 42" && buildErr != nil {
			t.Fatal(buildErr)
		}
		if source != "globalThis.value = 42" && buildErr == nil {
			t.Fatal("invalid seed accepted")
		}
		if blob != nil {
			if err := blob.Release(); err != nil {
				t.Fatal(err)
			}
		}
		after, err := readThreadPowerPolicy()
		if err != nil || after != before {
			t.Fatalf("builder changed calling thread policy: before=%+v after=%+v err=%v", before, after, err)
		}
	}
}

func TestPageThreadPolicyRestoresOriginalState(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	before, err := readThreadPowerPolicy()
	if err != nil {
		t.Skip(err)
	}
	restore := configurePageThreadPolicy()
	if restore == nil {
		after, err := readThreadPowerPolicy()
		if err != nil || after != before {
			t.Fatalf("policy changed without restore: %+v %+v %v", before, after, err)
		}
		return
	}
	defer func() {
		if err := restore(); err != nil {
			t.Error(err)
		}
	}()
	active, err := readThreadPowerPolicy()
	if err != nil || active.Control&1 == 0 || active.State&1 != 0 {
		t.Fatalf("active policy: %+v %v", active, err)
	}
	if err := restore(); err != nil {
		t.Fatal(err)
	}
	after, err := readThreadPowerPolicy()
	if err != nil || after != before {
		t.Fatalf("policy not restored: %+v %+v %v", before, after, err)
	}
}
