//go:build (windows || linux) && amd64

package v8

import (
	"crypto/sha256"
	"sync"
	"unsafe"

	gov8 "github.com/maclof/gov8"
)

type bootstrapKey struct {
	source [32]byte
	name   string
}

func bootstrapKeyFor(source, name string) bootstrapKey {
	// Sum256 only reads its input before returning. A string's immutable backing
	// bytes can be borrowed for this synchronous hash without copying a large
	// bootstrap script on every realm creation.
	bytes := unsafe.Slice(unsafe.StringData(source), len(source))
	return bootstrapKey{sha256.Sum256(bytes), name}
}

// Immutable serialized code contains no host closures, realm objects, or V8
// handles. Bound the process cache; different compatibility bundles and profile
// variants must not retain unbounded source/code. Compilation never holds mu.
type bootstrapCache struct {
	mu      sync.Mutex
	entries []bootstrapEntry
	bytes   int
}

type bootstrapEntry struct {
	key  bootstrapKey
	data *gov8.FunctionCodeCache
}

var bootstrapCode bootstrapCache

const bootstrapCacheBytes = 32 << 20

// Realm installation itself uses several small bootstrap scripts. Four slots
// evicted the expensive Worker graph before the next Page could consume it.
// Keep enough entries for platform helpers while retaining the byte budget.
const bootstrapCacheEntries = 16

func (c *bootstrapCache) get(key bootstrapKey) *gov8.FunctionCodeCache {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, e := range c.entries {
		if e.key == key {
			copy(c.entries[1:i+1], c.entries[:i])
			c.entries[0] = e
			return e.data
		}
	}
	return nil
}

func (c *bootstrapCache) put(key bootstrapKey, data *gov8.FunctionCodeCache) {
	if data.Len() == 0 || data.Len() > bootstrapCacheBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, e := range c.entries {
		if e.key == key {
			return
		}
	}
	for len(c.entries) >= bootstrapCacheEntries || c.bytes+data.Len() > bootstrapCacheBytes {
		last := len(c.entries) - 1
		c.bytes -= c.entries[last].data.Len()
		c.entries[last] = bootstrapEntry{}
		c.entries = c.entries[:last]
	}
	c.entries = append(c.entries, bootstrapEntry{})
	copy(c.entries[1:], c.entries[:len(c.entries)-1])
	c.entries[0] = bootstrapEntry{key, data}
	c.bytes += data.Len()
}
