package network

import "net/http"

type resourceRequestKey struct{}

// TransportResourceRequest exposes browser provenance without adding wire headers.
// It is available only to in-process transports; mutable header/body data must
// not be modified by the observer.
func TransportResourceRequest(r *http.Request) Request {
	request, _ := r.Context().Value(resourceRequestKey{}).(Request)
	return request
}
