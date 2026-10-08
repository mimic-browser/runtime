package browser

import (
	"context"

	"github.com/moreveal/mimic/internal/scheduler"
)

// StopLoading may be called by a debugger control command outside the Page
// lock. Only cancellation is immediate; document state changes on its loop.
func (p *Page) StopLoading() {
	p.CancelNavigation()
	p.loader.CancelDocumentRequests()
	p.mu.RLock()
	realm, loaderID := p.Top.Realm, p.loaderID
	p.mu.RUnlock()
	if realm == nil {
		return
	}
	realm.scheduler.Post(scheduler.Control, 0, func(context.Context) error {
		if p.Top.Realm != realm || p.LoaderID() != loaderID || realm.inactive {
			return nil
		}
		var stop func(*Realm)
		stop = func(r *Realm) {
			if r == nil || r.inactive {
				return
			}
			if r.documentStream != nil {
				r.documentStream.cancel()
				r.documentStream.parser.Abort()
			}
			// Chrome completes the stopped document without fabricating its
			// DOMContentLoaded/load events or resuming blocked parser scripts.
			r.loadEpoch++
			r.loadRequested, r.loadScheduled = false, false
			r.loadCallback = nil
			r.SetReadyState("complete")
			for _, child := range r.childFrames {
				if child.navigationCancel != nil {
					child.navigationCancel()
				}
				stop(child.Realm)
			}
		}
		stop(realm)
		return nil
	})
}
