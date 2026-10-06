package main

import (
	"context"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/moreveal/mimic/internal/workload"
)

func TestProfileCLIProcess(t *testing.T) {
	if os.Getenv("MIMIC_TEST_PROFILE_CLI") != "1" {
		return
	}
	flag.CommandLine = flag.NewFlagSet("mimic", flag.ExitOnError)
	os.Args = []string{"mimic", "--profile", os.Getenv("MIMIC_TEST_PROFILE_PATH"), "--listen", "127.0.0.1:0"}
	main()
	os.Exit(0)
}

func rejectProfileCLI(t *testing.T, path, diagnostic string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestProfileCLIProcess$")
	cmd.Env = append(os.Environ(), "MIMIC_TEST_PROFILE_CLI=1", "MIMIC_TEST_PROFILE_PATH="+path)
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil || err == nil || !strings.Contains(string(out), diagnostic) || strings.Contains(string(out), "Mimic listening") {
		t.Fatalf("profile was not rejected before listening: %v; %s", err, out)
	}
}

func TestProfileCLIRejectsRemovedConfiguration(t *testing.T) {
	// --profile now selects a workload artifact. Environment JSON remains a
	// CDP import/export format and must never become CLI configuration again.
	path := filepath.Join(t.TempDir(), "environment.json")
	if err := os.WriteFile(path, []byte(`{"browserMode":"headless"}`), 0600); err != nil {
		t.Fatal(err)
	}
	rejectProfileCLI(t, path, "Cannot load workload profile: zip: not a valid zip file")
}

func TestProfileCLIValidatesWorkloadBuild(t *testing.T) {
	path := filepath.Join(t.TempDir(), "workload.mprofile")
	profile := workload.Profile{
		Format: "mimic-workload-profile", Version: 1, RuntimeABI: workload.RuntimeABI,
		Engine: "v8", BrowserMode: "headful", Chrome: 152,
		BuildSHA256: strings.Repeat("0", 64), Confidence: "empirical-request-scoped",
		CaptureSHA256: []string{strings.Repeat("1", 64)},
	}
	if err := workload.WriteProfile(path, profile); err != nil {
		t.Fatal(err)
	}
	rejectProfileCLI(t, path, "Workload profile was validated with a different Mimic build")
}
