package trace

import "testing"

func TestDiagnosticHistoryBoundKeepsNetworkCompletions(t *testing.T) {
	r := New()
	r.Add(Network, "request", map[string]any{"id": "request"})
	r.Add(Network, "response", map[string]any{"id": "first"})
	if len(r.Events()) != 0 {
		t.Fatal("diagnostic history enabled by default")
	}
	r.Start()
	ids := make([]int64, 1024)
	for i := range ids {
		ids[i] = int64(i)
	}
	for i := 0; i < maxDiagnosticEvents+5; i++ {
		r.Add(API, "Document.querySelectorAll", map[string]any{"resultNodeIds": ids})
	}
	events := r.Events()
	if len(events) != maxDiagnosticEvents || events[0].Sequence != 7 {
		t.Fatalf("bounded history length=%d first=%d", len(events), events[0].Sequence)
	}
	data := events[len(events)-1].Data
	if data["resultCount"] != len(ids) || len(data["resultNodeIds"].([]int64)) != maxDiagnosticResultIDs {
		t.Fatalf("unexpected bounded result: %v", data)
	}
	if got := r.EventsSince(0); len(got) != 1 || got[0].Data["id"] != "first" {
		t.Fatalf("network completion lost: %v", got)
	}
	r.Stop()
	r.Add(API, "after-stop", nil)
	if len(r.Events()) != maxDiagnosticEvents {
		t.Fatal("capture continued after stop")
	}
	r.Clear()
	if len(r.Events()) != 0 || len(r.EventsSince(0)) != 1 {
		t.Fatal("clear lost semantic network events or retained diagnostics")
	}
}

func TestFilteredSubscribersPreserveExplicitTraceCapture(t *testing.T) {
	r := New()
	var network, all int
	stopNetwork := r.SubscribeKinds([]Kind{Network}, func(Event) { network++ })
	stopAll := r.Subscribe(func(Event) { all++ })
	if !r.Wants(API) {
		t.Fatal("all-event subscriber missed API events")
	}
	r.Add(API, "query", nil)
	r.Add(Network, "response", nil)
	if network != 1 || all != 2 || len(r.Events()) != 0 {
		t.Fatalf("delivery network=%d all=%d history=%d", network, all, len(r.Events()))
	}
	stopAll()
	if r.Wants(API) {
		t.Fatal("unobserved API event still requested")
	}
	r.Start()
	if !r.Wants(API) {
		t.Fatal("explicit capture did not request API events")
	}
	r.Add(API, "captured-query", nil)
	if got := r.Events(); len(got) != 1 || got[0].Name != "captured-query" {
		t.Fatalf("capture=%v", got)
	}
	stopNetwork()
}

func TestFilteredSubscriptionsDoNotReconfigurePropertyObservation(t *testing.T) {
	r := New()
	var changes []bool
	r.SetObservationChange(func(enabled, reset bool) { changes = append(changes, enabled) })
	initial := len(changes)
	stopNetwork := r.SubscribeKinds([]Kind{Network, Lifecycle}, func(Event) {})
	stopNetwork()
	if len(changes) != initial {
		t.Fatal("non-property subscription reconfigured realm observation")
	}
	stopAPI := r.SubscribeKinds([]Kind{API}, func(Event) {})
	stopSecond := r.SubscribeKinds([]Kind{API}, func(Event) {})
	stopAPI()
	if len(changes) != initial+1 || !changes[initial] {
		t.Fatalf("shared property demand transitions: %v", changes)
	}
	stopSecond()
	stopSecond() // Releasing an already removed subscription is a no-op.
	if len(changes) != initial+2 || changes[initial+1] {
		t.Fatalf("last property subscriber transitions: %v", changes)
	}
}
