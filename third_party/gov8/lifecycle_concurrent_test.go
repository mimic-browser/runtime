//go:build (windows || linux) && amd64

package gov8

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestConcurrentIsolateCreationGate(t *testing.T) {
	const childKey = "GOV8_CONCURRENT_ISOLATE_GATE_CHILD"
	if os.Getenv(childKey) != "1" {
		executable, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		command := exec.Command(executable, "-test.run=^TestConcurrentIsolateCreationGate$", "-test.count=1")
		command.Env = append(os.Environ(), childKey+"=1")
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("isolated lifecycle check: %v\n%s", err, output)
		}
		return
	}
	if err := Initialize(); err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	finished := make(chan struct{}, 2)
	for range 2 {
		go func() {
			if err := beginIsolateCreate(); err != nil {
				t.Errorf("begin isolate creation: %v", err)
				finished <- struct{}{}
				return
			}
			entered <- struct{}{}
			<-release
			abandonIsolateCreate()
			finished <- struct{}{}
		}()
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("independent isolate creations blocked each other")
		}
	}
	disposed := make(chan error, 1)
	go func() {
		_, err := Dispose()
		disposed <- err
	}()
	select {
	case err := <-disposed:
		close(release)
		t.Fatalf("platform disposed with in-flight isolate creations: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	for range 2 {
		<-finished
	}
	if err := <-disposed; err != nil {
		t.Fatal(err)
	}
	if err := DisposePlatform(); err != nil {
		t.Fatal(err)
	}
}
