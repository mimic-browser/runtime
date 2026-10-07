package browser

import (
	"fmt"
	"github.com/moreveal/mimic/internal/camera"
	"math"
)

type cameraConstraintError struct{ name string }

func (e *cameraConstraintError) Error() string {
	return fmt.Sprintf("Cannot satisfy camera constraint %s", e.name)
}
func containsString(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func cameraStrings(value any) []string {
	switch v := value.(type) {
	case string:
		return []string{v}
	case []any:
		var out []string
		for _, s := range v {
			if s, ok := s.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
func cameraDeviceConstraint(value any) (exact, ideal []string) {
	if v, ok := value.(map[string]any); ok {
		return cameraStrings(v["exact"]), cameraStrings(v["ideal"])
	}
	return nil, cameraStrings(value)
}
func cameraNumberConstraint(value any, actual float64) (bool, float64) {
	if v, ok := value.(float64); ok {
		return true, math.Abs(actual-v) / math.Max(math.Abs(v), 1)
	}
	v, ok := value.(map[string]any)
	if !ok {
		return true, 0
	}
	if n, ok := v["exact"].(float64); ok && math.Abs(actual-n) > 0.001 {
		return false, 0
	}
	if n, ok := v["min"].(float64); ok && actual < n {
		return false, 0
	}
	if n, ok := v["max"].(float64); ok && actual > n {
		return false, 0
	}
	if n, ok := v["ideal"].(float64); ok {
		return true, math.Abs(actual-n) / math.Max(math.Abs(n), 1)
	}
	return true, 0
}
func selectCameraFormat(formats []camera.Format, constraints map[string]any) (camera.Format, error) {
	candidates := append([]camera.Format(nil), formats...)
	for _, name := range []string{"width", "height", "frameRate", "aspectRatio"} {
		filtered := candidates[:0]
		for _, f := range candidates {
			actual := cameraFormatValue(f, name)
			if ok, _ := cameraNumberConstraint(constraints[name], actual); ok {
				filtered = append(filtered, f)
			}
		}
		candidates = filtered
		if len(candidates) == 0 {
			return camera.Format{}, &cameraConstraintError{name}
		}
	}
	if exact, _ := cameraDeviceConstraint(constraints["resizeMode"]); len(exact) > 0 && !containsString(exact, "none") {
		return camera.Format{}, &cameraConstraintError{"resizeMode"}
	}
	// Desktop adapters do not infer a facing direction from device names.
	if exact, _ := cameraDeviceConstraint(constraints["facingMode"]); len(exact) > 0 {
		return camera.Format{}, &cameraConstraintError{"facingMode"}
	}
	// Advanced dictionaries filter only when all their required constraints fit.
	if advanced, ok := constraints["advanced"].([]any); ok {
		for _, entry := range advanced {
			if v, ok := entry.(map[string]any); ok {
				filtered := make([]camera.Format, 0, len(candidates))
				for _, f := range candidates {
					fits := true
					for _, name := range []string{"width", "height", "frameRate", "aspectRatio"} {
						constraint := v[name]
						if number, ok := constraint.(float64); ok {
							constraint = map[string]any{"exact": number}
						}
						if ok, _ := cameraNumberConstraint(constraint, cameraFormatValue(f, name)); !ok {
							fits = false
						}
					}
					if exact, _ := cameraDeviceConstraint(v["resizeMode"]); len(exact) > 0 && !containsString(exact, "none") {
						fits = false
					}
					if facing, _ := cameraDeviceConstraint(v["facingMode"]); len(facing) > 0 {
						fits = false
					}
					if fits {
						filtered = append(filtered, f)
					}
				}
				if len(filtered) > 0 {
					candidates = filtered
				}
			}
		}
	}
	best := camera.Format{}
	score := math.Inf(1)
	for _, f := range candidates {
		distance := 0.0
		for _, name := range []string{"width", "height", "frameRate", "aspectRatio"} {
			v := constraints[name]
			if v == nil {
				switch name {
				case "width":
					v = float64(640)
				case "height":
					v = float64(480)
				case "frameRate":
					v = float64(30)
				}
			}
			_, d := cameraNumberConstraint(v, cameraFormatValue(f, name))
			if constraints[name] == nil {
				distance += d * 0.001
			} else {
				distance += d
			}
		}
		if distance < score {
			best = f
			score = distance
		}
	}
	return best, nil
}
func cameraFormatValue(f camera.Format, name string) float64 {
	switch name {
	case "width":
		return float64(f.Width)
	case "height":
		return float64(f.Height)
	case "frameRate":
		return f.FrameRate
	case "aspectRatio":
		return float64(f.Width) / float64(f.Height)
	}
	return 0
}
