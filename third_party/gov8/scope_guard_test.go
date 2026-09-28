//go:build (windows || linux) && amd64

package gov8_test

import (
	"runtime"
	"testing"

	"github.com/maclof/gov8"
)

func TestExecutionGuardsKeepScopeCloseAndLIFOChecks(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := gov8.Initialize(); err != nil {
		t.Fatal(err)
	}
	defer gov8.Shutdown()
	iso := advNewIso(t)
	defer iso.Close()
	scope, err := iso.NewScope()
	if err != nil {
		t.Fatal(err)
	}
	guard, err := scope.NewDisallowJavascriptExecutionScope(gov8.DumpOnFailure)
	if err != nil {
		t.Fatal(err)
	}
	allow, err := guard.NewAllowJavascriptExecutionScope()
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.Close(); err == nil {
		t.Fatal("disallow guard closed before nested allow guard")
	}
	if err := scope.Close(); err == nil {
		t.Fatal("scope closed while execution guards were active")
	}
	if err := allow.Close(); err != nil {
		t.Fatal(err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if err := scope.Close(); err != nil {
		t.Fatal(err)
	}
}
