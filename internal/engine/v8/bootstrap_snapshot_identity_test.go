//go:build (windows || linux) && amd64

package v8

import (
	"strings"
	"testing"

	gov8 "github.com/maclof/gov8"
)

func TestBootstrapSnapshotIdentityIncludesNativeArtifact(t *testing.T) {
	identity, err := (Factory{}).BootstrapSnapshotIdentity()
	if err != nil {
		t.Fatal(err)
	}
	native, err := gov8.NativeLibraryIdentity()
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(identity, "\x00")
	if len(parts) != 3 || parts[2] != native {
		t.Fatalf("snapshot identity omits the loaded native artifact: %q", identity)
	}
}
