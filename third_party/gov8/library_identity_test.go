//go:build (windows || linux) && amd64

package gov8

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestNativeLibraryIdentityDistinguishesArtifacts(t *testing.T) {
	source, err := shimDLLPath()
	if err != nil {
		t.Fatal(err)
	}
	contents, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var identities, versions []string
	for index := 0; index < 2; index++ {
		// A trailing byte does not change PE/ELF code or version exports, but
		// proves that identity describes the artifact rather than its version.
		if index != 0 {
			contents = append(contents, 0)
		}
		library := filepath.Join(dir, "artifact-"+string(rune('a'+index))+filepath.Ext(source))
		if err := os.WriteFile(library, contents, 0600); err != nil {
			t.Fatal(err)
		}
		output := library + ".json"
		command := exec.Command(executable, "-test.run=^TestNativeLibraryIdentityHelper$", "-test.count=1")
		command.Env = append(os.Environ(), "GOV8_SHIM_LIBRARY="+library,
			"GOV8_IDENTITY_TEST_OUTPUT="+output, "GOV8_IDENTITY_OTHER_PATH="+source)
		if result, err := command.CombinedOutput(); err != nil {
			t.Fatalf("artifact %d: %v\n%s", index, err, result)
		}
		data, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		var result struct{ Identity, Version string }
		if err := json.Unmarshal(data, &result); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(contents)
		if result.Identity != hex.EncodeToString(digest[:]) {
			t.Fatalf("artifact %d has incorrect loaded identity: %s", index, result.Identity)
		}
		identities = append(identities, result.Identity)
		versions = append(versions, result.Version)
	}
	if identities[0] == identities[1] || versions[0] != versions[1] {
		t.Fatalf("same-version artifacts were not distinguished: identities=%v versions=%v", identities, versions)
	}
}

func TestNativeLibraryIdentityHelper(t *testing.T) {
	output := os.Getenv("GOV8_IDENTITY_TEST_OUTPUT")
	if output == "" {
		t.Skip("subprocess helper")
	}
	identity, err := NativeLibraryIdentity()
	if err != nil {
		t.Fatal(err)
	}
	version, err := RuntimeVersionString()
	if err != nil {
		t.Fatal(err)
	}
	// Changing the override after load must not relabel the mapped artifact.
	t.Setenv("GOV8_SHIM_LIBRARY", os.Getenv("GOV8_IDENTITY_OTHER_PATH"))
	again, err := NativeLibraryIdentity()
	if err != nil || again != identity {
		t.Fatalf("loaded identity changed: %s -> %s: %v", identity, again, err)
	}
	data, err := json.Marshal(struct{ Identity, Version string }{identity, version})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(output, data, 0600); err != nil {
		t.Fatal(err)
	}
}
