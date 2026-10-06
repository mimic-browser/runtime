package optimize

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/workload"
)

func fakeRun(status string, body int64) []Run {
	return []Run{{Status: status, WorkloadStatus: status, ElapsedMs: 100, BrowserCPUms: 50, Metrics: workload.Metrics{EncodedBodyBytes: body, ResponseAcquisitions: body / 10, ClassicScriptsExecuted: 1}}}
}

func TestSearchLearnsNonmonotonicCombination(t *testing.T) {
	no := false
	s := Search{Budget: 12, BestCost: Cost{Bytes: 100, Responses: 10, Scripts: 1, Elapsed: 100, CPU: 50}, memo: map[string]bool{}, started: time.Now()}
	s.Evaluate = func(ctx context.Context, c Candidate) []Run {
		if len(c.Policy.Rules) == 2 {
			return fakeRun("PASS", 30)
		}
		return fakeRun("FAIL", 100)
	}
	a := action{ID: "A", Rule: &network.ResourceRule{ID: "A", Match: network.ResourceMatch{URLGlob: "A"}, Work: network.ResourceWork{Network: &no, CacheRead: &no}}}
	b := action{ID: "B", Rule: &network.ResourceRule{ID: "B", Match: network.ResourceMatch{URLGlob: "B"}, Work: network.ResourceWork{Network: &no, CacheRead: &no}}}
	if s.try(context.Background(), []action{a}) || s.try(context.Background(), []action{b}) {
		t.Fatal("individual removals unexpectedly passed")
	}
	if !s.try(context.Background(), []action{a, b}) || s.BestCost.Bytes != 30 {
		t.Fatal("nonmonotonic passing combination was lost")
	}
}

func TestUncoveredDownstreamIsASeparateValidatedCandidate(t *testing.T) {
	no := false
	calls := 0
	s := Search{Budget: 4, BestCost: Cost{Bytes: 100, Responses: 10, Elapsed: 100, CPU: 50, Scripts: 1}, memo: map[string]bool{}, started: time.Now()}
	s.Evaluate = func(ctx context.Context, c Candidate) []Run {
		calls++
		if len(c.Policy.Rules) == 1 {
			return []Run{{Status: "UNSUPPORTED", WorkloadStatus: "PASS", Metrics: workload.Metrics{CaptureMisses: []string{"GET https://site.test/fallback.js"}}}}
		}
		if len(c.Policy.Rules) == 2 {
			return fakeRun("PASS", 20)
		}
		t.Fatalf("unexpected rules: %+v", c.Policy.Rules)
		return nil
	}
	parent := action{ID: "parent", Rule: &network.ResourceRule{ID: "parent", Match: network.ResourceMatch{URLGlob: "https://site.test/optional.js"}, Work: network.ResourceWork{CacheRead: &no, Network: &no}}}
	if !s.try(context.Background(), []action{parent}) || calls != 2 || len(s.Rejected) != 1 || s.BestCost.Bytes != 20 {
		t.Fatalf("uncovered trial was accepted or repair was not separately validated: %+v", s)
	}
	if len(s.Best.Policy.Rules) != 2 {
		t.Fatal("passing fallback decision was not saved")
	}
}

func TestOracleFailureDoesNotBecomeCoverageRepair(t *testing.T) {
	calls := 0
	s := Search{Budget: 5, memo: map[string]bool{}, started: time.Now(), BestCost: Cost{Bytes: 100}}
	s.Evaluate = func(ctx context.Context, c Candidate) []Run {
		calls++
		return []Run{{Status: "UNSUPPORTED", WorkloadStatus: "FAIL", Metrics: workload.Metrics{CaptureMisses: []string{"GET https://site.test/required"}}}}
	}
	if s.try(context.Background(), []action{{ID: "remove", Script: "source"}}) || calls != 1 {
		t.Fatal("workload failure was treated as evidence to block a required fallback")
	}
}

func TestNetworkObjectiveDominatesNoisyRuntimeNumbers(t *testing.T) {
	base := Cost{Bytes: 100, Responses: 10, Scripts: 10, Elapsed: 100, CPU: 80}
	if !(Cost{Bytes: 50, Responses: 5, Scripts: 10, Elapsed: 110, CPU: 85}).better(base) {
		t.Fatal("network savings were lost to timing noise")
	}
	if (Cost{Bytes: 101, Responses: 10, Scripts: 0, Elapsed: 1, CPU: 1}).better(base) {
		t.Fatal("CPU pruning overruled acquisition objective")
	}
	if (Cost{Bytes: 100, Responses: 10, Scripts: 1, Elapsed: 500, CPU: 500}).better(base) {
		t.Fatal("equal-traffic expensive runtime intervention selected")
	}
}

func TestPresentationPlainAndColor(t *testing.T) {
	var plain, colored strings.Builder
	presentation{out: &plain}.line("PASS", "ok")
	presentation{out: &colored, color: true}.line("PASS", "ok")
	if strings.Contains(plain.String(), "\x1b") || !strings.Contains(colored.String(), "\x1b[32m") {
		t.Fatal("semantic color/plain output boundary")
	}
}

func TestSearchPreservesHeadersButAvoidsUnobservedBody(t *testing.T) {
	baseline := fakeRun("PASS", 100)
	baseline[0].Metrics.Resources = []workload.Resource{{URL: "https://site.test/status", Kind: "fetch", Owner: "frame"}}
	s := Search{Budget: 12, BestCost: cost(baseline)}
	s.Evaluate = func(_ context.Context, c Candidate) []Run {
		if len(c.Policy.Rules) == 1 && c.Policy.Rules[0].Work.Body == "none" {
			return fakeRun("PASS", 0)
		}
		return fakeRun("FAIL", 100)
	}
	best := s.Run(context.Background(), baseline, map[string]int64{"https://site.test/status": 100}, nil)
	if len(best.Policy.Rules) != 1 || best.Policy.Rules[0].Work.Body != "none" {
		t.Fatalf("headers-only action not discovered: %+v", best)
	}
}

func TestSearchExploresFailingUncoveredBranchWithoutAcceptingIt(t *testing.T) {
	baseline := fakeRun("PASS", 100)
	baseline[0].Metrics.Resources = []workload.Resource{{URL: "https://site.test/optional.js", Kind: "script"}}
	s := Search{Budget: 12, BestCost: cost(baseline)}
	calls := 0
	s.Evaluate = func(_ context.Context, c Candidate) []Run {
		calls++
		if len(c.Policy.Rules) == 1 {
			return []Run{{Status: "UNSUPPORTED", WorkloadStatus: "FAIL", Metrics: workload.Metrics{CaptureMisses: []string{"GET https://site.test/fallback.js"}}}}
		}
		if len(c.Policy.Rules) == 2 {
			return fakeRun("PASS", 20)
		}
		return fakeRun("FAIL", 100)
	}
	best := s.Run(context.Background(), baseline, map[string]int64{"https://site.test/optional.js": 100}, nil)
	if len(best.Policy.Rules) != 2 || s.BestCost.Bytes != 20 || len(s.Rejected) == 0 || calls != 2 {
		t.Fatalf("branch was not independently validated: best=%+v search=%+v calls=%d", best, s, calls)
	}
}

func TestSearchDoesNotAcceptNoiseWithoutPassingConfirmation(t *testing.T) {
	no := false
	for _, budget := range []int{1, 2} {
		calls := 0
		s := Search{Budget: budget, BestCost: Cost{Bytes: 100, Responses: 10, Scripts: 1, Elapsed: 100, CPU: 50}, NoiseBytes: 10, memo: map[string]bool{}, started: time.Now()}
		s.Evaluate = func(_ context.Context, _ Candidate) []Run {
			calls++
			if calls == 1 {
				return fakeRun("PASS", 95)
			}
			return fakeRun("FAIL", 95)
		}
		if s.try(context.Background(), []action{{ID: "noise", Rule: &network.ResourceRule{ID: "noise", Work: network.ResourceWork{Network: &no}}}}) || s.BestCost.Bytes != 100 || len(s.accepted) > 0 {
			t.Fatalf("noise was accepted with budget %d: %+v", budget, s)
		}
	}
}

func TestSearchRejectsPassingAcquisitionRegression(t *testing.T) {
	no := false
	s := Search{Budget: 2, BestCost: Cost{Bytes: 100, Responses: 10, Scripts: 1}, memo: map[string]bool{}, started: time.Now()}
	s.Evaluate = func(_ context.Context, _ Candidate) []Run { return fakeRun("PASS", 150) }
	if s.try(context.Background(), []action{{ID: "fallback", Rule: &network.ResourceRule{ID: "fallback", Work: network.ResourceWork{Network: &no}}}}) || len(s.accepted) > 0 || s.BestCost.Bytes != 100 {
		t.Fatal("a passing but more expensive branch displaced the network plan")
	}
}

func TestMatchedValidationKeepsTheManualAcquisitionFloor(t *testing.T) {
	manual := Summary{BodyBytes: 50, Responses: 2}
	for _, candidate := range []Summary{{BodyBytes: 51, Responses: 2}, {BodyBytes: 40, Responses: 3}} {
		if !acquisitionRegresses(candidate, manual) {
			t.Fatal("installed specialization could regress the competent manual comparison")
		}
	}
	if acquisitionRegresses(manual, manual) || acquisitionRegresses(Summary{BodyBytes: 40, Responses: 1}, manual) {
		t.Fatal("matched tie or improvement was rejected")
	}
}

func TestSearchPrioritizesRecordedFallbackGroup(t *testing.T) {
	known := "https://site.test/app.js"
	fallback := "https://site.test/fallback.js"
	baseline := fakeRun("PASS", 100)
	baseline[0].Metrics.Resources = []workload.Resource{{URL: known, Kind: "script"}}
	reference := fakeRun("PASS", 110)
	reference[0].Metrics.Resources = []workload.Resource{{URL: known, Kind: "script"}, {URL: fallback, Kind: "script"}}
	s := Search{Budget: 3, BestCost: cost(reference), SeedRuns: reference, BranchURLs: map[string]bool{fallback: true}}
	var probes []Candidate
	s.Evaluate = func(_ context.Context, candidate Candidate) []Run {
		probes = append(probes, candidate)
		for _, rule := range candidate.Policy.Rules {
			if rule.Match.URLGlob == known {
				return fakeRun("FAIL", 110)
			}
		}
		return fakeRun("PASS", 100)
	}
	best := s.Run(context.Background(), baseline, map[string]int64{known: 100, fallback: 10}, nil)
	if len(probes) < 2 || len(probes[1].Policy.Rules) != 1 || probes[1].Policy.Rules[0].Match.URLGlob != fallback || len(best.Policy.Rules) != 1 {
		t.Fatalf("small fallback was starved by the larger required bundle: %+v", probes)
	}
}
