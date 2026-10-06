package workload

import (
	"archive/zip"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/moreveal/mimic/internal/network"
)

func testProfile() Profile {
	return Profile{Format: "mimic-workload-profile", Version: 1, RuntimeABI: RuntimeABI, Engine: "v8", BrowserMode: "headful", Chrome: 152, BuildSHA256: strings.Repeat("a", 64), Confidence: "empirical-request-scoped", CaptureSHA256: []string{strings.Repeat("b", 64)}, Documents: []DocumentEvidence{{URL: "https://site.test/", Status: 200, SHA256: hash([]byte("known document"))}}, Requests: []RequestEvidence{{URL: "https://site.test/api/product?id=42", Method: "GET", Kind: "fetch", DocumentURL: "https://site.test/", DocumentSHA256: hash([]byte("known document")), SourceURL: "https://site.test/", BodySHA256: hash(nil), HeadersSHA256: hash([]byte("{}"))}}}
}

func TestProfilePageAdmissionUnknownsAndNavigation(t *testing.T) {
	p := testProfile()
	var notes []string
	gate := NewGate(p, func(note string) { notes = append(notes, note) })
	otherPage := NewGate(p, nil)
	document, _ := url.Parse("https://site.test/")
	product, _ := url.Parse("https://site.test/api/product?id=42")
	request := network.Request{URL: product, SourceURL: document, Initiator: network.Fetch}
	if gate.Allows(request) {
		t.Fatal("admitted before document evidence")
	}
	gate.Observe(network.Request{URL: document, Initiator: network.Navigation}, network.Response{URL: document, Status: 200, Body: []byte("known document")})
	if !gate.Allows(request) || otherPage.Allows(request) {
		t.Fatal("Page admission was missing or leaked")
	}
	request.Body = []byte("different input")
	if gate.Allows(request) {
		t.Fatal("unknown request body specialized")
	}
	request.Body = nil
	request.Headers = http.Header{"Authorization": []string{"different-state"}}
	if gate.Allows(request) {
		t.Fatal("unknown explicit request headers specialized")
	}
	request.Headers = nil
	changedState := *document
	changedState.Fragment = "different-view"
	request.SourceURL = &changedState
	if gate.Allows(request) {
		t.Fatal("unknown SPA hash state specialized")
	}
	request.SourceURL = document
	request.ClientIsSubframe = true
	if gate.Allows(request) {
		t.Fatal("same-URL child realm inherited top-level admission")
	}
	request.ClientIsSubframe = false
	unknown, _ := url.Parse("https://site.test/api/product?id=43")
	request.URL = unknown
	if gate.Allows(request) {
		t.Fatal("unknown semantic state was specialized")
	}
	request.URL = product
	request.Method = "POST"
	if gate.Allows(request) {
		t.Fatal("unknown method was specialized")
	}
	request.Method = "GET"
	if gate.Allows(network.Request{URL: document, Initiator: network.Navigation}) || gate.Allows(request) {
		t.Fatal("navigation did not revoke admission before work")
	}
	gate.Observe(network.Request{URL: document, Initiator: network.Navigation}, network.Response{URL: document, Status: 200, Body: []byte("changed document")})
	if !gate.Allows(request) || len(notes) != 0 {
		t.Fatal("HTML content changes revoked a known request scope")
	}
	gate.Allows(network.Request{URL: document, Initiator: network.Navigation})
	gate.Observe(network.Request{Initiator: network.Navigation}, network.Response{URL: unknown, Status: 200})
	if gate.Allows(request) || len(notes) != 1 {
		t.Fatal("untrained route did not take the general path")
	}
}

func TestProfileScriptIdentityAndRealmBoundary(t *testing.T) {
	p := testProfile()
	script := "https://site.test/app.js?b=2&a=1"
	source := "window.ready=true;"
	p.Plan.SuppressClassic = []string{hash(mustJSON([]string{script, source}))}
	p.Requests = append(p.Requests, RequestEvidence{DocumentURL: "https://site.test/", SourceURL: "https://site.test/", URL: script, Kind: "script"})
	g := NewGate(p, nil)
	u, _ := url.Parse("https://site.test/")
	g.Observe(network.Request{Initiator: network.Navigation}, network.Response{URL: u, Status: 200, Body: []byte("known document")})
	if g.AdmitClassic(u.String(), script, source, true) {
		t.Fatal("known external source was not suppressed")
	}
	if !g.AdmitClassic(u.String(), script, source+"changed", true) || !g.AdmitClassic("https://site.test/frame", script, source, true) || !g.AdmitClassic(u.String(), script, source, false) {
		t.Fatal("unknown source, child realm or inline author was suppressed")
	}
}

func TestProfileArtifactIntegrityAndNames(t *testing.T) {
	t.Setenv("MIMIC_DATA_DIR", t.TempDir())
	path, err := ProfilePath("shop")
	if err != nil {
		t.Fatal(err)
	}
	if err = WriteProfile(path, testProfile()); err != nil {
		t.Fatal(err)
	}
	if _, err = ReadProfile(path); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(t.TempDir(), "corrupt.mprofile"))
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, _ := z.Create("profile.json")
	w.Write([]byte(`{}`))
	w, _ = z.Create("integrity.sha256")
	w.Write([]byte(strings.Repeat("0", 64)))
	z.Close()
	f.Close()
	if _, err = ReadProfile(f.Name()); err == nil {
		t.Fatal("corrupt profile was admitted")
	}
	for _, name := range []string{"", "..", "a b"} {
		if _, err = ProfilePath(name); err == nil {
			t.Fatalf("invalid profile name %q", name)
		}
	}
}

func TestCaptureStorageLimitPreservesLiveBody(t *testing.T) {
	s, _ := New(true, "", nil)
	defer s.Close()
	s.storageLimit = 3
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("complete live body")) }))
	defer server.Close()
	body := readResponse(t, s.Wrap(http.DefaultTransport), server.URL)
	if body != "complete live body" {
		t.Fatal("recording limit changed live body")
	}
	stat, _ := s.spool.Stat()
	if stat.Size() != 3 {
		t.Fatalf("unbounded spool: %d", stat.Size())
	}
	if len(s.Snapshot().Unsupported) == 0 || s.Save(filepath.Join(t.TempDir(), "limited.mcap")) == nil {
		t.Fatal("limited recording was published as valid")
	}
}

func TestVolatileNormalizationPreservesSemanticQueryOrder(t *testing.T) {
	keys := []string{"ts"}
	first := "https://site.test/calc?op=add&v=3&ts=1&op=multiply&v=2"
	same := "https://site.test/calc?op=add&v=3&ts=999&op=multiply&v=2"
	other := "https://site.test/calc?op=multiply&v=2&ts=1&op=add&v=3"
	if normalizeRequestURL(first, keys) != normalizeRequestURL(same, keys) {
		t.Fatal("declared volatile value was not normalized")
	}
	if normalizeRequestURL(first, keys) == normalizeRequestURL(other, keys) {
		t.Fatal("nonvolatile semantic parameter order was lost")
	}
}

func TestProfileRequestScopeDoesNotBroadenOnHTMLChanges(t *testing.T) {
	p := testProfile()
	doc, _ := url.Parse("https://site.test/")
	resource, _ := url.Parse(p.Requests[0].URL)
	for _, body := range []string{"<h1>Price 20</h1>", "<h1>Price 30</h1><script nonce='new'>updated()</script>"} {
		g := NewGate(p, nil)
		g.Observe(network.Request{Initiator: network.Navigation}, network.Response{URL: doc, Status: 200, Body: []byte(body)})
		known := network.Request{URL: resource, SourceURL: doc, Initiator: network.Fetch}
		if !g.Allows(known) {
			t.Fatal("content disabled known request")
		}
		for _, raw := range []string{"https://site.test/api/product?id=43", "https://site.test/api/product?id=42&ts=1", "https://other.test/api/product?id=42"} {
			u, _ := url.Parse(raw)
			changed := known
			changed.URL = u
			if g.Allows(changed) {
				t.Fatalf("untrained request admitted: %s", raw)
			}
		}
		changed := known
		changed.ClientIsWorker = true
		if g.Allows(changed) {
			t.Fatal("worker admitted")
		}
		g.Allows(network.Request{Initiator: network.Navigation, URL: doc})
		g.Observe(network.Request{Initiator: network.Navigation}, network.Response{URL: doc, Status: 201})
		if g.Allows(known) {
			t.Fatal("unknown document status admitted")
		}
	}
}

func TestProfileVolatilityIsExplicitAndOldAdmissionRejected(t *testing.T) {
	p := testProfile()
	p.Confidence = "empirical-document-guarded"
	if p.Validate() == nil {
		t.Fatal("old admission silently reinterpreted")
	}
	p.Confidence = "empirical-request-scoped"
	p.VolatileQuery = []string{"ts"}
	p.Requests[0].URL += "&ts=1"
	g := NewGate(p, nil)
	doc, _ := url.Parse(p.Documents[0].URL)
	g.Observe(network.Request{Initiator: network.Navigation}, network.Response{URL: doc, Status: 200})
	u, _ := url.Parse("https://site.test/api/product?id=42&ts=999")
	if !g.Allows(network.Request{URL: u, SourceURL: doc, Initiator: network.Fetch}) {
		t.Fatal("explicit volatility was lost")
	}
}

func TestProfileClassicSuppressionNeedsCoverageOnCurrentRoute(t *testing.T) {
	p := testProfile()
	script := "https://site.test/shared.js"
	source := "optional()"
	p.Plan.SuppressClassic = []string{hash(mustJSON([]string{script, source}))}
	doc, _ := url.Parse(p.Documents[0].URL)
	g := NewGate(p, nil)
	g.Observe(network.Request{Initiator: network.Navigation}, network.Response{URL: doc, Status: 200})
	if !g.AdmitClassic(doc.String(), script, source, true) {
		t.Fatal("script without route coverage suppressed")
	}
	p.Requests = append(p.Requests, RequestEvidence{DocumentURL: doc.String(), SourceURL: doc.String(), URL: script, Kind: "script"})
	g = NewGate(p, nil)
	g.Observe(network.Request{Initiator: network.Navigation}, network.Response{URL: doc, Status: 200, Body: []byte("new HTML")})
	if g.AdmitClassic(doc.String(), script, source, true) {
		t.Fatal("known exact script not suppressed")
	}
	if !g.AdmitClassic(doc.String(), script, source+";required()", true) {
		t.Fatal("changed source suppressed")
	}
}
