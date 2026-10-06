package network

import (
	"net/url"
	"testing"
)

func TestGeneratedRulesShareManualCompilerAndSnapshots(t *testing.T) {
	target, _ := url.Parse("https://example.test/optional.js")
	no := false
	s := &ResourcePolicyState{}
	rules := []ResourceRule{{ID: "generated", Match: ResourceMatch{URLGlob: target.String()}, Work: ResourceWork{Network: &no}}}
	if err := s.InstallSpecialization(rules); err != nil {
		t.Fatal(err)
	}
	before := s.Capture()
	r := Request{URL: target, specializationAllowed: true}
	if before.decide(r).RuleID != "generated" {
		t.Fatal("generated rule not admitted")
	}
	r.specializationAllowed = false
	if before.decide(r).RuleID != "" {
		t.Fatal("unguarded request specialized")
	}
	r.specializationAllowed = true
	if _, err := s.Update(ResourcePolicy{Rules: []ResourceRule{{ID: "explicit-allow", Match: ResourceMatch{URLGlob: target.String()}}}}); err != nil {
		t.Fatal(err)
	}
	if s.Capture().decide(r).RuleID != "explicit-allow" {
		t.Fatal("manual allow did not override specialization")
	}
	if before.decide(r).RuleID != "generated" {
		t.Fatal("in-flight snapshot changed")
	}
	if _, err := s.Update(ResourcePolicy{}); err != nil {
		t.Fatal(err)
	}
	if s.Capture().decide(r).RuleID != "generated" {
		t.Fatal("manual update lost generated plan")
	}
}
