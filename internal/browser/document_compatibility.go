package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/network"
)

func (r *Realm) installDocumentCompatibility(host map[string]any) {
	host["disableStyleProjectionCache"] = r.transientFn(func(engine.Value, []engine.Value) (engine.Value, error) {
		// Animation time changes without a DOM mutation. Keep those observations
		// in the canonical owner rather than freezing an intermediate sample.
		r.styleProjections.disable()
		r.document.InvalidateObservations()
		return nil, nil
	})
	if r.agent.Page().ctx.browser.devPreview {
		host["installDevPreview"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
			r.previewRead = args[0]
			return nil, nil
		})
	}
	host["installComputedStyleFlatTree"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		r.computedStyleFlatRead = args[0]
		return nil, nil
	})
	host["installProtocolBoxModel"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		r.protocolBoxModelRead = args[0]
		return nil, nil
	})
	host["installProtocolScroll"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		r.protocolScroll = args[0]
		return nil, nil
	})
	// These observations consume scalar arguments synchronously. Persisting
	// their engine values would retain one set of V8 roots on every style read.
	// Cross-realm work below captures only converted Go values, not borrowed args.
	host["foreignComputedStyleFlatTree"] = r.packedFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		id := int64(numarg(args, 0))
		kind, property := strarg(args, 1), strarg(args, 2)
		root := r.document.OwnerDocumentID(id)
		if root == r.document.Root().ID && r.mainWorld == nil {
			return r.val(nil), nil
		}
		p := r.agent.Page()
		p.mu.RLock()
		var owner *Realm
		for _, candidate := range p.realmOwners {
			if candidate.mainWorld == nil && !candidate.inactive && !candidate.closed && candidate.document.SharesNodeArena(r.document) && candidate.document.Root().ID == root {
				owner = candidate
				break
			}
		}
		p.mu.RUnlock()
		if owner == nil {
			switch kind {
			case "innerText":
				// Inert/detached trees retain the local textContent fallback.
				return r.val(nil), nil
			case "value", "pseudoValue":
				return r.val(""), nil
			case "values":
				return r.val("{}"), nil
			case "rect", "layout":
				return r.val(map[string]any{"x": 0, "y": 0, "width": 0, "height": 0, "left": 0, "top": 0, "right": 0, "bottom": 0, "offsetLeft": 0, "offsetTop": 0}), nil
			default:
				return r.val(false), nil
			}
		}
		var observation any
		keyNode := id
		// A documentValues projection contains every element in the owner
		// document. Keying it by the element that happened to request the batch
		// makes each isolated world rebuild the same document-wide projection.
		if kind == "documentValues" || kind == "pseudoContent" {
			keyNode = root
		}
		key := styleProjectionKey{keyNode, kind, canonicalStyleProjectionProperty(kind, property)}
		// Child viewport geometry can depend on the parent realm's style state.
		// Until that dependency is represented, retain only top-document scalars.
		cacheable := (kind == "value" || kind == "values" || kind == "documentValues" || kind == "pseudoContent" || kind == "pseudoValue" || kind == "document" || kind == "" || kind == "box" ||
			(kind == "visibility" && !strings.Contains(property, `"contentVisibilityAuto":true`))) && len(property) <= 512 && owner == p.Top.Realm
		epoch := owner.styleProjectionEpoch(kind)
		if cacheable {
			if value, ok := owner.styleProjections.get(epoch, key); ok {
				return r.val(value), nil
			}
		}
		run := func(ctx context.Context) error {
			if deferred, ok := owner.runtime.(*deferredRuntime); ok {
				if _, err := deferred.ready(); err != nil {
					return err
				}
			}
			return owner.runOnOwner(ctx, func(ctx context.Context) error {
				if owner.computedStyleFlatRead == nil {
					return fmt.Errorf("computed style owner is unavailable")
				}
				restore := r.enterFrameDocumentEntry(owner)
				defer restore()
				values := []engine.Value{owner.val(id), owner.val(kind), owner.val(property)}
				for _, value := range values {
					defer releaseDebuggerValue(owner, value)
				}
				value, err := owner.runtime.Call(ctx, owner.computedStyleFlatRead, nil, values...)
				defer releaseDebuggerValue(owner, value)
				if err == nil {
					observation = value.Export()
				}
				return err
			})
		}
		// Borrowed getters can enter this bridge directly, without an outer
		// cross-frame call. Yield the current isolate while the owner runs.
		var err error
		if nested, ok := r.runtime.(engine.ReentrantRuntime); ok {
			err = nested.RunNested(context.Background(), run)
		} else {
			err = run(context.Background())
		}
		if err == nil && cacheable && epoch == owner.styleProjectionEpoch(kind) {
			owner.styleProjections.put(epoch, key, observation)
		}
		return r.val(observation), err
	}, "nss")
	// Resolved declarations exist only for nodes connected to a live browsing
	// document. Visibility and display:none do not make that document inactive.
	host["computedStyleAvailable"] = r.packedFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		id := int64(numarg(args, 0))
		if !r.document.IsConnected(id) {
			return r.val(false), nil
		}
		root := r.document.OwnerDocumentID(id)
		p := r.agent.Page()
		p.mu.RLock()
		defer p.mu.RUnlock()
		for _, owner := range p.realmOwners {
			if !owner.inactive && !owner.closed && owner.document.SharesNodeArena(r.document) && owner.document.Root().ID == root {
				return r.val(true), nil
			}
		}
		return r.val(false), nil
	}, "n")
	// DOM adoption changes a node's owner document, not the realm of its JS
	// wrapper. Resolve browsing documents through their canonical realm before
	// constructing a local inert-document wrapper from shared DOM node data.
	host["documentReference"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		id := int64(numarg(args, 0))
		p := r.agent.Page()
		p.mu.RLock()
		var owner *Realm
		for _, candidate := range p.realmOwners {
			if candidate.document.SharesNodeArena(r.document) && candidate.document.Root().ID == id {
				owner = candidate
				break
			}
		}
		p.mu.RUnlock()
		if owner == nil {
			return nil, nil
		}
		return r.crossFrameResult(owner, func(context.Context) (engine.Value, error) {
			return owner.runtime.Get("document"), nil
		})
	})
	host["stylesheetResource"] = r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		u, err := r.resolveDocument(strarg(args, 0))
		if err != nil {
			return nil, nil
		}
		node, exists := r.document.Get(int64(numarg(args, 2)))
		if !exists {
			return nil, nil
		}
		request := r.elementRequest(u, node.Attributes, network.Stylesheet)
		res, ok := r.retainedStylesheet(request)
		if !ok || res.URL == nil || res.Status < 200 || res.Status >= 300 {
			return nil, nil
		}
		clean, corsErr := resourceResponseOrigin(request, res)
		if corsErr != nil {
			return nil, nil
		}
		result := map[string]any{"url": res.URL.String(), "crossOrigin": !clean}
		// The CSSOM owner already keys sheet identity by the resolved URL. On
		// revalidation it needs availability and identity, not another copy of
		// the entire response through the Go/V8 bridge.
		if strarg(args, 1) != res.URL.String() {
			result["body"] = string(res.Body)
		}
		return r.val(result), nil
	})

	host["notificationPermission"] = r.fn(func(_ engine.Value, _ []engine.Value) (engine.Value, error) {
		permission := r.agent.Page().environmentView().Permissions["notifications"]
		if permission != "granted" && permission != "denied" {
			permission = "default"
		}
		return r.val(permission), nil
	})
	host["documentLastModified"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		if r.lastModified.IsZero() {
			return r.val(0), nil
		}
		return r.val(r.lastModified.UnixMilli()), nil
	})
	host["createDocumentFragment"] = r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		return r.val(nodeData(r.document.CreateDocumentFragment())), nil
	})
	host["parseInertDocument"] = r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		node, err := r.document.ParseInertDocument(strarg(args, 0), strarg(args, 1), r.documentURL().String())
		if err != nil {
			return nil, err
		}
		return r.val(nodeData(node)), nil
	})
	host["attributeNameNS"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		return r.val(r.document.AttributeNameNS(int64(numarg(args, 0)), strarg(args, 1), strarg(args, 2))), nil
	})
	host["setAttributeNS"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		return nil, r.document.SetAttributeNS(int64(numarg(args, 0)), strarg(args, 1), strarg(args, 2), strarg(args, 3))
	})
	host["elementByID"] = r.packedFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		return r.val(r.document.ElementByID(int64(numarg(args, 0)), strarg(args, 1))), nil
	}, "ns")
	host["createXMLDocument"] = r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		return r.val(nodeData(r.document.CreateXMLDocument(strarg(args, 0), strarg(args, 1)))), nil
	})
	host["createDocumentElement"] = r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		return r.val(nodeData(r.document.CreateDocumentElement(int64(numarg(args, 0)), strarg(args, 1), strarg(args, 2)))), nil
	})
	host["createHTMLDocument"] = r.transientFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		var title *string
		if len(args) > 0 {
			value := strarg(args, 0)
			title = &value
		}
		return r.val(nodeData(r.document.CreateHTMLDocument(title))), nil
	})
	host["nodeOwnerDocument"] = r.packedFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		return r.val(r.document.OwnerDocumentID(int64(numarg(args, 0)))), nil
	}, "n")
	host["adoptNodeDocument"] = r.packedFn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		r.document.AdoptNode(int64(numarg(args, 0)), int64(numarg(args, 1)))
		return nil, nil
	}, "nn")
}

func canonicalStyleProjectionProperty(kind, property string) string {
	if kind != "values" && kind != "documentValues" {
		return property
	}
	var properties []string
	if json.Unmarshal([]byte(property), &properties) != nil {
		return property
	}
	sort.Strings(properties)
	encoded, err := json.Marshal(properties)
	if err != nil {
		return property
	}
	return string(encoded)
}
