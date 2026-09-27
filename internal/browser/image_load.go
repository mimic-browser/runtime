package browser

import (
	"context"
	"strings"

	"github.com/moreveal/mimic/internal/imageresource"
	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/scheduler"
	"github.com/moreveal/mimic/internal/trace"
)

type imageLoad struct {
	decodeDisallowed bool
	complete         bool
	currentSrc       string
	resource         *imageresource.Resource
	originClean      bool
	queued           bool
	waiting          bool
	cancel           context.CancelFunc
	blocks           bool
}

// A document's available images are distinct from the HTTP cache: Chrome 152
// reuses a successfully decoded image for another element even with no-store.
// Only successful results are retained, with CORS mode/credentials in the key.
type availableImage struct {
	resource    *imageresource.Resource
	originClean bool
}

func (r *Realm) startDocumentImages() {
	for _, node := range r.document.FindAllByTagName("img") {
		r.updateImage(node.ID, false)
	}
}

// Image requests belong to their document even before the element is inserted.
// Attribute changes in one task coalesce; superseded completions cannot fire on
// the replacement request. All observable work stays on the Page event loop.
func (r *Realm) updateImage(id int64, changed bool) {
	if r.imageLoads == nil {
		r.imageLoads = map[int64]*imageLoad{}
	}
	previous := r.imageLoads[id]
	if previous != nil && (!changed && !previous.waiting || previous.queued) {
		return
	}
	if previous != nil && previous.cancel != nil {
		previous.cancel()
	}
	reason := "image"
	node, exists := r.document.Get(id)
	lazy := exists && strings.EqualFold(node.Attributes["loading"], "lazy")
	// Chrome excludes lazy images from the Window load delay, including lazy
	// images currently near the viewport. Their request and element load/error
	// lifecycle continue independently.
	current := &imageLoad{originClean: true, queued: true, blocks: !lazy && r.beginLoadBlocker(reason)}
	if previous != nil {
		current.resource = previous.resource
		current.currentSrc = previous.currentSrc
		current.originClean = previous.originClean
	}
	r.imageLoads[id] = current
	if previous != nil && previous.blocks {
		previous.blocks = false
		r.endLoadBlocker(reason)
	}
	finish := func(ctx context.Context, kind string) error {
		if r.imageLoads[id] != current {
			return nil
		}
		current.complete = true
		r.resourceRevision.Add(1)
		if kind != "load" {
			current.resource = nil
		}
		defer func() {
			if current.blocks {
				current.blocks = false
				r.endLoadBlocker(reason)
			}
		}()
		if kind == "" {
			return nil
		}
		return r.dispatchResourceEvent(ctx, id, kind)
	}
	// Queue an ordinary resource task: script transport starts still take
	// precedence, but unrelated timers must not starve an image indefinitely.
	r.scheduler.Post(scheduler.Network, 0, func(ctx context.Context) error {
		current.queued = false
		node, ok := r.document.Get(id)
		if !ok {
			return finish(ctx, "")
		}
		src, exists := node.Attributes["src"]
		if !exists {
			return finish(ctx, "")
		}
		if strings.TrimSpace(src) == "" {
			return finish(ctx, "error")
		}
		u, err := r.resolveDocument(src)
		if err != nil {
			return finish(ctx, "error")
		}
		request := r.elementRequest(u, node.Attributes, network.Image)
		if err := request.CheckContentPolicy(); err != nil {
			return finish(ctx, "error")
		}
		key := preloadRequestKey(request)
		if available, ok := r.availableImages.get(key); ok && r.agent.Page().loader.ResourceReuseAllowed(request) {
			current.currentSrc = u.String()
			current.resource = available.resource
			current.originClean = available.originClean
			current.decodeDisallowed = !r.agent.Page().loader.ResourceDecodeAllowed(request)
			return finish(ctx, "load")
		}
		// Detached lazy images cannot intersect a viewport. Already available
		// images above still complete, including clones of a loaded lazy image.
		if strings.EqualFold(node.Attributes["loading"], "lazy") && !r.document.IsConnected(id) {
			current.waiting = true
			if current.blocks {
				current.blocks = false
				r.endLoadBlocker(reason)
			}
			return nil
		}
		resourceContext, cancel := context.WithCancel(r.resourceContext)
		current.cancel = cancel
		r.resourceWG.Add(1)
		go func() {
			defer r.resourceWG.Done()
			defer cancel()
			response, err := r.loadResource(resourceContext, request)
			if resourceContext.Err() != nil {
				return
			}
			var resource *imageresource.Resource
			kind := "load"
			originClean, corsErr := resourceResponseOrigin(request, response)
			if response.Partial && len(response.Body) != 0 {
				candidate := imageresource.New(response.Body, response.Headers.Get("Content-Type"))
				if metadata, metadataErr := candidate.RequireMetadata(); metadataErr == nil {
					r.agent.Page().trace.Add(trace.Resource, "policyMetadata", map[string]any{"url": u.String(), "capability": "intrinsicImageDimensions", "source": "prefix", "width": metadata.Width, "height": metadata.Height})
				} else {
					r.agent.Page().trace.Add(trace.Resource, "policyMetadata", map[string]any{"url": u.String(), "capability": "intrinsicImageDimensions", "source": "prefix", "unavailable": metadataErr.Error()})
				}
			}
			if err == nil {
				err = corsErr
			}
			// Chrome decodes an image response even with an HTTP error status.
			// Transport/CORS failures and invalid image bytes determine failure.
			if err != nil {
				kind = "error"
			} else {
				if response.DecodeDisallowed {
					resource, err = imageresource.MetadataOnly(response.Body, response.Headers.Get("Content-Type"))
				} else {
					if response.DecodedPixelBudgetEnabled() {
						resource = imageresource.NewWithDecodeBudget(response.Body, response.Headers.Get("Content-Type"), response.ReserveDecodedPixelBytes)
					} else {
						resource = imageresource.New(response.Body, response.Headers.Get("Content-Type"))
					}
					_, err = resource.RequireValidatedImage()
				}
				if err != nil {
					kind = "error"
					resource = nil
					r.agent.Page().trace.Add(trace.Error, "imageDecode", map[string]any{"url": u.String(), "error": err.Error(), "realm": r.ID})
				}
			}
			r.scheduler.Post(scheduler.Network, 0, func(ctx context.Context) error {
				if r.imageLoads[id] != current {
					return nil
				}
				current.currentSrc = u.String()
				current.resource = resource
				current.decodeDisallowed = response.DecodeDisallowed
				current.originClean = originClean
				if resource != nil {
					if r.availableImages == nil {
						r.availableImages = &availableImageCache{}
					}
					r.availableImages.put(key, availableImage{resource, originClean})
				}
				r.notifyPerformanceObservers(ctx)
				return finish(ctx, kind)
			})
		}()
		return nil
	})
}
