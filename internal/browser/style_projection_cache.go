package browser

import "sync"

// A projection contains only immutable scalar readbacks, never JS wrappers or
// another DOM/style model. Each owner realm has its own bounded cache. All
// observable inputs must match before a foreign caller can avoid actor entry.
type styleProjectionEpoch struct {
	document, resources uint64
	target              int64
	width, height       int
	colorScheme         string
	reducedMotion       bool
	deviceScale         float64
}

type styleProjectionKey struct {
	node           int64
	kind, property string
}

type styleProjectionCache struct {
	mu      sync.Mutex
	epoch   styleProjectionEpoch
	values  map[styleProjectionKey]any
	bytes   int
	dynamic bool
}

func (r *Realm) styleDocumentRevision() uint64 {
	return r.document.ObservationRevision()
}

func (r *Realm) styleProjectionEpoch(kind string) styleProjectionEpoch {
	e := r.agent.Page().environmentView()
	resources := r.resourceRevision.Load()
	// Image completion changes intrinsic geometry, not computed declarations,
	// flat-tree availability, or ordinary CSS visibility. Keep scalar style
	// projections hot while images load; box projections retain the complete
	// resource epoch because their dimensions can genuinely change.
	if kind == "value" || kind == "values" || kind == "documentValues" || kind == "pseudoContent" || kind == "document" || kind == "" || kind == "visibility" {
		resources = r.styleResourceRevision.Load()
	}
	return styleProjectionEpoch{r.styleDocumentRevision(), resources, r.selectorTargetID,
		e.Window.ViewportWidth, e.Window.ViewportHeight, e.Preferences.ColorScheme, e.Preferences.ReducedMotion, e.Display.DeviceScaleFactor}
}

func (c *styleProjectionCache) get(epoch styleProjectionEpoch, key styleProjectionKey) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dynamic || c.epoch != epoch {
		return nil, false
	}
	value, ok := c.values[key]
	return value, ok
}

func (c *styleProjectionCache) put(epoch styleProjectionEpoch, key styleProjectionKey, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dynamic {
		return
	}
	if c.epoch != epoch {
		c.values = nil
		c.bytes = 0
		c.epoch = epoch
	}
	if c.values == nil {
		c.values = make(map[styleProjectionKey]any)
	}
	// Stop admission rather than flushing a hot cache when an enormous document
	// exceeds the budget. A genuine input mutation starts a new observation.
	scalarBytes := 0
	switch scalar := value.(type) {
	case string:
		scalarBytes = len(scalar)
	case bool:
		scalarBytes = 1
	default:
		return
	}
	size := scalarBytes + len(key.property) + len(key.kind) + 32
	if _, exists := c.values[key]; exists {
		return
	}
	if len(c.values) < 32768 && c.bytes+size <= 8*1024*1024 {
		c.values[key] = value
		c.bytes += size
	}
}

func (c *styleProjectionCache) disable() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.dynamic = true
	c.values = nil
	c.bytes = 0
}

func (c *styleProjectionCache) isDynamic() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.dynamic
}
