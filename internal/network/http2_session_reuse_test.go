package network

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	tlsclient "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"golang.org/x/net/http2"
)

func TestHTTP2StreamLimitReusesSessionInsteadOfDialing(t *testing.T) {
	var connections atomic.Int32
	entered, release, fastEntered := make(chan struct{}), make(chan struct{}), make(chan struct{}, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hold" {
			w.WriteHeader(200)
			w.(http.Flusher).Flush()
			close(entered)
			<-release
			io.WriteString(w, "held")
			return
		}
		fastEntered <- struct{}{}
		io.WriteString(w, "ok")
	}))
	server.TLS = &tls.Config{GetConfigForClient: func(*tls.ClientHelloInfo) (*tls.Config, error) { connections.Add(1); return nil, nil }}
	if err := http2.ConfigureServer(server.Config, &http2.Server{MaxConcurrentStreams: 1}); err != nil {
		t.Fatal(err)
	}
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	transport, err := NewTLSClientTransport(profiles.Chrome_152, tlsclient.WithInsecureSkipVerify(), tlsclient.WithDisableHttp3())
	if err != nil {
		t.Fatal(err)
	}
	defer transport.CloseIdleConnections()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/hold", nil)
	held, err := transport.RoundTrip(req)
	if err != nil {
		close(release)
		t.Fatal(err)
	}
	defer held.Body.Close()
	<-entered
	// A canceled waiter must leave the established session usable, without
	// opening a second connection or consuming the peer's only stream slot.
	queuedContext, cancelQueued := context.WithCancel(ctx)
	queued := make(chan error, 1)
	go func() {
		request, _ := http.NewRequestWithContext(queuedContext, "GET", server.URL+"/canceled", nil)
		response, err := transport.RoundTrip(request)
		if response != nil {
			response.Body.Close()
		}
		queued <- err
	}()
	time.Sleep(50 * time.Millisecond)
	cancelQueued()
	select {
	case err := <-queued:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("queued cancellation returned %v", err)
		}
	case <-time.After(time.Second):
		t.Error("queued cancellation waited for the occupied stream")
	}
	next := make(chan error, 1)
	go func() {
		req, _ := http.NewRequestWithContext(ctx, "GET", server.URL+"/next", nil)
		response, err := transport.RoundTrip(req)
		if err == nil {
			_, err = io.Copy(io.Discard, response.Body)
			response.Body.Close()
		}
		next <- err
	}()
	select {
	case <-fastEntered:
		t.Error("request bypassed the peer's stream limit by opening another TLS session")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	io.Copy(io.Discard, held.Body)
	if err := <-next; err != nil {
		t.Fatal(err)
	}
	if got := connections.Load(); got != 1 {
		t.Errorf("TLS connections=%d, want one reused HTTP/2 session", got)
	}
}
