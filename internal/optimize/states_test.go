package optimize

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkloadStatesValidateInputs(t *testing.T) {
	for _, v := range []struct {
		name, body string
		valid      bool
	}{
		{"two-inputs", `{"states":[{"name":"first","env":{"ARTICLE":"one"}},{"name":"second","env":{"ARTICLE":"two"}}]}`, true},
		{"duplicate-name", `{"states":[{"name":"same"},{"name":"same"}]}`, false},
		{"reserved-endpoint", `{"states":[{"name":"first","env":{"mimic_cdp_url":"wrong"}}]}`, false},
		{"empty", `{"states":[]}`, false},
		{"unknown", `{"states":[{"name":"first","command":"other"}]}`, false},
		{"trailing", `{"states":[{"name":"first"}]} {}`, false},
		{"windows-duplicate", `{"states":[{"name":"first","env":{"ARTICLE":"one","article":"two"}}]}`, false},
	} {
		t.Run(v.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "states.json")
			if err := os.WriteFile(path, []byte(v.body), 0600); err != nil {
				t.Fatal(err)
			}
			states, err := loadStates(path)
			if (err == nil) != v.valid {
				t.Fatalf("states=%+v err=%v", states, err)
			}
		})
	}
}
