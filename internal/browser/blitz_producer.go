package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/layoutblitz"
	"github.com/moreveal/mimic/internal/trace"
	"os"
	"slices"
	"strings"
	"time"
)

type blitzDocument struct {
	snapshotReads    uint64
	snapshot         []byte
	document         layoutblitz.Document
	key              string
	fallback         string
	sheets           map[uint64]blitzStylesheetInput
	states           map[uint64]blitzElementState
	controls         map[uint64]blitzControlValue
	controlRevision  uint64
	sheetOrder       []uint64
	fontRevision     uint64
	fontsInitialized bool
	generation       uint64
}

type blitzElementState struct {
	ID    uint64 `json:"id"`
	Mask  uint32 `json:"mask"`
	Flags uint32 `json:"flags"`
}

type blitzStylesheetInput struct {
	ID      uint64 `json:"id"`
	Text    string `json:"text"`
	BaseURL string `json:"baseURL"`
}

type blitzCallStat struct {
	Count  uint64
	Micros int64
}

func (r *Realm) reportBlitzCalls() {
	if len(r.blitzCalls) == 0 {
		return
	}
	data, _ := json.Marshal(r.blitzCalls)
	fmt.Fprintf(os.Stderr, "BLITZ calls realm=%s world=%s %s\n", r.ID, r.worldName, data)
	r.blitzCalls = nil
}

func (r *Realm) installBlitzProducer(host map[string]any) {
	// Native production is unconditional. Legacy production is reachable only
	// through explicit semantic admission, never through an engine env switch.
	profile := os.Getenv("MIMIC_PROFILE_BLITZ") == "1"
	host["blitzTracing"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		return r.val(os.Getenv("MIMIC_PROFILE_BLITZ") == "1"), nil
	})
	host["blitzLegacyTrace"] = r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		fmt.Fprintf(os.Stderr, "BLITZ legacy world=%s %s\n", r.worldName, strarg(args, 0))
		return nil, nil
	})
	host["blitzEpoch"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		owner := r
		if r.mainWorld != nil {
			owner = r.mainWorld
		}
		return r.val(owner.blitzKey()), nil
	})
	host["blitzEnabled"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) { return r.val(true), nil })
	host["blitzInlineStyles"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		return r.val(r.document.InlineStyleSources()), nil
	})
	host["registerBlitzInputs"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		r.blitzInputs = args[0]
		return nil, nil
	})
	host["blitzObserve"] = r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		owner := r
		if r.mainWorld != nil {
			owner = r.mainWorld
		}
		id := int64(numarg(args, 0))
		kind, property := strarg(args, 1), strarg(args, 2)
		if profile {
			started := time.Now()
			defer func() {
				if r.blitzCalls == nil {
					r.blitzCalls = make(map[string]blitzCallStat)
				}
				key := kind + "/" + property
				stat := r.blitzCalls[key]
				stat.Count++
				stat.Micros += time.Since(started).Microseconds()
				r.blitzCalls[key] = stat
			}()
		}
		if kind != "snapshot" && !owner.document.IsConnected(id) {
			return r.val(nil), nil
		}
		var observation any
		prepare := func(ctx context.Context) error {
			if deferred, ok := owner.runtime.(*deferredRuntime); ok {
				if _, err := deferred.ready(); err != nil {
					return err
				}
			}
			return owner.runOnOwner(ctx, func(ctx context.Context) error {
				var err error
				observation, err = owner.observeBlitz(id, kind, property)
				return err
			})
		}
		var prepareErr error
		if nested, ok := r.runtime.(engine.ReentrantRuntime); ok {
			prepareErr = nested.RunNested(context.Background(), prepare)
		} else {
			prepareErr = prepare(context.Background())
		}
		if err := prepareErr; err != nil {
			return nil, err
		}
		return r.val(observation), nil
	})
}

func (owner *Realm) observeBlitz(id int64, kind, property string) (any, error) {
	if err := owner.prepareBlitz(); err != nil {
		return nil, err
	}
	if owner.blitz.fallback != "" {
		return nil, nil
	}
	if kind == "node" {
		packet, err := owner.blitz.document.PackedNodeStyle(id)
		return engine.BinaryBuffer(packet), err
	}
	if kind == "snapshot" {
		started := time.Now()
		built := owner.blitz.snapshot == nil
		if owner.blitz.snapshot == nil {
			var err error
			owner.blitz.snapshot, err = owner.blitz.document.PackedSnapshot()
			if err != nil {
				return nil, err
			}
		}
		owner.blitz.snapshotReads++
		if os.Getenv("MIMIC_PROFILE_BLITZ") == "1" && (built || owner.blitz.snapshotReads%1000 == 0) {
			fmt.Fprintf(os.Stderr, "BLITZ snapshot key=%s built=%t reads=%d bytes=%d ms=%.3f\n", owner.blitz.key, built, owner.blitz.snapshotReads, len(owner.blitz.snapshot), float64(time.Since(started).Microseconds())/1000)
		}
		return engine.BinaryBuffer(owner.blitz.snapshot), nil
	}
	native := owner.blitz.document.Owner
	if kind == "transform" {
		return native.GeometryTransform(uint64(id))
	}
	if kind == "style" {
		// These initial/serialization semantics differ in the pinned Stylo
		// version. They have no geometry dependency and can use the migration
		// oracle without replacing authoritative native layout products.
		if property == "alignment-baseline" || property == "place-items" {
			return nil, nil
		}
		value, err := native.ComputedStyle(uint64(id), property)
		if err != nil {
			return nil, err
		}
		// Stylo has no declaration semantics for some Chrome properties. An
		// empty serializer result is not their initial value: use the existing
		// correctness oracle until those properties have native support.
		if value == "" && !strings.HasPrefix(property, "--") {
			return nil, nil
		}
		return value, nil
	}
	if kind == "styles" {
		var names []string
		if err := json.Unmarshal([]byte(property), &names); err != nil {
			return nil, fmt.Errorf("blitz: invalid style batch: %w", err)
		}
		values, err := native.StyleBatch(uint64(id), names)
		if err != nil {
			return nil, err
		}
		// Match the scalar observation contract: an empty native serialization
		// asks the JS compatibility oracle for that property.
		result := make([]any, len(values))
		for i, value := range values {
			// Keep the scalar and batch projections on the same Chrome
			// serialization path for these pinned Stylo differences.
			if names[i] == "alignment-baseline" || names[i] == "place-items" {
				continue
			}
			if value != "" || strings.HasPrefix(names[i], "--") {
				result[i] = value
			}
		}
		return result, nil
	}
	box, err := native.Rect(uint64(id))
	if err != nil {
		return nil, err
	}
	return map[string]any{"x": box.X, "y": box.Y, "left": box.X, "top": box.Y, "width": box.Width, "height": box.Height, "right": box.X + box.Width, "bottom": box.Y + box.Height, "clientWidth": box.ClientWidth, "clientHeight": box.ClientHeight, "contentWidth": box.ContentWidth, "contentHeight": box.ContentHeight, "overflowWidth": box.ContentWidth}, nil
}

func (r *Realm) blitzKey() string {
	environment := r.agent.Page().environmentView()
	w := environment.Window
	return fmt.Sprintf("%s:%d:%d:%d:%d:%d:%d:%d:%d:%s:%t:%g:%s", r.ID, r.document.Revision(), r.document.ObservationRevision(), r.styleDocumentRevision(), r.styleResourceRevision.Load(), r.resourceRevision.Load(), w.ViewportWidth, w.ViewportHeight, r.selectorTargetID, environment.Preferences.ColorScheme, environment.Preferences.ReducedMotion, environment.Display.DeviceScaleFactor, r.documentBaseURL())
}

func (r *Realm) prepareBlitz() (resultErr error) {
	if r.blitz == nil {
		r.blitz = &blitzDocument{}
	}
	state := r.blitz
	accounting := newBlitzAccounting()
	previousBuilds, previousGeneration := state.document.Builds, state.generation
	defer func() { accounting.finish(r.ID, state, previousBuilds, previousGeneration, resultErr) }()
	w := r.agent.Page().environmentView().Window
	key := r.blitzKey()
	if state.key == key {
		accounting.mark("keyValidation")
		return nil
	}
	accounting.mark("keyValidation")
	var inputs struct {
		Unsupported     string                 `json:"unsupported"`
		RestyleOnChange bool                   `json:"restyleOnChange"`
		States          []blitzElementState    `json:"states"`
		Sheets          []blitzStylesheetInput `json:"sheets"`
		Controls        []blitzControlValue    `json:"controls"`
	}
	if r.blitzInputs == nil {
		return fmt.Errorf("blitz: owner input adapter not initialized")
	}
	err := r.runOnOwner(context.Background(), func(ctx context.Context) error {
		value, err := r.runtime.Call(ctx, r.blitzInputs, nil)
		defer releaseDebuggerValue(r, value)
		if err != nil {
			return err
		}
		return json.Unmarshal([]byte(fmt.Sprint(value.Export())), &inputs)
	})
	if err != nil {
		return err
	}
	accounting.mark("inputs")
	state.fallback = inputs.Unsupported
	if state.fallback == "" {
		state.fallback = layoutblitz.AdmissionReason(r.document)
	}
	if r.agent.Page().environmentView().Preferences.ReducedMotion {
		state.fallback = "native reduced-motion device adapter pending"
	}
	if r.agent.Page().environmentView().Display.DeviceScaleFactor != 1 {
		state.fallback = "native device-scale adapter pending"
	}
	if frame, ok := r.agent.(*Frame); ok && frame.parent != nil {
		state.fallback = "frame viewport adapter pending"
	}
	if state.fallback != "" {
		state.snapshot = nil
		state.document.Close()
		state.sheets = nil
		state.key = key
		r.agent.Page().Trace().Add(trace.JS, "blitzFallback", map[string]any{"reason": state.fallback})
		return nil
	}
	syncStarted := time.Now()
	state.document.BaseURL = r.documentBaseURL().String()
	if err := state.document.Sync(r.document, uint32(w.ViewportWidth), uint32(w.ViewportHeight)); err != nil {
		return err
	}
	if err := state.document.Owner.ColorScheme(r.agent.Page().environmentView().Preferences.ColorScheme == "dark"); err != nil {
		return err
	}
	accounting.mark("canonicalSync")
	if previousBuilds != state.document.Builds || state.sheets == nil {
		state.snapshot = nil
		state.sheets = make(map[uint64]blitzStylesheetInput)
		state.states = nil
		state.controls = nil
		state.sheetOrder = nil
		state.fontsInitialized = false
	}
	if !state.fontsInitialized || state.fontRevision != r.fontCollectionRevision {
		fonts, reason, err := r.blitzFontInputs()
		if err != nil {
			return err
		}
		if reason != "" {
			state.snapshot = nil
			state.fallback = reason
			state.document.Close()
			state.sheets = nil
			state.key = key
			r.agent.Page().Trace().Add(trace.JS, "blitzFallback", map[string]any{"reason": reason})
			return nil
		}
		// The freshly created native font context is already empty of webfonts.
		if state.fontsInitialized || len(fonts) != 0 {
			if err := state.document.Owner.ReplaceFonts(fonts); err != nil {
				return err
			}
		}
		state.fontRevision, state.fontsInitialized = r.fontCollectionRevision, true
	}
	accounting.mark("fonts")
	for _, image := range r.blitzImageInputs() {
		if err := state.document.Owner.ImageIntrinsic(image); err != nil {
			return err
		}
	}
	accounting.mark("images")
	order := make([]uint64, len(inputs.Sheets))
	for index, sheet := range inputs.Sheets {
		order[index] = sheet.ID
	}
	orderChanged := !slices.Equal(order, state.sheetOrder)
	for _, sheet := range inputs.Sheets {
		if old, ok := state.sheets[sheet.ID]; ok && old == sheet && !orderChanged {
			continue
		}
		if err := state.document.Owner.StylesheetAtURL(sheet.ID, sheet.Text, sheet.BaseURL); err != nil {
			return err
		}
		state.sheets[sheet.ID] = sheet
	}
	state.sheetOrder = order
	activeSheets := make(map[uint64]bool, len(inputs.Sheets))
	for _, sheet := range inputs.Sheets {
		activeSheets[sheet.ID] = true
	}
	for id := range state.sheets {
		if !activeSheets[id] {
			if err := state.document.Owner.Stylesheet(id, ""); err != nil {
				return err
			}
			delete(state.sheets, id)
		}
	}
	accounting.mark("sheets")
	currentStates := make(map[uint64]blitzElementState, len(inputs.States)+1)
	for _, element := range inputs.States {
		currentStates[element.ID] = element
	}
	if r.selectorTargetID > 0 {
		id := uint64(r.selectorTargetID)
		element := currentStates[id]
		element.ID, element.Mask, element.Flags = id, element.Mask|16, element.Flags|16
		currentStates[id] = element
	}
	for id, old := range state.states {
		if _, exists := currentStates[id]; !exists {
			if err := state.document.Owner.State(id, old.Mask, 0); err != nil {
				return err
			}
		}
	}
	for id, element := range currentStates {
		// Include bits formerly owned by this transaction, including :target.
		mask := element.Mask | state.states[id].Mask
		if err := state.document.Owner.State(id, mask, element.Flags); err != nil {
			return err
		}
	}
	state.states = currentStates
	accounting.mark("states")
	if err := state.syncControlValues(inputs.Controls, r.document.Revision()); err != nil {
		return err
	}
	accounting.mark("controls")
	if inputs.RestyleOnChange {
		// Rematch reverse dependencies once per changed canonical epoch while
		// retaining nodes, parsed stylesheets and fonts. Clean reads return at
		// the key fast path above; they never pay for another style traversal.
		if err := state.document.Owner.RestyleSubtree(); err != nil {
			return err
		}
	}
	generation, err := state.document.Owner.Resolve(0)
	if err != nil {
		return err
	}
	accounting.mark("nativeResolve")
	if generation != state.generation {
		state.snapshot = nil
	}
	state.generation = generation
	if os.Getenv("MIMIC_PROFILE_BLITZ") == "1" {
		fmt.Fprintf(os.Stderr, "BLITZ sync key=%s builds=%d updates=%d ms=%.3f\n", key, state.document.Builds, state.document.Updates, float64(time.Since(syncStarted).Microseconds())/1000)
	}
	state.key = key
	return nil
}
