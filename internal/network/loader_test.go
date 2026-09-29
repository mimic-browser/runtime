package network

import (
	"bytes"
	"context"
	"errors"
	"github.com/moreveal/mimic/internal/state"
	"github.com/moreveal/mimic/internal/trace"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

type blockingBody struct {
	closed chan struct{}
}

func (b *blockingBody) Read([]byte) (int, error) {
	<-b.closed
	return 0, io.ErrClosedPipe
}

func (b *blockingBody) Close() error {
	select {
	case <-b.closed:
	default:
		close(b.closed)
	}
	return nil
}

type blockingBodyTransport struct {
	body *blockingBody
}

func (t blockingBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: t.body, Request: req}, nil
}

func TestLoadCancellationInterruptsResponseBody(t *testing.T) {
	loader := NewLoader(testEnvironment, NewCookieStore(), trace.New())
	body := &blockingBody{closed: make(chan struct{})}
	loader.SetTransport(blockingBodyTransport{body: body})
	resource, _ := url.Parse("https://example.test/stream")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := loader.Load(ctx, Request{URL: resource, Initiator: Other})
		done <- err
	}()
	time.Sleep(10 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("load error = %v, want context cancellation", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled response body remained blocked")
	}
}

func testEnvironment() state.Environment {
	return state.ChromeDesktopWindows(state.Product{Name: "Chrome", Version: "152.0.0.0", FullVersion: "152.0.7977.82"})
}

type countingTransport struct{ calls int }

func (t *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.calls++
	return &http.Response{StatusCode: 200, Header: http.Header{"Cache-Control": {"max-age=60"}}, Body: io.NopCloser(bytes.NewBufferString("cached")), Request: req}, nil
}

type statusTransport struct{ status int }

func (t statusTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: t.status, Header: make(http.Header), Body: io.NopCloser(bytes.NewBufferString("error document")), Request: req}, nil
}

type recordingTransport struct {
	requests        []*http.Request
	responseHeaders http.Header
}

func (t *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.requests = append(t.requests, req.Clone(req.Context()))
	headers := t.responseHeaders.Clone()
	if headers == nil {
		headers = make(http.Header)
	}
	return &http.Response{StatusCode: 200, Header: headers, Body: io.NopCloser(bytes.NewReader(nil)), Request: req}, nil
}

type criticalCHTransport struct {
	requests []*http.Request
}

func (t *criticalCHTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.requests = append(t.requests, req.Clone(req.Context()))
	headers := make(http.Header)
	if len(t.requests) == 1 {
		// List-valued response headers may be split across field lines.
		// A leading unsupported hint must not hide later accepted hints.
		headers["Accept-Ch"] = []string{"Sec-CH-Prefers-Color-Scheme", "Sec-CH-UA-Arch", "Sec-CH-UA-Bitness"}
		headers.Set("Critical-CH", "Sec-CH-UA-Arch")
	}
	headers.Set("Cache-Control", "no-store")
	return &http.Response{StatusCode: 200, Proto: "HTTP/2.0", Header: headers, Body: io.NopCloser(bytes.NewReader(nil)), Request: req}, nil
}

func TestTopLevelCriticalClientHintsRestartUsesCanonicalSession(t *testing.T) {
	session := NewSessionState()
	recorder := trace.New()
	recorder.Start()
	loader := NewLoaderWithSession(testEnvironment, NewCookieStore(), session, recorder)
	transport := &criticalCHTransport{}
	loader.SetTransport(transport)
	u, _ := url.Parse("https://example.test/")

	response, err := loader.Load(context.Background(), Request{URL: u, Initiator: Navigation})
	if err != nil {
		t.Fatal(err)
	}
	if len(transport.requests) != 2 {
		t.Fatalf("transport calls = %d, want one initial navigation and one internal restart", len(transport.requests))
	}
	if got := transport.requests[0].Header.Get("Sec-CH-UA-Arch"); got != "" {
		t.Fatalf("initial navigation unexpectedly sent an unaccepted high-entropy hint: %q", got)
	}
	if got := transport.requests[1].Header.Get("Sec-CH-UA-Arch"); got != `"x86"` {
		t.Fatalf("restarted navigation Sec-CH-UA-Arch = %q, want x86", got)
	}
	if got := session.ClientHints(u); !got["sec-ch-ua-arch"] || !got["sec-ch-ua-bitness"] {
		t.Fatalf("canonical client-hints state was not updated: %#v", got)
	}
	if response.BrowserVisibleTiming.Phases["responseComplete"] < response.TransportTiming.Phases["responseComplete"] {
		t.Fatalf("browser-visible navigation timing lost the restart interval: browser=%#v transport=%#v", response.BrowserVisibleTiming, response.TransportTiming)
	}
	restarts := 0
	for _, event := range recorder.Events() {
		if event.Kind == trace.Network && event.Name == "criticalClientHintsRestart" {
			restarts++
		}
	}
	if restarts != 1 {
		t.Fatalf("restart trace events = %d, want 1", restarts)
	}
}

func TestBrowserRequestHeadersDeriveFromResourceType(t *testing.T) {
	l := NewLoader(testEnvironment, NewCookieStore(), trace.New())
	transport := &recordingTransport{}
	l.SetTransport(transport)
	document, _ := url.Parse("https://example.test/")
	crossScript, _ := url.Parse("https://cdn.example/script.js")
	favicon, _ := url.Parse("https://example.test/favicon.ico")
	if _, err := l.Load(context.Background(), Request{URL: document, Initiator: Navigation}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Load(context.Background(), Request{URL: crossScript, Referrer: document, Initiator: Script}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Load(context.Background(), Request{URL: crossScript, Referrer: document, Initiator: Script, Mode: "cors"}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Load(context.Background(), Request{URL: favicon, Referrer: document, Initiator: Other}); err != nil {
		t.Fatal(err)
	}
	navigation, script, corsScript, icon := transport.requests[0].Header, transport.requests[1].Header, transport.requests[2].Header, transport.requests[3].Header
	if navigation.Get("Sec-Fetch-Mode") != "navigate" || navigation.Get("Sec-Fetch-Dest") != "document" || navigation.Get("Upgrade-Insecure-Requests") != "1" || !strings.Contains(navigation.Get("Accept"), "text/html") {
		t.Fatalf("incorrect navigation headers: %#v", navigation)
	}
	if navigation.Get("Priority") != "u=0, i" {
		t.Fatalf("incorrect navigation priority: %q", navigation.Get("Priority"))
	}
	if script.Get("Sec-Fetch-Mode") != "no-cors" || script.Get("Sec-Fetch-Dest") != "script" || script.Get("Sec-Fetch-Site") != "cross-site" || script.Get("Accept") != "*/*" {
		t.Fatalf("incorrect script headers: %#v", script)
	}
	if corsScript.Get("Sec-Fetch-Mode") != "cors" || corsScript.Get("Sec-Fetch-Dest") != "script" {
		t.Fatalf("incorrect crossorigin script headers: %#v", corsScript)
	}
	if icon.Get("Sec-Fetch-Mode") != "no-cors" || icon.Get("Sec-Fetch-Dest") != "image" || icon.Get("Priority") != "u=1, i" || !strings.Contains(icon.Get("Accept"), "image/avif") {
		t.Fatalf("incorrect fallback icon headers: %#v", icon)
	}
}

func TestScriptInitiatedPOSTDerivesOriginFromCanonicalSource(t *testing.T) {
	l := NewLoader(testEnvironment, NewCookieStore(), trace.New())
	// The origin assertion uses a cross-origin XHR, whose server must opt in.
	transport := &recordingTransport{responseHeaders: http.Header{"Access-Control-Allow-Origin": {"https://app.example.test"}}}
	l.SetTransport(transport)
	source, _ := url.Parse("https://app.example.test/path")
	target, _ := url.Parse("https://api.example.test/submit")
	if _, err := l.Load(context.Background(), Request{URL: target, SourceURL: source, Referrer: source, Method: http.MethodPost, Initiator: XHR}); err != nil {
		t.Fatal(err)
	}
	if got := transport.requests[0].Header.Get("Origin"); got != "https://app.example.test" {
		t.Fatalf("Origin = %q", got)
	}
}

type synthetic struct{}

func (synthetic) Before(_ context.Context, r Request) (Decision, error) {
	return Decision{Response: &Response{Status: 200, Headers: http.Header{"Content-Type": {"text/plain"}}, Body: []byte("synthetic"), URL: r.URL}}, nil
}
func (synthetic) After(_ context.Context, _ Request, r Response) (Response, error) {
	r.Headers.Set("X-Observed", "yes")
	return r, nil
}
func TestInterceptionBeforeTransport(t *testing.T) {
	l := NewLoader(testEnvironment, NewCookieStore(), trace.New())
	l.Use(synthetic{})
	u, _ := url.Parse("https://invalid.example/")
	r, err := l.Load(context.Background(), Request{URL: u, Initiator: Fetch})
	if err != nil {
		t.Fatal(err)
	}
	if string(r.Body) != "synthetic" || r.Headers.Get("X-Observed") != "yes" || !r.Synthetic {
		t.Fatalf("%+v", r)
	}
}
func TestCacheDisabledIsCanonicalPolicy(t *testing.T) {
	session := NewSessionState()
	l := NewLoaderWithSession(testEnvironment, NewCookieStore(), session, trace.New())
	transport := &countingTransport{}
	l.SetTransport(transport)
	u, _ := url.Parse("https://example.test/cache")
	for range 2 {
		if _, err := l.Load(context.Background(), Request{URL: u, Method: http.MethodGet}); err != nil {
			t.Fatal(err)
		}
	}
	if transport.calls != 1 {
		t.Fatalf("cache missed: calls=%d", transport.calls)
	}
	l.Policy().SetCacheDisabled(true)
	if _, err := l.Load(context.Background(), Request{URL: u, Method: http.MethodGet}); err != nil {
		t.Fatal(err)
	}
	if transport.calls != 2 {
		t.Fatalf("disabled cache was used: calls=%d", transport.calls)
	}
}

func TestHTTPErrorStatusRemainsAResponse(t *testing.T) {
	l := NewLoader(testEnvironment, NewCookieStore(), trace.New())
	l.SetTransport(statusTransport{status: http.StatusForbidden})
	u, _ := url.Parse("https://example.test/forbidden")
	res, err := l.Load(context.Background(), Request{URL: u, Initiator: Navigation})
	if err != nil {
		t.Fatalf("HTTP status must not become a transport error: %v", err)
	}
	if res.Status != http.StatusForbidden || string(res.Body) != "error document" {
		t.Fatalf("unexpected response: %+v", res)
	}
}

func TestConnectionPoolIsWarmWithinSessionAndColdAcrossSessions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL + "/resource")

	type observation struct{ cold, reused bool }
	run := func(session *SessionState) []observation {
		recorder := trace.New()
		recorder.Start()
		loader := NewLoaderWithSession(testEnvironment, NewCookieStore(), session, recorder)
		for range 2 {
			if _, err := loader.Load(context.Background(), Request{URL: u, Initiator: Fetch}); err != nil {
				t.Fatal(err)
			}
		}
		var observed []observation
		for _, event := range recorder.Events() {
			if event.Kind == trace.Network && event.Name == "transport" {
				cold, _ := event.Data["sessionCold"].(bool)
				timing, _ := event.Data["timing"].(TransportTimingSnapshot)
				observed = append(observed, observation{cold: cold, reused: timing.Reused})
			}
		}
		return observed
	}

	if observed := run(NewSessionState()); len(observed) != 2 || !observed[0].cold || observed[0].reused || observed[1].cold || !observed[1].reused {
		t.Fatalf("one session did not transition cold to warm: %v", observed)
	}
	if observed := run(NewSessionState()); len(observed) != 2 || !observed[0].cold || observed[0].reused || observed[1].cold || !observed[1].reused {
		t.Fatalf("isolated session inherited connection state: %v", observed)
	}
}

func TestRedirectEstablishesTargetOriginInSameSessionPool(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("target")) }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL+"/final")
		w.WriteHeader(http.StatusFound)
	}))
	defer source.Close()
	u, _ := url.Parse(source.URL + "/start")
	session := NewSessionState()
	loader := NewLoaderWithSession(testEnvironment, NewCookieStore(), session, trace.New())
	if _, err := loader.Load(context.Background(), Request{URL: u, Initiator: Navigation}); err != nil {
		t.Fatal(err)
	}
	connections := session.Connections()
	if len(connections) != 2 {
		t.Fatalf("redirect origins did not share the canonical pool: %+v", connections)
	}
}
