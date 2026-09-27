package network

import (
	"errors"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SessionState owns network resources shared by one browser context. Request
// overrides belong to each Page's Loader, not to this shared resource store.
type SessionState struct {
	mu             sync.RWMutex
	responseBodies *bodyStore
	closed         bool
	cache          map[string][]cacheEntry
	clientHints    map[string]map[string]bool
	blobs          map[string]blobEntry
	connections    map[string]ConnectionRecord
}
type blobEntry struct {
	body        []byte
	contentType string
	owner       string
	mediaSource MediaSourceResource
}

// MediaSourceResource distinguishes attachment resources from readable Blob
// bytes. The browser owns the source state; this store only retains its identity
// under an object URL, just as it retains Blob bytes under a Blob object URL.
type MediaSourceResource interface {
	MediaSourceID() string
}
type cacheEntry struct {
	response       Response
	expires        time.Time
	vary           []string
	requestHeaders http.Header
}
type ConnectionStatus string

const (
	ConnectionCold        ConnectionStatus = "cold"
	ConnectionConnecting  ConnectionStatus = "connecting"
	ConnectionEstablished ConnectionStatus = "established"
	ConnectionReusable    ConnectionStatus = "reusable"
	ConnectionClosed      ConnectionStatus = "closed"
)

type ConnectionRecord struct {
	Key            string
	Origin         string
	Status         ConnectionStatus
	ConnectionID   string
	Protocol       string
	Reused         bool
	LastTransition time.Time
}

type ConnectionAttempt struct {
	Key, Origin string
	Cold        bool
}

func NewSessionState() *SessionState {
	return &SessionState{responseBodies: newBodyStore(), cache: map[string][]cacheEntry{}, clientHints: map[string]map[string]bool{}, blobs: map[string]blobEntry{}, connections: map[string]ConnectionRecord{}}
}

func connectionKey(u *url.URL) (string, string) {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", ""
	}
	origin := u.Scheme + "://" + u.Host
	return origin, origin
}

// BeginConnection is the single browser-session connection lookup used by all
// resource consumers. The key can later grow to include proxy, credentials and
// network-isolation keys without changing those consumers.
func (s *SessionState) BeginConnection(u *url.URL) ConnectionAttempt {
	key, origin := connectionKey(u)
	if key == "" {
		return ConnectionAttempt{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	record, exists := s.connections[key]
	cold := !exists || record.Status == ConnectionCold || record.Status == ConnectionClosed
	s.connections[key] = ConnectionRecord{Key: key, Origin: origin, Status: ConnectionConnecting, ConnectionID: record.ConnectionID, Protocol: record.Protocol, Reused: !cold, LastTransition: time.Now()}
	return ConnectionAttempt{Key: key, Origin: origin, Cold: cold}
}

func (s *SessionState) CompleteConnection(attempt ConnectionAttempt, timing TransportTimingSnapshot, protocol string, closed bool) ConnectionRecord {
	if attempt.Key == "" {
		return ConnectionRecord{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	status := ConnectionReusable
	if closed {
		status = ConnectionClosed
	} else if !timing.ReuseKnown {
		status = ConnectionEstablished
	}
	record := ConnectionRecord{Key: attempt.Key, Origin: attempt.Origin, Status: status, ConnectionID: timing.ConnectionID, Protocol: protocol, Reused: timing.Reused, LastTransition: time.Now()}
	s.connections[attempt.Key] = record
	return record
}

func (s *SessionState) Connections() []ConnectionRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]ConnectionRecord, 0, len(s.connections))
	for _, record := range s.connections {
		out = append(out, record)
	}
	return out
}

func (s *SessionState) PutBlob(raw string, body []byte, contentType, owner string) {
	s.mu.Lock()
	s.blobs[raw] = blobEntry{body: append([]byte(nil), body...), contentType: contentType, owner: owner}
	s.mu.Unlock()
}

// Object URLs belong to the creating execution context, independently of
// whether their Blob objects remain reachable in another realm.
func (s *SessionState) RevokeBlobsForOwner(owner string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for raw, entry := range s.blobs {
		if entry.owner == owner {
			delete(s.blobs, raw)
		}
	}
}

func (s *SessionState) RevokeBlob(raw string) {
	s.mu.Lock()
	delete(s.blobs, raw)
	s.mu.Unlock()
}

func (s *SessionState) Blob(raw string) ([]byte, string, bool) {
	s.mu.RLock()
	entry, ok := s.blobs[raw]
	s.mu.RUnlock()
	return append([]byte(nil), entry.body...), entry.contentType, ok && entry.mediaSource == nil
}

func (s *SessionState) PutMediaSourceURL(raw, owner string, source MediaSourceResource) {
	s.mu.Lock()
	s.blobs[raw] = blobEntry{owner: owner, mediaSource: source}
	s.mu.Unlock()
}

func (s *SessionState) MediaSourceURL(raw string) MediaSourceResource {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.blobs[raw].mediaSource
}
func (s *SessionState) ClearCache() {
	s.mu.Lock()
	cache := s.cache
	s.cache = map[string][]cacheEntry{}
	s.mu.Unlock()
	for _, variants := range cache {
		for _, entry := range variants {
			entry.response.sharedBody.release()
		}
	}
}

// Close follows Page teardown and drops the Context's remaining storage owners.
func (s *SessionState) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.ClearCache()
	s.responseBodies.close()
	s.mu.Lock()
	s.blobs = map[string]blobEntry{}
	s.mu.Unlock()
}
func (s *SessionState) AcceptClientHints(u *url.URL, header string) {
	if u == nil || header == "" {
		return
	}
	origin := u.Scheme + "://" + u.Host
	s.mu.Lock()
	hints := map[string]bool{}
	for _, name := range strings.Split(header, ",") {
		hints[strings.ToLower(strings.TrimSpace(name))] = true
	}
	s.clientHints[origin] = hints
	s.mu.Unlock()
}
func (s *SessionState) ClientHints(u *url.URL) map[string]bool {
	if u == nil {
		return nil
	}
	origin := u.Scheme + "://" + u.Host
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string]bool{}
	for name, enabled := range s.clientHints[origin] {
		out[name] = enabled
	}
	return out
}
func (s *SessionState) GetCached(req Request, now time.Time) (Response, bool) {
	res, found, err := s.getCached(req, now)
	res.sharedBody = nil
	return res, found && err == nil
}

func (s *SessionState) getCached(req Request, now time.Time) (Response, bool, error) {
	return s.getCachedLimited(req, now, -1)
}

func (s *SessionState) getCachedLimited(req Request, now time.Time, limit int64) (Response, bool, error) {
	if req.Method != http.MethodGet {
		return Response{}, false, nil
	}
	s.mu.RLock()
	var response Response
	found := false
	for _, e := range s.cache[req.URL.String()] {
		if now.After(e.expires) {
			continue
		}
		match := true
		for _, k := range e.vary {
			if req.Headers.Get(k) != e.requestHeaders.Get(k) {
				match = false
				break
			}
		}
		if match {
			response = e.response
			found = response.sharedBody.retain()
			break
		}
	}
	s.mu.RUnlock()
	if !found {
		return Response{}, false, nil
	}
	defer response.sharedBody.release()
	var err error
	response.Body, err = response.sharedBody.copyPrefix(limit)
	if err != nil {
		return Response{}, false, err
	}
	if limit >= 0 && int64(len(response.Body)) < response.sharedBody.size {
		response.Partial = true
	}
	response.Headers = response.Headers.Clone()
	return response, true, nil
}
func (s *SessionState) PutCached(req Request, res Response, now time.Time) error {
	// Callers may have mutated their public response since it was returned.
	res.sharedBody = nil
	_, err := s.putCached(req, res, now)
	return err
}

func (s *SessionState) putCached(req Request, res Response, now time.Time) (Response, error) {
	if req.Method != http.MethodGet || res.Status != http.StatusOK {
		return res, nil
	}
	maxAge := cacheFreshnessRemaining(res.Headers, now)
	if maxAge <= 0 {
		return res, nil
	}
	vary := []string{}
	for _, v := range strings.Split(res.Headers.Get("Vary"), ",") {
		v = strings.TrimSpace(v)
		if v == "*" {
			return res, nil
		}
		if v != "" {
			vary = append(vary, http.CanonicalHeaderKey(v))
		}
	}
	slices.Sort(vary)
	vary = slices.Compact(vary)
	body, err := s.responseBodies.retainResponse(res)
	if err != nil {
		return res, err
	}
	copy := res
	copy.Body, copy.sharedBody = nil, body
	copy.Headers = res.Headers.Clone()
	var dropped []*storedBody
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		body.release()
		return res, errors.New("network session is closed")
	}
	// A fresh representation replaces the same Vary variant. Keep different
	// variants, but never return an older matching response ahead of its update.
	key := req.URL.String()
	entries := s.cache[key]
	kept := entries[:0]
	for _, entry := range entries {
		if now.After(entry.expires) || !slices.Equal(entry.vary, vary) {
			dropped = append(dropped, entry.response.sharedBody)
			continue
		}
		match := true
		for _, name := range entry.vary {
			if req.Headers.Get(name) != entry.requestHeaders.Get(name) {
				match = false
				break
			}
		}
		if !match {
			kept = append(kept, entry)
		} else {
			dropped = append(dropped, entry.response.sharedBody)
		}
	}
	clear(entries[len(kept):])
	s.cache[key] = append(kept, cacheEntry{copy, now.Add(maxAge), vary, req.Headers.Clone()})
	res.sharedBody = body
	s.mu.Unlock()
	for _, body := range dropped {
		body.release()
	}
	return res, nil
}

// Freshness is measured from the response's origin date, including intermediary
// Age. Without an explicit lifetime Chrome uses 10% of the time since the last
// modification. Never turn an explicit zero lifetime into a heuristic hit.
func cacheFreshnessRemaining(headers http.Header, now time.Time) time.Duration {
	var lifetime time.Duration
	explicit := false
	for _, part := range strings.Split(headers.Get("Cache-Control"), ",") {
		key, value, _ := strings.Cut(strings.TrimSpace(part), "=")
		switch strings.ToLower(key) {
		case "no-store", "no-cache":
			return 0
		case "max-age":
			explicit = true
			seconds, err := strconv.ParseInt(strings.Trim(value, "\" "), 10, 64)
			if err != nil || seconds < 0 {
				return 0
			}
			// Avoid duration overflow on untrusted response headers.
			if seconds > int64((time.Duration(1<<63-1))/time.Second) {
				seconds = int64((time.Duration(1<<63 - 1)) / time.Second)
			}
			lifetime = time.Duration(seconds) * time.Second
		}
	}
	date, err := http.ParseTime(headers.Get("Date"))
	if err != nil {
		date = now
	}
	if !explicit {
		if expires := headers.Get("Expires"); expires != "" {
			explicit = true
			end, err := http.ParseTime(expires)
			if err != nil {
				return 0
			}
			lifetime = end.Sub(date)
		}
	}
	if !explicit {
		modified, err := http.ParseTime(headers.Get("Last-Modified"))
		if err != nil || !modified.Before(date) {
			return 0
		}
		lifetime = date.Sub(modified) / 10
	}
	age := max(time.Duration(0), now.Sub(date))
	if seconds, err := strconv.ParseInt(headers.Get("Age"), 10, 64); err == nil && seconds > 0 {
		if seconds > int64((time.Duration(1<<63-1))/time.Second) {
			return 0
		}
		age = max(age, time.Duration(seconds)*time.Second)
	}
	if lifetime <= age {
		return 0
	}
	return lifetime - age
}
