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
	return Profile{Format: "mimic-workload-profile", Version: 1, RuntimeABI: RuntimeABI, Engine: "v8", BrowserMode: "headful", Chrome: 152, BuildSHA256: strings.Repeat("a", 64), Confidence: "empirical-document-guarded", CaptureSHA256: []string{strings.Repeat("b", 64)}, Documents: []DocumentEvidence{{URL: "https://site.test/", Status: 200, SHA256: hash([]byte("known document"))}}, Requests: []RequestEvidence{{URL: "https://site.test/api/product?id=42", Method: "GET", Kind: "fetch", DocumentURL: "https://site.test/", DocumentSHA256: hash([]byte("known document")), SourceURL: "https://site.test/", BodySHA256: hash(nil), HeadersSHA256: hash([]byte("{}"))}}}
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
	if gate.Allows(request) || len(notes) != 1 {
		t.Fatal("changed document did not take the general path")
	}
}

func TestProfileScriptIdentityAndRealmBoundary(t *testing.T) {
	p := testProfile()
	script := "https://site.test/app.js?b=2&a=1"
	source := "window.ready=true;"
	p.Plan.SuppressClassic = []string{hash(mustJSON([]string{script, source}))}
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
