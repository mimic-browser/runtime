package cdp

import "fmt"

// Inspection observes canonical runtime state without a graphics consumer.
func (s *session) handlePresentationBoundary(method string) (any, bool, error) {
	switch method {
	case "Page.startScreencast", "DOM.getNodeForLocation":
		return nil, true, fmt.Errorf("Visual page presentation is unsupported; use DOM, CSS, Network and Runtime inspection")
	case "Page.stopScreencast", "Page.screencastFrameAck":
		return map[string]any{}, true, nil
	}
	return nil, false, nil
}
