package browser

import (
	"encoding/base64"
	"github.com/moreveal/mimic/internal/engine"
)

func (r *Realm) installImageResources(host map[string]any) {
	host["compressCanvasScanline"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		encoded, err := compressCanvasScanline(strarg(args, 0))
		if err != nil {
			return nil, err
		}
		return r.val(encoded), nil
	})
	host["imageInfo"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		id := int64(numarg(args, 0))
		node, ok := r.document.Get(id)
		if !ok {
			return nil, nil
		}
		src, present := node.Attributes["src"]
		state := r.imageLoads[id]
		width, height := 0, 0
		complete := !present || src == ""
		current := ""
		if present && src != "" && state != nil {
			complete = state.complete
			current = state.currentSrc
			if state.resource != nil {
				metadata, _ := state.resource.RequireMetadata()
				width, height = metadata.Width, metadata.Height
			}
		}
		return r.val(map[string]any{"decoded": state != nil && state.resource != nil && !state.decodeDisallowed && present && src != "", "complete": complete, "width": width, "height": height, "currentSrc": current}), nil
	})
	host["imagePixels"] = r.fn(func(_ engine.Value, args []engine.Value) (engine.Value, error) {
		state := r.imageLoads[int64(numarg(args, 0))]
		if state == nil || state.resource == nil || state.decodeDisallowed {
			return nil, nil
		}
		decoded, err := state.resource.RequireDecodedImage()
		if err != nil {
			return nil, err
		}
		return r.val(map[string]any{"width": decoded.Width, "height": decoded.Height, "vector": decoded.Vector, "unavailable": decoded.PixelsUnavailable, "originClean": state.originClean, "pixels": base64.StdEncoding.EncodeToString(decoded.Pixels)}), nil
	})
}
