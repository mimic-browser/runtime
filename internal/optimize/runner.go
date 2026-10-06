package optimize

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/workload"
)

type Config struct {
	Executable, Directory, Listen, Engine, BrowserMode string
	Command                                            []string
	States                                             []WorkloadState
	EndpointEnv, WebSocketEnv                          []string
	VolatileQuery                                      []string
	Timeout                                            time.Duration
	ResultFile, ResultEnv                              string
}

type Run struct {
	Label          string           `json:"label"`
	State          int              `json:"state"`
	Status         string           `json:"status"`
	WorkloadStatus string           `json:"workloadStatus"`
	Reason         string           `json:"reason,omitempty"`
	Directory      string           `json:"directory"`
	ExitCode       int              `json:"exitCode"`
	ElapsedMs      float64          `json:"elapsedMs"`
	BrowserCPUms   float64          `json:"browserCpuMs"`
	PeakRSS        uint64           `json:"peakRssBytes"`
	RetainedRSS    uint64           `json:"retainedRssBytes"`
	Metrics        workload.Metrics `json:"metrics"`
	LogTruncated   bool             `json:"logTruncated"`
}

type Runner struct {
	Config    Config
	sequence  int
	results   map[int]any
	hasResult map[int]bool
	Runs      []Run
}

type trial struct {
	Label, Capture string
	State          int
	Record         bool
	Policy         network.ResourcePolicy
	Suppressed     []string
	Profile        string
	Inventory      bool
}

// boundedLog never short-writes the child's output. Excess is discarded, with
// explicit diagnostics, so a verbose workload cannot exhaust managed storage.
type boundedLog struct {
	mu        sync.Mutex
	file      *os.File
	size      int64
	truncated bool
}

func (b *boundedLog) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	remaining := int64(1<<20) - b.size
	n := int64(len(p))
	if remaining < n {
		n = max(remaining, 0)
		b.truncated = true
	}
	if n > 0 {
		if _, err := b.file.Write(p[:n]); err != nil {
			return 0, err
		}
		b.size += n
	}
	return len(p), nil
}

func endpoint(listen string) (string, error) {
	l, err := net.Listen("tcp", listen)
	if err != nil {
		return "", fmt.Errorf("Cannot reserve trial address %s: %w. Omit --listen to use a private free port; fixed endpoints must be free. Learn more: %sworkloads/", listen, err, documentation)
	}
	if strings.HasSuffix(listen, ":0") {
		listen = l.Addr().String()
	}
	l.Close()
	return "http://" + listen, nil
}

func (r *Runner) run(ctx context.Context, t trial) (result Run) {
	r.sequence++
	result = Run{Label: t.Label, State: t.State, Status: "MIMIC_FAILURE", WorkloadStatus: "NOT_RUN", ExitCode: -1}
	result.Directory = filepath.Join(r.Config.Directory, fmt.Sprintf("%04d-%s", r.sequence, t.Label))
	defer func() {
		b, _ := json.MarshalIndent(result, "", "  ")
		_ = os.WriteFile(filepath.Join(result.Directory, "run.json"), b, 0600)
		r.Runs = append(r.Runs, result)
	}()
	if err := os.Mkdir(result.Directory, 0700); err != nil {
		result.Reason = err.Error()
		return
	}
	url, err := endpoint(r.Config.Listen)
	if err != nil {
		result.Reason = err.Error()
		return
	}
	control, err := endpoint("127.0.0.1:0")
	if err != nil {
		result.Reason = err.Error()
		return
	}
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		result.Reason = err.Error()
		return
	}
	token := hex.EncodeToString(key)
	env := os.Environ()
	setenv := func(key, value string) {
		prefix := key + "="
		out := env[:0]
		for _, entry := range env {
			matches := strings.HasPrefix(entry, prefix)
			if runtime.GOOS == "windows" && len(entry) >= len(prefix) {
				matches = strings.EqualFold(entry[:len(prefix)], prefix)
			}
			if !matches {
				out = append(out, entry)
			}
		}
		env = append(out, prefix+value)
	}
	if len(r.Config.States) > 0 {
		if t.State < 0 || t.State >= len(r.Config.States) {
			result.Reason = "Trial does not have a configured workload state"
			return
		}
	}
	setenv("MIMIC_BOOTSTRAP_CACHE_DIR", "")
	setenv("MIMIC_WORKLOAD_TOKEN", token)
	for _, name := range append([]string{"MIMIC_CDP_URL", "MIMIC_ENDPOINT", "PW_MIMIC_ENDPOINT"}, r.Config.EndpointEnv...) {
		setenv(name, url)
	}
	args := []string{"-listen", strings.TrimPrefix(url, "http://"), "-engine", r.Config.Engine, "-browser-mode", r.Config.BrowserMode, "-chrome", "152", "-workload-control", strings.TrimPrefix(control, "http://")}
	if t.Inventory {
		args = append(args, "-workload-inventory")
	}
	if t.Record {
		args = append(args, "-workload-capture", t.Capture)
		if len(r.Config.VolatileQuery) > 0 {
			args = append(args, "-workload-volatile-query", strings.Join(r.Config.VolatileQuery, ","))
		}
	} else {
		args = append(args, "-workload-replay", t.Capture)
	}
	if t.Profile != "" {
		args = append(args, "-profile", t.Profile)
	} else if len(t.Suppressed) > 0 {
		captureID, _ := workload.FileSHA256(t.Capture)
		binaryID, _ := workload.FileSHA256(r.Config.Executable)
		plan := workload.Plan{Format: "mimic-workload-plan", Version: 1, OfflineOnly: true, CaptureSHA256: captureID, BinarySHA256: binaryID, Policy: t.Policy, SuppressClassic: t.Suppressed}
		path := filepath.Join(result.Directory, "candidate.mplan")
		if err = workload.WritePlan(path, plan); err != nil {
			result.Reason = err.Error()
			return
		}
		args = append(args, "-workload-plan", path)
	} else if len(t.Policy.Rules) > 0 || len(t.Policy.Presets) > 0 || t.Policy.Budgets != (network.ResourceBudgets{}) {
		b, _ := json.Marshal(t.Policy)
		path := filepath.Join(result.Directory, "policy.json")
		if err = os.WriteFile(path, b, 0600); err != nil {
			result.Reason = err.Error()
			return
		}
		args = append(args, "-resource-policy", path)
	}
	logs := map[string]*boundedLog{}
	for _, name := range []string{"mimic.stdout", "mimic.stderr", "workload.stdout", "workload.stderr"} {
		f, e := os.Create(filepath.Join(result.Directory, name))
		if e != nil {
			result.Reason = e.Error()
			return
		}
		logs[name] = &boundedLog{file: f}
		defer f.Close()
	}
	defer func() {
		for _, log := range logs {
			if log.truncated {
				result.LogTruncated = true
			}
		}
	}()
	cmd := exec.Command(r.Config.Executable, args...)
	cmd.Env = env
	cmd.Stdout = logs["mimic.stdout"]
	cmd.Stderr = logs["mimic.stderr"]
	browser, err := startProcess(cmd)
	if err != nil {
		result.Reason = err.Error()
		return
	}
	defer browser.stop()
	if t.Record {
		defer func() {
			browser.stop()
			entries, _ := os.ReadDir(filepath.Dir(t.Capture))
			for _, entry := range entries {
				if !entry.IsDir() && strings.HasPrefix(entry.Name(), "mimic-capture-spool-") {
					_ = os.Remove(filepath.Join(filepath.Dir(t.Capture), entry.Name()))
				}
			}
		}()
	}
	httpClient := &http.Client{Timeout: 3 * time.Second}
	controlCall := func(path string, out any) error {
		req, e := http.NewRequestWithContext(ctx, "POST", control+path, nil)
		if e != nil {
			return e
		}
		req.Header.Set("Authorization", "Bearer "+token)
		response, e := httpClient.Do(req)
		if e != nil {
			return e
		}
		defer response.Body.Close()
		if response.StatusCode != 200 && response.StatusCode != 204 {
			return fmt.Errorf("trial control HTTP %d", response.StatusCode)
		}
		if out != nil {
			return json.NewDecoder(io.LimitReader(response.Body, 32<<20)).Decode(out)
		}
		_, e = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		return e
	}
	var version struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
	}
	readyDeadline := time.Now().Add(30 * time.Second)
	for {
		if ctx.Err() != nil {
			result.Status = "CANCELLED"
			result.Reason = ctx.Err().Error()
			return
		}
		select {
		case <-browser.done:
			result.Reason = "Mimic exited before becoming ready; see mimic.stderr"
			return
		default:
		}
		if controlCall("/ready", nil) == nil {
			request, _ := http.NewRequestWithContext(ctx, "GET", url+"/json/version", nil)
			response, e := httpClient.Do(request)
			if e == nil {
				e = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&version)
				response.Body.Close()
				if e == nil && version.WebSocketDebuggerURL != "" {
					break
				}
			}
		}
		if time.Now().After(readyDeadline) {
			result.Reason = "Mimic did not become ready; verify the listen address is free"
			return
		}
		select {
		case <-ctx.Done():
		case <-time.After(30 * time.Millisecond):
		}
	}
	if len(r.Config.States) > 0 {
		for name, value := range r.Config.States[t.State].Env {
			setenv(name, value)
		}
	}
	// Endpoint ownership wins over external state input names, including custom
	// endpoint variables. State inputs affect only the client, not Mimic itself.
	for _, name := range append([]string{"MIMIC_CDP_URL", "MIMIC_ENDPOINT", "PW_MIMIC_ENDPOINT"}, r.Config.EndpointEnv...) {
		setenv(name, url)
	}
	for _, name := range r.Config.WebSocketEnv {
		setenv(name, version.WebSocketDebuggerURL)
	}
	resultPath := r.Config.ResultFile
	if r.Config.ResultEnv != "" {
		resultPath = filepath.Join(result.Directory, "result.json")
		setenv(r.Config.ResultEnv, resultPath)
	}
	var oldResult time.Time
	if info, e := os.Stat(resultPath); e == nil {
		oldResult = info.ModTime()
	}
	clientCommand := exec.Command(r.Config.Command[0], r.Config.Command[1:]...)
	clientCommand.Env = env
	clientCommand.Stdout = logs["workload.stdout"]
	clientCommand.Stderr = logs["workload.stderr"]
	start := time.Now()
	before, rss := browser.stats()
	result.PeakRSS = rss
	client, err := startProcess(clientCommand)
	if err != nil {
		result.Status = "UNSUPPORTED"
		result.WorkloadStatus = "NOT_RUN"
		result.Reason = "Cannot launch workload: " + err.Error()
		if runtime.GOOS == "windows" && (strings.EqualFold(filepath.Ext(clientCommand.Path), ".cmd") || strings.EqualFold(filepath.Ext(clientCommand.Path), ".bat")) {
			result.Reason += ". Run Windows batch commands through cmd /d /c (for example: mimic optimize -- cmd /d /c npm test)"
		}
		return
	}
	defer client.stop()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(r.Config.Timeout)
	defer timeout.Stop()
	running := true
	for running {
		select {
		case <-client.done:
			result.ExitCode = client.cmd.ProcessState.ExitCode()
			result.WorkloadStatus = "PASS"
			if client.err != nil {
				result.WorkloadStatus = "FAIL"
				if result.ExitCode < 0 || uint32(result.ExitCode) >= 0x80000000 {
					result.WorkloadStatus = "CRASH"
				}
				result.Reason = "Workload exited unsuccessfully; see workload.stderr"
			}
			result.Status = result.WorkloadStatus
			running = false
		case <-browser.done:
			result.WorkloadStatus = "MIMIC_FAILURE"
			result.Status = "MIMIC_FAILURE"
			result.Reason = "Mimic exited during the workload"
			running = false
		case <-ctx.Done():
			result.WorkloadStatus = "CANCELLED"
			result.Status = "CANCELLED"
			result.Reason = ctx.Err().Error()
			running = false
		case <-timeout.C:
			result.WorkloadStatus = "TIMEOUT"
			result.Status = "TIMEOUT"
			result.Reason = "Workload exceeded its trial timeout"
			running = false
		case <-ticker.C:
			_, rss = browser.stats()
			result.PeakRSS = max(result.PeakRSS, rss)
		}
	}
	result.ElapsedMs = float64(time.Since(start)) / float64(time.Millisecond)
	after, rss := browser.stats()
	result.BrowserCPUms = after - before
	if before < 0 || after < 0 {
		result.BrowserCPUms = -1
	}
	result.RetainedRSS = rss
	result.PeakRSS = max(result.PeakRSS, rss)
	client.stop()
	if result.Status == "CANCELLED" {
		return
	}
	var snapshot struct {
		Metrics      workload.Metrics `json:"metrics"`
		CaptureError string           `json:"captureError"`
	}
	if err = controlCall("/snapshot", &snapshot); err != nil {
		result.Status = "MIMIC_FAILURE"
		result.Reason = "Could not collect trial evidence: " + err.Error()
		return
	}
	result.Metrics = snapshot.Metrics
	if snapshot.Metrics.CDPConnections == 0 && result.WorkloadStatus != "MIMIC_FAILURE" {
		result.Status = "UNSUPPORTED"
		result.Reason = "The workload did not connect to its trial Mimic instance. Read MIMIC_CDP_URL or use --endpoint-env with your existing endpoint setting. A hardcoded localhost:9222 connection targets a different endpoint; --listen 127.0.0.1:9222 requires that port to be free. Learn more: " + documentation + "workloads/"
	} else if len(snapshot.Metrics.Violations) > 0 {
		result.Status = "INVALID"
		result.Reason = snapshot.Metrics.Violations[0]
	} else if len(snapshot.Metrics.CaptureMisses) > 0 {
		result.Status = "UNSUPPORTED"
		result.Reason = "Recorded environment does not cover " + snapshot.Metrics.CaptureMisses[0]
		if len(snapshot.Metrics.ReplayDiagnostics) > 0 {
			result.Reason += "\n" + snapshot.Metrics.ReplayDiagnostics[0]
		}
	} else if len(snapshot.Metrics.Unsupported) > 0 {
		result.Status = "UNSUPPORTED"
		result.Reason = snapshot.Metrics.Unsupported[0]
	} else if snapshot.CaptureError != "" {
		result.Status = "UNSUPPORTED"
		result.Reason = snapshot.CaptureError
	} else if snapshot.Metrics.Active > 0 {
		result.Status = "UNSUPPORTED"
		result.Reason = "Workload ended with unfinished transport requests"
	}
	if result.Status == "PASS" && resultPath != "" {
		info, e := os.Stat(resultPath)
		if e != nil || info.ModTime().Equal(oldResult) {
			result.Status = "FAIL"
			result.Reason = "Workload did not produce its structured result"
		} else if info.Size() > 8<<20 {
			result.Status = "UNSUPPORTED"
			result.Reason = "Structured result exceeds 8 MiB"
		} else {
			b, e := os.ReadFile(resultPath)
			var value any
			decoder := json.NewDecoder(bytes.NewReader(b))
			decoder.UseNumber()
			if e == nil {
				e = decoder.Decode(&value)
			}
			if e != nil {
				result.Status = "FAIL"
				result.Reason = "Structured result is not valid JSON"
			} else if r.hasResult[t.State] && !reflect.DeepEqual(value, r.results[t.State]) {
				result.Status = "FAIL"
				result.Reason = "Structured result differs from the successful baseline"
			} else {
				if r.results == nil {
					r.results = map[int]any{}
					r.hasResult = map[int]bool{}
				}
				r.results[t.State] = value
				r.hasResult[t.State] = true
			}
		}
	}
	_ = controlCall("/stop", nil)
	select {
	case <-browser.done:
	case <-time.After(10 * time.Second):
		result.Status = "MIMIC_FAILURE"
		result.Reason = "Mimic did not finish owned teardown"
	}
	return
}
