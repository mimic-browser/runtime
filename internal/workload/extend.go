package workload

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
)

// ExtendCapture adds previously absent immutable GET resources, not a second
// browser state. Existing bytes must agree: site changes require a new state,
// never last-writer-wins. Both parent artifacts remain available as evidence.
func ExtendCapture(basePath, branchPath, output string) (int, error) {
	resolvedOutput, err := filepath.Abs(output)
	if err != nil {
		return 0, err
	}
	for _, source := range []string{basePath, branchPath} {
		resolved, e := filepath.Abs(source)
		if e != nil {
			return 0, e
		}
		if strings.EqualFold(resolved, resolvedOutput) {
			return 0, fmt.Errorf("capture extension must preserve its parent artifacts")
		}
	}
	base, err := New(false, basePath, nil)
	if err != nil {
		return 0, err
	}
	defer base.Close()
	branch, err := New(false, branchPath, nil)
	if err != nil {
		return 0, err
	}
	defer branch.Close()
	if strings.Join(base.capture.VolatileQuery, "\x00") != strings.Join(branch.capture.VolatileQuery, "\x00") {
		return 0, fmt.Errorf("branch query normalization differs")
	}
	known := map[string][]*entry{}
	for _, e := range base.capture.Entries {
		known[e.URL] = append(known[e.URL], e)
	}
	added := 0
	branchIdentity, err := FileSHA256(branchPath)
	if err != nil {
		return 0, err
	}
	total := int64(0)
	for _, e := range base.capture.Entries {
		total += e.BodyBytes
	}
	for _, e := range branch.capture.Entries {
		if existing := known[e.URL]; len(existing) > 0 {
			for _, old := range existing {
				samePhase := old.HistoryPhase == nil || e.HistoryPhase == nil || (*old.HistoryPhase == *e.HistoryPhase && old.SourceURL == e.SourceURL)
				if old.Method == e.Method && old.Context == e.Context && samePhase && (old.Status != e.Status || old.BodySHA256 != e.BodySHA256) {
					return 0, fmt.Errorf("existing response changed: %s; record a separate state", e.URL)
				}
			}
			continue
		}
		req, requestErr := http.NewRequest(e.Method, e.URL, nil)
		if requestErr != nil {
			return 0, fmt.Errorf("invalid branch URL: %w", requestErr)
		}
		req.Header = e.RequestHeaders.Clone()
		req.Host = e.RequestHost
		if !immutableRepresentation(e) || !freshRepresentationEquivalent(e, req) || e.Failure != "" {
			return 0, fmt.Errorf("new branch response is not a fresh immutable GET: %s", e.URL)
		}
		total += e.BodyBytes
		if total > 512<<20 || len(base.capture.Entries) >= 10000 {
			return 0, fmt.Errorf("extended capture exceeds storage limits")
		}
		e.BranchCaptureSHA256 = branchIdentity
		base.capture.Entries = append(base.capture.Entries, e)
		added++
	}
	if err := validateOccurrences(base.capture); err != nil {
		return 0, err
	}
	for _, path := range []string{basePath, branchPath} {
		identity, err := FileSHA256(path)
		if err != nil {
			return 0, err
		}
		base.capture.Parents = append(base.capture.Parents, identity)
	}
	return added, writeCapture(output, base.capture, nil)
}
