package cdp

import (
	"context"
	"fmt"
	"reflect"
	"strings"

	"github.com/moreveal/mimic/internal/browser"
	"github.com/moreveal/mimic/internal/trace"
)

// Inspector observations and edits consume the canonical realm CSSOM.
func (s *session) handleCSS(ctx context.Context, method string, params map[string]any) (any, bool, error) {
	if value, handled, err := s.handleCSSTracking(ctx, method, params); handled {
		return value, true, err
	}
	switch method {
	case "CSS.enable":
		// Frozen Chromium 152 InspectorCSSAgent::enable requires the DOM agent.
		if !s.domainEnabled("DOM") {
			return nil, true, fmt.Errorf("DOM agent needs to be enabled first.")
		}
		s.setDomain("CSS", true)
		s.ensureInspectorTurn()
		s.publishStyleSheets()
		return map[string]any{}, true, nil
	case "CSS.disable":
		s.setDomain("CSS", false)
		s.styleTracking = nil
		s.styleSheets = nil
		s.releaseInspectorTurn()
		s.releaseCSSStorage()
		return map[string]any{}, true, nil
	case "CSS.getComputedStyleForNode":
		if !s.domainEnabled("CSS") {
			return nil, true, fmt.Errorf("CSS agent was not enabled")
		}
		frame, id, err := s.nodeOwner(ctx, params)
		if err != nil {
			return nil, true, err
		}
		result, err := s.page.ProtocolComputedStyleInFrame(ctx, frame, id)
		return result, true, err
	case "CSS.getMatchedStylesForNode", "CSS.getInlineStylesForNode":
		if !s.domainEnabled("CSS") {
			return nil, true, fmt.Errorf("CSS agent was not enabled")
		}
		frame, id, err := s.nodeOwner(ctx, params)
		if err != nil {
			return nil, true, err
		}
		if frame == nil {
			return nil, true, fmt.Errorf("Could not find node with given id")
		}
		operation := "matched"
		if method == "CSS.getInlineStylesForNode" {
			operation = "inline"
		}
		result, err := s.page.ProtocolInspectorCSS(ctx, frame, map[string]any{"method": operation, "nodeId": id})
		if err == nil {
			qualifyStyleIDs(result, frame.RealmID()+":")
		}
		return result, true, err
	case "CSS.getStyleSheetText", "CSS.setStyleSheetText", "CSS.setStyleTexts", "CSS.setRuleSelector":
		if !s.domainEnabled("CSS") {
			return nil, true, fmt.Errorf("CSS agent was not enabled")
		}
		if method == "CSS.setStyleTexts" {
			styles := []any{}
			for _, edit := range params["edits"].([]any) {
				result, err := s.inspectorSheetRequest(ctx, edit.(map[string]any), "editStyle")
				if err != nil {
					return nil, true, err
				}
				styles = append(styles, result["style"])
			}
			return map[string]any{"styles": styles}, true, nil
		}
		operation := "text"
		if method != "CSS.getStyleSheetText" {
			operation = "setText"
		}
		request := params
		if method == "CSS.setRuleSelector" {
			operation = "editSelector"
			request = map[string]any{"styleSheetId": params["styleSheetId"], "range": params["range"], "text": params["selector"]}
		}
		result, err := s.inspectorSheetRequest(ctx, request, operation)
		if err == nil && method == "CSS.setRuleSelector" {
			result = map[string]any{"selectorList": map[string]any{"text": params["selector"], "selectors": []any{map[string]any{"text": params["selector"]}}}}
		}
		return result, true, err
	default:
		return nil, false, nil
	}
}

type inspectorSheet struct {
	text    string
	frame   *browser.Frame
	localID string
	header  map[string]any
}

func (s *session) publishStyleSheets() {
	revision := s.page.InspectorRevision()
	if s.styleSheets != nil && s.styleSheetRevision == revision {
		return
	}
	s.styleSheetRevision = revision
	s.page.PruneInspectorNodeIDs()
	if s.styleSheets == nil {
		s.styleSheets = make(map[string]inspectorSheet)
	}
	seen := make(map[string]bool)
	var visit func(*browser.Frame)
	visit = func(frame *browser.Frame) {
		result, err := s.page.ProtocolInspectorCSS(s.ctx, frame, map[string]any{"method": "headers"})
		if err != nil {
			s.page.Trace().Add(trace.Error, "inspectorCSS", map[string]any{"frameId": frame.ID, "error": err.Error()})
			// A failed observation is not evidence that a sheet was removed.
			for id, sheet := range s.styleSheets {
				if sheet.frame == frame {
					seen[id] = true
				}
			}
		}
		if err == nil {
			for _, value := range result["headers"].([]any) {
				header := value.(map[string]any)
				text := stringValue(header["inspectorText"])
				delete(header, "inspectorText")
				localID := stringValue(header["styleSheetId"])
				id := frame.RealmID() + ":" + localID
				header["styleSheetId"], header["frameId"] = id, frame.ID
				if owner := int64(coordinateValue(header["ownerNode"])); owner != 0 {
					header["ownerNode"] = s.page.InspectorNodeID(frame, owner)
				}
				seen[id] = true
				prior, exists := s.styleSheets[id]
				s.styleSheets[id] = inspectorSheet{frame: frame, localID: localID, header: header, text: text}
				if !exists {
					s.event("CSS.styleSheetAdded", map[string]any{"header": header})
				} else if prior.text != text || !reflect.DeepEqual(prior.header, header) {
					s.event("CSS.styleSheetChanged", map[string]any{"styleSheetId": id})
				}
			}
		}
		for _, child := range frame.Children() {
			visit(child)
		}
	}
	visit(s.page.Top)
	for id := range s.styleSheets {
		if !seen[id] {
			delete(s.styleSheets, id)
			s.event("CSS.styleSheetRemoved", map[string]any{"styleSheetId": id})
		}
	}
}

func qualifyStyleIDs(value any, prefix string) {
	switch v := value.(type) {
	case map[string]any:
		if id, ok := v["styleSheetId"].(string); ok {
			v["styleSheetId"] = prefix + id
		}
		for key, child := range v {
			if key != "styleSheetId" {
				qualifyStyleIDs(child, prefix)
			}
		}
	case []any:
		for _, child := range v {
			qualifyStyleIDs(child, prefix)
		}
	}
}

func (s *session) inspectorSheetRequest(ctx context.Context, p map[string]any, method string) (map[string]any, error) {
	id := stringValue(p["styleSheetId"])
	var frame *browser.Frame
	local := id
	if entry, ok := s.styleSheets[id]; ok {
		frame, local = entry.frame, entry.localID
	} else {
		var visit func(*browser.Frame)
		visit = func(f *browser.Frame) {
			if strings.HasPrefix(id, f.RealmID()+":inline-") {
				frame = f
				local = strings.TrimPrefix(id, f.RealmID()+":")
			}
			for _, c := range f.Children() {
				visit(c)
			}
		}
		visit(s.page.Top)
	}
	if frame == nil {
		return nil, fmt.Errorf("No stylesheet with given id found")
	}
	request := map[string]any{"method": method, "styleSheetId": local}
	for _, key := range []string{"text", "range"} {
		if value, ok := p[key]; ok {
			request[key] = value
		}
	}
	result, err := s.page.ProtocolInspectorCSS(ctx, frame, request)
	if err == nil {
		qualifyStyleIDs(result, frame.RealmID()+":")
	}
	return result, err
}

func (s *session) releaseCSSStorage() {
	for _, c := range s.server.clientSnapshot() {
		for _, other := range c.snapshot() {
			if other != s && other.page == s.page && other.ctx.Err() == nil && other.domainEnabled("CSS") {
				return
			}
		}
	}
	var visit func(*browser.Frame)
	visit = func(frame *browser.Frame) {
		_, _ = s.page.ProtocolInspectorCSS(context.Background(), frame, map[string]any{"method": "release"})
		for _, child := range frame.Children() {
			visit(child)
		}
	}
	visit(s.page.Top)
}
