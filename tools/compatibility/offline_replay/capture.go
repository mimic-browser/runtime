package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/moreveal/mimic/internal/trace"
)

type recordedRequest struct {
	ID, URL, Method, Context, Initiator, PostData string
	Headers                                       map[string]string
	Sequence                                      uint64
}

type fixture struct {
	Index                int               `json:"index"`
	Request              recordedRequest   `json:"request"`
	Status               int               `json:"status,omitempty"`
	Headers              map[string]string `json:"headers,omitempty"`
	Body                 []byte            `json:"-"`
	Source, SourceSHA256 string
	Failure              string `json:"failure,omitempty"`
	Local                string `json:"local,omitempty"`
	Cycle                int    `json:"cycle"`
	// A Critical-CH retry has two request observations but only its final
	// response body is captured. The duplicate response is an explicit replay
	// inference, not another recorded network response.
	UnavailableBody   bool `json:"unavailableBody,omitempty"`
	CriticalRetryCopy bool `json:"criticalRetryCopy,omitempty"`
	// Data URL loads can emit only a local response. No wire method or
	// request headers are inferred from that response.
	ResponseOnlyLocal bool `json:"responseOnlyLocal,omitempty"`
}

type capture struct {
	CapturedAt              time.Time
	Fixtures                []*fixture
	DocumentURL, TopContext string
	SourceHashes            map[string]string
}

func digest(b []byte) string    { sum := sha256.Sum256(b); return hex.EncodeToString(sum[:]) }
func shortHash(s string) string { return digest([]byte(s))[:16] }

func readCapture(dir string) (*capture, error) {
	data, err := os.ReadFile(filepath.Join(dir, "trace.json"))
	if err != nil {
		return nil, err
	}
	var envelope struct {
		Result struct{ Events []trace.Event }
	}
	if err = json.Unmarshal(data, &envelope); err != nil {
		return nil, err
	}
	c := &capture{SourceHashes: map[string]string{"trace.json": digest(data)}}
	requests := map[string][]recordedRequest{}
	retries := map[string]int{}
	responseIndex := 0
	for _, event := range envelope.Result.Events {
		if c.CapturedAt.IsZero() && !event.Time.IsZero() {
			c.CapturedAt = event.Time
		}
		if event.Kind != trace.Network {
			continue
		}
		d := event.Data
		id := stringField(d, "id")
		switch event.Name {
		case "request":
			r := recordedRequest{ID: id, URL: stringField(d, "url"), Method: stringField(d, "method"), Context: stringField(d, "context"), Initiator: stringField(d, "initiator"), PostData: stringField(d, "postData"), Headers: stringMap(d["headers"]), Sequence: event.Sequence}
			requests[id] = append(requests[id], r)
			if c.DocumentURL == "" && r.Initiator == "navigation" {
				c.DocumentURL = r.URL
				c.TopContext = r.Context
			}
		case "criticalClientHintsRestart":
			retries[id]++
		case "response", "failed":
			rrs := requests[id]
			responseOnly := false
			if len(rrs) == 0 {
				local, ok := responseOnlyDataRequest(event)
				if !ok {
					return nil, fmt.Errorf("network outcome %d has no request", event.Sequence)
				}
				rrs = []recordedRequest{local}
				responseOnly = true
			}
			f := &fixture{Index: -1, Request: rrs[len(rrs)-1], ResponseOnlyLocal: responseOnly}
			if event.Name == "failed" {
				f.Failure = stringField(d, "error")
			} else {
				f.Index = responseIndex
				responseIndex++
				f.Source = "root-" + id + ".json"
				bodyFile, readErr := os.ReadFile(filepath.Join(dir, f.Source))
				if readErr != nil {
					return nil, readErr
				}
				f.SourceSHA256 = digest(bodyFile)
				c.SourceHashes[f.Source] = f.SourceSHA256
				var saved struct {
					Response struct {
						Status  int
						Headers map[string]string
						URL     string
					}
					Result *struct {
						Body          string
						Base64Encoded bool
					}
				}
				if err = json.Unmarshal(bodyFile, &saved); err != nil {
					return nil, err
				}
				if saved.Response.URL != f.Request.URL {
					return nil, fmt.Errorf("response %d URL does not match request", f.Index)
				}
				f.Status = saved.Response.Status
				f.Headers = saved.Response.Headers
				if saved.Result == nil {
					if !boolField(d, "synthetic") {
						return nil, fmt.Errorf("response %d has no saved body", f.Index)
					}
					f.UnavailableBody = true
				}
				if saved.Result != nil {
					f.Body = []byte(saved.Result.Body)
					if saved.Result.Base64Encoded {
						f.Body, err = base64.StdEncoding.DecodeString(saved.Result.Body)
						if err != nil {
							return nil, err
						}
					}
				}
				if boolField(d, "fromCache") {
					f.Local = "cache"
				} else if boolField(d, "synthetic") {
					f.Local = "synthetic"
				}
			}
			if len(rrs) > 1 {
				if len(rrs) != retries[id]+1 || f.Failure != "" || f.Local != "" {
					return nil, fmt.Errorf("request %s has unsupported duplicate/redirect topology", shortHash(id))
				}
				for _, r := range rrs[:len(rrs)-1] {
					copy := *f
					copy.Request = r
					copy.CriticalRetryCopy = true
					c.Fixtures = append(c.Fixtures, &copy)
				}
			}
			c.Fixtures = append(c.Fixtures, f)
		}
	}
	if c.DocumentURL == "" {
		return nil, fmt.Errorf("capture has no top navigation")
	}
	sort.SliceStable(c.Fixtures, func(i, j int) bool { return c.Fixtures[i].Request.Sequence < c.Fixtures[j].Request.Sequence })
	cycle := 0
	for _, f := range c.Fixtures {
		if f.Request.Context == c.TopContext && f.Request.Initiator == "navigation" && !f.CriticalRetryCopy {
			cycle++
		}
		f.Cycle = cycle
		if f.CriticalRetryCopy {
			f.Cycle = cycle + 1
		}
	}
	return c, nil
}

func responseOnlyDataRequest(event trace.Event) (recordedRequest, bool) {
	d := event.Data
	u, err := url.Parse(stringField(d, "url"))
	if event.Name != "response" || !boolField(d, "synthetic") || err != nil || u.Scheme != "data" || stringField(d, "id") == "" || stringField(d, "context") == "" || stringField(d, "initiator") == "" {
		return recordedRequest{}, false
	}
	return recordedRequest{ID: stringField(d, "id"), URL: u.String(), Context: stringField(d, "context"), Initiator: stringField(d, "initiator"), Sequence: event.Sequence}, true
}

func stringField(m map[string]any, k string) string {
	if m[k] == nil {
		return ""
	}
	if v, ok := m[k].(string); ok {
		return v
	}
	return fmt.Sprint(m[k])
}
func boolField(m map[string]any, k string) bool { v, _ := m[k].(bool); return v }
func stringMap(v any) map[string]string {
	result := map[string]string{}
	if m, ok := v.(map[string]any); ok {
		for k, v := range m {
			result[strings.ToLower(k)] = fmt.Sprint(v)
		}
	}
	if m, ok := v.(map[string]string); ok {
		for k, v := range m {
			result[strings.ToLower(k)] = v
		}
	}
	return result
}
