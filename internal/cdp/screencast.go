package cdp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/moreveal/mimic/internal/browser"
	"math"
	"sort"
	"sync"
	"time"
)

type screencast struct {
	cancel          context.CancelFunc
	done            chan struct{}
	wake            chan struct{}
	ack             chan int
	removeTurn      func()
	renderer        *blinkRenderer
	mu              sync.Mutex
	lastFrame       int
	overlayRevision uint64
	highlight       *presentationHighlight
}

// Rasterize directly at the viewer's requested resolution instead of scaling
// a 1x bitmap up in DevTools. The explicit frame limits bound allocations.
func screencastRasterScale(width, height int, deviceScale float64, options map[string]any) float64 {
	scale := deviceScale
	if scale <= 0 {
		scale = 1
	}
	maxWidth, maxHeight := intValue(options["maxWidth"], 0), intValue(options["maxHeight"], 0)
	if maxWidth > 0 || maxHeight > 0 {
		scale = 2
	}
	if maxWidth > 0 {
		scale = math.Min(scale, float64(maxWidth)/float64(width))
	}
	if maxHeight > 0 {
		scale = math.Min(scale, float64(maxHeight)/float64(height))
	}
	return scale
}

func (s *session) castState() *screencast {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return s.screencast
}

func (s *session) stopScreencast() {
	s.stateMu.Lock()
	cast := s.screencast
	s.screencast = nil
	s.stateMu.Unlock()
	if cast == nil {
		return
	}
	cast.cancel()
	s.page.LockCommands()
	cast.removeTurn()
	s.releaseInspectorTurn()
	s.page.UnlockCommands()
	<-cast.done
}

// Renderer startup, image encoding and ACK waits never hold a Page turn.
func (s *session) handleScreencast(method string, p map[string]any) (any, bool, error) {
	switch method {
	case "DOM.getNodeForLocation":
		cast := s.castState()
		if cast == nil {
			return nil, true, fmt.Errorf("Presentation hit testing requires an active screencast")
		}
		ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
		defer cancel()
		realm, node, _, _, err := cast.renderer.hit(ctx, coordinateValue(p["x"]), coordinateValue(p["y"]))
		if err != nil {
			return nil, true, err
		}
		s.page.LockCommands()
		defer s.page.UnlockCommands()
		frame, id := s.presentationNode(realm, node)
		if frame == nil {
			return nil, true, fmt.Errorf("Presentation node is no longer active")
		}
		result := map[string]any{"backendNodeId": id, "frameId": frame.ID}
		if s.domainEnabled("DOM") {
			result["nodeId"] = id
		}
		return result, true, nil
	case "Page.screencastFrameAck":
		if cast := s.castState(); cast != nil {
			select {
			case cast.ack <- intValue(p["sessionId"], 0):
			default:
			}
		}
		return map[string]any{}, true, nil
	case "Page.stopScreencast":
		s.stopScreencast()
		return map[string]any{}, true, nil
	case "Page.startScreencast":
		format := stringValue(p["format"])
		if format == "" {
			format = "jpeg"
		}
		if format != "jpeg" && format != "png" {
			return nil, true, fmt.Errorf("Unsupported screencast format")
		}
		quality := intValue(p["quality"], 80)
		if quality < 0 || quality > 100 {
			return nil, true, fmt.Errorf("Invalid screencast quality")
		}
		for _, name := range []string{"maxWidth", "maxHeight", "everyNthFrame"} {
			if _, present := p[name]; present && intValue(p[name], 0) <= 0 {
				return nil, true, fmt.Errorf("Invalid %s", name)
			}
		}
		if intValue(p["everyNthFrame"], 1) != 1 {
			return nil, true, fmt.Errorf("Frame sampling is unsupported for change-driven screencasts")
		}
		s.stopScreencast()
		s.page.LockCommands()
		env := s.page.Environment()
		width, height := env.Window.ViewportWidth, env.Window.ViewportHeight
		s.page.UnlockCommands()
		ctx, cancel := context.WithCancel(s.ctx)
		startupCtx, startupCancel := context.WithTimeout(ctx, 20*time.Second)
		renderer, err := newBlinkRenderer(startupCtx, s.server.DevToolsChrome, width, height)
		startupCancel()
		if err != nil {
			cancel()
			return nil, true, err
		}
		cast := &screencast{cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}, 1), ack: make(chan int, 1), renderer: renderer}
		// Starting a view explicitly requests its first frame, independent of
		// whether the next Page turn changes the current observation revision.
		cast.wake <- struct{}{}
		s.page.LockCommands()
		var lastRevision uint64
		_, cast.removeTurn = s.page.SubscribeTurn(func() {
			revision := s.page.InspectorRevision()
			if revision != lastRevision {
				lastRevision = revision
				select {
				case cast.wake <- struct{}{}:
				default:
				}
			}
		})
		s.stateMu.Lock()
		s.screencast = cast
		s.stateMu.Unlock()
		s.page.UnlockCommands()
		s.transport.work.Add(1)
		go func() { defer s.transport.work.Done(); s.runScreencast(ctx, cast, format, quality, p, 15*time.Second) }()
		s.event("Page.screencastVisibilityChanged", map[string]any{"visible": true})
		return map[string]any{}, true, nil
	}
	return nil, false, nil
}

func (s *session) runScreencast(ctx context.Context, cast *screencast, format string, quality int, options map[string]any, captureTimeout time.Duration) {
	defer close(cast.done)
	defer cast.renderer.Close()
	var prior [32]byte
	var sequence int
	waiting, dirty := false, false
	var retry *time.Timer
	var retryWake <-chan time.Time
	retried := false
	defer func() {
		if retry != nil {
			retry.Stop()
		}
	}()
	captureFailed := func(err error) {
		s.event("Log.entryAdded", map[string]any{"entry": map[string]any{"source": "rendering", "level": "error", "text": err.Error(), "timestamp": float64(time.Now().UnixMilli())}})
		// A transient timeout must not strand the requested frame until some
		// unrelated DOM mutation. Retry once; successful or idle views never
		// retain a rendering timer, and teardown cancels the pending work.
		if !retried && errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			retried = true
			retry = time.NewTimer(100 * time.Millisecond)
			retryWake = retry.C
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-cast.wake:
			dirty = true
		case <-retryWake:
			retryWake = nil
			dirty = true
		case ack := <-cast.ack:
			if ack == sequence {
				waiting = false
			}
		}
		if waiting || !dirty {
			continue
		}
		dirty = false
		// The one-slot wake channel and frame ACK already coalesce updates.
		// Capture the completed canonical turn immediately; an additional fixed
		// delay adds latency to every keystroke without bounding work further.
		captureCtx, cancel := context.WithTimeout(ctx, captureTimeout)
		s.page.LockCommands()
		snapshot, err := s.page.CaptureInspectorSnapshot(captureCtx)
		env := s.page.Environment()
		s.page.UnlockCommands()
		if err != nil {
			cancel()
			captureFailed(err)
			continue
		}
		h := sha256.New()
		keys := make([]string, 0, len(snapshot.Files))
		for key := range snapshot.Files {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			_, _ = h.Write([]byte(key))
			_, _ = h.Write(snapshot.Files[key])
		}
		_, _ = h.Write([]byte(fmt.Sprint(snapshot.InspectorScroll, env.Window.ViewportWidth, env.Window.ViewportHeight)))
		scrollState, _ := json.Marshal(snapshot.InspectorFrames)
		_, _ = h.Write(scrollState)
		cast.mu.Lock()
		overlayRevision, highlight := cast.overlayRevision, cast.highlight
		cast.mu.Unlock()
		_, _ = h.Write([]byte(fmt.Sprint(overlayRevision)))
		var hash [32]byte
		copy(hash[:], h.Sum(nil))
		if hash == prior && sequence != 0 {
			cancel()
			continue
		}
		scale := screencastRasterScale(env.Window.ViewportWidth, env.Window.ViewportHeight, env.Display.DeviceScaleFactor, options)
		_, err = cast.renderer.call(captureCtx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": env.Window.ViewportWidth, "height": env.Window.ViewportHeight, "deviceScaleFactor": scale, "mobile": false})
		var data string
		if err == nil {
			clip := map[string]any{"x": 0, "y": 0, "width": env.Window.ViewportWidth, "height": env.Window.ViewportHeight, "scale": 1}
			data, err = cast.renderer.render(captureCtx, snapshot, format, quality, clip, highlight)
		}
		cancel()
		if err != nil {
			captureFailed(err)
			continue
		}
		retried = false
		if retry != nil {
			retry.Stop()
			retryWake = nil
		}
		prior = hash
		sequence++
		waiting = true
		cast.mu.Lock()
		cast.lastFrame = sequence
		cast.mu.Unlock()
		var scrollX, scrollY float64
		if len(snapshot.InspectorScroll) == 2 {
			scrollX, scrollY = snapshot.InspectorScroll[0], snapshot.InspectorScroll[1]
		}
		s.event("Page.screencastFrame", map[string]any{"data": data, "sessionId": sequence, "metadata": map[string]any{"offsetTop": 0, "pageScaleFactor": 1, "deviceWidth": env.Window.ViewportWidth, "deviceHeight": env.Window.ViewportHeight, "scrollOffsetX": scrollX, "scrollOffsetY": scrollY, "timestamp": float64(time.Now().UnixMilli()) / 1000}})
	}
}

func (s *session) handlePresentationInput(method string, p map[string]any) (any, bool, error) {
	cast := s.castState()
	if cast == nil || method != "Input.dispatchMouseEvent" {
		return nil, false, nil
	}
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	realm, nodeID, x, y, err := cast.renderer.hit(ctx, coordinateValue(p["x"]), coordinateValue(p["y"]))
	if err != nil {
		return nil, true, err
	}
	s.page.LockCommands()
	defer s.page.UnlockCommands()
	s.stateMu.RLock()
	inspectMode := s.inspectMode
	s.stateMu.RUnlock()
	if inspectMode == "searchForNode" || inspectMode == "searchForUAShadowDOM" {
		if p["type"] == "mouseReleased" {
			_, ownerID := s.presentationNode(realm, nodeID)
			if ownerID != 0 {
				s.event("Overlay.inspectNodeRequested", map[string]any{"backendNodeId": ownerID})
			}
		}
		return map[string]any{}, true, nil
	}
	p["x"], p["y"] = x, y
	err = s.page.DispatchPresentationMouse(ctx, realm, nodeID, p)
	return map[string]any{}, true, err
}

// Caller holds the Page command boundary; mirror IDs never escape to clients.
func (s *session) presentationNode(realm string, local int64) (*browser.Frame, int64) {
	var owner *browser.Frame
	var visit func(*browser.Frame)
	visit = func(frame *browser.Frame) {
		if frame.RealmID() == realm {
			owner = frame
			return
		}
		for _, child := range frame.Children() {
			visit(child)
		}
	}
	visit(s.page.Top)
	if owner == nil {
		return nil, 0
	}
	if _, ok := s.page.InspectorCanonicalNode(owner, local); !ok {
		return nil, 0
	}
	return owner, s.page.InspectorNodeID(owner, local)
}
