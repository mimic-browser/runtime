package http2

import (
	"context"
	http "github.com/bogdanfinn/fhttp"
)

// HeaderPriorities belongs to one ClientConn. Its methods execute under that
// connection's mutex and must not invoke request bodies or other author code.
type HeaderPriorities interface {
	OpenStream(*http.Request, uint32) PriorityParam
	CloseStream(uint32)
}

type RequestPriority uint8

const (
	PriorityLow RequestPriority = iota
	PriorityMedium
	PriorityHigh
	PriorityVeryHigh
)

type requestPriorityKey struct{}

// WithRequestPriority carries browser-owned scheduling metadata, independently
// of an author-supplied HTTP Priority header.
func WithRequestPriority(ctx context.Context, priority RequestPriority) context.Context {
	return context.WithValue(ctx, requestPriorityKey{}, priority)
}
func RequestPriorityFromContext(ctx context.Context) RequestPriority {
	priority, ok := ctx.Value(requestPriorityKey{}).(RequestPriority)
	if !ok || priority > PriorityVeryHigh {
		return PriorityVeryHigh
	}
	return priority
}
