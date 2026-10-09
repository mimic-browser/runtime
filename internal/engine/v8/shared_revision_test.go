//go:build (windows || linux) && amd64

package v8

import (
	"context"
	"fmt"
	"testing"
)

func TestSharedRevisionPublicationAndViewLifetime(t *testing.T) {
	r := (Factory{}).New().(*adapter)
	defer r.Close()
	buffer, revision, err := r.NewSharedRevision()
	if err != nil {
		t.Fatal(err)
	}
	defer revision.Close()
	if err := r.Set("revisionBuffer", buffer); err != nil {
		t.Fatal(err)
	}
	r.ReleaseValue(buffer)
	if _, err := r.Eval(context.Background(), `globalThis.revisionWord = new BigUint64Array(revisionBuffer); delete globalThis.revisionBuffer`, "revision-view.js"); err != nil {
		t.Fatal(err)
	}
	// Include values beyond JavaScript's exact Number range: the revision must
	// cross the bridge intact, without an ordinary floating-point conversion.
	for _, next := range []uint64{0, 1, 1<<53 + 3, 1<<63 + 17} {
		revision.Publish(next)
		value, err := r.Eval(context.Background(), `Atomics.load(revisionWord, 0).toString()`, "revision-read.js")
		if err != nil {
			t.Fatal(err)
		}
		want := fmt.Sprint(next)
		if value.Export() != want {
			t.Fatalf("published %d, read %v", next, value.Export())
		}
		r.ReleaseValue(value)
	}
	if err := revision.Close(); err != nil {
		t.Fatal(err)
	}
	// Closing the publisher's counted reference must not free a reachable view.
	value, err := r.Eval(context.Background(), `Atomics.load(revisionWord, 0).toString()`, "revision-after-close.js")
	if err != nil || value.Export() != "9223372036854775825" {
		t.Fatalf("view after publisher close: %v %v", value, err)
	}
}
