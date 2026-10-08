package cdp

import (
	"context"
	"fmt"
)

// The read-only CSS slice consumes canonical browser style state. Enabling the
// domain does not claim inspector stylesheet editing or notification support;
// those boundaries are recorded in protocol_support.json.
func (s *session) handleCSS(ctx context.Context, method string, params map[string]any) (any, bool, error) {
	switch method {
	case "CSS.enable":
		// Frozen Chromium 152 InspectorCSSAgent::enable requires the DOM agent.
		if !s.domainEnabled("DOM") {
			return nil, true, fmt.Errorf("DOM agent needs to be enabled first.")
		}
		s.setDomain("CSS", true)
		return map[string]any{}, true, nil
	case "CSS.disable":
		s.setDomain("CSS", false)
		return map[string]any{}, true, nil
	case "CSS.getComputedStyleForNode":
		if !s.domainEnabled("CSS") {
			return nil, true, fmt.Errorf("CSS agent was not enabled")
		}
		id, err := s.nodeID(ctx, params)
		if err != nil {
			return nil, true, err
		}
		result, err := s.page.ProtocolComputedStyle(ctx, id)
		return result, true, err
	default:
		return nil, false, nil
	}
}
