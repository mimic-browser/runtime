package cdp

import (
	"context"
	"fmt"
	"strings"

	"github.com/moreveal/mimic/internal/browser"
)

func (s *session) handleDOMSearch(ctx context.Context, method string, p map[string]any) (any, bool, error) {
	switch method {
	case "DOM.discardSearchResults":
		delete(s.domSearches, stringValue(p["searchId"]))
		if len(s.domSearches) == 0 {
			s.domSearches = nil
		}
		return map[string]any{}, true, nil
	case "DOM.getSearchResults":
		ids, ok := s.domSearches[stringValue(p["searchId"])]
		if !ok {
			return nil, true, fmt.Errorf("No search session with given id found")
		}
		from, to := intValue(p["fromIndex"], 0), intValue(p["toIndex"], 0)
		if from < 0 || to <= from || to > len(ids) {
			return nil, true, fmt.Errorf("Invalid search result range")
		}
		for _, id := range ids[from:to] {
			owner, local, err := s.nodeOwner(ctx, map[string]any{"nodeId": id})
			if err != nil {
				return nil, true, err
			}
			d, _ := s.page.InspectorDocument(owner)
			s.emitDOMAncestors(d, local)
		}
		return map[string]any{"nodeIds": ids[from:to]}, true, nil
	case "DOM.performSearch":
		if !s.domainEnabled("DOM") {
			return nil, true, fmt.Errorf("DOM agent was not enabled")
		}
		query := strings.TrimSpace(stringValue(p["query"]))
		if query == "" {
			return nil, true, fmt.Errorf("Search query is empty")
		}
		if strings.HasPrefix(query, "//") {
			return nil, true, fmt.Errorf("XPath inspector searches are unsupported")
		}
		if len(s.domSearches) >= 8 {
			return nil, true, fmt.Errorf("Discard existing search results before starting another search")
		}
		ids, err := s.searchInspectorDOM(ctx, query)
		if err != nil {
			return nil, true, err
		}
		s.domSearchSequence++
		id := fmt.Sprintf("search-%d", s.domSearchSequence)
		if s.domSearches == nil {
			s.domSearches = map[string][]int64{}
		}
		s.domSearches[id] = ids
		// Search handles need document-lifecycle invalidation even when the
		// frontend has not requested a DOM tree yet.
		if s.domInspector == nil {
			d, _ := s.page.Document()
			s.domInspector = &domInspector{document: d, nodes: make(map[int64]*inspectorNode)}
			s.ensureInspectorTurn()
		}
		return map[string]any{"searchId": id, "resultCount": len(ids)}, true, nil
	}
	return nil, false, nil
}

// Search walks canonical document/shadow trees and shares the realm selector
// engine. Only bounded result handles are retained after an explicit request.
func (s *session) searchInspectorDOM(ctx context.Context, query string) ([]int64, error) {
	ids := []int64{}
	seen := map[int64]bool{}
	needle := strings.ToLower(query)
	add := func(frame *browser.Frame, local int64) error {
		id := s.page.InspectorNodeID(frame, local)
		if !seen[id] {
			if len(ids) >= 100000 {
				return fmt.Errorf("Inspector search exceeds the result budget")
			}
			seen[id] = true
			ids = append(ids, id)
		}
		return nil
	}
	var visit func(*browser.Frame) error
	visit = func(frame *browser.Frame) error {
		d, ok := s.page.InspectorDocument(frame)
		if !ok {
			return nil
		}
		shadows, err := frame.Realm.ShadowSnapshots(ctx)
		if err != nil {
			return err
		}
		roots := []int64{d.Root().ID}
		for _, root := range shadows {
			roots = append(roots, root.RootID)
		}
		for _, root := range roots {
			matched, err := s.page.QueryDOMInFrame(ctx, frame, root, query, true)
			if err == nil {
				for _, id := range matched {
					if err := add(frame, id); err != nil {
						return err
					}
				}
			} else if !strings.Contains(err.Error(), "SyntaxError") {
				return err
			}
			var walk func(int64) error
			walk = func(id int64) error {
				node, ok := s.page.InspectorCanonicalNode(frame, id)
				if !ok {
					return nil
				}
				match := strings.Contains(strings.ToLower(node.Text), needle) || strings.Contains(strings.ToLower(node.TagName), needle)
				for name, value := range node.Attributes {
					match = match || strings.Contains(strings.ToLower(name), needle) || strings.Contains(strings.ToLower(value), needle)
				}
				if match {
					if err := add(frame, id); err != nil {
						return err
					}
				}
				for _, child := range node.Children {
					if err := walk(child); err != nil {
						return err
					}
				}
				return nil
			}
			if err := walk(root); err != nil {
				return err
			}
		}
		for _, child := range frame.Children() {
			if err := visit(child); err != nil {
				return err
			}
		}
		return nil
	}
	err := visit(s.page.Top)
	return ids, err
}
