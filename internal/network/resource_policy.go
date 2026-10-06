package network

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strings"
	"sync"
	"sync/atomic"
)

// ResourcePolicy is an opt-in Context contract for the current Mimic release. Nil policy preserves
// the ordinary loader path. Rules are evaluated in order, with the first match
// winning; an omitted work field retains the ordinary behavior.
type ResourcePolicy struct {
	ReportOnly bool            `json:"reportOnly,omitempty"`
	Presets    []string        `json:"presets,omitempty"`
	Rules      []ResourceRule  `json:"rules,omitempty"`
	Budgets    ResourceBudgets `json:"budgets,omitempty"`
}

type ResourceRule struct {
	ID    string        `json:"id"`
	Match ResourceMatch `json:"match"`
	Work  ResourceWork  `json:"work"`
}

type ResourceMatch struct {
	Kinds        []string `json:"kinds,omitempty"`
	Hosts        []string `json:"hosts,omitempty"`
	Origins      []string `json:"origins,omitempty"`
	URLGlob      string   `json:"urlGlob,omitempty"`
	Owners       []string `json:"owners,omitempty"`
	Mechanisms   []string `json:"mechanisms,omitempty"`
	TopLevelSite string   `json:"topLevelSite,omitempty"`
}

type ResourceWork struct {
	CacheRead   *bool  `json:"cacheRead,omitempty"`
	Network     *bool  `json:"network,omitempty"`
	Body        string `json:"body,omitempty"` // full, none, prefix
	PrefixBytes int64  `json:"prefixBytes,omitempty"`
	Decode      *bool  `json:"decode,omitempty"`
	CacheRetain *bool  `json:"cacheRetain,omitempty"`
	DebugRetain *bool  `json:"debugRetain,omitempty"`
}

// Budget fields are distinct from rule permissions. Zero means unlimited.
type ResourceBudgets struct {
	MaxRequests      int64 `json:"maxRequests,omitempty"`
	MaxConcurrent    int64 `json:"maxConcurrent,omitempty"`
	MaxWireBytes     int64 `json:"maxWireBytes,omitempty"`
	MaxBodyBytes     int64 `json:"maxBodyBytes,omitempty"`
	MaxResponseBytes int64 `json:"maxResponseBytes,omitempty"`
	MaxDecodedBytes  int64 `json:"maxDecodedBytes,omitempty"`
	MaxRetainedBytes int64 `json:"maxRetainedBytes,omitempty"`
}

type ResourceDecision struct {
	Generation uint64
	RuleID     string
	Work       ResourceWork
	ReportOnly bool
}

type ResourcePolicyStats struct {
	Generation                 uint64           `json:"generation"`
	Requests                   int64            `json:"requests"`
	NetworkAcquisitions        int64            `json:"networkAcquisitions"`
	CacheHits                  int64            `json:"cacheHits"`
	Blocked                    int64            `json:"blocked"`
	HeaderOnly                 int64            `json:"headerOnly"`
	Prefixes                   int64            `json:"prefixes"`
	WireBytesKnown             int64            `json:"wireBytesKnown"`
	EncodedNetworkBodyBytes    int64            `json:"encodedNetworkBodyBytes"`
	BodyBytesConsumed          int64            `json:"bodyBytesConsumed"`
	RetainedBodyBytes          int64            `json:"retainedBodyBytes"`
	DecodedPixelWorkBytes      int64            `json:"decodedPixelWorkBytes"`
	KnownAvoidedBodyReadBytes  int64            `json:"knownAvoidedBodyReadBytes"`
	UnknownAvoidedBodyRequests int64            `json:"unknownAvoidedBodyRequests"`
	WouldBlock                 int64            `json:"wouldBlock"`
	WouldBypassCache           int64            `json:"wouldBypassCache"`
	BudgetDenied               int64            `json:"budgetDenied"`
	ByRule                     map[string]int64 `json:"byRule"`
}

type compiledResourcePolicy struct {
	config     ResourcePolicy
	generation uint64
	generated  []ResourceRule
}

// ResourcePolicyState belongs to one BrowserContext and is shared by its Page
// loaders. A request captures a pointer to one immutable generation.
type ResourcePolicyState struct {
	current      atomic.Pointer[compiledResourcePolicy]
	mu           sync.Mutex
	stats        ResourcePolicyStats
	active       int64
	bodyReserved int64
}

var errBodyBudget = errors.New("resource policy: body byte budget exceeded")
var errRetainedBudget = errors.New("resource policy: retained body byte budget exceeded")
var errDecodedBudget = errors.New("resource policy: decoded pixel byte budget exceeded")

type resourcePolicyBodyReader struct {
	state    *ResourcePolicyState
	policy   *compiledResourcePolicy
	source   io.Reader
	expected int64
	read     int64
}

func (r *resourcePolicyBodyReader) Read(dst []byte) (int, error) {
	if r.expected >= 0 && r.read >= r.expected {
		return 0, io.EOF
	}
	n, err := r.state.readBody(r.policy, r.source, dst)
	r.read += int64(n)
	return n, err
}

func ParseResourcePolicy(raw []byte) (ResourcePolicy, error) {
	var p ResourcePolicy
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return p, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return p, fmt.Errorf("multiple JSON values")
	}
	return p, p.Validate()
}

func (p ResourcePolicy) Validate() error {
	presetSeen := map[string]bool{}
	for _, preset := range p.Presets {
		if presetSeen[preset] {
			return fmt.Errorf("duplicate resource policy preset %q", preset)
		}
		presetSeen[preset] = true
		switch preset {
		case "noVisualAssets", "headersOnly", "dataExtraction", "noSpeculativeLoads":
		default:
			return fmt.Errorf("unknown resource policy preset %q", preset)
		}
	}
	seen := map[string]bool{}
	for i, rule := range p.Rules {
		if rule.ID == "" || seen[rule.ID] || strings.HasPrefix(rule.ID, "preset:") {
			return fmt.Errorf("rules[%d].id must be nonempty and unique", i)
		}
		seen[rule.ID] = true
		for _, kind := range rule.Match.Kinds {
			if !knownResourceKind(kind) {
				return fmt.Errorf("rules[%d].match.kinds: unknown kind %q", i, kind)
			}
		}
		for _, origin := range rule.Match.Origins {
			if _, err := parsePolicyOrigin(origin); err != nil {
				return fmt.Errorf("rules[%d].match.origins: %w", i, err)
			}
		}
		if rule.Match.URLGlob != "" {
			if _, err := path.Match(rule.Match.URLGlob, ""); err != nil {
				return fmt.Errorf("rules[%d].match.urlGlob: %w", i, err)
			}
		}
		switch rule.Work.Body {
		case "", "full", "none":
			if rule.Work.PrefixBytes != 0 {
				return fmt.Errorf("rules[%d].work.prefixBytes requires body=prefix", i)
			}
		case "prefix":
			if rule.Work.PrefixBytes <= 0 {
				return fmt.Errorf("rules[%d].work.prefixBytes must be positive", i)
			}
		default:
			return fmt.Errorf("rules[%d].work.body: unknown mode %q", i, rule.Work.Body)
		}
		if rule.Work.Decode != nil && *rule.Work.Decode && rule.Work.Body == "none" {
			return fmt.Errorf("rules[%d].work.decode requires body", i)
		}
	}
	for name, value := range map[string]int64{"maxRequests": p.Budgets.MaxRequests, "maxConcurrent": p.Budgets.MaxConcurrent, "maxWireBytes": p.Budgets.MaxWireBytes, "maxBodyBytes": p.Budgets.MaxBodyBytes, "maxResponseBytes": p.Budgets.MaxResponseBytes, "maxDecodedBytes": p.Budgets.MaxDecodedBytes, "maxRetainedBytes": p.Budgets.MaxRetainedBytes} {
		if value < 0 {
			return fmt.Errorf("budgets.%s must be nonnegative", name)
		}
	}
	for name, value := range map[string]int64{"maxWireBytes": p.Budgets.MaxWireBytes} {
		if value != 0 {
			return fmt.Errorf("budgets.%s is not yet supported", name)
		}
	}
	return nil
}

func knownResourceKind(kind string) bool {
	switch kind {
	case "document", "iframe", "script", "stylesheet", "image", "font", "media", "favicon", "preload", "fetch", "xhr", "worker", "websocket", "other":
		return true
	}
	return false
}

func ResourcePolicySchema() map[string]any {
	boolField := map[string]any{"type": "boolean"}
	positive := map[string]any{"type": "integer", "minimum": 1}
	nonnegative := map[string]any{"type": "integer", "minimum": 0}
	unsupportedBudget := map[string]any{"type": "integer", "minimum": 0, "x-mimic-supported": false}
	stringsField := map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
	return map[string]any{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type":    "object", "additionalProperties": false,
		"properties": map[string]any{
			"reportOnly": boolField,
			"presets":    map[string]any{"type": "array", "uniqueItems": true, "items": map[string]any{"enum": []string{"noVisualAssets", "headersOnly", "dataExtraction", "noSpeculativeLoads"}}},
			"rules": map[string]any{"type": "array", "items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"id"}, "properties": map[string]any{
				"id":    map[string]any{"type": "string", "minLength": 1},
				"match": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"kinds": stringsField, "hosts": stringsField, "origins": stringsField, "urlGlob": map[string]any{"type": "string"}, "owners": stringsField, "mechanisms": stringsField, "topLevelSite": map[string]any{"type": "string"}}},
				"work":  map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"cacheRead": boolField, "network": boolField, "body": map[string]any{"enum": []string{"full", "none", "prefix"}}, "prefixBytes": positive, "decode": boolField, "cacheRetain": boolField, "debugRetain": boolField}}}}},
			"budgets": map[string]any{"type": "object", "additionalProperties": false, "properties": map[string]any{"maxRequests": nonnegative, "maxConcurrent": nonnegative, "maxWireBytes": unsupportedBudget, "maxBodyBytes": nonnegative, "maxResponseBytes": nonnegative, "maxDecodedBytes": nonnegative, "maxRetainedBytes": nonnegative}},
		},
	}
}

func (s *ResourcePolicyState) Update(config ResourcePolicy) (uint64, error) {
	if err := config.Validate(); err != nil {
		return 0, err
	}
	// Copy caller-owned slices before publishing the immutable generation.
	raw, err := json.Marshal(config)
	if err != nil {
		return 0, err
	}
	var owned ResourcePolicy
	if err := json.Unmarshal(raw, &owned); err != nil {
		return 0, err
	}
	owned.Rules = append(owned.Rules, presetRules(owned.Presets)...)
	owned.Presets = nil
	s.mu.Lock()
	defer s.mu.Unlock()
	generation := s.stats.Generation + 1
	var generated []ResourceRule
	if previous := s.current.Load(); previous != nil {
		generated = previous.generated
	}
	s.current.Store(&compiledResourcePolicy{config: owned, generation: generation, generated: generated})
	s.stats.Generation = generation
	return generation, nil
}

func presetRules(names []string) []ResourceRule {
	var rules []ResourceRule
	no := false
	for _, name := range names {
		switch name {
		case "noVisualAssets", "dataExtraction":
			rules = append(rules, ResourceRule{ID: "preset:" + name, Match: ResourceMatch{Kinds: []string{"image", "font", "media", "favicon"}}, Work: ResourceWork{CacheRead: &no, Network: &no}})
		case "headersOnly":
			rules = append(rules, ResourceRule{ID: "preset:headersOnly", Work: ResourceWork{Body: "none"}})
		case "noSpeculativeLoads":
			rules = append(rules, ResourceRule{ID: "preset:noSpeculativeLoads", Match: ResourceMatch{Mechanisms: []string{"preload", "prefetch"}}, Work: ResourceWork{CacheRead: &no, Network: &no}})
		}
	}
	return rules
}

// ExpandedRules is the common manual/generated resource frontend. The caller
// owns the returned representation; installation clones it before publication.
func ExpandedRules(config ResourcePolicy) []ResourceRule {
	rules := append([]ResourceRule(nil), config.Rules...)
	rules = append(rules, presetRules(config.Presets)...)
	for index := range rules {
		rules[index].ID = fmt.Sprintf("generated:%d:%s", index, rules[index].ID)
	}
	return rules
}

func (s *ResourcePolicyState) beginAcquisition(p *compiledResourcePolicy) (func(), error) {
	s.mu.Lock()
	b := p.config.Budgets
	exceeded := b.MaxRequests > 0 && s.stats.NetworkAcquisitions >= b.MaxRequests || b.MaxConcurrent > 0 && s.active >= b.MaxConcurrent
	if exceeded {
		s.stats.BudgetDenied++
		if !p.config.ReportOnly {
			s.mu.Unlock()
			return nil, fmt.Errorf("resource policy: network request budget exceeded")
		}
	}
	s.stats.NetworkAcquisitions++
	s.active++
	s.mu.Unlock()
	return func() { s.mu.Lock(); s.active--; s.mu.Unlock() }, nil
}

func (s *ResourcePolicyState) recordBudgetExceeded() {
	s.mu.Lock()
	s.stats.BudgetDenied++
	s.mu.Unlock()
}

func (s *ResourcePolicyState) Capture() *compiledResourcePolicy { return s.current.Load() }

// InstallSpecialization compiles generated resource actions into the existing
// policy generation. Explicit manual matches win; omitted manual fields keep
// their ordinary meaning. Admission is captured separately by each Page loader.
func (s *ResourcePolicyState) InstallSpecialization(rules []ResourceRule) error {
	config := ResourcePolicy{Rules: rules}
	if err := config.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return err
	}
	var owned ResourcePolicy
	if err := json.Unmarshal(raw, &owned); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.current.Load()
	next := &compiledResourcePolicy{}
	if current != nil {
		*next = *current
	}
	next.generated = owned.Rules
	next.generation = s.stats.Generation + 1
	s.stats.Generation = next.generation
	s.current.Store(next)
	return nil
}

func (s *ResourcePolicyState) Policy() (ResourcePolicy, bool) {
	p := s.Capture()
	if p == nil {
		return ResourcePolicy{}, false
	}
	raw, _ := json.Marshal(p.config)
	var copy ResourcePolicy
	_ = json.Unmarshal(raw, &copy)
	return copy, true
}

func (s *ResourcePolicyState) Stats() ResourcePolicyStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.stats
	out.ByRule = make(map[string]int64, len(s.stats.ByRule))
	for k, v := range s.stats.ByRule {
		out.ByRule[k] = v
	}
	return out
}

func (p *compiledResourcePolicy) decide(r Request) ResourceDecision {
	d := ResourceDecision{Generation: p.generation, ReportOnly: p.config.ReportOnly}
	for _, rule := range p.config.Rules {
		if rule.Match.matches(r) {
			d.RuleID, d.Work = rule.ID, rule.Work
			return d
		}
	}
	if r.specializationAllowed {
		for _, rule := range p.generated {
			if rule.Match.matches(r) {
				d.RuleID, d.Work = rule.ID, rule.Work
				break
			}
		}
	}
	return d
}

func (m ResourceMatch) matches(r Request) bool {
	if r.URL == nil {
		return false
	}
	if len(m.Kinds) > 0 && !containsString(m.Kinds, r.ResourceKind()) {
		return false
	}
	if len(m.Owners) > 0 && !containsString(m.Owners, r.Owner) {
		return false
	}
	if len(m.Mechanisms) > 0 && !containsString(m.Mechanisms, r.Mechanism) {
		return false
	}
	if len(m.Hosts) > 0 {
		match := false
		for _, h := range m.Hosts {
			host := strings.ToLower(r.URL.Hostname())
			h = strings.ToLower(h)
			if host == h || strings.HasPrefix(h, ".") && strings.HasSuffix(host, h) {
				match = true
				break
			}
		}
		if !match {
			return false
		}
	}
	if len(m.Origins) > 0 {
		matched := false
		for _, value := range m.Origins {
			origin, _ := parsePolicyOrigin(value) // Validated before publication.
			if samePolicyOrigin(origin, r.URL) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	if m.URLGlob != "" {
		// path.Match does not let * cross slashes. Match against the entire URL
		// after replacing slash with a non-separator rune.
		glob := strings.ReplaceAll(m.URLGlob, "/", "\x00")
		value := strings.ReplaceAll(r.URL.String(), "/", "\x00")
		ok, _ := path.Match(glob, value)
		if !ok {
			return false
		}
	}
	if m.TopLevelSite != "" {
		if r.TopLevelURL == nil {
			return false
		}
		if strings.Contains(m.TopLevelSite, "://") {
			if !strings.EqualFold(SchemefulSite(r.TopLevelURL), m.TopLevelSite) {
				return false
			}
		} else if !strings.EqualFold(r.TopLevelURL.Hostname(), m.TopLevelSite) {
			return false
		}
	}
	return true
}

func containsString(values []string, value string) bool {
	for _, item := range values {
		if item == value {
			return true
		}
	}
	return false
}

func parsePolicyOrigin(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, fmt.Errorf("invalid HTTP origin %q", raw)
	}
	return u, nil
}

func samePolicyOrigin(a, b *url.URL) bool {
	if a == nil || b == nil || !strings.EqualFold(a.Scheme, b.Scheme) || !strings.EqualFold(a.Hostname(), b.Hostname()) {
		return false
	}
	port := func(u *url.URL) string {
		if p := u.Port(); p != "" {
			return p
		}
		if strings.EqualFold(u.Scheme, "https") {
			return "443"
		}
		return "80"
	}
	return port(a) == port(b)
}

func (r Request) ResourceKind() string {
	if r.Kind != "" {
		return r.Kind
	}
	switch r.Initiator {
	case Navigation:
		return "document"
	case Iframe:
		return "iframe"
	case Script:
		return "script"
	case Stylesheet:
		return "stylesheet"
	case Image:
		return "image"
	case Worker:
		return "worker"
	case Fetch:
		return "fetch"
	case XHR:
		return "xhr"
	default:
		return "other"
	}
}

func (s *ResourcePolicyState) recordDecision(d ResourceDecision, blocked bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.Requests++
	if d.RuleID != "" {
		if s.stats.ByRule == nil {
			s.stats.ByRule = map[string]int64{}
		}
		s.stats.ByRule[d.RuleID]++
	}
	if blocked {
		if d.ReportOnly {
			s.stats.WouldBlock++
		} else {
			s.stats.Blocked++
		}
	}
}

func (s *ResourcePolicyState) recordBlocked(reportOnly bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if reportOnly {
		s.stats.WouldBlock++
	} else {
		s.stats.Blocked++
	}
}

func (s *ResourcePolicyState) recordUnknownAvoidance() {
	s.mu.Lock()
	s.stats.UnknownAvoidedBodyRequests++
	s.mu.Unlock()
}

func (s *ResourcePolicyState) recordKnownAvoidance(bytes int64) {
	if bytes <= 0 {
		return
	}
	s.mu.Lock()
	s.stats.KnownAvoidedBodyReadBytes += bytes
	s.mu.Unlock()
}

func (s *ResourcePolicyState) recordBody(consumed, wire int64, mode string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.BodyBytesConsumed += consumed
	s.stats.EncodedNetworkBodyBytes += wire
	if mode == "none" {
		s.stats.HeaderOnly++
	}
	if mode == "prefix" {
		s.stats.Prefixes++
	}
}

// readBody charges actual bytes returned by Read, not the caller's buffer
// capacity. Admission is reserved before I/O so concurrent Pages cannot exceed
// the Context limit; unused reservation is returned even on a short read.
func (s *ResourcePolicyState) readBody(p *compiledResourcePolicy, source io.Reader, dst []byte) (int, error) {
	if p == nil {
		return source.Read(dst)
	}
	limit := p.config.Budgets.MaxBodyBytes
	if limit == 0 || p.config.ReportOnly {
		n, err := source.Read(dst)
		s.mu.Lock()
		s.stats.BodyBytesConsumed += int64(n)
		if limit > 0 && s.stats.BodyBytesConsumed-int64(n) <= limit && s.stats.BodyBytesConsumed > limit {
			s.stats.BudgetDenied++
		}
		s.mu.Unlock()
		return n, err
	}
	s.mu.Lock()
	remaining := limit - s.stats.BodyBytesConsumed - s.bodyReserved
	if remaining <= 0 {
		s.stats.BudgetDenied++
		s.mu.Unlock()
		return 0, errBodyBudget
	}
	if int64(len(dst)) > remaining {
		dst = dst[:remaining]
	}
	s.bodyReserved += int64(len(dst))
	s.mu.Unlock()
	n, err := source.Read(dst)
	s.mu.Lock()
	s.bodyReserved -= int64(len(dst))
	s.stats.BodyBytesConsumed += int64(n)
	s.mu.Unlock()
	return n, err
}

func (s *ResourcePolicyState) consumeCachedBody(p *compiledResourcePolicy, bytes int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	limit := p.config.Budgets.MaxBodyBytes
	if limit > 0 && bytes > limit-s.stats.BodyBytesConsumed-s.bodyReserved {
		s.stats.BudgetDenied++
		if !p.config.ReportOnly {
			return errBodyBudget
		}
	}
	s.stats.BodyBytesConsumed += bytes
	return nil
}

func (s *ResourcePolicyState) reserveRetained(p *compiledResourcePolicy, bytes int64) error {
	if p == nil || bytes <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stats.RetainedBodyBytes += bytes
	return nil
}

func (s *ResourcePolicyState) releaseRetained(bytes int64) {
	s.mu.Lock()
	s.stats.RetainedBodyBytes -= bytes
	s.mu.Unlock()
}

func (s *ResourcePolicyState) reserveDecoded(p *compiledResourcePolicy, bytes int64) error {
	if p == nil || bytes <= 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	limit := p.config.Budgets.MaxDecodedBytes
	if limit > 0 && bytes > limit-s.stats.DecodedPixelWorkBytes {
		s.stats.BudgetDenied++
		if !p.config.ReportOnly {
			return errDecodedBudget
		}
	}
	s.stats.DecodedPixelWorkBytes += bytes
	return nil
}

func (s *ResourcePolicyState) recordCache(hit bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if hit {
		s.stats.CacheHits++
	} else {
		s.stats.NetworkAcquisitions++
	}
}

func (s *ResourcePolicyState) recordWouldBypassCache() {
	s.mu.Lock()
	s.stats.WouldBypassCache++
	s.mu.Unlock()
}
