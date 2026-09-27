//go:build (windows || linux) && amd64

package gov8

import (
	"fmt"
	"sync"
	"testing"
)

func TestVersionStringsFromFreshGoroutines(t *testing.T) {
	version, err := EngineVersion()
	if err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("%d.%d.%d.%d-rusty", version.Major, version.Minor, version.Build, version.Patch)
	var workers sync.WaitGroup
	for index := 0; index < 64; index++ {
		workers.Go(func() {
			// Fresh goroutines exercise stack growth through the native call
			// bridge; warm actor stacks can conceal an invalid uintptr buffer.
			build, buildErr := VersionString()
			runtime, runtimeErr := RuntimeVersionString()
			if buildErr != nil || runtimeErr != nil || build != want || runtime != want {
				t.Errorf("unstable native version strings: build=%q runtime=%q errors=%v/%v; want %q", build, runtime, buildErr, runtimeErr, want)
			}
		})
	}
	workers.Wait()
}
