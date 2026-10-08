package cdp

import (
	"encoding/json"
	"sync"
	"testing"
)

func TestNetworkConnectionNumericIdentity(t *testing.T) {
	s := &Server{}
	if got := s.networkConnectionID(""); got != 0 {
		t.Fatalf("absent transport connection = %d", got)
	}
	first := s.networkConnectionID("127.0.0.1:1000->127.0.0.1:8000")
	second := s.networkConnectionID("127.0.0.1:1001->127.0.0.1:8000")
	if first == 0 || first == second {
		t.Fatalf("distinct transport identities collapsed: %d, %d", first, second)
	}
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			if got := s.networkConnectionID("127.0.0.1:1000->127.0.0.1:8000"); got != first {
				t.Errorf("reused connection changed from %d to %d", first, got)
			}
		})
	}
	wg.Wait()
	raw, err := json.Marshal(map[string]any{"connectionId": first})
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		ConnectionID float64 `json:"connectionId"`
	}
	if err = json.Unmarshal(raw, &response); err != nil || response.ConnectionID != float64(first) {
		t.Fatalf("CDP numeric connection identity: %s, %v", raw, err)
	}
}
