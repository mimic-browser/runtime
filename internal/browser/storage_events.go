package browser

import (
	"context"

	"github.com/moreveal/mimic/internal/scheduler"
)

func (r *Realm) notifyStorage(area string, key, oldValue, newValue any) {
	sourcePage := r.agent.Page()
	row := map[string]any{"area": area, "key": key, "oldValue": oldValue, "newValue": newValue, "url": r.documentURL().String()}
	for _, page := range sourcePage.ctx.Pages() {
		if area == "session" && page != sourcePage {
			continue
		}
		page.mu.RLock()
		recipients := make([]*Realm, 0, len(page.realmOwners))
		for _, owner := range page.realmOwners {
			if owner.agent != r.agent && owner.origin == r.origin {
				recipients = append(recipients, owner)
			}
		}
		page.mu.RUnlock()
		for _, recipient := range recipients {
			recipient.scheduler.Post(scheduler.Source("storage"), 0, func(ctx context.Context) error {
				if recipient.closed || recipient.inactive || recipient.storageNotifier == nil {
					return nil
				}
				_, err := recipient.runtime.Call(ctx, recipient.storageNotifier, nil, recipient.val(row))
				return err
			})
		}
	}
}
