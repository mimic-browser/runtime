package optimize

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/workload"
)

type Candidate struct {
	Policy     network.ResourcePolicy `json:"resources"`
	Suppressed []string               `json:"suppressClassic,omitempty"`
	Changes    []string               `json:"changes,omitempty"`
}

type Cost struct {
	Bytes, Responses, Scripts int64
	Elapsed, CPU              float64
}

func cost(runs []Run) Cost {
	var c Cost
	for _, r := range runs {
		c.Bytes += r.Metrics.EncodedBodyBytes
		c.Responses += r.Metrics.ResponseAcquisitions
		c.Scripts += r.Metrics.ClassicScriptsExecuted
		c.Elapsed += r.ElapsedMs
		c.CPU += r.BrowserCPUms
	}
	return c
}
func (c Cost) better(other Cost) bool {
	if c.Bytes != other.Bytes {
		return c.Bytes < other.Bytes
	}
	if c.Responses != other.Responses {
		return c.Responses < other.Responses
	}
	// Cheap execution-stage wins are secondary, with exploratory regression
	// bounds. Finalists are measured repeatedly before the artifact is published.
	return c.Scripts < other.Scripts && c.Elapsed <= other.Elapsed*1.1 && c.CPU <= other.CPU*1.1
}
func passing(runs []Run) bool {
	if len(runs) == 0 {
		return false
	}
	for _, r := range runs {
		if r.Status != "PASS" {
			return false
		}
	}
	return true
}

type action struct {
	Rule       *network.ResourceRule
	Script, ID string
	Bytes      int64
	Cause      string
}

func candidate(actions []action) Candidate {
	c := Candidate{}
	seen := map[string]bool{}
	for _, a := range actions {
		if seen[a.ID] {
			continue
		}
		seen[a.ID] = true
		if a.Rule != nil {
			c.Policy.Rules = append(c.Policy.Rules, *a.Rule)
		}
		if a.Script != "" {
			c.Suppressed = append(c.Suppressed, a.Script)
		}
		c.Changes = append(c.Changes, a.ID)
	}
	return c
}

type Search struct {
	Evaluate          func(context.Context, Candidate) []Run
	Progress          func(int, Cost)
	Budget            int
	WallBudget        time.Duration
	Best              Candidate
	Seed              Candidate
	SeedRuns          []Run
	BestCost          Cost
	NoiseBytes        int64
	BranchURLs        map[string]bool
	Trials, MemoHits  int
	Rejected          []Candidate
	accepted          []action
	memo              map[string]bool
	started           time.Time
	volatile          []string
	current           []Run
	failedSingletons  []action
	stageDeadline     time.Time
	stageBudget       int
	uncoveredBranches [][]action
}

func (s *Search) available(ctx context.Context) bool {
	return ctx.Err() == nil && s.Trials < s.Budget && (s.WallBudget == 0 || time.Since(s.started) < s.WallBudget) && (s.stageDeadline.IsZero() || time.Now().Before(s.stageDeadline)) && (s.stageBudget == 0 || s.Trials < s.stageBudget)
}
func (s *Search) try(ctx context.Context, additions []action) bool {
	if !s.available(ctx) {
		return false
	}
	// New exclusions must precede broad allow rules in a reference seed.
	actions := append(append([]action(nil), additions...), s.accepted...)
	c := candidate(actions)
	keyBytes, _ := json.Marshal(struct {
		Policy     network.ResourcePolicy
		Suppressed []string
	}{c.Policy, c.Suppressed})
	key := string(keyBytes)
	if found, ok := s.memo[key]; ok {
		s.MemoHits++
		return found
	}
	runs := s.Evaluate(ctx, c)
	s.Trials++
	// An uncovered, failing workload proves nothing about the hypothetical
	// branch. Preserve the invalid trial and queue a DIFFERENT plan: deny its
	// newly discovered fallback requests and rerun the original assertions.
	// This is bounded exploration, not acceptance/repair of a failed oracle.
	if !passing(runs) && len(s.uncoveredBranches) < 8 {
		var branch []action
		no := false
		for _, run := range runs {
			if run.Status != "UNSUPPORTED" || run.WorkloadStatus != "FAIL" || len(run.Metrics.Unsupported) > 0 || len(run.Metrics.Violations) > 0 {
				continue
			}
			for _, miss := range run.Metrics.CaptureMisses {
				parts := strings.SplitN(miss, " ", 2)
				if len(parts) != 2 || !strings.HasPrefix(parts[1], "http") {
					continue
				}
				id := "branch:" + parts[1]
				duplicate := false
				for _, a := range actions {
					if a.ID == id {
						duplicate = true
						break
					}
				}
				if duplicate {
					continue
				}
				rule := network.ResourceRule{ID: fmt.Sprintf("branch:%x", sha256Sum([]byte(parts[1]))), Match: network.ResourceMatch{URLGlob: glob(parts[1], s.volatile)}, Work: network.ResourceWork{CacheRead: &no, Network: &no}}
				branch = append(branch, action{ID: id, Rule: &rule})
				if len(branch) >= 4 {
					break
				}
			}
		}
		if len(branch) > 0 {
			s.uncoveredBranches = append(s.uncoveredBranches, append(append([]action(nil), additions...), branch...))
		}
	}
	// A capture miss remains an invalid trial. When its ordinary workload
	// nevertheless passed, try a distinct plan that explicitly denies the new
	// downstream request. This learns fallback combinations without inventing
	// a response or ever acquiring uncaptured data from the live site.
	if !passing(runs) && s.available(ctx) {
		var repairs []action
		no := false
		for _, run := range runs {
			if run.Status != "UNSUPPORTED" || run.WorkloadStatus != "PASS" {
				continue
			}
			for _, miss := range run.Metrics.CaptureMisses {
				parts := strings.SplitN(miss, " ", 2)
				if len(parts) != 2 || !strings.HasPrefix(parts[1], "http") {
					continue
				}
				id := "downstream:" + parts[1]
				duplicate := false
				for _, a := range actions {
					if a.ID == id {
						duplicate = true
						break
					}
				}
				if duplicate {
					continue
				}
				rule := network.ResourceRule{ID: fmt.Sprintf("downstream:%x", sha256Sum([]byte(parts[1]))), Match: network.ResourceMatch{URLGlob: glob(parts[1], s.volatile)}, Work: network.ResourceWork{CacheRead: &no, Network: &no}}
				repairs = append(repairs, action{ID: id, Rule: &rule})
				if len(repairs) >= 4 {
					break
				}
			}
		}
		if len(repairs) > 0 {
			s.Rejected = append(s.Rejected, c)
			s.memo[key] = false
			if s.Progress != nil {
				s.Progress(s.Trials, s.BestCost)
			}
			return s.try(ctx, append(append([]action(nil), additions...), repairs...))
		}
	}
	ok := passing(runs)
	s.memo[key] = ok
	if ok {
		value := cost(runs)
		if value.Bytes < s.BestCost.Bytes && s.BestCost.Bytes-value.Bytes <= s.NoiseBytes {
			if !s.available(ctx) {
				s.memo[key] = false
				s.Rejected = append(s.Rejected, c)
				return false // No budget to distinguish this saving from observed noise.
			}
			confirmation := s.Evaluate(ctx, c)
			s.Trials++
			if !passing(confirmation) {
				s.memo[key] = false
				s.Rejected = append(s.Rejected, c)
				return false
			}
			confirmed := cost(confirmation)
			if confirmed.Bytes > value.Bytes {
				value = confirmed
			}
		}
		neutral := value.Bytes == s.BestCost.Bytes && value.Responses == s.BestCost.Responses && value.Scripts <= s.BestCost.Scripts
		if !value.better(s.BestCost) && !neutral {
			s.memo[key] = false
			s.Rejected = append(s.Rejected, c)
			return false // A passing fallback graph can still acquire more network data.
		}
		s.accepted = actions
		s.current = runs
		manualTie := len(s.Best.Changes) == 1 && s.Best.Changes[0] == "Manual reference policy" && value.Bytes == s.BestCost.Bytes && value.Responses == s.BestCost.Responses && value.Scripts <= s.BestCost.Scripts
		if value.better(s.BestCost) || manualTie {
			s.Best = c
			s.BestCost = value
		}
	} else {
		s.Rejected = append(s.Rejected, c)
		if len(additions) == 1 {
			s.failedSingletons = append(s.failedSingletons, additions[0])
		}
	}
	if s.Progress != nil {
		s.Progress(s.Trials, s.BestCost)
	}
	return ok
}

func (s *Search) eliminate(ctx context.Context, actions []action) {
	if len(actions) == 0 || !s.available(ctx) {
		return
	}
	if s.try(ctx, actions) || len(actions) == 1 {
		return
	}
	// After a small group fails, prefer informative single-resource probes.
	// Repeated binary splits add expensive workload timeouts without yielding
	// extra evidence when most members are required. Nonmonotonic combinations
	// are still tested explicitly later; this is an ordering heuristic only.
	if len(actions) <= 4 {
		for _, a := range actions {
			s.try(ctx, []action{a})
		}
		return
	}
	middle := len(actions) / 2
	s.eliminate(ctx, actions[:middle])
	s.eliminate(ctx, actions[middle:])
}

func glob(raw string, volatile []string) string {
	escape := func(text string) string {
		var b strings.Builder
		for _, char := range text {
			if strings.ContainsRune("\\*?[", char) {
				b.WriteByte('\\')
			}
			b.WriteRune(char)
		}
		return b.String()
	}
	if len(volatile) == 0 {
		return escape(raw)
	}
	parts := strings.SplitN(raw, "?", 2)
	if len(parts) < 2 {
		return escape(raw)
	}
	keys := map[string]bool{}
	for _, key := range volatile {
		keys[key] = true
	}
	parameters := strings.Split(parts[1], "&")
	for n, p := range parameters {
		pair := strings.SplitN(p, "=", 2)
		key, _ := url.QueryUnescape(pair[0])
		if len(pair) == 2 && keys[key] {
			parameters[n] = escape(pair[0]+"=") + "*"
		} else {
			parameters[n] = escape(p)
		}
	}
	return escape(parts[0]+"?") + strings.Join(parameters, "&")
}

func (s *Search) Run(ctx context.Context, baselines []Run, encoded map[string]int64, volatile []string) Candidate {
	s.volatile = volatile
	s.memo = map[string]bool{}
	s.started = time.Now()
	seed := s.Seed
	if len(s.Best.Policy.Rules) > 0 || len(s.Best.Policy.Presets) > 0 {
		seed = s.Best
	}
	for _, rule := range network.ExpandedRules(seed.Policy) {
		rule := rule
		s.accepted = append(s.accepted, action{ID: "Seed: " + rule.ID, Rule: &rule})
	}
	s.current = s.SeedRuns
	resources := map[string]workload.Resource{}
	scripts := map[string]workload.Script{}
	baselineURLs := map[string]bool{}
	for _, run := range baselines {
		for _, resource := range run.Metrics.Resources {
			baselineURLs[resource.URL] = true
		}
	}
	if len(s.SeedRuns) > 0 {
		baselines = s.SeedRuns
	}
	for _, baseline := range baselines {
		for _, r := range baseline.Metrics.Resources {
			if r.Kind != "document" && strings.HasPrefix(r.URL, "http") {
				resources[r.URL] = r
			}
		}
		for _, script := range baseline.Metrics.Scripts {
			if script.External {
				scripts[script.ID] = script
			}
		}
	}
	urls := make([]string, 0, len(resources))
	for raw := range resources {
		urls = append(urls, raw)
	}
	sort.Slice(urls, func(i, j int) bool {
		if s.BranchURLs[urls[i]] != s.BranchURLs[urls[j]] {
			return s.BranchURLs[urls[i]]
		}
		// Removing an obvious parent can activate less-obvious fallback work.
		// Explore those new edges before spending the budget on required bundles.
		if baselineURLs[urls[i]] != baselineURLs[urls[j]] {
			return !baselineURLs[urls[i]]
		}
		if encoded[urls[i]] == encoded[urls[j]] {
			return urls[i] < urls[j]
		}
		return encoded[urls[i]] > encoded[urls[j]]
	})
	no := false
	groups := map[string][]action{}
	var all []action
	var groupKeys []string
	for n, raw := range urls {
		r := resources[raw]
		rule := network.ResourceRule{ID: fmt.Sprintf("learned-resource-%d", n), Match: network.ResourceMatch{URLGlob: glob(raw, volatile)}, Work: network.ResourceWork{CacheRead: &no, Network: &no}}
		a := action{ID: "Avoid " + raw, Rule: &rule, Bytes: encoded[raw], Cause: r.CauseURL}
		all = append(all, a)
		key := r.Kind + "/" + r.Owner + "/" + r.Mechanism + "/" + r.CauseURL
		if s.BranchURLs[raw] {
			key = "recorded-branch/" + key
		}
		if _, ok := groups[key]; !ok {
			groupKeys = append(groupKeys, key)
		}
		groups[key] = append(groups[key], a)
	}
	// Whole-URL actions remain narrow when a profile is later applied live.
	// First try combinations, then split; successful causal subtrees vanish
	// from later inventories naturally, without a full instruction graph.
	// Reserve some search for later stages instead of letting long failing
	// resource timeouts consume the entire experiment budget.
	if s.Budget >= 12 {
		s.stageBudget = s.Budget * 3 / 4
	}
	if s.WallBudget > 0 {
		s.stageDeadline = s.started.Add(s.WallBudget * 3 / 4)
	}
	if len(all) > 0 && !s.try(ctx, all) {
		for _, key := range groupKeys {
			s.eliminate(ctx, groups[key])
		}
	}
	s.stageBudget, s.stageDeadline = 0, time.Time{}
	// Evaluate queued graph branches before treating the captured request set
	// as a static list. Every accepted branch must become fully covered PASS.
	branches := append([][]action(nil), s.uncoveredBranches...)
	for _, branch := range branches {
		s.try(ctx, branch)
	}
	// A Fetch response may be needed for status/headers without its body being
	// consumed. Try this only after avoiding the transaction entirely: body
	// consumers receive an errored stream, never a fabricated empty payload.
	var headersOnly []action
	remaining := resources
	if len(s.current) > 0 {
		remaining = map[string]workload.Resource{}
		for _, run := range s.current {
			for _, resource := range run.Metrics.Resources {
				if resource.Bytes > 0 {
					remaining[resource.URL] = resource
				}
			}
		}
	}
	for n, raw := range urls {
		if remaining[raw].Kind != "fetch" {
			continue
		}
		rule := network.ResourceRule{ID: fmt.Sprintf("learned-headers-%d", n), Match: network.ResourceMatch{URLGlob: glob(raw, volatile), Kinds: []string{"fetch"}}, Work: network.ResourceWork{Body: "none"}}
		headersOnly = append(headersOnly, action{ID: "Acquire headers only: " + raw, Rule: &rule, Bytes: encoded[raw]})
	}
	s.eliminate(ctx, headersOnly)
	var author []action
	if len(s.current) > 0 {
		scripts = map[string]workload.Script{}
		for _, run := range s.current {
			for _, script := range run.Metrics.Scripts {
				if script.External {
					scripts[script.ID] = script
				}
			}
		}
	}
	for id, script := range scripts {
		author = append(author, action{ID: "Suppress author execution: " + script.URL, Script: id, Bytes: int64(script.Bytes)})
	}
	sort.Slice(author, func(i, j int) bool {
		if author[i].Bytes == author[j].Bytes {
			return author[i].Script < author[j].Script
		}
		return author[i].Bytes > author[j].Bytes
	})
	s.eliminate(ctx, author)
	// Bound combination work so it does not starve execution-stage actions.
	// Individual failures do not prove that a containing combination fails.
	failed := s.failedSingletons
	if len(failed) > 8 {
		failed = failed[:8]
	}
	probes := 0
	for i := 0; i < len(failed) && s.available(ctx) && probes < 8; i++ {
		for j := i + 1; j < len(failed) && s.available(ctx) && probes < 8; j++ {
			s.try(ctx, []action{failed[i], failed[j]})
			probes++
		}
	}
	return s.Best
}
