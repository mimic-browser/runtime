package workload

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/moreveal/mimic/internal/network"
)

const RuntimeABI = "workload-execution-1"

type DocumentEvidence struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Status int    `json:"status"`
}

type RequestEvidence struct {
	DocumentURL    string `json:"documentURL"`
	DocumentSHA256 string `json:"documentSHA256"`
	SourceURL      string `json:"sourceURL"`
	URL            string `json:"url"`
	Method         string `json:"method"`
	Kind           string `json:"kind"`
	BodySHA256     string `json:"bodySHA256"`
	HeadersSHA256  string `json:"headersSHA256"`
}

// ExecutionPlan shares resource rules and compilation with the manual frontend.
// Author admission is a separate execution stage, not a network permission.
type ExecutionPlan struct {
	Resources       []network.ResourceRule `json:"resources,omitempty"`
	SuppressClassic []string               `json:"suppressClassic,omitempty"`
}

type Profile struct {
	Format        string             `json:"format"`
	Version       int                `json:"version"`
	RuntimeABI    string             `json:"runtimeABI"`
	Name          string             `json:"name,omitempty"`
	Engine        string             `json:"engine"`
	BrowserMode   string             `json:"browserMode"`
	Chrome        int                `json:"chrome"`
	BuildSHA256   string             `json:"buildSHA256"`
	CreatedAt     string             `json:"createdAt"`
	Confidence    string             `json:"confidence"`
	Documents     []DocumentEvidence `json:"documents"`
	Requests      []RequestEvidence  `json:"requests"`
	CaptureSHA256 []string           `json:"captureSHA256"`
	VolatileQuery []string           `json:"volatileQuery,omitempty"`
	Plan          ExecutionPlan      `json:"plan"`
	Report        json.RawMessage    `json:"report,omitempty"`
}

func (p Profile) Validate() error {
	if p.Format != "mimic-workload-profile" || p.Version != 1 || p.RuntimeABI != RuntimeABI {
		return fmt.Errorf("incompatible workload profile; re-optimize with this Mimic release")
	}
	if p.Chrome != 152 || (p.Engine != "v8" && p.Engine != "quickjs" && p.Engine != "goja") ||
		(p.BrowserMode != "headful" && p.BrowserMode != "headless") || len(p.BuildSHA256) != 64 {
		return fmt.Errorf("invalid workload profile runtime identity")
	}
	if p.Confidence != "empirical-request-scoped" {
		return fmt.Errorf("incompatible profile admission; re-optimize with this Mimic release")
	}
	if len(p.CaptureSHA256) == 0 {
		return fmt.Errorf("workload profile has no validation evidence")
	}
	for _, document := range p.Documents {
		u, err := url.Parse(document.URL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || len(document.SHA256) != 64 || document.Status < 200 || document.Status >= 300 {
			return fmt.Errorf("invalid document evidence")
		}
	}
	return (network.ResourcePolicy{Rules: p.Plan.Resources}).Validate()
}

func ReadProfile(path string) (Profile, error) {
	var p Profile
	z, err := zip.OpenReader(path)
	if err != nil {
		return p, err
	}
	defer z.Close()
	if len(z.File) != 2 {
		return p, fmt.Errorf("invalid profile container")
	}
	files := map[string][]byte{}
	for _, member := range z.File {
		if member.Name != "profile.json" && member.Name != "integrity.sha256" {
			return p, fmt.Errorf("unknown profile member")
		}
		if _, duplicate := files[member.Name]; duplicate || member.UncompressedSize64 > 16<<20 {
			return p, fmt.Errorf("duplicate or oversized profile member")
		}
		r, e := member.Open()
		if e != nil {
			return p, e
		}
		b, e := io.ReadAll(io.LimitReader(r, (16<<20)+1))
		r.Close()
		if e != nil {
			return p, e
		}
		files[member.Name] = b
	}
	if string(files["integrity.sha256"]) != hash(files["profile.json"]) {
		return p, fmt.Errorf("profile integrity check failed")
	}
	if err := json.Unmarshal(files["profile.json"], &p); err != nil {
		return p, err
	}
	return p, p.Validate()
}

func WriteProfile(path string, p Profile) error {
	if err := p.Validate(); err != nil {
		return err
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if len(b) > 16<<20 {
		return fmt.Errorf("profile metadata exceeds 16 MiB")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".profile-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	z := zip.NewWriter(f)
	for _, member := range []struct {
		name string
		body []byte
	}{{"profile.json", b}, {"integrity.sha256", []byte(hash(b))}} {
		w, e := z.Create(member.name)
		if e == nil {
			_, e = w.Write(member.body)
		}
		if e != nil {
			z.Close()
			f.Close()
			return e
		}
	}
	if err = z.Close(); err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func FileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func DataDir() (string, error) {
	if directory := os.Getenv("MIMIC_DATA_DIR"); directory != "" {
		return filepath.Abs(directory)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "Mimic"), nil
}

func ProfilePath(name string) (string, error) {
	if strings.ContainsAny(name, "/\\") || strings.EqualFold(filepath.Ext(name), ".mprofile") {
		return filepath.Abs(name)
	}
	if name == "" || name == "." || name == ".." {
		return "", fmt.Errorf("provide a profile name or .mprofile path")
	}
	upper := strings.ToUpper(name)
	if upper == "CON" || upper == "PRN" || upper == "AUX" || upper == "NUL" || len(upper) == 4 && (strings.HasPrefix(upper, "COM") || strings.HasPrefix(upper, "LPT")) && upper[3] >= '1' && upper[3] <= '9' {
		return "", fmt.Errorf("profile name is reserved on Windows")
	}
	for _, char := range name {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' || char == '_') {
			return "", fmt.Errorf("profile names use letters, numbers, dash or underscore")
		}
	}
	base, err := DataDir()
	return filepath.Join(base, "profiles", name+".mprofile"), err
}

// Gate belongs to one Page. Unknown documents and resources take the general
// path. Document digests retain capture provenance; live admission is scoped to
// recorded routes and individual request inputs, not HTML bytes. This is empirical
// specialization, not proof about server state or rollback of omitted effects.
type Gate struct {
	mu          sync.Mutex
	profile     Profile
	documentURL string
	active      bool
	note        func(string)
	suppressed  map[string]bool
	requests    map[requestScope]bool
}

func NewGate(p Profile, note func(string)) *Gate {
	g := &Gate{profile: p, note: note, suppressed: map[string]bool{}, requests: map[requestScope]bool{}}
	for _, id := range p.Plan.SuppressClassic {
		g.suppressed[id] = true
	}
	for _, r := range p.Requests {
		g.requests[g.scope(r.DocumentURL, r.SourceURL, r.URL, r.Method, r.Kind, r.BodySHA256, r.HeadersSHA256)] = true
	}
	return g
}

// Keep request fields separate: URL or method contents cannot collide with a
// delimiter-based composite key. No path/payload/header similarity is inferred.
type requestScope struct {
	Document, Source, URL, Method, Kind, Body, Headers string
}

func (g *Gate) scope(document, source, rawURL, method, kind, body, headers string) requestScope {
	if method == "" {
		method = "GET"
	}
	return requestScope{g.normalize(document), g.normalize(source), g.normalize(rawURL), method, kind, body, headers}
}

func (g *Gate) normalize(raw string) string {
	raw = normalizeRequestURL(raw, g.profile.VolatileQuery)
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return u.String()
}

func (g *Gate) Allows(r network.Request) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if r.ResourceKind() == "document" {
		// Top-level navigation creates a new admission boundary before work.
		if r.Initiator == network.Navigation {
			g.active = false
			g.documentURL = ""
		}
		return false
	}
	method := r.Method
	if method == "" {
		method = "GET"
	}
	body, headers := RequestIdentity(r)
	known := r.URL != nil && r.SourceURL != nil && g.requests[g.scope(g.documentURL, r.SourceURL.String(), r.URL.String(), method, r.ResourceKind(), body, headers)]
	return g.active && known && !r.ClientIsWorker && !r.ClientIsSubframe && r.SourceURL != nil
}

func (g *Gate) Observe(r network.Request, res network.Response) {
	if r.ResourceKind() != "document" || r.Initiator != network.Navigation || res.URL == nil || res.Status < 200 || res.Status >= 300 {
		return
	}
	documentURL := g.normalize(res.URL.String())
	g.mu.Lock()
	g.documentURL = documentURL
	g.active = false
	for _, known := range g.profile.Documents {
		if known.Status == res.Status && g.normalize(known.URL) == documentURL {
			g.active = true
			break
		}
	}
	active := g.active
	g.mu.Unlock()
	if !active && g.note != nil {
		g.note("Profile has no evidence for this document URL/status; using general Mimic: " + res.URL.String())
	}
}

func (g *Gate) AdmitClassic(documentURL, rawURL, source string, external bool) bool {
	if !external {
		return true
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	identityURL := normalizeRequestURL(rawURL, g.profile.VolatileQuery)
	if !g.active || g.normalize(documentURL) != g.documentURL || !g.suppressed[hash(mustJSON([]string{identityURL, source}))] {
		return true
	}
	// A script learned on another route must not inherit suppression merely
	// because its bytes happen to match. The final multi-state oracle checks
	// the shared plan; this boundary retains its recorded per-route coverage.
	for _, request := range g.profile.Requests {
		if request.Kind == "script" && g.normalize(request.DocumentURL) == g.documentURL &&
			g.normalize(request.SourceURL) == g.normalize(documentURL) && g.normalize(request.URL) == g.normalize(rawURL) {
			return false
		}
	}
	return true
}

func normalizeRequestURL(raw string, keys []string) string {
	if len(keys) == 0 {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	selected := map[string]bool{}
	for _, key := range keys {
		selected[key] = true
	}
	parameters := strings.Split(u.RawQuery, "&")
	for i, parameter := range parameters {
		pair := strings.SplitN(parameter, "=", 2)
		key, err := url.QueryUnescape(pair[0])
		if err == nil && selected[key] && len(pair) == 2 {
			parameters[i] = pair[0] + "=" + url.QueryEscape("<volatile>")
		}
	}
	u.RawQuery = strings.Join(parameters, "&")
	return u.String()
}

// Clone gives each Browser immutable ownership of the validated specialization.
func (p Profile) Clone() Profile {
	b, _ := json.Marshal(p)
	var result Profile
	_ = json.Unmarshal(b, &result)
	return result
}

// RequestIdentity guards inputs available before acquisition. Implicit cookie
// headers and external server state remain outside this empirical guard.
func RequestIdentity(r network.Request) (string, string) {
	headers := r.Headers
	if len(headers) == 0 {
		return hash(r.Body), hash([]byte("{}"))
	}
	return hash(r.Body), hash(mustJSON(headers))
}
