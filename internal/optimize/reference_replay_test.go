package optimize

import "testing"

func TestReferenceReplayNeverPromotesInvalidTrial(t *testing.T) {
	row := Run{Status: "UNSUPPORTED", WorkloadStatus: "FAIL"}
	row.Metrics.CDPConnections = 1
	row.Metrics.CaptureMisses = []string{"GET https://example.test/fallback"}
	if !canProbeReferenceReplay(row, true) || canProbeReferenceReplay(row, false) {
		t.Fatal("only a supplied reference may be tested as a different covered trial")
	}
	for _, mutation := range []func(*Run){
		func(r *Run) { r.Status = "FAIL" },
		func(r *Run) { r.Metrics.CaptureMisses = nil },
		func(r *Run) { r.Metrics.CDPConnections = 0 },
		func(r *Run) { r.Metrics.Violations = []string{"corrupt"} },
		func(r *Run) { r.Metrics.Unsupported = []string{"socket"} },
		func(r *Run) { r.WorkloadStatus = "CRASH" },
	} {
		copy := row
		mutation(&copy)
		if canProbeReferenceReplay(copy, true) {
			t.Fatalf("invalid reference probe: %+v", copy)
		}
	}
}
