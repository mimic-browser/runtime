package browser

import (
	"image"
	"math"
	"sync"
	"time"

	"github.com/moreveal/mimic/internal/camera"
	"golang.org/x/image/draw"
)

func mediaCameraLimits(d *MediaDeviceProfile) (width, height int, fps float64) {
	for _, mode := range d.Modes {
		width, height, fps = max(width, mode.Width), max(height, mode.Height), max(fps, mode.FrameRate)
	}
	return
}

type mediaCameraSelection struct {
	camera.Format
	ResizeMode string
}

func selectMediaCameraFormat(d *MediaDeviceProfile, constraints map[string]any) (mediaCameraSelection, error) {
	if d.Processing.Resize != "crop-and-scale" {
		format, err := selectCameraFormat(d.Modes, constraints)
		return mediaCameraSelection{format, "none"}, err
	}
	width, height, fps := mediaCameraLimits(d)
	ranges := map[string]mediaCameraRange{
		"width":       {1, float64(width), float64(d.DefaultMode.Width)},
		"height":      {1, float64(height), float64(d.DefaultMode.Height)},
		"frameRate":   {1, fps, d.DefaultMode.FrameRate},
		"aspectRatio": {1 / float64(height), float64(width), float64(d.DefaultMode.Width) / float64(d.DefaultMode.Height)},
	}
	for _, name := range []string{"width", "height", "frameRate", "aspectRatio"} {
		interval, ok := ranges[name].constrain(constraints[name], false)
		if !ok || (name == "width" || name == "height") && math.Ceil(interval.lower) > math.Floor(interval.upper) {
			return mediaCameraSelection{}, &cameraConstraintError{name}
		}
		ranges[name] = interval
	}
	resize, idealResize := cameraDeviceConstraint(constraints["resizeMode"])
	allowedResize := 3 // 1: none, 2: crop-and-scale.
	if len(resize) > 0 {
		allowedResize = 0
		if containsString(resize, "none") {
			allowedResize |= 1
		}
		if containsString(resize, "crop-and-scale") {
			allowedResize |= 2
		}
	}
	if allowedResize == 0 {
		return mediaCameraSelection{}, &cameraConstraintError{"resizeMode"}
	}
	if facing, _ := cameraDeviceConstraint(constraints["facingMode"]); len(facing) > 0 {
		return mediaCameraSelection{}, &cameraConstraintError{"facingMode"}
	}
	selected, ok := mediaCameraCandidate(d, ranges, constraints, allowedResize&2 == 0)
	if !ok {
		return mediaCameraSelection{}, &cameraConstraintError{"aspectRatio"}
	}
	// Advanced sets are optional as a whole. Each accepted set intersects the
	// existing feasible space; bare advanced numeric values are requirements.
	if advanced, ok := constraints["advanced"].([]any); ok {
		for _, entry := range advanced {
			v, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			next := make(map[string]mediaCameraRange, len(ranges))
			fits := true
			for name, interval := range ranges {
				var ok bool
				next[name], ok = interval.constrain(v[name], true)
				fits = fits && ok
			}
			exact, _ := cameraDeviceConstraint(v["resizeMode"])
			if s, ok := v["resizeMode"].(string); ok {
				exact = []string{s}
			}
			nextResize := allowedResize
			if len(exact) > 0 {
				mask := 0
				if containsString(exact, "none") {
					mask |= 1
				}
				if containsString(exact, "crop-and-scale") {
					mask |= 2
				}
				nextResize &= mask
				fits = fits && nextResize != 0
			}
			if facing, _ := cameraDeviceConstraint(v["facingMode"]); len(facing) > 0 || v["facingMode"] != nil {
				fits = false
			}
			if fits {
				if candidate, ok := mediaCameraCandidate(d, next, constraints, nextResize&2 == 0); ok {
					ranges, allowedResize, selected = next, nextResize, candidate
				}
			}
		}
	}
	resizeMode := "crop-and-scale"
	if allowedResize == 1 {
		resizeMode = "none"
	} else if allowedResize&1 != 0 && !containsString(idealResize, "crop-and-scale") {
		for _, mode := range d.Modes {
			if mode.Width == selected.Width && mode.Height == selected.Height {
				resizeMode = "none"
				break
			}
		}
	}
	return mediaCameraSelection{selected, resizeMode}, nil
}

type mediaCameraRange struct{ lower, upper, preferred float64 }

func (r mediaCameraRange) constrain(value any, required bool) (mediaCameraRange, bool) {
	if n, ok := value.(float64); ok {
		if required {
			value = map[string]any{"exact": n}
		} else {
			value = map[string]any{"ideal": n}
		}
	}
	if v, ok := value.(map[string]any); ok {
		for _, key := range []string{"min", "max", "exact", "ideal"} {
			if n, ok := v[key].(float64); ok {
				if math.IsNaN(n) || math.IsInf(n, 0) {
					return r, false
				}
				switch key {
				case "min":
					r.lower = max(r.lower, n)
				case "max":
					r.upper = min(r.upper, n)
				case "exact":
					r.lower, r.upper = max(r.lower, n), min(r.upper, n)
				case "ideal":
					r.preferred = n
				}
			}
		}
	}
	r.preferred = min(r.upper, max(r.lower, r.preferred))
	return r, r.lower <= r.upper
}

func mediaCameraCandidate(d *MediaDeviceProfile, ranges map[string]mediaCameraRange, constraints map[string]any, none bool) (camera.Format, bool) {
	w, h, fps, ratio := ranges["width"], ranges["height"], ranges["frameRate"], ranges["aspectRatio"]
	best, score := camera.Format{}, math.Inf(1)
	consider := func(width, height int) {
		if float64(width) < w.lower || float64(width) > w.upper || float64(height) < h.lower || float64(height) > h.upper || width*height > 16*1024*1024 {
			return
		}
		actualRatio := float64(width) / float64(height)
		if actualRatio+0.001 < ratio.lower || actualRatio-0.001 > ratio.upper {
			return
		}
		candidate := camera.Format{Width: width, Height: height, FrameRate: fps.preferred}
		distance := 0.0
		for _, name := range []string{"width", "height", "frameRate", "aspectRatio"} {
			interval := ranges[name]
			actual := cameraFormatValue(candidate, name)
			_, fitness := cameraNumberConstraint(constraints[name], actual)
			if constraints[name] == nil && name != "aspectRatio" {
				fitness = math.Abs(actual-interval.preferred) / max(1, interval.preferred) * 0.001
			}
			distance += fitness
		}
		if distance < score {
			best, score = candidate, distance
		}
	}
	if none {
		for _, mode := range d.Modes {
			consider(mode.Width, mode.Height)
		}
	} else {
		maxWidth, maxHeight, _ := mediaCameraLimits(d)
		if constraints["aspectRatio"] == nil && ratio.lower == 1/float64(maxHeight) && ratio.upper == float64(maxWidth) && w.preferred*h.preferred <= 16*1024*1024 {
			for _, width := range []int{int(math.Floor(w.preferred)), int(math.Ceil(w.preferred))} {
				for _, height := range []int{int(math.Floor(h.preferred)), int(math.Ceil(h.preferred))} {
					consider(width, height)
				}
			}
			return best, !math.IsInf(score, 1)
		}
		// At most 8192 widths with a handful of feasible heights each. This
		// avoids a width×height search and handles portrait ratios and joint
		// required bounds without advertising impossible output candidates.
		for width := int(math.Ceil(w.lower)); width <= int(math.Floor(w.upper)); width++ {
			lower := max(h.lower, float64(width)/(ratio.upper+0.001))
			upper := min(h.upper, float64(width)/max(ratio.lower-0.001, 1e-12), float64(16*1024*1024/width))
			for _, height := range []float64{h.preferred, lower, upper, float64(width) / ratio.preferred} {
				height = min(upper, max(lower, height))
				consider(width, int(math.Floor(height)))
				consider(width, int(math.Ceil(height)))
			}
		}
	}
	return best, !math.IsInf(score, 1)
}

func mediaCameraCapabilities(t *cameraTrack) map[string]any {
	d := t.source.mediaDevice
	w, h, fps := mediaCameraLimits(d)
	minWidth, minHeight, minFPS := w, h, fps
	for _, mode := range d.Modes {
		minWidth, minHeight, minFPS = min(minWidth, mode.Width), min(minHeight, mode.Height), min(minFPS, mode.FrameRate)
	}
	resize := []string{"none"}
	if d.Processing.Resize == "crop-and-scale" {
		minWidth, minHeight, minFPS = 1, 1, 1
		resize = append(resize, "crop-and-scale")
	}
	return map[string]any{
		"deviceId": t.deviceID, "groupId": t.groupID,
		"width":       map[string]any{"min": minWidth, "max": w},
		"height":      map[string]any{"min": minHeight, "max": h},
		"frameRate":   map[string]any{"min": minFPS, "max": fps},
		"aspectRatio": map[string]any{"min": float64(minWidth) / float64(h), "max": float64(w) / float64(minHeight)},
		"facingMode":  []string{}, "resizeMode": resize,
	}
}

// A track retains just one immutable output frame. Clones own independent
// output selection; deterministic noise uses source sequence, never read count.
type cameraOutput struct {
	mu            sync.Mutex
	frame         *image.RGBA
	stamp         time.Time
	sequence      uint64
	inputSequence uint64
	width, height int
	enabled       bool
	closed        bool
	generation    uint64
	fps           float64
}

type cameraObservation struct {
	image    *image.RGBA
	stamp    time.Time
	sequence uint64
}

func (t *cameraTrack) observation() cameraObservation {
	s := t.source
	s.mu.RLock()
	input, stamp, sequence := s.frame, s.stamp, s.sequence
	s.mu.RUnlock()
	if input == nil || t.stopped {
		return cameraObservation{}
	}
	width, height := input.Bounds().Dx(), input.Bounds().Dy()
	noise := 0.0
	if s.mediaDevice != nil {
		width, height, noise = t.width, t.height, s.mediaDevice.Processing.Noise
	}
	if t.output == nil {
		t.output = &cameraOutput{}
	}
	return t.output.observe(input, stamp, sequence, width, height, t.frameRate, noise, t.enabled)
}

func (o *cameraOutput) observe(input *image.RGBA, stamp time.Time, sequence uint64, width, height int, fps, noise float64, enabled bool) cameraObservation {
	return o.observeAt(o.currentGeneration(), input, stamp, sequence, width, height, fps, noise, enabled)
}

func (o *cameraOutput) observeAt(generation uint64, input *image.RGBA, stamp time.Time, sequence uint64, width, height int, fps, noise float64, enabled bool) cameraObservation {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed || generation != o.generation {
		return cameraObservation{}
	}
	unchanged := o.width == width && o.height == height && o.enabled == enabled && o.fps == fps
	if o.frame != nil && unchanged && (o.inputSequence == sequence || fps > 0 && stamp.Sub(o.stamp) < time.Duration(float64(time.Second)/fps)*9/10) {
		return cameraObservation{o.frame, o.stamp, o.sequence}
	}
	frame := input
	if !enabled {
		frame = image.NewRGBA(image.Rect(0, 0, width, height))
		for i := 3; i < len(frame.Pix); i += 4 {
			frame.Pix[i] = 255
		}
	} else if width != input.Bounds().Dx() || height != input.Bounds().Dy() || noise > 0 {
		frame = image.NewRGBA(image.Rect(0, 0, width, height))
		// Center crop to the selected output ratio, then perform one CPU scale.
		sourceRect := input.Bounds()
		if float64(sourceRect.Dx())/float64(sourceRect.Dy()) > float64(width)/float64(height) {
			w := max(1, int(float64(sourceRect.Dy())*float64(width)/float64(height)))
			x := sourceRect.Min.X + (sourceRect.Dx()-w)/2
			sourceRect.Min.X, sourceRect.Max.X = x, x+w
		} else {
			h := max(1, int(float64(sourceRect.Dx())*float64(height)/float64(width)))
			y := sourceRect.Min.Y + (sourceRect.Dy()-h)/2
			sourceRect.Min.Y, sourceRect.Max.Y = y, y+h
		}
		draw.ApproxBiLinear.Scale(frame, frame.Bounds(), input, sourceRect, draw.Src, nil)
		if noise > 0 {
			random := uint32(sequence) ^ 0x9e3779b9
			for i := 0; i < len(frame.Pix); i++ {
				if i%4 == 3 {
					continue
				}
				random = random*1664525 + 1013904223
				delta := int((float64(random>>24)/255*2 - 1) * noise)
				frame.Pix[i] = byte(min(255, max(0, int(frame.Pix[i])+delta)))
			}
		}
	}
	o.frame, o.stamp, o.inputSequence = frame, stamp, sequence
	o.width, o.height, o.fps, o.enabled = width, height, fps, enabled
	o.sequence++
	return cameraObservation{frame, stamp, o.sequence}
}

func (o *cameraOutput) close() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.closed = true
	o.frame = nil
}

func (o *cameraOutput) currentGeneration() uint64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.generation
}

// Mode/enabled changes are owned by the Page. An earlier sender snapshot
// cannot publish an old recipe back into the current output cache.
func (o *cameraOutput) invalidate() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.generation++
	o.frame = nil
}
