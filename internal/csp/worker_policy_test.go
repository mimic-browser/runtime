package csp

import (
	"net/url"
	"testing"
)

func TestWorkerSourceFallback(t *testing.T) {
	base, _ := url.Parse("https://example.test/page")
	target, _ := url.Parse("https://example.test/worker.js")
	for _, tc := range []struct {
		policy  string
		allowed bool
	}{
		{"worker-src 'self'; child-src 'none'", true},
		{"child-src 'none'; script-src 'self'", false},
		{"script-src 'none'; default-src 'self'", false},
		{"default-src 'none'", false},
		{"child-src 'self'; default-src 'none'", true},
	} {
		if got := Parse(tc.policy).AllowsResource("worker-src", base, target); got != tc.allowed {
			t.Errorf("%s: %v, want %v", tc.policy, got, tc.allowed)
		}
	}
}
