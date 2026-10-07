package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/scheduler"
	"github.com/moreveal/mimic/internal/trace"
)

// Resource discovery reads the canonical computed declarations. It neither
// renders backgrounds nor creates a second selector/cascade state model.
func (r *Realm) syncCSSImageResources(ctx context.Context) error {
	if r.mainWorld != nil || r.inactive || r.closed || r.readyState == "loading" || r.computedStyleFlatRead == nil {
		return nil
	}
	epoch := r.styleProjectionEpoch("values")
	if r.cssImageEpochValid && r.cssImageEpoch == epoch {
		return nil
	}
	r.cssImageEpoch, r.cssImageEpochValid = epoch, true
	// A resource checkpoint must not execute selector/style JavaScript when no
	// canonical input can contain an image. Apart from unnecessary work, doing
	// so observes author-modified intrinsics even on completely unstyled pages.
	// CSSOM edits conservatively admit discovery because their source lives in
	// the owning realm; no independent declaration/cascade state is maintained.
	if !r.cssImageCSSOMModified && !r.cssImageSourceCandidate() {
		return nil
	}
	args := []engine.Value{r.val(0), r.val("imageResources"), r.val("")}
	for _, arg := range args {
		defer releaseDebuggerValue(r, arg)
	}
	value, err := r.runtime.Call(ctx, r.computedStyleFlatRead, nil, args...)
	defer releaseDebuggerValue(r, value)
	if err != nil {
		return err
	}
	var urls []string
	if err = json.Unmarshal([]byte(fmt.Sprint(value.Export())), &urls); err != nil {
		return err
	}
	if r.cssImageLoads == nil {
		r.cssImageLoads = make(map[preloadKey]bool)
	}
	for _, raw := range urls {
		u, resolveErr := r.resolveDocument(raw)
		if resolveErr != nil {
			return resolveErr
		}
		request := r.elementRequest(u, nil, network.Image)
		request.PerformanceInitiatorType = "css"
		key := preloadRequestKey(request)
		if r.cssImageLoads[key] {
			continue
		}
		r.cssImageLoads[key] = true
		blocks := r.beginLoadBlocker("css-image")
		request = r.withResourceTiming(request)
		r.resourceWG.Add(1)
		go func() {
			defer r.resourceWG.Done()
			_, loadErr := r.agent.Page().loader.Load(r.resourceContext, request)
			if r.resourceContext.Err() != nil {
				return
			}
			r.scheduler.Post(scheduler.Network, 0, func(taskContext context.Context) error {
				if loadErr != nil {
					r.agent.Page().trace.Add(trace.Error, "cssImageLoad", map[string]any{"url": raw, "error": loadErr.Error(), "realm": r.ID})
				}
				r.notifyPerformanceObservers(taskContext)
				if blocks {
					r.endLoadBlocker("css-image")
				}
				return nil
			})
		}()
	}
	return nil
}

func (r *Realm) cssImageSourceCandidate() bool {
	possible := func(source string) bool {
		lower := strings.ToLower(source)
		return strings.Contains(lower, "url") || strings.Contains(lower, "@import") || strings.Contains(source, `\`)
	}
	for _, source := range r.document.InlineStyleSources() {
		if possible(source) {
			return true
		}
	}
	for _, node := range r.document.FindAllByTagName("style") {
		if possible(r.document.TextContent(node.ID)) {
			return true
		}
	}
	// Linked sheets may complete asynchronously or be inserted after parsing.
	// Admit them before their body is available; the existing resource revision
	// schedules another discovery when the canonical sheet becomes available.
	for _, node := range r.document.FindAllByTagName("link") {
		if hasLinkRelation(node.Attributes["rel"], "stylesheet") {
			return true
		}
	}
	return false
}
