// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"context"

	"github.com/go-webengine/engine/css"
)

// LoadStylesheets fetches every <link rel="stylesheet"> of doc that applies
// under m (a <link media="print"> only under css.Print, media="screen" only
// under screen), plus their leading @import chains, and returns the sheet
// texts in cascade order — imports before importer, links in document order
// — ready for css.CascadeMedia(doc.Root, m, sheets). A relative href
// resolves against doc.URL; only http(s) targets are fetched; a sheet that
// fails to fetch is skipped and the rest still apply. The fan-out is bounded
// (64 sheets, 4 MB each, @import two levels deep, 10 s in all).
//
// It is the exported entry point for a consumer that runs the engine's own
// cascade + layout but paints to something other than the built-in raster
// canvas (a PDF, say), so that consumer styles a page exactly as the engine
// itself does — external stylesheets being the dominant fidelity factor on a
// real site — instead of re-implementing fetch, @import and media
// selection. RenderDocument uses the same code path internally, for screen.
func (e *Engine) LoadStylesheets(ctx context.Context, doc *Document, m css.Media) []string {
	return e.loadStylesheets(ctx, doc, m)
}
