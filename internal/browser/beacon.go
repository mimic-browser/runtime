package browser

import (
	"net/http"
	"sync"

	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/network"
)

// The Fetch group belongs to the initiating document. Its accepted uploads
// outlive that document, but never the owning Context or its shared transport.
type keepaliveBudget struct {
	mu    sync.Mutex
	bytes int
}

func (c *Context) beginKeepalive(group *keepaliveBudget, size int) (func(), bool) {
	c.keepaliveMu.Lock()
	defer c.keepaliveMu.Unlock()
	if c.lifetime.Err() != nil {
		return nil, false
	}
	group.mu.Lock()
	defer group.mu.Unlock()
	if size > 65536-group.bytes {
		return nil, false
	}
	group.bytes += size
	c.keepaliveWG.Add(1)
	c.activeKeepalives.Add(1)
	return func() {
		group.mu.Lock()
		group.bytes -= size
		group.mu.Unlock()
		c.activeKeepalives.Add(-1)
		c.keepaliveWG.Done()
	}, true
}

func (r *Realm) hostSendBeacon(_ engine.Value, args []engine.Value) (engine.Value, error) {
	p := r.agent.Page()
	u, err := r.resolveDocument(strarg(args, 0))
	if err != nil || u.Scheme != "http" && u.Scheme != "https" || p.closed.Load() {
		return r.val(false), nil
	}
	body := byteSlice(arg(args, 1))
	release, accepted := p.ctx.beginKeepalive(&r.keepaliveBudget, len(body))
	if !accepted {
		return r.val(false), nil
	}
	headers := make(http.Header)
	mode := "no-cors"
	if contentType := strarg(args, 2); contentType != "" && contentType != "null" && contentType != "undefined" {
		headers.Set("Content-Type", contentType)
		if !network.CORSSafelistedRequestHeader("content-type", contentType) {
			mode = "cors"
		}
	}
	request := network.Request{ContextID: r.agent.ContextID(), URL: u, Referrer: r.documentURL(), SourceURL: r.documentURL(),
		Method: "POST", Body: body, Headers: headers, Initiator: network.Fetch, Credentials: "include", Mode: mode,
		Mechanism: "api", Kind: "beacon", PerformanceInitiatorType: "beacon",
		ContentPolicyDirective: "connect-src", ContentPolicy: r.contentPolicy(), ContentPolicyURL: r.documentURL()}
	r.applyClientHints(&request)
	request = r.withResourceTiming(request)
	// No realm/runtime handle enters the transport goroutine. Navigation and
	// Page close must not wait on it; Context cancellation joins it before pool teardown.
	go func() {
		defer release()
		_, _ = p.loader.Load(p.ctx.lifetime, request) // Loader retains failure diagnostics.
		if p.closed.Load() {
			p.loader.CloseResponseBodies()
		}
	}()
	return r.val(true), nil
}
