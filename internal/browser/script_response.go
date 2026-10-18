package browser

import (
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"github.com/moreveal/mimic/internal/network"
	"strings"
)

// A fetched HTTP response is still observable through Fetch even when its
// status is unsuccessful. Executable resource consumers reject those bodies
// before evaluating them; transport success alone does not make a script.
func scriptResponseError(response network.Response, integrity ...string) error {
	if response.Status < 200 || response.Status > 299 {
		return fmt.Errorf("script resource returned HTTP %d", response.Status)
	}
	if len(integrity) > 0 {
		return scriptIntegrityError(response.Body, integrity[0])
	}
	return nil
}

// SRI accepts any matching digest at the strongest supported level. Unknown
// algorithms do not prevent loading; a recognized but mismatched digest does.
func scriptIntegrityError(body []byte, metadata string) error {
	strongest := 0
	var expected []string
	for _, token := range strings.Fields(metadata) {
		algorithmAndDigest, _, _ := strings.Cut(token, "?")
		algorithm, digest, found := strings.Cut(algorithmAndDigest, "-")
		if !found {
			continue
		}
		strength := 0
		switch algorithm {
		case "sha256":
			strength = 1
		case "sha384":
			strength = 2
		case "sha512":
			strength = 3
		default:
			continue
		}
		if strength > strongest {
			strongest = strength
			expected = expected[:0]
		}
		if strength == strongest {
			expected = append(expected, digest)
		}
	}
	if strongest == 0 {
		return nil
	}
	var actual string
	switch strongest {
	case 1:
		sum := sha256.Sum256(body)
		actual = base64.StdEncoding.EncodeToString(sum[:])
	case 2:
		sum := sha512.Sum384(body)
		actual = base64.StdEncoding.EncodeToString(sum[:])
	case 3:
		sum := sha512.Sum512(body)
		actual = base64.StdEncoding.EncodeToString(sum[:])
	}
	for _, digest := range expected {
		if digest == actual {
			return nil
		}
	}
	return fmt.Errorf("script resource failed integrity verification")
}

func (r *Realm) dispatchResourceEvent(ctx context.Context, nodeID int64, eventType string) error {
	if deferred, ok := r.runtime.(*deferredRuntime); ok {
		if _, err := deferred.ready(); err != nil {
			return err
		}
	}
	if r.resourceEventDispatcher == nil {
		return nil
	}
	_, err := r.runtime.Call(ctx, r.resourceEventDispatcher, nil, r.val(nodeID), r.val(eventType))
	return err
}
