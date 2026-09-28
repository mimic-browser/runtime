package trace

import "testing"

func TestDiagnosticHistoryBoundKeepsNetworkCompletions(t *testing.T) {
	r := New()
	r.Add(Network, "response", map[string]any{"id": "first"})
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
	r.Clear()
	if len(r.Events()) != 0 || len(r.EventsSince(0)) != 0 {
		t.Fatal("clear retained events")
	}
}
