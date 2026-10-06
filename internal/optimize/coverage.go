package optimize

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/moreveal/mimic/internal/network"
)

// coverageExclusions are real execution decisions, never response matching
// exceptions. An uncovered trial remains invalid. Only a subsequent fresh,
// fully covered trial can validate these exclusions against the workload oracle.
func coverageExclusions(run Run, existing network.ResourcePolicy) (network.ResourcePolicy, bool) {
	if run.Status != "UNSUPPORTED" || run.WorkloadStatus != "PASS" || len(run.Metrics.Unsupported) > 0 || len(run.Metrics.Violations) > 0 {
		return existing, false
	}
	no := false
	changed := false
	for _, miss := range run.Metrics.CaptureMisses {
		parts := strings.SplitN(miss, " ", 2)
		if len(parts) != 2 {
			continue
		}
		parsed, err := url.Parse(parts[1])
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			continue
		}
		// Query-independent path elimination is tested as an action. We do not
		// reinterpret timestamp/UUID parameters as equivalent recorded responses.
		parsed.RawQuery = ""
		parsed.Fragment = ""
		patterns := []string{glob(parsed.String(), nil), glob(parsed.String(), nil) + "\\?*"}
		for _, pattern := range patterns {
			duplicate := false
			for _, rule := range existing.Rules {
				if rule.Match.URLGlob == pattern {
					duplicate = true
					break
				}
			}
			if duplicate {
				continue
			}
			existing.Rules = append(existing.Rules, network.ResourceRule{ID: fmt.Sprintf("coverage-exclusion-%x", sha256Sum([]byte(pattern))), Match: network.ResourceMatch{URLGlob: pattern}, Work: network.ResourceWork{Network: &no, CacheRead: &no}})
			changed = true
		}
		if len(existing.Rules) >= 128 {
			break
		}
	}
	return existing, changed
}
