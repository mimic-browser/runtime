package optimize

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestOptimizeDefaultEndpointCoexistsWithFixedListener(t *testing.T) {
	binary := os.Getenv("MIMIC_OPTIMIZE_TEST_BINARY")
	if binary == "" {
		t.Skip("set MIMIC_OPTIMIZE_TEST_BINARY to a fresh Mimic build")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:9222")
	if err != nil {
		t.Skip("port 9222 is already owned by another process")
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("existing listener"))
	})}
	go server.Serve(listener)
	defer server.Close()
	t.Setenv("MIMIC_ORACLE_HELPER", "1")
	t.Setenv("MIMIC_DATA_DIR", t.TempDir())
	command := exec.Command(binary, "optimize", "--name", "coexists", "--browser-mode", "headless", "--max-trials", "0", "--search-time", "0", "--repetitions", "3", "--timeout", "3s", "--", os.Args[0], "-test.run=TestExternalOracleHelper", "--", "pass")
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "Optimization complete") {
		t.Fatalf("default dynamic endpoint conflicted with existing 9222 listener: %v\n%s", err, output)
	}
	response, err := http.Get("http://127.0.0.1:9222/")
	if err != nil {
		t.Fatal("Optimize interfered with the existing listener", err)
	}
	response.Body.Close()
}

func TestOptimizeRecordsAndValidatesDistinctExternalInputs(t *testing.T) {
	binary := os.Getenv("MIMIC_OPTIMIZE_TEST_BINARY")
	if binary == "" {
		t.Skip("set MIMIC_OPTIMIZE_TEST_BINARY to a fresh Mimic build")
	}
	t.Setenv("MIMIC_ORACLE_HELPER", "1")
	data := t.TempDir()
	t.Setenv("MIMIC_DATA_DIR", data)
	states := filepath.Join(t.TempDir(), "states.json")
	if err := os.WriteFile(states, []byte(`{"states":[{"name":"One","env":{"ARTICLE":"one"}},{"name":"Two","env":{"ARTICLE":"two"}}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "optimize", "--name", "states", "--states-file", states, "--result-env", "WORKLOAD_RESULT", "--browser-mode", "headless", "--max-trials", "0", "--search-time", "0", "--repetitions", "3", "--timeout", "3s", "--", os.Args[0], "-test.run=TestExternalOracleHelper", "--", "result-state")
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), "Replay stable: 6/6") {
		t.Fatalf("multi-state orchestration failed: %v\n%s", err, output)
	}
	paths, err := filepath.Glob(filepath.Join(data, "optimization", "run-*", "report.json"))
	if err != nil || len(paths) != 1 {
		t.Fatalf("missing multi-state report: %v %v", paths, err)
	}
	body, err := os.ReadFile(paths[0])
	var report Report
	if err != nil || json.Unmarshal(body, &report) != nil || !report.Success || report.RecordedStates != 2 || len(report.StateSummary) != 2 {
		t.Fatalf("incomplete independent validation report: %s err=%v", body, err)
	}
	for state := 0; state < 2; state++ {
		if report.StateSummary[state]["Optimized"].Passes != 3 {
			t.Fatalf("state %d did not pass matched validation", state)
		}
	}
}

func TestNativeRunnerExternalOracle(t *testing.T) {
	binary := os.Getenv("MIMIC_OPTIMIZE_TEST_BINARY")
	if binary == "" {
		t.Skip("set MIMIC_OPTIMIZE_TEST_BINARY to a fresh Mimic build")
	}
	t.Setenv("MIMIC_ORACLE_HELPER", "1")
	for _, v := range []struct {
		mode, status string
		timeout      time.Duration
	}{{"pass", "PASS", time.Second}, {"fail", "FAIL", time.Second}, {"timeout", "TIMEOUT", 100 * time.Millisecond}, {"misdirected", "UNSUPPORTED", time.Second}} {
		t.Run(v.mode, func(t *testing.T) {
			dir := t.TempDir()
			r := Runner{Config: Config{Executable: binary, Directory: dir, Listen: "127.0.0.1:0", Engine: "v8", BrowserMode: "headless", Command: []string{os.Args[0], "-test.run=TestExternalOracleHelper", "--", v.mode}, Timeout: v.timeout}}
			row := r.run(context.Background(), trial{Label: v.mode, Record: true, Capture: filepath.Join(dir, "environment.mcap")})
			if row.Status != v.status || row.WorkloadStatus != func() string {
				if v.mode == "misdirected" {
					return "PASS"
				}
				return v.status
			}() {
				t.Fatalf("oracle classification: %+v", row)
			}
		})
	}
}

func TestNativeRunnerOptionalResultRemainsAnAdditionalOracle(t *testing.T) {
	binary := os.Getenv("MIMIC_OPTIMIZE_TEST_BINARY")
	if binary == "" {
		t.Skip("set MIMIC_OPTIMIZE_TEST_BINARY to a fresh Mimic build")
	}
	t.Setenv("MIMIC_ORACLE_HELPER", "1")
	dir := t.TempDir()
	runner := Runner{Config: Config{Executable: binary, Directory: dir, Listen: "127.0.0.1:0", Engine: "v8", BrowserMode: "headless", ResultEnv: "WORKLOAD_RESULT", Timeout: time.Second}}
	for index, mode := range []string{"result-good", "result-good", "result-changed", "pass"} {
		runner.Config.Command = []string{os.Args[0], "-test.run=TestExternalOracleHelper", "--", mode}
		row := runner.run(context.Background(), trial{Label: mode, Record: true, Capture: filepath.Join(dir, mode+".mcap")})
		expected := "PASS"
		if index >= 2 {
			expected = "FAIL"
		}
		if row.Status != expected || row.WorkloadStatus != "PASS" {
			t.Fatalf("structured result comparison bypassed/replaced the external oracle: %+v", row)
		}
	}
}

func TestNativeRunnerStateInputsAndResultOraclesAreIsolated(t *testing.T) {
	binary := os.Getenv("MIMIC_OPTIMIZE_TEST_BINARY")
	if binary == "" {
		t.Skip("set MIMIC_OPTIMIZE_TEST_BINARY to a fresh Mimic build")
	}
	t.Setenv("MIMIC_ORACLE_HELPER", "1")
	t.Setenv("ARTICLE", "outside")
	dir := t.TempDir()
	r := Runner{Config: Config{Executable: binary, Directory: dir, Listen: "127.0.0.1:0", Engine: "v8", BrowserMode: "headless", ResultEnv: "WORKLOAD_RESULT", Command: []string{os.Args[0], "-test.run=TestExternalOracleHelper", "--", "result-state"}, Timeout: time.Second, States: []WorkloadState{{Name: "one", Env: map[string]string{"ARTICLE": "one"}}, {Name: "two", Env: map[string]string{"ARTICLE": "two"}}}}}
	for i, state := range []int{0, 1, 0, 1} {
		row := r.run(context.Background(), trial{Label: "state", State: state, Record: true, Capture: filepath.Join(dir, fmt.Sprintf("state-%d.mcap", i))})
		if row.Status != "PASS" {
			t.Fatalf("independent state results did not pass: %+v", row)
		}
	}
	if os.Getenv("ARTICLE") != "outside" || reflect.DeepEqual(r.results[0], r.results[1]) {
		t.Fatal("state inputs or result baselines leaked across states")
	}
	r.Config.States[1].Env["ARTICLE"] = "changed"
	row := r.run(context.Background(), trial{Label: "changed", State: 1, Record: true, Capture: filepath.Join(dir, "changed.mcap")})
	if row.Status != "FAIL" || row.WorkloadStatus != "PASS" {
		t.Fatalf("state-specific result divergence was accepted: %+v", row)
	}
}
