package cdp

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
)

type stylePropertyWatch struct{ name, value string }
type computedStyleTracking struct {
	properties []stylePropertyWatch
	node       int64
	nodeStyle  string
	values     map[int64]map[string]string
	dirty      []int64
	revision   uint64
}

func (s *session) trackedStyle(ctx context.Context, id int64) (map[string]string, string, error) {
	frame, local, err := s.nodeOwner(ctx, map[string]any{"nodeId": id})
	if err != nil {
		return nil, "", err
	}
	result, err := s.page.ProtocolComputedStyleInFrame(ctx, frame, local)
	if err != nil {
		return nil, "", err
	}
	values := make(map[string]string)
	for _, entry := range result["computedStyle"].([]any) {
		property := entry.(map[string]any)
		values[stringValue(property["name"])] = stringValue(property["value"])
	}
	wire, _ := json.Marshal(result)
	return values, string(wire), nil
}

func (s *session) handleCSSTracking(ctx context.Context, method string, p map[string]any) (any, bool, error) {
	switch method {
	case "CSS.trackComputedStyleUpdates", "CSS.trackComputedStyleUpdatesForNode":
		if !s.domainEnabled("CSS") {
			return nil, true, fmt.Errorf("CSS agent was not enabled")
		}
		if s.styleTracking == nil {
			s.styleTracking = &computedStyleTracking{}
		}
		tracking := s.styleTracking
		tracking.revision = 0
		if method == "CSS.trackComputedStyleUpdates" {
			tracking.properties = nil
			tracking.values = nil
			tracking.dirty = nil
			for _, entry := range p["propertiesToTrack"].([]any) {
				property := entry.(map[string]any)
				tracking.properties = append(tracking.properties, stylePropertyWatch{stringValue(property["name"]), stringValue(property["value"])})
			}
		} else {
			tracking.node = int64(intValue(p["nodeId"], 0))
			tracking.nodeStyle = ""
			if tracking.node != 0 {
				_, wire, err := s.trackedStyle(ctx, tracking.node)
				if err != nil {
					return nil, true, err
				}
				tracking.nodeStyle = wire
			}
		}
		if len(tracking.properties) == 0 && tracking.node == 0 {
			s.styleTracking = nil
		}
		return map[string]any{}, true, nil
	case "CSS.takeComputedStyleUpdates":
		if !s.domainEnabled("CSS") {
			return nil, true, fmt.Errorf("CSS agent was not enabled")
		}
		ids := []int64{}
		if tracking := s.styleTracking; tracking != nil {
			ids = append(ids, tracking.dirty...)
			tracking.dirty = nil
		}
		return map[string]any{"nodeIds": ids}, true, nil
	}
	return nil, false, nil
}

func (s *session) publishTrackedStyles() {
	tracking := s.styleTracking
	if tracking == nil {
		return
	}
	revision := s.page.InspectorRevision()
	if revision == tracking.revision {
		return
	}
	tracking.revision = revision
	if tracking.node != 0 {
		_, wire, err := s.trackedStyle(s.ctx, tracking.node)
		if err == nil && wire != tracking.nodeStyle {
			tracking.nodeStyle = wire
			s.event("CSS.computedStyleUpdated", map[string]any{"nodeId": tracking.node})
		}
	}
	if len(tracking.properties) == 0 || s.domInspector == nil {
		return
	}
	if tracking.values == nil {
		tracking.values = make(map[int64]map[string]string)
	}
	for id, node := range s.domInspector.nodes {
		canonical, ok := s.page.InspectorCanonicalNode(node.frame, node.localID)
		if !ok || canonical.Type != "element" {
			continue
		}
		values, _, err := s.trackedStyle(s.ctx, id)
		if err != nil {
			continue
		}
		prior, known := tracking.values[id]
		if known {
			for _, watch := range tracking.properties {
				if prior[watch.name] != values[watch.name] && (prior[watch.name] == watch.value || values[watch.name] == watch.value) {
					if !slices.Contains(tracking.dirty, id) {
						tracking.dirty = append(tracking.dirty, id)
					}
					break
				}
			}
		}
		selected := make(map[string]string)
		for _, watch := range tracking.properties {
			selected[watch.name] = values[watch.name]
		}
		tracking.values[id] = selected
	}
	for id := range tracking.values {
		if s.domInspector.nodes[id] == nil {
			delete(tracking.values, id)
		}
	}
}
