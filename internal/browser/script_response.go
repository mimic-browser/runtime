package browser

import (
	"context"
	"fmt"
	"github.com/moreveal/mimic/internal/network"
)

// A fetched HTTP response is still observable through Fetch even when its
// status is unsuccessful. Executable resource consumers reject those bodies
// before evaluating them; transport success alone does not make a script.
func scriptResponseError(response network.Response) error {
	if response.Status < 200 || response.Status > 299 {
		return fmt.Errorf("script resource returned HTTP %d", response.Status)
	}
	return nil
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
