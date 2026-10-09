package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/trace"
)

type inputProtocolError struct{ message string }

type inputFrameRect struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// ProtocolBoxModel reads layout from the document's canonical owner realm.
// CDP object wrappers may belong to an isolated world, but protocol geometry
// is a browser operation and must not rebuild layout through that wrapper.
func (p *Page) ProtocolBoxModel(ctx context.Context, frame *Frame, nodeID int64) (map[string]any, error) {
	if frame == nil || frame.Realm == nil || frame.Realm.closed || frame.Realm.inactive {
		return nil, fmt.Errorf("Could not compute box model")
	}
	r := frame.Realm
	var model map[string]any
	err := r.scheduler.RunInline(ctx, func(ctx context.Context) error {
		if r.protocolBoxModelRead == nil {
			return fmt.Errorf("protocol box model is unavailable")
		}
		value, err := r.runtime.Call(ctx, r.protocolBoxModelRead, nil, r.val(nodeID))
		if err != nil {
			return err
		}
		defer releaseDebuggerValue(r, value)
		model, _ = value.Export().(map[string]any)
		if model == nil {
			return fmt.Errorf("Could not compute box model")
		}
		return nil
	})
	return model, err
}

// ScrollNodeIntoView is a browser operation, not an invocation of a replaceable
// author method. The node's document owner shares state with all CDP worlds.
func (p *Page) ScrollNodeIntoView(ctx context.Context, nodeID int64, rect any) error {
	frame, ok := p.FrameForDOMNode(nodeID)
	return p.scrollNodeIntoView(ctx, frame, ok, nodeID, rect)
}

// ScrollNodeIntoViewInFrame preserves the remote object's frame ownership for
// CDP calls. Numeric DOM ids alone are not unique across frame documents.
func (p *Page) ScrollNodeIntoViewInFrame(ctx context.Context, frameID string, nodeID int64, rect any) error {
	frame, ok := p.Frame(frameID)
	return p.scrollNodeIntoView(ctx, frame, ok, nodeID, rect)
}

func (p *Page) scrollNodeIntoView(ctx context.Context, frame *Frame, ok bool, nodeID int64, rect any) error {
	if !ok || frame.Realm == nil {
		return fmt.Errorf("Node is detached from document")
	}
	r := frame.Realm
	payload, err := json.Marshal(map[string]any{"action": "protocolInto", "opts": map[string]any{"block": "center", "inline": "center", "behavior": "instant", "ifNeeded": true, "rect": rect}})
	if err != nil {
		return err
	}
	err = r.scheduler.RunInline(ctx, func(ctx context.Context) error {
		if r.protocolScroll == nil {
			return fmt.Errorf("protocol scroll is unavailable")
		}
		value, err := r.runtime.Call(ctx, r.protocolScroll, nil, r.val(nodeID), r.val(string(payload)))
		if value != nil {
			releaseDebuggerValue(r, value)
		}
		return err
	})
	if err == nil {
		p.inputHintRealm, p.inputHintNodeID = r, nodeID
	}
	return err
}

func (e *inputProtocolError) Error() string     { return e.message }
func (e *inputProtocolError) ProtocolCode() int { return -32602 }

// DispatchProtocolInput enters the current document's trusted user-interaction
// path. The document owns focus, pressed buttons and editing state, so sessions
// and isolated worlds observe the same input operation and default action.
func (p *Page) DispatchProtocolInput(ctx context.Context, method string, params map[string]any) error {
	started := time.Now()
	defer func() { p.profileInputPhase(method, "total", started) }()
	r := p.Top.Realm
	if r == nil || r.closed || r.inactive {
		return fmt.Errorf("input document is unavailable")
	}
	operation := ""
	switch method {
	case "Input.dispatchKeyEvent":
		kind, _ := params["type"].(string)
		switch kind {
		case "keyDown", "keyUp", "rawKeyDown", "char":
		default:
			return &inputProtocolError{"Unexpected event type '" + kind + "'"}
		}
		if commands, ok := params["commands"].([]any); ok {
			for _, command := range commands {
				if command != "selectAll" {
					return fmt.Errorf("keyboard editing command %q is not supported", command)
				}
			}
		}
		operation = "key"
	case "Input.insertText":
		operation = "text"
	case "Input.dispatchMouseEvent":
		kind, _ := params["type"].(string)
		switch kind {
		case "mouseMoved", "mousePressed", "mouseReleased", "mouseWheel":
		default:
			return &inputProtocolError{"Unexpected event type '" + kind + "'"}
		}
		if pointer, _ := params["pointerType"].(string); pointer != "" && pointer != "mouse" {
			return fmt.Errorf("pen input is not supported")
		}
		button, _ := params["button"].(string)
		switch button {
		case "", "none", "left", "middle", "right", "back", "forward":
		default:
			return &inputProtocolError{"Invalid mouse button '" + button + "'"}
		}
		operation = "mouse"
	case "Input.setIgnoreInputEvents":
		p.inputIgnored, _ = params["ignore"].(bool)
		return nil
	default:
		return fmt.Errorf("unsupported input operation %s", method)
	}
	if p.inputIgnored {
		return nil
	}
	if deferred, ok := r.runtime.(*deferredRuntime); ok {
		if _, err := deferred.ready(); err != nil {
			return err
		}
	}
	phaseStarted := time.Now()
	encoded, err := json.Marshal(params)
	if err != nil {
		return err
	}
	p.profileInputPhase(method, "validate+marshal", phaseStarted)
	p.realmEvaluationDepth++
	defer func() { p.realmEvaluationDepth--; p.collectRealmOwners() }()
	return r.scheduler.RunInline(ctx, func(ctx context.Context) error {
		phaseStarted := time.Now()
		target := r
		if operation == "mouse" {
			var err error
			target, params, err = p.mouseInputTarget(ctx, target, params)
			if err != nil {
				return err
			}
			encoded, err = json.Marshal(params)
			if err != nil {
				return err
			}
		}
		p.profileInputPhase(method, "mouseTarget", phaseStarted)
		phaseStarted = time.Now()
		hint := int64(0)
		if operation == "mouse" && p.inputHintRealm == target {
			hint = p.inputHintNodeID
		}
		_, err := r.invokeInputWorld(ctx, target, hint, operation, string(encoded))
		p.profileInputPhase(method, "invokeInputWorld", phaseStarted)
		if operation == "mouse" && params["type"] == "mouseReleased" {
			p.inputHintRealm, p.inputHintNodeID = nil, 0
		}
		return err
	})
}

func (p *Page) profileInputPhase(method, phase string, started time.Time) {
	if os.Getenv("MIMIC_PROFILE_CDP") != "1" {
		return
	}
	p.Trace().Add(trace.CDP, "inputPhase", map[string]any{
		"method": method,
		"phase":  phase,
		"ms":     float64(time.Since(started)) / float64(time.Millisecond),
	})
}

// mouseInputTarget maps top-level CDP coordinates through the active iframe
// tree. CDP exposes one input surface per Page even though DOM handles belong
// to individual frame realms.
func (p *Page) mouseInputTarget(ctx context.Context, owner *Realm, params map[string]any) (*Realm, map[string]any, error) {
	x, xOK := numberParameter(params["x"])
	y, yOK := numberParameter(params["y"])
	if !xOK || !yOK {
		return owner, params, nil
	}
	frame, _ := owner.agent.(*Frame)
	if frame == nil {
		return owner, params, nil
	}
	for _, child := range frame.Children() {
		if child.Realm == nil || child.ElementNodeID() == 0 {
			continue
		}
		raw, err := owner.invokeInputWorld(ctx, owner, child.ElementNodeID(), "rect", "{}")
		if err != nil {
			return nil, nil, err
		}
		var rect inputFrameRect
		if err := json.Unmarshal([]byte(raw), &rect); err != nil {
			return nil, nil, err
		}
		if rect.Width <= 0 || rect.Height <= 0 || x < rect.X || y < rect.Y || x >= rect.X+rect.Width || y >= rect.Y+rect.Height {
			continue
		}
		local := make(map[string]any, len(params))
		for key, value := range params {
			local[key] = value
		}
		local["x"], local["y"] = x-rect.X, y-rect.Y
		return p.mouseInputTarget(ctx, child.Realm, local)
	}
	return owner, params, nil
}

func numberParameter(value any) (float64, bool) {
	switch value := value.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	default:
		return 0, false
	}
}

func (r *Realm) invokeInputWorld(ctx context.Context, target *Realm, nodeID int64, operation, payload string) (string, error) {
	if target.closed || target.inactive {
		return "", fmt.Errorf("input dispatcher is unavailable")
	}
	var result string
	invoke := func(ctx context.Context) error {
		// An isolated world can observe CSS/input before the main world runs JS.
		if deferred, ok := target.runtime.(*deferredRuntime); ok {
			if _, err := deferred.ready(); err != nil {
				return err
			}
		}
		started := time.Now()
		err := target.runOnOwner(ctx, func(ctx context.Context) error {
			if target.inputDispatcher == nil {
				return fmt.Errorf("input dispatcher is unavailable")
			}
			values := []engine.Value{target.val(nodeID), target.val(operation), target.val(payload)}
			for _, value := range values {
				defer releaseDebuggerValue(target, value)
			}
			callStarted := time.Now()
			value, err := target.runtime.Call(ctx, target.inputDispatcher, nil, values...)
			r.agent.Page().profileInputPhase("Input.dispatch", "runtime.Call", callStarted)
			if err != nil {
				return err
			}
			defer releaseDebuggerValue(target, value)
			if value != nil {
				result = value.String()
			}
			return nil
		})
		r.agent.Page().profileInputPhase("Input.dispatch", "runOnOwner", started)
		return err
	}
	var err error
	if nested, ok := r.runtime.(engine.ReentrantRuntime); ok && r != target {
		err = nested.RunNested(ctx, invoke)
	} else {
		err = invoke(ctx)
	}
	if err == nil && r != target {
		r.agent.Page().requireCheckpoint(target)
	}
	return result, err
}

func (r *Realm) installProtocolInput(host map[string]any) {
	host["recordScrollPosition"] = r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		p := r.agent.Page()
		p.mu.Lock()
		if numarg(args, 2) == 0 && p.historyIndex >= 0 && p.historyIndex < len(p.history) {
			if state := p.history[p.historyIndex].frames[r.agent.ContextID()]; state != nil && state.realmID == r.ID {
				state.scrollX, state.scrollY = numarg(args, 0), numarg(args, 1)
			}
		}
		p.mu.Unlock()
		if p.turnObservers != nil {
			p.inspectorViewEpoch++
		}
		return nil, nil
	})
	host["scrollParentFrame"] = r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		frame, ok := r.agent.(*Frame)
		if !ok || frame.parent == nil || frame.parent.Realm == nil {
			return nil, nil
		}
		_, err := r.invokeInputWorld(context.Background(), frame.parent.Realm, frame.elementID, "scroll", strarg(args, 0))
		return nil, err
	})
	host["invalidateStyleObservations"] = r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		if changed, _ := arg(args, 0).(bool); changed {
			r.cssImageCSSOMModified = true
		}
		r.document.InvalidateObservations()
		return nil, nil
	})
	host["allowContentEventHandler"] = r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		page := r.agent.Page()
		allowed := page.cspBypassed() || r.contentPolicy().AllowsEventHandler(strarg(args, 0))
		return r.val(allowed), nil
	})
	host["activateProtocolInput"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		for _, world := range r.documentWorlds() {
			world.activationAt = world.scheduler.Now()
			world.activationConsumed = false
		}
		return nil, nil
	})
	host["isIsolatedInputWorld"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		return r.val(r.mainWorld != nil), nil
	})
	host["mainWorldInput"] = r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		owner := r
		if r.mainWorld != nil {
			owner = r.mainWorld
		}
		result, err := r.invokeInputWorld(context.Background(), owner, int64(numarg(args, 0)), strarg(args, 1), strarg(args, 2))
		if err != nil {
			return nil, err
		}
		return r.val(result), nil
	})
	host["broadcastInputEvent"] = r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		allowed := true
		for _, world := range r.documentWorlds() {
			if world == r || world.inputDispatcher == nil {
				continue
			}
			result, err := r.invokeInputWorld(context.Background(), world, int64(numarg(args, 0)), "event", strarg(args, 1))
			if err != nil {
				return nil, err
			}
			var response struct{ Allowed bool }
			if err := json.Unmarshal([]byte(result), &response); err != nil {
				return nil, err
			}
			allowed = allowed && response.Allowed
		}
		return r.val(allowed), nil
	})
}

// DispatchPresentationMouse trusts a hit established by the active external
// Blink presentation. It still delivers events and default actions through the
// canonical Page input owner. Ordinary protocol hit testing is unchanged.
func (p *Page) DispatchPresentationMouse(ctx context.Context, realmID string, nodeID int64, params map[string]any) error {
	if p.inputIgnored {
		return nil
	}
	var target *Realm
	for _, frame := range p.frames {
		if frame.RealmID() == realmID {
			target = frame.Realm
			break
		}
	}
	if target == nil || target.closed || target.inactive {
		return fmt.Errorf("Presentation document is no longer active")
	}
	if node, ok := target.document.Get(nodeID); !ok || node.Type != "element" {
		return fmt.Errorf("Presentation node is no longer available")
	}
	payload, err := json.Marshal(params)
	if err != nil {
		return err
	}
	return target.scheduler.RunInline(ctx, func(ctx context.Context) error {
		_, err := target.invokeInputWorld(ctx, target, nodeID, "presentationMouse", string(payload))
		return err
	})
}
