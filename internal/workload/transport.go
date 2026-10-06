package workload

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	hashpkg "hash"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/moreveal/mimic/internal/network"
)

type entry struct {
	BranchCaptureSHA256 string      `json:"branchCaptureSHA256,omitempty"`
	Failure             string      `json:"failure,omitempty"`
	Context             int         `json:"context"`
	Key                 string      `json:"key"`
	URL                 string      `json:"url"`
	Method              string      `json:"method"`
	RequestHeaders      http.Header `json:"requestHeaders,omitempty"`
	RequestHost         string      `json:"requestHost,omitempty"`
	SourceURL           string      `json:"sourceURL,omitempty"`
	HistoryPhase        *int        `json:"historyPhase,omitempty"`
	Kind                string      `json:"kind"`
	Owner               string      `json:"owner"`
	Mechanism           string      `json:"mechanism"`
	Status              int         `json:"status"`
	Headers             http.Header `json:"headers"`
	Trailers            http.Header `json:"trailers,omitempty"`
	ContentLength       int64       `json:"contentLength"`
	Protocol            string      `json:"protocol"`
	Major               int         `json:"major"`
	Minor               int         `json:"minor"`
	Complete            bool        `json:"complete"`
	BodySHA256          string      `json:"bodySHA256"`
	BodyBytes           int64       `json:"bodyBytes"`
	segments            []segment
	file                *zip.File
	emptyRequestBody    bool
	insecureRequest     bool
}
type segment struct{ offset, size int64 }
type capture struct {
	Parents            []string `json:"parentCaptureSHA256,omitempty"`
	CoverageIssues     []string `json:"coverageIssues,omitempty"`
	QueryNormalization string   `json:"queryNormalization,omitempty"`
	VolatileQuery      []string `json:"volatileQuery,omitempty"`
	Format             string   `json:"format"`
	Version            int      `json:"version"`
	Entries            []*entry `json:"entries"`
}
type Resource struct {
	DocumentURL          string `json:"documentURL,omitempty"`
	DocumentSHA256       string `json:"documentSHA256,omitempty"`
	RequestBodySHA256    string `json:"requestBodySHA256"`
	RequestHeadersSHA256 string `json:"requestHeadersSHA256"`
	Method               string `json:"method"`
	CauseURL             string `json:"causeURL,omitempty"`
	SourceURL            string `json:"sourceURL,omitempty"`
	URL                  string `json:"url"`
	Kind                 string `json:"kind"`
	Owner                string `json:"owner"`
	Mechanism            string `json:"mechanism"`
	Bytes                int    `json:"bytes"`
	Synthetic            bool   `json:"synthetic"`
}
type Script struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	External bool   `json:"external"`
	Bytes    int    `json:"bytes"`
}
type Metrics struct {
	CDPConnections           int64              `json:"cdpConnections"`
	Attempts                 []Resource         `json:"attempts,omitempty"`
	Documents                []DocumentEvidence `json:"documents,omitempty"`
	ResponseAcquisitions     int64              `json:"responseAcquisitions"`
	ReplayDiagnostics        []string           `json:"replayDiagnostics,omitempty"`
	CaptureMisses            []string           `json:"captureMisses"`
	Unsupported              []string           `json:"unsupported"`
	RetainedBodyBytes        int64              `json:"retainedBodyBytes"`
	PeakRetainedBodyBytes    int64              `json:"peakRetainedBodyBytes"`
	Resources                []Resource         `json:"resources"`
	Requests                 int64              `json:"requests"`
	EncodedBodyBytes         int64              `json:"encodedBodyBytes"`
	ProcessedBodyBytes       int64              `json:"processedBodyBytes"`
	ScriptsAcquired          int64              `json:"scriptsAcquired"`
	ClassicScriptsExecuted   int64              `json:"classicScriptsExecuted"`
	ClassicScriptsSuppressed int64              `json:"classicScriptsSuppressed"`
	Scripts                  []Script           `json:"scripts"`
	Violations               []string           `json:"violations"`
	Active                   int                `json:"active"`
}

// Session owns all mutable experiment state. Each Context gets an independent
// replay cursor for each exact request shape. Unconsumed capture entries are
// permitted: eliminating a resource is the point of specialization.
type Session struct {
	documentsByFrame map[string]DocumentEvidence
	archive          *zip.ReadCloser
	spool            *os.File
	spoolSize        int64
	inventory        bool
	bodyStores       map[*network.SessionState]bool
	mu               sync.Mutex
	mode             string
	contexts         int
	capture          capture
	index            map[string][]*entry
	ambiguous        map[string]bool
	suppress         map[string]bool
	metrics          Metrics
	volatileQuery    map[string]bool
	storageLimit     int64
}

func New(record bool, replayPath string, suppress []string) (*Session, error) {
	return NewWithSpoolDirectory(record, replayPath, suppress, "")
}

func NewWithSpoolDirectory(record bool, replayPath string, suppress []string, spoolDirectory string) (*Session, error) {
	s := &Session{mode: "measure", capture: capture{Format: "mimic-workload-capture", Version: artifactVersion}, index: map[string][]*entry{}, suppress: map[string]bool{}, bodyStores: map[*network.SessionState]bool{}}
	s.inventory = true
	s.storageLimit = 512 << 20
	s.volatileQuery = map[string]bool{}
	for _, id := range suppress {
		s.suppress[id] = true
	}
	if record {
		s.mode = "record"
		var err error
		s.spool, err = os.CreateTemp(spoolDirectory, "mimic-capture-spool-*")
		if err != nil {
			return nil, err
		}
	}
	if replayPath == "" {
		return s, nil
	}
	if record {
		s.Close()
		return nil, fmt.Errorf("record and replay are mutually exclusive")
	}
	s.mode = "replay"
	z, err := zip.OpenReader(replayPath)
	if err != nil {
		return nil, err
	}
	s.archive = z
	loaded := false
	defer func() {
		if !loaded {
			s.Close()
		}
	}()
	files := map[string]*zip.File{}
	for _, f := range z.File {
		if _, duplicate := files[f.Name]; duplicate {
			return nil, fmt.Errorf("duplicate capture member: %s", f.Name)
		}
		files[f.Name] = f
	}
	m := files["manifest.json"]
	if m == nil {
		return nil, fmt.Errorf("missing capture manifest")
	}
	rd, err := m.Open()
	if err != nil {
		return nil, err
	}
	err = json.NewDecoder(io.LimitReader(rd, 32<<20)).Decode(&s.capture)
	rd.Close()
	if err != nil {
		return nil, err
	}
	if s.capture.Format != "mimic-workload-capture" || s.capture.Version != artifactVersion {
		return nil, fmt.Errorf("unsupported capture")
	}
	if len(s.capture.Entries) > 10000 || len(z.File) != len(s.capture.Entries)+1 {
		return nil, fmt.Errorf("invalid capture member count")
	}
	for _, key := range s.capture.VolatileQuery {
		s.volatileQuery[key] = true
	}
	var recordedBytes int64
	for i, e := range s.capture.Entries {
		if !e.Complete {
			return nil, fmt.Errorf("incomplete capture entry %d", i)
		}
		f := files[fmt.Sprintf("bodies/%d", i)]
		if f == nil || f.UncompressedSize64 > 32<<20 {
			return nil, fmt.Errorf("missing/oversized body %d", i)
		}
		if int64(f.UncompressedSize64) != e.BodyBytes || len(e.BodySHA256) != 64 {
			return nil, fmt.Errorf("invalid body identity %d", i)
		}
		recordedBytes += e.BodyBytes
		if recordedBytes > 512<<20 {
			return nil, fmt.Errorf("capture storage limit exceeded")
		}
		e.file = f
		// Derive empty-body/certificate identity from the authoritative wire key
		// instead of assuming that every GET had no request payload.
		if request, e2 := http.NewRequest(e.Method, e.URL, nil); e2 == nil && e.RequestHeaders != nil {
			request.Header, request.Host = e.RequestHeaders.Clone(), e.RequestHost
			for _, insecure := range []bool{false, true} {
				key, e2 := s.requestKey(request, insecure)
				if e2 == nil && key == e.Key {
					e.emptyRequestBody, e.insecureRequest = true, insecure
					break
				}
			}
		}
		s.index[fmt.Sprintf("%d:%s", e.Context, e.Key)] = append(s.index[fmt.Sprintf("%d:%s", e.Context, e.Key)], e)
	}
	if len(s.capture.VolatileQuery) > 0 && s.capture.QueryNormalization != "preserve-order-1" {
		return nil, fmt.Errorf("capture query normalization is incompatible; record fresh evidence")
	}
	s.ambiguous = ambiguousOccurrences(s.capture)
	for _, e := range s.capture.Entries {
		if !validRecordedResponse(e) {
			return nil, fmt.Errorf("invalid captured response")
		}
	}
	loaded = true
	return s, nil
}

func (s *Session) Close() error {
	var err error
	if s.archive != nil {
		err = s.archive.Close()
		s.archive = nil
	}
	if s.spool != nil {
		name := s.spool.Name()
		if e := s.spool.Close(); err == nil {
			err = e
		}
		os.Remove(name)
		s.spool = nil
	}
	return err
}

// A sequence number alone cannot tell which occurrence survives elimination
// of an earlier initiator. Until causal request identities exist, repeated
// identical wire requests with different results are an unsupported capture.
func validateOccurrences(c capture) error {
	seen := map[string]string{}
	for _, e := range c.Entries {
		if !validRecordedResponse(e) {
			return fmt.Errorf("unsupported captured response or failure")
		}
		key := occurrenceScope(e)
		value := hash(mustJSON([]any{e.Status, e.Headers, e.Trailers, e.ContentLength, e.Protocol, e.Failure, e.BodySHA256, e.BodyBytes}))
		if old, ok := seen[key]; ok && old != value {
			return fmt.Errorf("ambiguous repeated request: %s", e.URL)
		}
		seen[key] = value
	}
	return nil
}

func validRecordedResponse(e *entry) bool {
	if e.Failure == "context-canceled" {
		return e.Status == 0 && e.BodyBytes == 0
	}
	return (e.Failure == "" || e.Failure == "context-canceled-body") && e.Status >= 100 && e.Status <= 599
}

func ambiguousOccurrences(c capture) map[string]bool {
	seen := map[string]string{}
	ambiguous := map[string]bool{}
	for _, e := range c.Entries {
		key := occurrenceScope(e)
		value := hash(mustJSON([]any{e.Status, e.Headers, e.Trailers, e.ContentLength, e.Protocol, e.Failure, e.BodySHA256, e.BodyBytes}))
		if old, ok := seen[key]; ok && old != value {
			ambiguous[key] = true
		}
		seen[key] = value
	}
	return ambiguous
}

func occurrenceScope(e *entry) string {
	key := fmt.Sprintf("%d:%s", e.Context, e.Key)
	if e.HistoryPhase != nil {
		key += fmt.Sprintf(":%d:%s", *e.HistoryPhase, e.SourceURL)
	}
	return key
}
func matchesPhase(e *entry, r network.Request) bool {
	if e.HistoryPhase == nil {
		return true
	} // No browser phase was recorded for this request.
	source := ""
	if r.SourceURL != nil {
		source = r.SourceURL.String()
	}
	return *e.HistoryPhase == r.HistoryPhase && e.SourceURL == source
}

func hash(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }

// SetInventory is called before creating Contexts. Final matched benchmarks
// retain cost counters but omit resource/script inventories used by search.
func (s *Session) SetInventory(enabled bool) { s.inventory = enabled }
func (s *Session) SetVolatileQuery(keys []string) error {
	if s.mode != "record" {
		if len(keys) > 0 {
			return fmt.Errorf("normalization is stored in the capture and cannot be changed on replay")
		}
		return nil
	}
	for _, key := range keys {
		if key == "" {
			return fmt.Errorf("empty volatile query key")
		}
		s.volatileQuery[key] = true
	}
	s.capture.VolatileQuery = append([]string(nil), keys...)
	if len(keys) > 0 {
		s.capture.QueryNormalization = "preserve-order-1"
	}
	return nil
}
func (s *Session) normalizeURL(raw string) string {
	return normalizeRequestURL(raw, s.capture.VolatileQuery)
}
func (s *Session) requestKey(r *http.Request, insecure bool) (string, error) {
	bodyHash := sha256.New()
	if r.Body != nil {
		if r.GetBody == nil {
			return "", fmt.Errorf("request body cannot be inspected without consuming it")
		}
		b, err := r.GetBody()
		if err != nil {
			return "", err
		}
		defer b.Close()
		n, err := io.Copy(bodyHash, io.LimitReader(b, (32<<20)+1))
		if err != nil {
			return "", err
		}
		if n > 32<<20 {
			return "", fmt.Errorf("oversized request body")
		}
	}
	headers := r.Header.Clone()
	if headers.Get("Referer") != "" {
		headers.Set("Referer", s.normalizeURL(headers.Get("Referer")))
	}
	return hash(mustJSON([]any{r.Method, s.normalizeURL(r.URL.String()), r.Host, headers, hex.EncodeToString(bodyHash.Sum(nil)), insecure})), nil
}

func (s *Session) Wrap(base network.Transport) network.Transport {
	s.mu.Lock()
	id := s.contexts
	s.contexts++
	s.mu.Unlock()
	return &transport{session: s, base: base, context: id, cursor: map[string]int{}, active: map[string]int{}}
}

func (s *Session) violationLocked(message string) error {
	s.metrics.Violations = append(s.metrics.Violations, message)
	return fmt.Errorf("workload replay violation: %s", message)
}
func (s *Session) Snapshot() Metrics {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.updateRetentionLocked()
	m := s.metrics
	m.Resources = append([]Resource(nil), m.Resources...)
	m.Attempts = append([]Resource(nil), m.Attempts...)
	m.Documents = append([]DocumentEvidence(nil), m.Documents...)
	m.Scripts = append([]Script(nil), m.Scripts...)
	m.Violations = append([]string(nil), m.Violations...)
	m.CaptureMisses = append([]string(nil), m.CaptureMisses...)
	m.ReplayDiagnostics = append([]string(nil), m.ReplayDiagnostics...)
	m.Unsupported = append([]string(nil), m.Unsupported...)
	return m
}
func (s *Session) Save(path string) error {
	return s.save(path, false)
}

// SaveEvidence preserves complete responses even when some repeated request
// shapes are ambiguous. It still returns the coverage error. Replay rejects
// those shapes at acquisition; a validated exclusion can avoid them entirely.
func (s *Session) SaveEvidence(path string) error {
	return s.save(path, true)
}
func (s *Session) save(path string, preserve bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mode != "record" {
		return fmt.Errorf("not recording")
	}
	if s.metrics.Active != 0 || len(s.metrics.Violations) > 0 || len(s.metrics.Unsupported) > 0 {
		return fmt.Errorf("recording has active requests or violations")
	}
	for _, e := range s.capture.Entries {
		if !e.Complete {
			return fmt.Errorf("incomplete response: %s", e.URL)
		}
	}
	if err := validateOccurrences(s.capture); err != nil {
		if preserve {
			s.capture.CoverageIssues = append(s.capture.CoverageIssues, err.Error())
			if writeErr := writeCapture(path, s.capture, s.spool); writeErr != nil {
				return writeErr
			}
		}
		return err
	}
	return writeCapture(path, s.capture, s.spool)
}
func (s *Session) AdmitClassic(url, source string, external bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	var id string
	if s.inventory || (external && len(s.suppress) > 0) {
		id = hash(mustJSON([]string{s.normalizeURL(url), source}))
	}
	if s.inventory {
		s.metrics.Scripts = append(s.metrics.Scripts, Script{ID: id, URL: url, External: external, Bytes: len(source)})
	}
	if external && s.suppress[id] {
		s.metrics.ClassicScriptsSuppressed++
		return false
	}
	s.metrics.ClassicScriptsExecuted++
	return true
}

type transport struct {
	session *Session
	base    network.Transport
	context int
	cursor  map[string]int
	active  map[string]int
}

func (t *transport) WantsResourceMetadata() {}

func (t *transport) RoundTrip(r *http.Request) (*http.Response, error) { return t.roundTrip(r, false) }
func (t *transport) RoundTripIgnoringCertificateErrors(r *http.Request) (*http.Response, error) {
	return t.roundTrip(r, true)
}
func (t *transport) RejectUnsupported(feature string) error {
	s := t.session
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metrics.Unsupported = append(s.metrics.Unsupported, feature)
	return fmt.Errorf("unsupported workload capture/replay: %s", feature)
}
func (t *transport) BeforeWebSocket() error {
	if t.session.mode == "replay" {
		return t.RejectUnsupported("WebSocket is not covered by this capture")
	}
	_ = t.RejectUnsupported("WebSocket cannot be recorded by this experiment")
	return nil
}
func (t *transport) WebSocketBase() network.Transport { return t.base }
func (t *transport) CloseIdleConnections() {
	if c, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}
func (s *Session) updateRetentionLocked() {
	var retained int64
	for store := range s.bodyStores {
		retained += store.BodyStorageStats().ResidentBytes
	}
	s.metrics.RetainedBodyBytes = retained
	if retained > s.metrics.PeakRetainedBodyBytes {
		s.metrics.PeakRetainedBodyBytes = retained
	}
}
func (t *transport) ObserveResource(request network.Request, r network.Response, store *network.SessionState) {
	s := t.session
	s.mu.Lock()
	s.metrics.ProcessedBodyBytes += int64(len(r.Body))
	s.bodyStores[store] = true
	s.updateRetentionLocked()
	if request.URL != nil && s.inventory {
		var source string
		if request.SourceURL != nil {
			source = request.SourceURL.String()
		}
		bodyIdentity, headersIdentity := RequestIdentity(request)
		document := s.documentsByFrame[request.ExecutionOwner]
		s.metrics.Resources = append(s.metrics.Resources, Resource{DocumentURL: document.URL, DocumentSHA256: document.SHA256, RequestBodySHA256: bodyIdentity, RequestHeadersSHA256: headersIdentity, URL: request.URL.String(), Method: request.Method, CauseURL: request.CauseURL, SourceURL: source, Kind: request.ResourceKind(), Owner: request.Owner, Mechanism: request.Mechanism, Bytes: len(r.Body), Synthetic: r.Synthetic})
		if request.ResourceKind() == "document" && r.URL != nil && r.Status >= 200 && r.Status < 300 {
			s.metrics.Documents = append(s.metrics.Documents, DocumentEvidence{URL: r.URL.String(), SHA256: hash(r.Body), Status: r.Status})
			if request.Initiator == network.Navigation && request.ContextID != "" {
				if s.documentsByFrame == nil {
					s.documentsByFrame = map[string]DocumentEvidence{}
				}
				s.documentsByFrame[request.ContextID] = DocumentEvidence{URL: r.URL.String(), SHA256: hash(r.Body), Status: r.Status}
			}
		}
	}
	s.mu.Unlock()
}

func (t *transport) BeforeResource(request network.Request) {
	s := t.session
	if !s.inventory || request.URL == nil {
		return
	}
	method := request.Method
	if method == "" {
		method = "GET"
	}
	var source string
	if request.SourceURL != nil {
		source = request.SourceURL.String()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.metrics.Attempts) < 10000 {
		bodyIdentity, headersIdentity := RequestIdentity(request)
		document := s.documentsByFrame[request.ExecutionOwner]
		s.metrics.Attempts = append(s.metrics.Attempts, Resource{DocumentURL: document.URL, DocumentSHA256: document.SHA256, RequestBodySHA256: bodyIdentity, RequestHeadersSHA256: headersIdentity, URL: request.URL.String(), Method: method, Kind: request.ResourceKind(), Owner: request.Owner, Mechanism: request.Mechanism, SourceURL: source, CauseURL: request.CauseURL})
	}
}

func (s *Session) ObserveSuppressedClassic(url, source string, external bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.metrics.ClassicScriptsSuppressed++
	if s.inventory {
		s.metrics.Scripts = append(s.metrics.Scripts, Script{ID: hash(mustJSON([]string{s.normalizeURL(url), source})), URL: url, External: external, Bytes: len(source)})
	}
}

func (t *transport) roundTrip(r *http.Request, insecure bool) (*http.Response, error) {
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	s := t.session
	key, err := s.requestKey(r, insecure)
	if err != nil {
		unsupported := t.RejectUnsupported(err.Error())
		if s.mode == "replay" {
			return nil, unsupported
		}
		// An unrecordable live request still follows the ordinary transport. The
		// unsupported evidence prevents optimization; it does not rewrite the body.
		if insecure {
			if base, ok := t.base.(interface {
				RoundTripIgnoringCertificateErrors(*http.Request) (*http.Response, error)
			}); ok {
				return base.RoundTripIgnoringCertificateErrors(r)
			}
		}
		return t.base.RoundTrip(r)
	}
	if r.URL.Scheme != "http" && r.URL.Scheme != "https" {
		return nil, t.RejectUnsupported(r.URL.Scheme)
	}
	s.mu.Lock()
	// Identical concurrent occurrences are safe when their complete captured
	// results are identical. validateOccurrences rejects ambiguous results;
	// the cursor below still bounds the number of acquisitions.
	t.active[key]++
	s.metrics.Active++
	s.metrics.Requests++
	metadata := network.TransportResourceRequest(r)
	activeKey := key
	finish := func() { s.mu.Lock(); t.active[activeKey]--; s.metrics.Active--; s.mu.Unlock() }
	var e *entry
	if s.mode == "replay" {
		entries := s.index[fmt.Sprintf("%d:%s", t.context, key)]
		filterPhase := func(input []*entry) []*entry {
			var selected []*entry
			for _, recorded := range input {
				if matchesPhase(recorded, metadata) {
					selected = append(selected, recorded)
				}
			}
			return selected
		}
		entries = filterPhase(entries)
		// Explicitly fresh public/immutable representations permit cache reuse
		// across non-Vary request headers. Authentication, conditional requests,
		// Host, Vary, variants and occurrence coverage remain strict.
		if len(entries) == 0 {
			var equivalent *entry
			for _, recorded := range s.capture.Entries {
				if recorded.Context == t.context && recorded.insecureRequest == insecure && (matchesPhase(recorded, metadata) || immutableRepresentation(recorded)) && freshRepresentationEquivalent(recorded, r) {
					if equivalent != nil && (equivalent.Key != recorded.Key || equivalent.BodySHA256 != recorded.BodySHA256) {
						equivalent = nil
						break // Ambiguous variants remain uncovered.
					}
					equivalent = recorded
				}
			}
			if equivalent != nil {
				entries = s.index[fmt.Sprintf("%d:%s", t.context, equivalent.Key)]
				if !immutableRepresentation(equivalent) {
					entries = filterPhase(entries)
				}
				key = equivalent.Key
			}
		}
		cursorKey := key
		if len(entries) > 0 {
			cursorKey = occurrenceScope(entries[0])
		}
		if len(entries) > 0 && s.ambiguous[occurrenceScope(entries[0])] {
			s.metrics.CaptureMisses = append(s.metrics.CaptureMisses, r.Method+" "+r.URL.String())
			s.metrics.ReplayDiagnostics = append(s.metrics.ReplayDiagnostics, "Repeated recorded responses differ; no occurrence can be selected safely")
			s.mu.Unlock()
			finish()
			return nil, fmt.Errorf("ambiguous captured request: %s", r.URL.String())
		}
		n := t.cursor[cursorKey]
		if n >= len(entries) {
			s.metrics.CaptureMisses = append(s.metrics.CaptureMisses, r.Method+" "+r.URL.String())
			if len(entries) > 0 {
				s.metrics.ReplayDiagnostics = append(s.metrics.ReplayDiagnostics, "The recorded occurrence count was exhausted")
			} else {
				for _, recorded := range s.capture.Entries {
					if recorded.Method == r.Method && s.normalizeURL(recorded.URL) == s.normalizeURL(r.URL.String()) {
						s.metrics.ReplayDiagnostics = append(s.metrics.ReplayDiagnostics, "This URL was recorded, but its exact headers, request body or Browser Context did not match")
						if recorded.RequestHeaders != nil {
							for name, values := range recorded.RequestHeaders {
								if string(mustJSON(values)) != string(mustJSON(r.Header.Values(name))) {
									s.metrics.ReplayDiagnostics = append(s.metrics.ReplayDiagnostics, "Request header differs: "+name)
								}
							}
							for name := range r.Header {
								if _, present := recorded.RequestHeaders[name]; !present {
									s.metrics.ReplayDiagnostics = append(s.metrics.ReplayDiagnostics, "Request header added: "+name)
								}
							}
						}
						break
					}
				}
			}
			err = fmt.Errorf("capture miss (not a workload semantic failure): %s %s", r.Method, r.URL.String())
			s.mu.Unlock()
			finish()
			return nil, err
		}
		e = entries[n]
		t.cursor[cursorKey] = n + 1
	} else if s.mode == "record" {
		if len(s.capture.Entries) >= 10000 {
			if len(s.metrics.Unsupported) == 0 || s.metrics.Unsupported[len(s.metrics.Unsupported)-1] != "capture request limit exceeded (10000)" {
				s.metrics.Unsupported = append(s.metrics.Unsupported, "capture request limit exceeded (10000)")
			}
		} else {
			e = &entry{Context: t.context, Key: key, URL: r.URL.String(), Method: r.Method, RequestHeaders: r.Header.Clone(), RequestHost: r.Host, Kind: metadata.ResourceKind(), Owner: metadata.Owner, Mechanism: metadata.Mechanism}
			if metadata.ExecutionOwner != "" {
				phase := metadata.HistoryPhase
				e.HistoryPhase = &phase
				if metadata.SourceURL != nil {
					e.SourceURL = metadata.SourceURL.String()
				}
			}
			s.capture.Entries = append(s.capture.Entries, e)
		}
	}
	s.mu.Unlock()
	var response *http.Response
	if s.mode == "replay" {
		if e.Failure == "context-canceled" {
			// Teardown cancellation is a browser-owned input, not a recorded
			// network result. Wait for the same ownership boundary on replay.
			<-r.Context().Done()
			finish()
			return nil, r.Context().Err()
		}
		body, err := e.file.Open()
		if err != nil {
			finish()
			return nil, t.RejectUnsupported("capture body cannot be opened: " + err.Error())
		}
		var replayBody io.ReadCloser = body
		if e.Failure == "context-canceled-body" {
			replayBody = &canceledReplayBody{ReadCloser: body, ctx: r.Context(), closed: make(chan struct{})}
		}
		response = &http.Response{StatusCode: e.Status, Status: fmt.Sprintf("%d %s", e.Status, http.StatusText(e.Status)), Header: e.Headers.Clone(), Trailer: e.Trailers.Clone(), ContentLength: e.ContentLength, Proto: e.Protocol, ProtoMajor: e.Major, ProtoMinor: e.Minor, Request: r, Body: replayBody}
	} else {
		if insecure {
			if base, ok := t.base.(interface {
				RoundTripIgnoringCertificateErrors(*http.Request) (*http.Response, error)
			}); ok {
				response, err = base.RoundTripIgnoringCertificateErrors(r)
			} else {
				err = fmt.Errorf("certificate override unsupported")
			}
		} else {
			response, err = t.base.RoundTrip(r)
		}
		if err != nil {
			s.mu.Lock()
			if s.mode == "record" && e != nil && r.Context().Err() == context.Canceled {
				e.Failure = "context-canceled"
				e.Complete = true
				e.BodySHA256 = hash(nil)
			} else {
				s.metrics.Unsupported = append(s.metrics.Unsupported, "transport error cannot be replayed: "+err.Error())
			}
			s.mu.Unlock()
			finish()
			return nil, err
		}
	}
	if response.StatusCode == 101 {
		response.Body.Close()
		finish()
		return nil, t.RejectUnsupported("HTTP upgrade")
	}
	s.mu.Lock()
	s.metrics.ResponseAcquisitions++
	if metadata.ResourceKind() == "script" || metadata.ResourceKind() == "worker" {
		s.metrics.ScriptsAcquired++
	}
	s.mu.Unlock()
	if s.mode == "record" && e != nil {
		s.mu.Lock()
		e.Status = response.StatusCode
		e.Headers = response.Header.Clone()
		e.ContentLength = response.ContentLength
		e.Protocol = response.Proto
		e.Major = response.ProtoMajor
		e.Minor = response.ProtoMinor
		s.mu.Unlock()
	}
	response.Body = &observedBody{ReadCloser: response.Body, ctx: r.Context(), session: s, entry: e, record: s.mode == "record" && e != nil, response: response, finish: finish, digest: sha256.New()}
	return response, nil
}

func freshRepresentationEquivalent(e *entry, r *http.Request) bool {
	if e.Failure != "" || !e.emptyRequestBody || e.Method != "GET" || r.Method != "GET" || e.URL != r.URL.String() || e.RequestHost != r.Host || e.RequestHeaders == nil || e.Status != http.StatusOK || e.Headers.Get("Set-Cookie") != "" || e.RequestHeaders.Get("Authorization") != "" || r.Header.Get("Authorization") != "" || (r.Body != nil && r.Body != http.NoBody) {
		return false
	}
	public, fresh := false, false
	var lifetime int64
	for _, directive := range strings.Split(strings.ToLower(strings.Join(e.Headers.Values("Cache-Control"), ",")), ",") {
		directive = strings.TrimSpace(directive)
		if directive == "private" || directive == "no-store" || directive == "no-cache" {
			return false
		}
		public = public || directive == "public"
		if strings.HasPrefix(directive, "max-age=") {
			age, err := strconv.ParseInt(strings.TrimPrefix(directive, "max-age="), 10, 64)
			lifetime = age
			fresh = err == nil && age > 0
		}
	}
	age, _ := strconv.ParseInt(e.Headers.Get("Age"), 10, 64)
	if (!public && !immutableRepresentation(e)) || !fresh || age >= lifetime || r.Header.Get("Cache-Control") != "" {
		return false
	}
	for _, name := range []string{"Range", "If-Range", "If-Match", "If-None-Match", "If-Modified-Since", "If-Unmodified-Since"} {
		if r.Header.Get(name) != "" {
			return false
		}
	}
	for _, line := range e.Headers.Values("Vary") {
		for _, field := range strings.Split(line, ",") {
			field = strings.TrimSpace(field)
			if field == "*" || string(mustJSON(e.RequestHeaders.Values(field))) != string(mustJSON(r.Header.Values(field))) {
				return false
			}
		}
	}
	return true
}

func immutableRepresentation(e *entry) bool {
	for _, directive := range strings.Split(strings.ToLower(strings.Join(e.Headers.Values("Cache-Control"), ",")), ",") {
		if strings.TrimSpace(directive) == "immutable" {
			return true
		}
	}
	return false
}

type observedBody struct {
	io.ReadCloser
	ctx      context.Context
	mu       sync.Mutex
	session  *Session
	entry    *entry
	record   bool
	response *http.Response
	digest   hashpkg.Hash
	read     int64
	eof      bool
	closed   bool
	finish   func()
}

func (b *observedBody) Read(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.digest.Write(p[:n])
	}
	b.read += int64(n)
	if err == io.EOF {
		b.eof = true
	}
	b.session.mu.Lock()
	b.session.metrics.EncodedBodyBytes += int64(n)
	b.session.mu.Unlock()
	if b.record && n > 0 {
		s := b.session
		s.mu.Lock()
		offset := s.spoolSize
		writeSize := min(n, int(max(s.storageLimit-offset, 0)))
		written, writeErr := s.spool.WriteAt(p[:writeSize], offset)
		if writeSize < n && (len(s.metrics.Unsupported) == 0 || s.metrics.Unsupported[len(s.metrics.Unsupported)-1] != "capture storage limit exceeded (512 MiB)") {
			s.metrics.Unsupported = append(s.metrics.Unsupported, "capture storage limit exceeded (512 MiB)")
		}
		s.spoolSize += int64(written)
		b.entry.segments = append(b.entry.segments, segment{offset: offset, size: int64(written)})
		if writeErr != nil {
			s.violationLocked("capture disk write failed: " + writeErr.Error())
		}
		s.mu.Unlock()
	}
	ownedCancellation := errors.Is(err, context.Canceled) && b.ctx.Err() == context.Canceled && (b.record || b.entry != nil && b.entry.Failure == "context-canceled-body")
	if err != nil && err != io.EOF && !ownedCancellation {
		b.session.mu.Lock()
		if b.session.mode == "record" {
			b.session.metrics.Unsupported = append(b.session.metrics.Unsupported, "body read could not be recorded completely: "+err.Error())
		} else {
			b.session.violationLocked("body read failed: " + err.Error())
		}
		b.session.mu.Unlock()
	}
	if b.eof && !b.record && b.entry != nil && hex.EncodeToString(b.digest.Sum(nil)) != b.entry.BodySHA256 {
		b.session.mu.Lock()
		err = b.session.violationLocked("capture body integrity mismatch")
		b.session.mu.Unlock()
	}
	return n, err
}
func (b *observedBody) Close() error {
	// Close must unblock an in-flight transport read; do not hold the read lock
	// while closing the underlying body (navigation cancellation can call it).
	err := b.ReadCloser.Close()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return err
	}
	b.closed = true
	if !b.record && b.entry != nil && b.read == b.entry.BodyBytes && hex.EncodeToString(b.digest.Sum(nil)) != b.entry.BodySHA256 {
		b.session.mu.Lock()
		err = b.session.violationLocked("capture body integrity mismatch")
		b.session.mu.Unlock()
	}
	if b.record {
		s := b.session
		s.mu.Lock()
		b.entry.BodySHA256 = hex.EncodeToString(b.digest.Sum(nil))
		b.entry.BodyBytes = b.read
		b.entry.Complete = b.eof || (b.response.ContentLength >= 0 && b.read == b.response.ContentLength)
		if !b.entry.Complete && b.ctx.Err() == context.Canceled {
			// The environment contains a received prefix and browser-owned
			// cancellation, not a complete HTTP response. Replay waits for that
			// same ownership boundary instead of manufacturing a successful EOF.
			b.entry.Failure = "context-canceled-body"
			b.entry.Complete = true
		}
		b.entry.Trailers = b.response.Trailer.Clone()
		if !b.entry.Complete {
			s.metrics.Unsupported = append(s.metrics.Unsupported, "incomplete capture response: "+b.entry.URL)
		}
		s.mu.Unlock()
	}
	b.finish()
	return err
}

func (s *Session) ObserveCDPConnection() { s.mu.Lock(); s.metrics.CDPConnections++; s.mu.Unlock() }

type canceledReplayBody struct {
	io.ReadCloser
	ctx    context.Context
	closed chan struct{}
	once   sync.Once
}

func (b *canceledReplayBody) Read(data []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := b.ReadCloser.Read(data)
	if err != io.EOF {
		return n, err
	}
	if n > 0 {
		return n, nil
	}
	select {
	case <-b.ctx.Done():
		return 0, b.ctx.Err()
	case <-b.closed:
		return 0, io.ErrClosedPipe
	}
}

func (b *canceledReplayBody) Close() error {
	b.once.Do(func() { close(b.closed) })
	return b.ReadCloser.Close()
}
