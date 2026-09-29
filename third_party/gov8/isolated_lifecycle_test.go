//go:build (windows || linux) && amd64

package gov8_test

import (
	"os"
	"os/exec"
	"testing"
)

// V8 can be initialized and disposed only once per process. Run each test
// that owns the platform lifecycle in a fresh test process.
func isolatePlatformLifecycle(t *testing.T) bool {
	t.Helper()
	const key = "GOV8_ISOLATED_LIFECYCLE_TEST"
	if os.Getenv(key) == t.Name() {
		return false
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^"+t.Name()+"$", "-test.count=1")
	command.Env = append(os.Environ(), key+"="+t.Name())
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("isolated V8 lifecycle: %v\n%s", err, output)
	}
	return true
}
