package cdp

import "context"

func (r *blinkRenderer) applyHighlight(ctx context.Context, highlight *presentationHighlight) error {
	if _, err := r.call(ctx, "DOM.enable", nil); err != nil {
		return err
	}
	if _, err := r.call(ctx, "Overlay.enable", nil); err != nil {
		return err
	}
	if highlight == nil {
		_, err := r.call(ctx, "Overlay.hideHighlight", nil)
		return err
	}
	result, err := r.call(ctx, "DOM.getDocument", map[string]any{"depth": -1, "pierce": true})
	if err != nil {
		return err
	}
	backend := presentationBackend(result["root"], highlight.key)
	// A removed node drops out of the current projection. The next document
	// rebuild has already removed its old highlight.
	if backend == nil {
		return nil
	}
	_, err = r.call(ctx, "Overlay.highlightNode", map[string]any{"backendNodeId": backend, "highlightConfig": highlight.config})
	return err
}

func presentationBackend(value any, key string) any {
	switch value := value.(type) {
	case map[string]any:
		if attrs, ok := value["attributes"].([]any); ok {
			for i := 0; i+1 < len(attrs); i += 2 {
				if attrs[i] == "data-mimic-preview-node" && attrs[i+1] == key {
					return value["backendNodeId"]
				}
			}
		}
		for _, name := range []string{"children", "shadowRoots", "contentDocument", "templateContent"} {
			if found := presentationBackend(value[name], key); found != nil {
				return found
			}
		}
	case []any:
		for _, child := range value {
			if found := presentationBackend(child, key); found != nil {
				return found
			}
		}
	}
	return nil
}
