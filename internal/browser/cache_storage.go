package browser

import (
	"encoding/json"
	"sort"

	"github.com/moreveal/mimic/internal/engine"
)

// Named buckets are shared by an origin in a Context. Open handles retain a
// deleted bucket only for their realm lifetime, exactly like unlinked storage.
// Records cross the binding as immutable snapshots; compare-and-swap commits
// preserve atomic batches across independently executing Pages.
type cacheBucket struct {
	id      uint64
	version uint64
	records string
	usage   int64
}

func protobufVarintSize(value int64) int64 {
	if value < 0 {
		return 10
	}
	size := int64(1)
	for value >= 1<<7 {
		value >>= 7
		size++
	}
	return size
}

func protobufBytesSize(field int64, bytes int64) int64 {
	return protobufVarintSize(field<<3|2) + protobufVarintSize(bytes) + bytes
}

func cacheHeaderSize(field int64, header []string) int64 {
	if len(header) < 2 {
		return 0
	}
	payload := protobufBytesSize(1, int64(len(header[0]))) + protobufBytesSize(2, int64(len(header[1])))
	return protobufBytesSize(field, payload)
}

// cacheStorageUsage accounts the canonical request/response content rather
// than the JSON transport used between the realm and Context. Chromium's simple
// disk-cache backend reports the entry key, response body and serialized
// CacheMetadata protobuf. The fixed payload sizes below are the fields Chromium
// writes for an ordinary synthetic Request/Response pair; variable strings and
// nested header messages retain protobuf length-prefix boundaries.
func cacheStorageUsage(records string) int64 {
	var entries []struct {
		Request struct {
			URL     string     `json:"url"`
			Method  string     `json:"method"`
			Headers [][]string `json:"headers"`
		} `json:"request"`
		Response struct {
			Status     int        `json:"status"`
			StatusText string     `json:"statusText"`
			Headers    [][]string `json:"headers"`
			Body       []byte     `json:"body"`
		} `json:"response"`
	}
	if err := json.Unmarshal([]byte(records), &entries); err != nil {
		return 0
	}
	var total int64
	for _, entry := range entries {
		// CacheRequest contains method plus the two navigation flags.
		requestPayload := int64(9)
		if entry.Request.Method != "GET" {
			requestPayload += int64(len(entry.Request.Method) - len("GET"))
		}
		for _, header := range entry.Request.Headers {
			requestPayload += cacheHeaderSize(2, header)
		}

		// CacheResponse's fixed fields occupy 36 bytes for status 200..599,
		// default response type, empty protocol strings and ordinary credentials.
		responsePayload := int64(36 + len(entry.Response.StatusText))
		contentType := ""
		for _, header := range entry.Response.Headers {
			responsePayload += cacheHeaderSize(4, header)
			if len(header) >= 2 && header[0] == "content-type" {
				contentType = header[1]
			}
		}
		if contentType != "" {
			responsePayload += protobufBytesSize(13, int64(len(contentType)))
		}

		metadata := protobufBytesSize(1, requestPayload) + protobufBytesSize(2, responsePayload) + 10 // entry_time
		total += int64(len(entry.Request.URL)+len(entry.Response.Body)) + metadata
	}
	return total
}

func (c *Context) cacheUsageLocked(origin string) int64 {
	var usage int64
	for _, bucket := range c.cacheNames[origin] {
		usage += bucket.usage
	}
	return usage
}

func addCacheHosts(r *Realm, h map[string]any) {
	h["cacheStorage"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		c := r.agent.Page().ctx
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.cacheNames == nil {
			c.cacheNames = map[string]map[string]*cacheBucket{}
		}
		names := c.cacheNames[r.origin]
		if names == nil {
			names = map[string]*cacheBucket{}
			c.cacheNames[r.origin] = names
		}
		switch strarg(a, 0) {
		case "open":
			name := strarg(a, 1)
			b := names[name]
			if b == nil {
				c.cacheSequence++
				b = &cacheBucket{id: c.cacheSequence, records: "[]"}
				names[name] = b
			}
			if r.cacheHandles == nil {
				r.cacheHandles = map[uint64]*cacheBucket{}
			}
			r.cacheHandles[b.id] = b
			return r.val(b.id), nil
		case "keys":
			keys := make([]string, 0, len(names))
			for key := range names {
				keys = append(keys, key)
			}
			sort.Slice(keys, func(i, j int) bool { return names[keys[i]].id < names[keys[j]].id })
			return r.val(keys), nil
		case "has":
			return r.val(names[strarg(a, 1)] != nil), nil
		case "delete":
			name := strarg(a, 1)
			existed := names[name] != nil
			delete(names, name)
			return r.val(existed), nil
		case "read", "write":
			b := r.cacheHandles[uint64(numarg(a, 1))]
			if b == nil {
				return nil, nil
			}
			if strarg(a, 0) == "read" {
				return r.val(map[string]any{"version": b.version, "records": b.records}), nil
			}
			if b.version != uint64(numarg(a, 2)) {
				return r.val(false), nil
			}
			b.records = strarg(a, 3)
			b.usage = cacheStorageUsage(b.records)
			b.version++
			return r.val(true), nil
		}
		return nil, nil
	})
}
