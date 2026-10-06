package optimize

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/moreveal/mimic/internal/cliui"
	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/workload"
)

const documentation = "https://mimic.boo/docs/optimize/"

type stringsFlag []string

func (v *stringsFlag) String() string         { return strings.Join(*v, ",") }
func (v *stringsFlag) Set(value string) error { *v = append(*v, value); return nil }

type presentation struct {
	out     io.Writer
	color   bool
	verbose bool
}

func (p presentation) line(kind, text string) {
	style := ""
	if p.color {
		switch kind {
		case "PASS":
			style = "\x1b[32m"
		case "FAIL":
			style = "\x1b[31m"
		case "NOTE", "UNSUPPORTED":
			style = "\x1b[33m"
		default:
			style = "\x1b[1;36m"
		}
	}
	if style != "" {
		fmt.Fprintf(p.out, "%s%s\x1b[0m  %s\n", style, kind, text)
	} else {
		fmt.Fprintf(p.out, "%s  %s\n", kind, text)
	}
}

func Main(arguments []string) int {
	ui := presentation{out: os.Stdout, color: cliui.SupportsColor(os.Stdout)}
	flags := flag.NewFlagSet("mimic optimize", flag.ContinueOnError)
	name := flags.String("name", "", "name for the managed workload profile")
	output := flags.String("output", "", "explicit .mprofile output path")
	listen := flags.String("listen", "127.0.0.1:0", "trial CDP address; default chooses a private free port")
	statesFile := flags.String("states-file", "", "optional JSON workload states with named environment inputs")
	engine := flags.String("engine", "v8", "ECMAScript engine")
	mode := flags.String("browser-mode", "headful", "browser environment")
	timeout := flags.Duration("timeout", 45*time.Second, "external workload deadline per trial")
	budget := flags.Int("max-trials", 64, "bounded candidate evaluations")
	wall := flags.Duration("search-time", 5*time.Minute, "candidate-search wall budget")
	repetitions := flags.Int("repetitions", 5, "matched measurements per variant and recorded state")
	manual := flags.String("manual-policy", "", "optional competent manual comparison policy")
	resultFile := flags.String("result-file", "", "existing workload JSON output to compare")
	resultEnv := flags.String("result-env", "", "existing workload variable selecting its JSON output")
	record := flags.Bool("record", false, "record a fresh baseline instead of reusing existing evidence")
	verbose := flags.Bool("verbose", false, "show individual trial outcomes")
	inspect := flags.String("inspect", "", "inspect a named profile or .mprofile path")
	var captures, supplements, endpoints, websockets, volatile stringsFlag
	flags.Var(&captures, "capture", "advanced: existing .mcap state (repeatable); disables live recording")
	flags.Var(&supplements, "supplemental-capture", "advanced: recorded immutable reference-branch resources for the first capture; never accesses live network")
	flags.Var(&endpoints, "endpoint-env", "existing workload variable expecting the CDP HTTP URL")
	flags.Var(&websockets, "websocket-env", "existing workload variable expecting the browser WebSocket URL")
	flags.Var(&volatile, "volatile-query-key", "known nonsemantic query key; never inferred")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: mimic optimize --name shop -- <workload command>\n       mimic optimize --output shop.mprofile -- <workload command>\n       mimic optimize --inspect shop\n\nYour CDP workload reads MIMIC_CDP_URL (or --endpoint-env).\nFor a fixed endpoint, select it explicitly with --listen.\nOrdinary assertions and its exit status define correctness.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(arguments); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	ui.verbose = *verbose
	fail := func(reason string) int {
		ui.line("FAIL", reason)
		fmt.Fprintln(ui.out, "Learn more: "+documentation+"troubleshooting/")
		return 1
	}
	if *inspect != "" {
		path, err := workload.ProfilePath(*inspect)
		if err != nil {
			return fail(err.Error())
		}
		profile, err := workload.ReadProfile(path)
		if err != nil {
			return fail(err.Error())
		}
		fmt.Fprintf(ui.out, "Profile: %s\nRuntime: %s, %s, Chrome %d\nRecorded states: %d\nResource rules: %d\nClassic execution exclusions: %d\nEvidence: %s\nCreated: %s\nFile: %s\n\nThis profile preserves behavior verified by its workload.\n%s\n", profile.Name, profile.Engine, profile.BrowserMode, profile.Chrome, len(profile.CaptureSHA256), len(profile.Plan.Resources), len(profile.Plan.SuppressClassic), profile.Confidence, profile.CreatedAt, path, documentation+"safety/")
		for _, rule := range profile.Plan.Resources {
			if rule.Work.Body == "none" {
				fmt.Fprintf(ui.out, "Acquire headers only: %s\n", describeMatch(rule.Match))
			}
			if rule.Work.Network != nil && !*rule.Work.Network {
				fmt.Fprintf(ui.out, "Avoid acquisition: %s\n", describeMatch(rule.Match))
			}
		}
		return 0
	}
	command := flags.Args()
	if len(command) == 0 {
		return fail("Provide the ordinary workload command after --")
	}
	_ = cliui.WriteOptimizeHeader(ui.out, ui.color)
	if *name == "" && *output == "" {
		*name = "workload"
	}
	if *timeout <= 0 || *budget < 0 || *repetitions < 3 || *wall < 0 {
		return fail("Use a positive timeout, nonnegative search budget and at least three matched repetitions")
	}
	if *engine != "v8" && *engine != "goja" && *engine != "quickjs" {
		return fail("Unknown ECMAScript engine")
	}
	if *mode != "headful" && *mode != "headless" {
		return fail("Unknown browser mode")
	}
	if *record && len(captures) > 0 {
		return fail("--record and --capture are mutually exclusive")
	}
	states, err := loadStates(*statesFile)
	if err != nil {
		return fail(err.Error())
	}
	if *statesFile != "" && len(captures) > 0 && len(captures) != len(states) {
		return fail("Provide one --capture per workload state, in the states-file order")
	}
	if *statesFile == "" && len(captures) > 1 {
		states = nil
		for state := range captures {
			states = append(states, WorkloadState{Name: fmt.Sprintf("capture-%d", state+1)})
		}
	}
	referencePolicy := network.ResourcePolicy{Presets: []string{"noVisualAssets", "noSpeculativeLoads"}}
	if *manual != "" {
		b, e := os.ReadFile(*manual)
		if e != nil {
			return fail(e.Error())
		}
		referencePolicy, e = network.ParseResourcePolicy(b)
		if e != nil {
			return fail(e.Error())
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return fail(err.Error())
	}
	binaryID, err := workload.FileSHA256(executable)
	if err != nil {
		return fail(err.Error())
	}
	base, err := workload.DataDir()
	if err != nil {
		return fail(err.Error())
	}
	if err = os.MkdirAll(filepath.Join(base, "optimization"), 0700); err != nil {
		return fail(err.Error())
	}
	_ = pruneManaged(filepath.Join(base, "optimization"), "")
	directory, err := os.MkdirTemp(filepath.Join(base, "optimization"), "run-*")
	if err != nil {
		return fail(err.Error())
	}
	activeMarker := filepath.Join(directory, ".active")
	if err = os.WriteFile(activeMarker, []byte(fmt.Sprint(os.Getpid())), 0600); err != nil {
		return fail(err.Error())
	}
	defer func() { _ = os.Remove(activeMarker); _ = pruneManaged(filepath.Join(base, "optimization"), directory) }()
	path := *output
	if path != "" {
		path, err = filepath.Abs(path)
	} else {
		path, err = workload.ProfilePath(*name)
	}
	if err != nil {
		return fail(err.Error())
	}
	runner := &Runner{Config: Config{Executable: executable, Directory: directory, Listen: *listen, Engine: *engine, BrowserMode: *mode, Command: command, States: states, EndpointEnv: endpoints, WebSocketEnv: websockets, VolatileQuery: volatile, Timeout: *timeout, ResultFile: *resultFile, ResultEnv: *resultEnv}}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	started := time.Now()
	report := Report{Format: "mimic-optimization-report", CreatedAt: time.Now().UTC().Format(time.RFC3339), Command: command, Directory: directory, BuildSHA256: binaryID}
	for _, state := range states {
		report.StateNames = append(report.StateNames, state.Name)
	}
	defer func() {
		report.Runs = runner.Runs
		b, _ := json.MarshalIndent(report, "", "  ")
		_ = os.WriteFile(filepath.Join(directory, "report.json"), b, 0600)
	}()
	// Each state has its own immutable environment and cache identity. Reuse
	// never bypasses fresh assertion/replay validation.
	cwd, _ := os.Getwd()
	explicitInputs := len(captures) > 0
	cachePaths := make([]string, len(states))
	for state, input := range states {
		identityBytes, _ := json.Marshal([]any{command, cwd, workload.RuntimeABI, *engine, *mode, volatile, input.Env})
		cacheID := fmt.Sprintf("%x", sha256Sum(identityBytes))
		cachePaths[state] = filepath.Join(base, "optimization", cacheID+".mcap")
	}
	if !explicitInputs {
		for state, input := range states {
			cachePath := cachePaths[state]
			capture := filepath.Join(directory, fmt.Sprintf("environment-%d.mcap", state))
			cached := false
			if !*record {
				if _, e := os.Stat(cachePath); e == nil {
					if e := os.Link(cachePath, capture); e != nil {
						if e = copyAtomic(cachePath, capture); e != nil {
							return fail(e.Error())
						}
					}
					cached = true
					ui.line("REPLAY", "Checking cached state: "+input.Name)
					probe := runner.run(ctx, trial{Label: "cached-validation", State: state, Capture: capture, Inventory: true})
					if probe.Status != "PASS" && !(probe.Status == "UNSUPPORTED" && probe.WorkloadStatus == "PASS") {
						if ctx.Err() != nil {
							return fail("Optimization cancelled")
						}
						ui.line("NOTE", "Cached state no longer validates; recording it again before search")
						capture = filepath.Join(directory, fmt.Sprintf("environment-fresh-%d.mcap", state))
						cached = false
					}
				}
			}
			if !cached {
				ui.line("RECORD", "Recording live baseline state: "+input.Name)
				row := runner.run(ctx, trial{Label: "baseline", State: state, Record: true, Capture: capture, Inventory: true})
				if row.Status != "PASS" {
					printFailure(ui, row)
					if _, saved := os.Stat(capture); row.WorkloadStatus != "PASS" || row.Status != "UNSUPPORTED" || saved != nil {
						return fail("Cannot optimize: baseline state " + input.Name + " was not valid")
					}
					ui.line("NOTE", "Complete evidence was preserved; testing explicit coverage exclusions")
				}
				ui.line("PASS", "Baseline workload passed: "+input.Name)
				report.TrainingBodyBytes += row.Metrics.EncodedBodyBytes
				if err = copyAtomic(capture, cachePath); err != nil {
					return fail("Cannot save recorded evidence: " + err.Error())
				}
			}
			captures = append(captures, capture)
		}
	}
	for n, supplement := range supplements {
		extended := filepath.Join(directory, fmt.Sprintf("environment-extended-%d.mcap", n))
		added, e := workload.ExtendCapture(captures[0], supplement, extended)
		if e != nil {
			return fail("Cannot extend recorded branch evidence: " + e.Error())
		}
		captures[0] = extended
		report.SupplementalResources += added
		ui.line("REPLAY", fmt.Sprintf("Added %d recorded immutable branch resources; existing responses were preserved", added))
	}
	ui.line("REPLAY", fmt.Sprintf("Validating %d recorded state(s), three fresh replays each", len(captures)))
	var baselines []Run
	var documents []workload.DocumentEvidence
	var requests []workload.RequestEvidence
	encoded := map[string]int64{}
	branchURLs := map[string]bool{}
	var captureIDs []string
	var replaySeed network.ResourcePolicy
	for state, capture := range captures {
		id, e := workload.FileSHA256(capture)
		if e != nil {
			return fail(e.Error())
		}
		captureIDs = append(captureIDs, id)
		inventory, e := workload.CaptureInventory(capture)
		if e != nil {
			return fail(e.Error())
		}
		if state == 0 {
			volatile = inventory.VolatileQuery
		} else if strings.Join(volatile, "\x00") != strings.Join(inventory.VolatileQuery, "\x00") {
			return fail("Recorded states use different explicit query normalization")
		}
		for raw, bytes := range inventory.EncodedCosts {
			encoded[raw] += bytes
		}
		for raw := range inventory.BranchURLs {
			branchURLs[raw] = true
		}
		var costs []Cost
		for repetition := 0; repetition < 3; repetition++ {
			row := runner.run(ctx, trial{Label: "replay", State: state, Capture: capture, Policy: replaySeed, Inventory: true})
			if canProbeReferenceReplay(row, *manual != "") {
				ui.line("NOTE", "Unconfigured replay entered an uncovered branch; testing the supplied manual reference as a separate local trial")
				probe := runner.run(ctx, trial{Label: "reference-replay", State: state, Capture: capture, Policy: referencePolicy, Inventory: true})
				if probe.Status == "PASS" {
					report.DefaultReplayUnsupported = true
					replaySeed = referencePolicy
					row = probe
				}
			}
			for attempts := 0; row.Status != "PASS" && attempts < 4; attempts++ {
				updated, changed := coverageExclusions(row, replaySeed)
				if !changed {
					break
				}
				report.DefaultReplayUnsupported = true
				replaySeed = updated
				ui.line("NOTE", "The workload passed with uncovered background requests; testing explicit request exclusions locally")
				row = runner.run(ctx, trial{Label: "coverage-exclusion", State: state, Capture: capture, Policy: replaySeed, Inventory: true})
			}
			if row.Status != "PASS" {
				printFailure(ui, row)
				return fail(fmt.Sprintf("Local replay of state %d failed at repetition %d; no trial Mimic instance will access the live site", state+1, repetition+1))
			}
			costs = append(costs, cost([]Run{row}))
			if repetition == 0 {
				baselines = append(baselines, row)
				documents = append(documents, row.Metrics.Documents...)
				for _, resource := range row.Metrics.Resources {
					method := resource.Method
					if method == "" {
						method = "GET"
					}
					requests = append(requests, workload.RequestEvidence{DocumentURL: resource.DocumentURL, DocumentSHA256: resource.DocumentSHA256, SourceURL: resource.SourceURL, URL: resource.URL, Method: method, Kind: resource.Kind, BodySHA256: resource.RequestBodySHA256, HeadersSHA256: resource.RequestHeadersSHA256})
				}
			}
		}
		for _, value := range costs[1:] {
			if value.Bytes != costs[0].Bytes || value.Responses != costs[0].Responses {
				report.NetworkCostVaries = true
			}
		}
		low, high := costs[0].Bytes, costs[0].Bytes
		for _, value := range costs {
			low = min(low, value.Bytes)
			high = max(high, value.Bytes)
		}
		report.NetworkNoiseBytes += high - low
	}
	ui.line("PASS", fmt.Sprintf("Replay stable: %d/%d", 3*len(captures), 3*len(captures)))
	if report.NetworkCostVaries {
		ui.line("NOTE", "All replay assertions passed, but background acquisition varied. Small apparent savings will require an additional passing trial; final measurements report the variation")
	}
	report.ReplaySeed = replaySeed
	reference := referencePolicy
	report.ReferencePolicy = reference
	evaluate := func(evaluationContext context.Context, label string, c Candidate) []Run {
		var runs []Run
		for state, capture := range captures {
			row := runner.run(evaluationContext, trial{Label: label, State: state, Capture: capture, Policy: c.Policy, Suppressed: c.Suppressed, Inventory: true})
			runs = append(runs, row)
			if ui.verbose {
				ui.line(row.Status, fmt.Sprintf("%s state %d: %.0f ms; %s", label, state+1, row.ElapsedMs, row.Reason))
			}
			if row.Status != "PASS" {
				break
			}
		}
		return runs
	}
	manualRuns := evaluate(ctx, "manual", Candidate{Policy: reference})
	// A competent manual policy can activate a previously unseen fallback.
	// In managed training only, record that reference branch once, outside
	// search, and extend evidence only with fresh immutable resources. Explicit
	// capture inputs retain their absolute offline boundary.
	for branchAttempt := 0; branchAttempt < len(captures) && *manual != "" && !explicitInputs && !passing(manualRuns) && manualRuns[len(manualRuns)-1].Status == "UNSUPPORTED" && len(manualRuns[len(manualRuns)-1].Metrics.Unsupported) == 0 && len(manualRuns[len(manualRuns)-1].Metrics.Violations) == 0; branchAttempt++ {
		state := manualRuns[len(manualRuns)-1].State
		branch := filepath.Join(directory, fmt.Sprintf("reference-branch-%d.mcap", state))
		ui.line("RECORD", "The manual reference activates an uncovered branch; recording that reference once before local search")
		row := runner.run(ctx, trial{Label: "record-reference-branch", Record: true, State: state, Capture: branch, Policy: reference, Inventory: true})
		report.TrainingBodyBytes += row.Metrics.EncodedBodyBytes
		if row.Status == "PASS" {
			extended := filepath.Join(directory, fmt.Sprintf("environment-reference-%d.mcap", state))
			added, e := workload.ExtendCapture(captures[state], branch, extended)
			if e == nil {
				captures[state] = extended
				report.SupplementalResources += added
				captureIDs[state], e = workload.FileSHA256(extended)
				if e != nil {
					return fail(e.Error())
				}
				inventory, e := workload.CaptureInventory(extended)
				if e != nil {
					return fail(e.Error())
				}
				for raw, bytes := range inventory.EncodedCosts {
					encoded[raw] = max(encoded[raw], bytes)
				}
				for raw := range inventory.BranchURLs {
					branchURLs[raw] = true
				}
				ui.line("REPLAY", fmt.Sprintf("Added %d immutable reference-branch resources; validating three local reference replays", added))
				for repetition := 0; repetition < 3; repetition++ {
					rows := []Run{runner.run(ctx, trial{Label: "reference-branch-validation", State: state, Capture: extended, Policy: reference, Inventory: true})}
					if !passing(rows) {
						printFailure(ui, rows[len(rows)-1])
						return fail("The recorded reference branch did not validate locally")
					}
				}
				if e = copyAtomic(extended, cachePaths[state]); e != nil {
					return fail(e.Error())
				}
			} else {
				ui.line("NOTE", "The reference evidence cannot be combined: "+e.Error())
				break
			}
		} else {
			printFailure(ui, row)
			break
		}
		manualRuns = evaluate(ctx, "manual", Candidate{Policy: reference})
	}
	for attempts := 0; !passing(manualRuns) && attempts < 4; attempts++ {
		updated, changed := coverageExclusions(manualRuns[len(manualRuns)-1], reference)
		if !changed {
			break
		}
		if report.OriginalManualPolicy == nil {
			original := reference
			report.OriginalManualPolicy = &original
		}
		reference = updated
		report.ManualCoverageAdjusted = true
		ui.line("NOTE", "Manual workload passed with uncovered requests. Its comparison will be explicitly labeled Manual+coverage exclusions")
		manualRuns = evaluate(ctx, "manual-coverage-exclusion", Candidate{Policy: reference})
	}
	report.ReferencePolicy = reference
	if !passing(manualRuns) {
		printFailure(ui, manualRuns[len(manualRuns)-1])
		if *manual != "" {
			if manualRuns[len(manualRuns)-1].Status == "UNSUPPORTED" {
				return fail("Cannot validate the manual comparison: its request branch is not covered by the recorded environment; this does not establish failure against the live site")
			}
			return fail("The supplied manual comparison fails this workload")
		}
		ui.line("NOTE", "The reference preset fails this workload; using unconfigured Mimic as reference")
		reference = replaySeed
		manualRuns = baselines
		report.ReferencePolicy = reference
	}
	search := Search{Budget: *budget, WallBudget: *wall, Best: Candidate{Policy: replaySeed}, Seed: Candidate{Policy: replaySeed}, SeedRuns: baselines, BestCost: cost(baselines), NoiseBytes: report.NetworkNoiseBytes, BranchURLs: branchURLs}
	if value := cost(manualRuns); value.better(search.BestCost) {
		search.Best = Candidate{Policy: reference, Changes: []string{"Manual reference policy"}}
		search.BestCost = value
		search.SeedRuns = manualRuns
	}
	search.Evaluate = func(ctx context.Context, c Candidate) []Run { return evaluate(ctx, "candidate", c) }
	progressAt := time.Now()
	progressCost := search.BestCost
	search.Progress = func(trials int, best Cost) {
		if trials == 1 || trials%8 == 0 || time.Since(progressAt) > 20*time.Second || best.Bytes < progressCost.Bytes || best.Responses < progressCost.Responses {
			ui.line("SEARCH", fmt.Sprintf("%d/%d candidates; best HTTP bodies %s, acquired responses %d", trials, *budget, humanBytes(best.Bytes), best.Responses))
			progressAt = time.Now()
			progressCost = best
		}
	}
	ui.line("SEARCH", "Optimizing locally: individual resources, combinations and author execution")
	searchStarted := time.Now()
	searchContext := ctx
	stopSearch := func() {}
	if *wall > 0 {
		searchContext, stopSearch = context.WithTimeout(ctx, *wall)
	}
	best := search.Run(searchContext, baselines, encoded, volatile)
	stopSearch()
	for _, row := range runner.Runs {
		if row.Status == "PASS" {
			for _, resource := range row.Metrics.Attempts {
				method := resource.Method
				if method == "" {
					method = "GET"
				}
				requests = append(requests, workload.RequestEvidence{DocumentURL: resource.DocumentURL, DocumentSHA256: resource.DocumentSHA256, SourceURL: resource.SourceURL, URL: resource.URL, Method: method, Kind: resource.Kind, BodySHA256: resource.RequestBodySHA256, HeadersSHA256: resource.RequestHeadersSHA256})
			}
		}
	}
	report.SearchMs = float64(time.Since(searchStarted)) / float64(time.Millisecond)
	report.Trials = search.Trials
	report.MemoHits = search.MemoHits
	report.Rejected = search.Rejected
	if ctx.Err() != nil {
		return fail("Optimization cancelled; completed evidence was preserved")
	}
	profile := workload.Profile{Format: "mimic-workload-profile", Version: 1, RuntimeABI: workload.RuntimeABI, Name: *name, Engine: *engine, BrowserMode: *mode, Chrome: 152, BuildSHA256: binaryID, CreatedAt: time.Now().UTC().Format(time.RFC3339), Confidence: "empirical-document-guarded", Documents: uniqueDocuments(documents), Requests: uniqueRequests(requests), CaptureSHA256: captureIDs, VolatileQuery: volatile, Plan: workload.ExecutionPlan{Resources: network.ExpandedRules(best.Policy), SuppressClassic: best.Suppressed}}
	candidatePath := filepath.Join(directory, "finalist.mprofile")
	if err = workload.WriteProfile(candidatePath, profile); err != nil {
		return fail(err.Error())
	}
	report.Selected = best
	report.RecordedStates = len(captures)
	ui.line("MEASURE", "Comparing Default, reference and optimized with matched instrumentation")
	matrix := map[string][]Run{"Default": {}, "Manual": {}, "Optimized": {}}
	variants := []string{"Default", "Manual", "Optimized"}
	for repetition := 0; repetition < *repetitions; repetition++ {
		for offset := 0; offset < 3; offset++ {
			variant := variants[(repetition+offset)%3]
			if variant == "Default" && report.DefaultReplayUnsupported {
				continue
			}
			for state, capture := range captures {
				t := trial{Label: "measure-" + variant, State: state, Capture: capture}
				if variant == "Manual" {
					t.Policy = reference
				}
				if variant == "Optimized" {
					t.Profile = candidatePath
				}
				row := runner.run(ctx, t)
				matrix[variant] = append(matrix[variant], row)
				if row.Status != "PASS" {
					printFailure(ui, row)
					return fail("A matched finalist failed; no profile was installed")
				}
			}
		}
	}
	report.Measurements = matrix
	report.Summary = map[string]Summary{}
	for name, rows := range matrix {
		if len(rows) > 0 {
			report.Summary[name] = summarize(rows)
		}
	}
	report.StateSummary = map[int]map[string]Summary{}
	for state := range captures {
		perState := map[string]Summary{}
		for variant, rows := range matrix {
			var selected []Run
			for _, row := range rows {
				if row.State == state {
					selected = append(selected, row)
				}
			}
			if len(selected) > 0 {
				perState[variant] = summarize(selected)
			}
		}
		report.StateSummary[state] = perState
		if acquisitionRegresses(perState["Optimized"], perState["Manual"]) {
			return fail(fmt.Sprintf("Guarded acquisition regressed against the manual/reference policy in state %d; no profile installed", state+1))
		}
		if !report.DefaultReplayUnsupported && (perState["Optimized"].BodyBytes > perState["Default"].BodyBytes || perState["Optimized"].Responses > perState["Default"].Responses) {
			return fail(fmt.Sprintf("Guarded acquisition regressed in state %d; no profile installed", state+1))
		}
	}
	report.TrainingMs = float64(time.Since(started)) / float64(time.Millisecond)
	// Traffic counters select the winner. A profile whose Page guards admit
	// less specialization than training predicted must not be called a win.
	baselineSummary := report.Summary["Default"]
	if report.DefaultReplayUnsupported {
		baselineSummary = report.Summary["Manual"]
		ui.line("NOTE", "Default replay has uncovered requests; its matched metrics are unavailable. Manual and optimized were measured locally with the same instrumentation")
	}
	optimized := report.Summary["Optimized"]
	if optimized.BodyBytes > baselineSummary.BodyBytes || optimized.Responses > baselineSummary.Responses {
		return fail("The guarded finalist regressed acquisition; profile was not installed")
	}
	if saving := baselineSummary.ElapsedMs - optimized.ElapsedMs; !report.DefaultReplayUnsupported && saving > baselineSummary.ElapsedMs*0.05 {
		report.BreakEvenRuns = int(report.TrainingMs/saving) + 1
	}
	var referenceSaving int64
	for state := range captures {
		referenceSaving += report.StateSummary[state]["Manual"].BodyBytes - report.StateSummary[state]["Optimized"].BodyBytes
	}
	if referenceSaving > 0 && report.TrainingBodyBytes > 0 {
		// Equal input frequency is an explicit estimate, not inferred production
		// usage. Acquisition break-even is separate from CPU/time training cost.
		bytes := report.TrainingBodyBytes * int64(len(captures))
		report.NetworkBreakEvenRuns = int((bytes + referenceSaving - 1) / referenceSaving)
		report.NetworkBreakEvenReference = "reference"
		if *manual != "" {
			report.NetworkBreakEvenReference = "manual"
		}
	}
	report.Success = true
	b, _ := json.Marshal(report)
	profile.Report = b
	if err = workload.WriteProfile(path, profile); err != nil {
		report.Success = false
		return fail(err.Error())
	}
	ui.line("PASS", "Optimization complete")
	printSummary(ui, report, *manual != "")
	savedLabel := path
	if *output == "" {
		savedLabel = *name
	}
	fmt.Fprintf(ui.out, "\nSaved profile: %s\nRun: mimic --profile %s\n", savedLabel, quoteArgument(func() string {
		if *name != "" && *output == "" {
			return *name
		}
		return path
	}()))
	ui.line("NOTE", fmt.Sprintf("Validated against %d recorded state(s). Profile admission is document-guarded, not a proof of future site behavior. Only behavior your workload verifies is covered.", len(captures)))
	fmt.Fprintln(ui.out, "Learn more: "+documentation+"safety/")
	fmt.Fprintln(ui.out, "Detailed evidence: "+filepath.Join(directory, "report.json"))
	return 0
}

type Report struct {
	SupplementalResources                           int
	NetworkCostVaries                               bool
	NetworkNoiseBytes                               int64
	OriginalManualPolicy                            *network.ResourcePolicy
	ManualCoverageAdjusted                          bool
	DefaultReplayUnsupported                        bool
	ReplaySeed                                      network.ResourcePolicy
	NetworkBreakEvenRuns                            int
	NetworkBreakEvenReference                       string
	TrainingBodyBytes                               int64
	Format, CreatedAt, BuildSHA256, Directory       string
	Command                                         []string
	StateNames                                      []string
	ReferencePolicy                                 network.ResourcePolicy
	Selected                                        Candidate
	Rejected                                        []Candidate
	RecordedStates, Trials, MemoHits, BreakEvenRuns int
	SearchMs, TrainingMs                            float64
	Success                                         bool
	Runs                                            []Run
	StateSummary                                    map[int]map[string]Summary
	Measurements                                    map[string][]Run
	Summary                                         map[string]Summary
}
type Summary struct {
	MinBodyBytes, MaxBodyBytes    int64
	BodyBytes, Responses, Scripts int64
	ElapsedMs, CPUms              float64
	PeakRSS, RetainedRSS          uint64
	Passes, Trials                int
}

func acquisitionRegresses(candidate, reference Summary) bool {
	return candidate.BodyBytes > reference.BodyBytes || candidate.Responses > reference.Responses
}

func summarize(rows []Run) Summary {
	var elapsed, cpu []float64
	var rss, retained []uint64
	var bodies, responses, scripts []int64
	for _, r := range rows {
		elapsed = append(elapsed, r.ElapsedMs)
		cpu = append(cpu, r.BrowserCPUms)
		rss = append(rss, r.PeakRSS)
		retained = append(retained, r.RetainedRSS)
		bodies = append(bodies, r.Metrics.EncodedBodyBytes)
		responses = append(responses, r.Metrics.ResponseAcquisitions)
		scripts = append(scripts, r.Metrics.ClassicScriptsExecuted)
	}
	sort.Float64s(elapsed)
	sort.Float64s(cpu)
	sort.Slice(rss, func(i, j int) bool { return rss[i] < rss[j] })
	sort.Slice(retained, func(i, j int) bool { return retained[i] < retained[j] })
	sort.Slice(bodies, func(i, j int) bool { return bodies[i] < bodies[j] })
	sort.Slice(responses, func(i, j int) bool { return responses[i] < responses[j] })
	sort.Slice(scripts, func(i, j int) bool { return scripts[i] < scripts[j] })
	n := len(rows) / 2
	return Summary{MinBodyBytes: bodies[0], MaxBodyBytes: bodies[len(bodies)-1], BodyBytes: bodies[n], Responses: responses[n], Scripts: scripts[n], ElapsedMs: elapsed[n], CPUms: cpu[n], PeakRSS: rss[n], RetainedRSS: retained[n], Passes: len(rows), Trials: len(rows)}
}
func printSummary(ui presentation, r Report, manual bool) {
	label := "Reference"
	if manual {
		label = "Manual"
	}
	if r.ManualCoverageAdjusted {
		label = "Manual+repair"
	}
	if r.RecordedStates > 1 {
		fmt.Fprintf(ui.out, "\n%-28s %14s %14s %12s\n", "Recorded state", label+" bodies", "Auto bodies", "Saved")
		for state := 0; state < r.RecordedStates; state++ {
			name := fmt.Sprintf("State %d", state+1)
			if state < len(r.StateNames) {
				name = r.StateNames[state]
			}
			manual := r.StateSummary[state]["Manual"].BodyBytes
			auto := r.StateSummary[state]["Optimized"].BodyBytes
			saving := "equal"
			if manual > 0 && auto < manual {
				saving = fmt.Sprintf("%.1f%%", 100*float64(manual-auto)/float64(manual))
			}
			fmt.Fprintf(ui.out, "%-28s %14s %14s %12s\n", name, humanBytes(manual), humanBytes(auto), saving)
		}
		ui.line("NOTE", "The following medians span all recorded states; per-state evidence is retained in the report")
	}
	fmt.Fprintf(ui.out, "\n%-22s %12s %12s %12s\n", "", "Default", label, "Optimized")
	for _, metric := range []string{"HTTP body bytes", "Acquired responses", "Workload time", "Browser CPU", "Sampled peak RSS", "Classic executions", "Workload"} {
		fmt.Fprintf(ui.out, "%-22s", metric)
		for _, name := range []string{"Default", "Manual", "Optimized"} {
			if name == "Default" && r.DefaultReplayUnsupported {
				value := "unavailable"
				if metric == "Workload" {
					value = "UNSUPPORTED"
				}
				fmt.Fprintf(ui.out, " %12s", value)
				continue
			}
			s := r.Summary[name]
			value := ""
			switch metric {
			case "HTTP body bytes":
				value = humanBytes(s.BodyBytes)
			case "Acquired responses":
				value = fmt.Sprint(s.Responses)
			case "Workload time":
				value = fmt.Sprintf("%.2f s", s.ElapsedMs/1000)
			case "Browser CPU":
				if s.CPUms < 0 {
					value = "unavailable"
				} else {
					value = fmt.Sprintf("%.0f ms", s.CPUms)
				}
			case "Sampled peak RSS":
				if s.PeakRSS == 0 {
					value = "unavailable"
				} else {
					value = humanBytes(int64(s.PeakRSS))
				}
			case "Classic executions":
				value = fmt.Sprint(s.Scripts)
			case "Workload":
				value = fmt.Sprintf("PASS %d/%d", s.Passes, s.Trials)
			}
			fmt.Fprintf(ui.out, " %12s", value)
		}
		fmt.Fprintln(ui.out)
	}
	for _, name := range []string{"Default", "Manual", "Optimized"} {
		s := r.Summary[name]
		if r.RecordedStates > 1 {
			var varies bool
			for _, state := range r.StateSummary {
				v := state[name]
				varies = varies || v.MinBodyBytes != v.MaxBodyBytes
			}
			if !varies {
				continue
			}
		}
		if s.MinBodyBytes != s.MaxBodyBytes {
			ui.line("NOTE", fmt.Sprintf("%s HTTP body acquisition varied from %s to %s across %d measurements", name, humanBytes(s.MinBodyBytes), humanBytes(s.MaxBodyBytes), s.Trials))
		}
	}
	fmt.Fprintf(ui.out, "\nSearch: %d candidates, %.1f s. Training/validation: %.1f s.\n", r.Trials, r.SearchMs/1000, r.TrainingMs/1000)
	if len(r.Selected.Changes) > 0 {
		var changes []string
		for _, change := range r.Selected.Changes {
			if ui.verbose || !strings.HasPrefix(change, "Seed:") {
				changes = append(changes, change)
			}
		}
		if len(changes) > 0 {
			fmt.Fprintln(ui.out, "Selected changes:")
		}
		limit := min(len(changes), 8)
		if ui.verbose {
			limit = len(changes)
		}
		for _, change := range changes[:limit] {
			fmt.Fprintln(ui.out, "  "+change)
		}
		if limit < len(changes) {
			fmt.Fprintf(ui.out, "  ... %d more (inspect the profile or detailed report)\n", len(changes)-limit)
		}
	}
	if r.BreakEvenRuns > 0 {
		fmt.Fprintf(ui.out, "Exploratory time break-even versus default: ~%d workload runs (local replay timing; not live latency).\n", r.BreakEvenRuns)
	}
	if r.NetworkBreakEvenRuns > 0 {
		fmt.Fprintf(ui.out, "Encoded-body training break-even: ~%d runs versus %s (equal input mix; CPU/time/storage costs are separate).\n", r.NetworkBreakEvenRuns, r.NetworkBreakEvenReference)
	}
	if r.Summary["Optimized"].BodyBytes >= r.Summary["Manual"].BodyBytes && r.Summary["Optimized"].Responses >= r.Summary["Manual"].Responses {
		ui.line("NOTE", "No network-acquisition advantage over the reference/manual policy was demonstrated")
	}
}
func printFailure(ui presentation, row Run) {
	ui.line(row.Status, row.Reason)
	if row.WorkloadStatus != "" && row.Status != row.WorkloadStatus {
		fmt.Fprintln(ui.out, "Workload oracle: "+row.WorkloadStatus)
	}
	for _, name := range []string{"workload.stderr", "mimic.stderr"} {
		b, err := os.ReadFile(filepath.Join(row.Directory, name))
		if err == nil && len(b) > 0 {
			if len(b) > 1500 {
				b = b[:1500]
			}
			fmt.Fprintf(ui.out, "%s:\n%s\n", name, strings.TrimSpace(cliui.TerminalText(string(b))))
		}
	}
	fmt.Fprintln(ui.out, "Trial diagnostics: "+row.Directory)
}
func humanBytes(n int64) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}
	if n < 1<<20 {
		return fmt.Sprintf("%.1f KiB", float64(n)/1024)
	}
	return fmt.Sprintf("%.2f MiB", float64(n)/(1<<20))
}
func quoteArgument(s string) string {
	if !strings.ContainsAny(s, " \t\"'`$;&()<>|!") {
		return s
	}
	if runtime.GOOS == "windows" {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
func uniqueDocuments(rows []workload.DocumentEvidence) []workload.DocumentEvidence {
	seen := map[workload.DocumentEvidence]bool{}
	var result []workload.DocumentEvidence
	for _, r := range rows {
		if !seen[r] {
			seen[r] = true
			result = append(result, r)
		}
	}
	return result
}
func uniqueRequests(rows []workload.RequestEvidence) []workload.RequestEvidence {
	seen := map[workload.RequestEvidence]bool{}
	var result []workload.RequestEvidence
	for _, r := range rows {
		if !seen[r] {
			seen[r] = true
			result = append(result, r)
		}
	}
	return result
}
