package browser

import (
	"context"
	"strings"

	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/scheduler"
)

// mediaLoad models resource selection and transport without decoding audio or
// video frames. Superseded requests cannot update the element that replaced
// their source.
type mediaLoad struct {
	cancel         context.CancelFunc
	currentSrc     string
	source         *mediaSource
	owner          *Realm
	nodeID         int64
	initialization *mp4Initialization
	readyState     int
}

func (load *mediaLoad) queueEvent(name string) {
	owner := load.owner
	owner.scheduler.Post(scheduler.DOM, 0, func(ctx context.Context) error {
		if owner.inactive || owner.closed || owner.mediaLoads[load.nodeID] != load {
			return nil
		}
		return owner.dispatchResourceEvent(ctx, load.nodeID, name)
	})
}

func (load *mediaLoad) stop() {
	if load.cancel != nil {
		load.cancel()
	}
	if load.source != nil && load.source.detach(load) {
		load.source.queueEvent("activeSourceBuffers", "removesourcebuffer")
		load.source.queueEvent("sourceBuffers", "removesourcebuffer")
		load.source.queueEvent("source", "sourceclose")
	}
}

func (r *Realm) stopMediaLoads() {
	for _, load := range r.mediaLoads {
		load.stop()
	}
}

func (r *Realm) updateMedia(id int64, explicit bool) {
	if r.mediaLoads == nil {
		r.mediaLoads = map[int64]*mediaLoad{}
	}
	current := &mediaLoad{owner: r, nodeID: id}
	if previous := r.mediaLoads[id]; previous != nil {
		// Selection clears currentSrc in its task, after queued detachment
		// events, rather than at the synchronous load() reset boundary.
		current.currentSrc = previous.currentSrc
		previous.stop()
	}
	r.mediaLoads[id] = current
	r.scheduler.Post(scheduler.Network, 0, func(ctx context.Context) error {
		if r.mediaLoads[id] != current {
			return nil
		}
		node, ok := r.document.Get(id)
		if !ok || (node.TagName != "AUDIO" && node.TagName != "VIDEO") {
			return nil
		}
		src := strings.TrimSpace(node.Attributes["src"])
		if !explicit && strings.EqualFold(strings.TrimSpace(node.Attributes["preload"]), "none") {
			if _, autoplay := node.Attributes["autoplay"]; !autoplay {
				return nil
			}
		}
		if _, hasSrc := node.Attributes["src"]; !hasSrc {
			for _, childID := range node.Children {
				child, exists := r.document.Get(childID)
				if !exists || child.TagName != "SOURCE" {
					continue
				}
				typeName := strings.TrimSpace(child.Attributes["type"])
				if typeName != "" && r.agent.Page().environmentView().Capabilities.Media.Formats.Support(typeName).Play == "" {
					continue
				}
				if candidate := strings.TrimSpace(child.Attributes["src"]); candidate != "" {
					src = candidate
					break
				}
			}
		}
		if src == "" {
			current.currentSrc = ""
			return r.dispatchResourceEvent(ctx, id, "error")
		}
		u, err := r.resolveDocument(src)
		if err != nil {
			return r.dispatchResourceEvent(ctx, id, "error")
		}
		current.currentSrc = u.String()
		request := r.elementRequest(u, node.Attributes, network.Other)
		request.Kind = "media"
		if resource := r.agent.Page().ctx.network.MediaSourceURL(u.String()); resource != nil {
			source, ok := resource.(*mediaSource)
			if !ok {
				return r.dispatchResourceEvent(ctx, id, "error")
			}
			if err := r.agent.Page().loader.CheckLocalResourcePolicy(request); err != nil {
				return r.dispatchResourceEvent(ctx, id, "error")
			}
			if !source.attach(current) {
				return r.dispatchResourceEvent(ctx, id, "error")
			}
			current.source = source
			source.queueEvent("source", "sourceopen")
			return nil
		}
		loadContext, cancel := context.WithCancel(r.resourceContext)
		current.cancel = cancel
		r.resourceWG.Add(1)
		go func() {
			defer r.resourceWG.Done()
			defer cancel()
			response, loadErr := r.loadResource(loadContext, request)
			if loadContext.Err() != nil || r.resourceContext.Err() != nil {
				return
			}
			r.scheduler.Post(scheduler.Network, 0, func(ctx context.Context) error {
				if r.mediaLoads[id] != current {
					return nil
				}
				r.notifyPerformanceObservers(ctx)
				if loadErr != nil || response.Status < 200 || response.Status >= 300 {
					return r.dispatchResourceEvent(ctx, id, "error")
				}
				if err := r.dispatchResourceEvent(ctx, id, "loadedmetadata"); err != nil {
					return err
				}
				return r.dispatchResourceEvent(ctx, id, "canplay")
			})
		}()
		return nil
	})
}
