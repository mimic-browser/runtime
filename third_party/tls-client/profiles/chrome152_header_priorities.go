package profiles

import (
	http "github.com/bogdanfinn/fhttp"
	"github.com/bogdanfinn/fhttp/http2"
)

// Chrome's exclusive dependency tree groups active streams by request priority.
// A newly opened lower-priority stream depends on the most recent active stream
// at its own or the nearest higher priority, not unconditionally on the root.
// This state belongs to a single HTTP/2 connection, including reconnects.
type chrome152HeaderPriorities struct {
	active map[uint32]http2.RequestPriority
	last   [4]uint32
}

func newChrome152HeaderPriorities() http2.HeaderPriorities {
	return &chrome152HeaderPriorities{active: make(map[uint32]http2.RequestPriority)}
}
func (p *chrome152HeaderPriorities) OpenStream(req *http.Request, id uint32) http2.PriorityParam {
	rank := http2.RequestPriorityFromContext(req.Context())
	var dependency uint32
	for level := rank; level <= http2.PriorityVeryHigh; level++ {
		if p.last[level] != 0 {
			dependency = p.last[level]
			break
		}
	}
	p.active[id] = rank
	p.last[rank] = id
	weights := [4]uint8{146, 182, 219, 255}
	return http2.PriorityParam{Exclusive: true, StreamDep: dependency, Weight: weights[rank]}
}
func (p *chrome152HeaderPriorities) CloseStream(id uint32) {
	rank, exists := p.active[id]
	if !exists {
		return
	}
	delete(p.active, id)
	if p.last[rank] != id {
		return
	}
	p.last[rank] = 0
	for active, level := range p.active {
		if level == rank && active > p.last[rank] {
			p.last[rank] = active
		}
	}
}
