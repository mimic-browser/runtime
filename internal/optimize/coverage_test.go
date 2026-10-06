package optimize

import (
	"context"
	"testing"

	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/workload"
)

func TestCoverageExclusionIsARealSubsequentlyValidatedDecision(t *testing.T) {
	invalid := Run{Status: "UNSUPPORTED", WorkloadStatus: "PASS", Metrics: workload.Metrics{CaptureMisses: []string{"POST https://app.test/events?timestamp=123"}}}
	policy, changed := coverageExclusions(invalid, network.ResourcePolicy{})
	if !changed || len(policy.Rules) != 2 {
		t.Fatal("missing bounded path actions")
	}
	if policy.Rules[1].Match.URLGlob != "https://app.test/events\\?*" {
		t.Fatal(policy.Rules[1].Match.URLGlob)
	}
	if _, changed := coverageExclusions(invalid, policy); changed {
		t.Fatal("duplicate exclusion")
	}
	invalid.WorkloadStatus = "FAIL"
	if _, changed := coverageExclusions(invalid, network.ResourcePolicy{}); changed {
		t.Fatal("oracle failure was repaired")
	}
}

func TestSearchRefinesStrongReferenceBeforeBroadAllow(t *testing.T) {
	s := Search{Budget: 8, Best: Candidate{Policy: network.ResourcePolicy{Rules: []network.ResourceRule{{ID: "allow-script", Match: network.ResourceMatch{Kinds: []string{"script"}}}}}}, BestCost: Cost{Bytes: 100, Responses: 10, Scripts: 1, Elapsed: 100, CPU: 50}}
	s.SeedRuns = fakeRun("PASS", 100)
	s.SeedRuns[0].Metrics.Resources = []workload.Resource{{URL: "https://app.test/optional.js", Kind: "script"}}
	s.Evaluate = func(ctx context.Context, c Candidate) []Run {
		if len(c.Policy.Rules) == 2 && c.Policy.Rules[0].Work.Network != nil && !*c.Policy.Rules[0].Work.Network && c.Policy.Rules[1].ID == "generated:0:allow-script" {
			return fakeRun("PASS", 20)
		}
		t.Fatalf("reference allow prevented refinement: %+v", c.Policy.Rules)
		return nil
	}
	s.Run(context.Background(), s.SeedRuns, map[string]int64{"https://app.test/optional.js": 80}, nil)
	if s.BestCost.Bytes != 20 {
		t.Fatal("strong seed was not refined")
	}
}
