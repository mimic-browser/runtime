package optimize

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// WorkloadState changes only the external process inputs. Browser state and
// captured environments remain independent; the same plan must pass every state.
type WorkloadState struct {
	Name string            `json:"name"`
	Env  map[string]string `json:"env,omitempty"`
}

func loadStates(path string) ([]WorkloadState, error) {
	if path == "" {
		return []WorkloadState{{Name: "default"}}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.Size() > 1<<20 {
		return nil, fmt.Errorf("workload states file must be at most 1 MiB")
	}
	var document struct {
		States []WorkloadState `json:"states"`
	}
	d := json.NewDecoder(f)
	d.DisallowUnknownFields()
	if err := d.Decode(&document); err != nil {
		return nil, fmt.Errorf("invalid workload states: %w", err)
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("workload states must contain one JSON object")
	}
	if len(document.States) == 0 || len(document.States) > 32 {
		return nil, fmt.Errorf("provide between 1 and 32 workload states")
	}
	names := map[string]bool{}
	for _, state := range document.States {
		if strings.TrimSpace(state.Name) == "" || strings.ContainsAny(state.Name, "\x00\r\n\x1b") || names[state.Name] {
			return nil, fmt.Errorf("workload state names must be unique nonempty plain text")
		}
		names[state.Name] = true
		keys := map[string]bool{}
		for key, value := range state.Env {
			upper := strings.ToUpper(key)
			if key == "" || strings.ContainsAny(key, "=\x00") || strings.ContainsRune(value, 0) || keys[upper] {
				return nil, fmt.Errorf("invalid or duplicate environment name in state %q", state.Name)
			}
			if upper == "MIMIC_CDP_URL" || upper == "MIMIC_ENDPOINT" || upper == "PW_MIMIC_ENDPOINT" || upper == "MIMIC_WORKLOAD_TOKEN" || upper == "MIMIC_DATA_DIR" || upper == "MIMIC_BOOTSTRAP_CACHE_DIR" {
				return nil, fmt.Errorf("state %q must not override Mimic's endpoint or internal control variables", state.Name)
			}
			keys[upper] = true
		}
	}
	return document.States, nil
}
