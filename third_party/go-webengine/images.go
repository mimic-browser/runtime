// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package engine

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"io"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-gfx/gfx/codec"
	"github.com/go-gfx/gfx/raster"
	"github.com/go-gfx/gfx/resample"

	"github.com/go-webengine/engine/css"
	"github.com/go-webengine/engine/dom"
)

// resampleMode maps a computed style's image-rendering to a go-gfx resampling
// filter. The default is Bicubic (a smooth, antialiasing Keys/Catmull-Rom
// resample — sharp on enlargement, low-pass on reduction); `image-rendering:
// pixelated`/`crisp-edges` opts into Nearest for hard-edged pixel art.
func resampleMode(st *css.Style) resample.Mode {
	if st != nil && st.ImageRendering == css.IRPixelated {
		return resample.Nearest
	}
	return resample.Bicubic
}

// resizeRaster scales src to w×h with mode, filtering colour in premultiplied-
// alpha space so a transparent pixel's colour cannot bleed into the visible
// edge of a cut-out (a logo/icon fringe). It falls back to the unscaled source
// when go-gfx rejects the target (non-positive dimensions), so a caller need not
// re-check what it already validated.
func resizeRaster(src *raster.Image, w, h int, mode resample.Mode) *raster.Image {
	out, err := resample.ResizePremultiplied(src, w, h, mode)
	if err != nil {
		return src
	}
	return out
}

// maxImagePixels bounds how many pixels one raster image may decode to. A
// file's header can declare a far larger canvas than its compressed body, and
// the decoder allocates that canvas before reading a pixel, so an unchecked
// declared size lets a small file exhaust memory: a 62KB PNG of zeros declaring
// 4000x4000 allocates 128MB in 40ms. Twenty-five megapixels (about 100MB of
// RGBA) still admits a 6000x4000 photograph.
const maxImagePixels = 25_000_000

var errImageTooLarge = errors.New("image declares more pixels than the engine decodes")

// decodeRaster decodes a raster image after checking its declared dimensions.
// Formats the standard image package cannot read a header for fall through to
// the codec unchecked, as before.
func decodeRaster(data []byte) (*raster.Image, error) {
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil &&
		int64(cfg.Width)*int64(cfg.Height) > maxImagePixels {
		return nil, errImageTooLarge
	}
	return codec.Decode(data)
}

// imgWorkers is the concurrency bound for image fetch+decode: enough to hide
// per-image network latency (the dominant cost) without unbounded fan-out.
func imgWorkers(n int) int {
	w := min(8, runtime.GOMAXPROCS(0))
	if w > n {
		w = n
	}
	return w
}

// parallelDo runs fn over indices [0,n) using a bounded worker pool. fn must
// write only to its own index i (no shared state), so the result is independent
// of scheduling and completion order — the determinism the callers rely on. fn
// is expected to short-circuit on a cancelled context itself.
func parallelDo(n int, fn func(i int)) {
	if n == 0 {
		return
	}
	ch := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < imgWorkers(n); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range ch {
				fn(i)
			}
		}()
	}
	for i := 0; i < n; i++ {
		ch <- i
	}
	close(ch)
	wg.Wait()
}

// LoadImages fetches and decodes every replaced element in doc — raster
// <img>, <img src="*.svg"> and inline <svg> — best-effort and concurrently,
// returning the intrinsic sizes layout.LayoutDocument needs to size their
// boxes and the decoded bitmaps a painter needs to draw them, both keyed by
// element. A relative src resolves against doc.URL. Images wider than
// viewportW are scaled down proportionally, so a returned bitmap is already
// the size its box will be laid out at. A fetch or decode failure just leaves
// that element out of both maps; the raster/vector budgets are MaxImages and
// MaxVectorImages.
//
// It is the exported entry point for a consumer that runs the engine's own
// cascade + layout but paints to something other than the built-in raster
// canvas (a PDF, say), so that consumer sizes and draws images exactly as the
// engine itself does instead of re-implementing fetch/decode/budgeting.
// RenderDocument uses the same code path internally.
func (e *Engine) LoadImages(ctx context.Context, doc *Document, sm css.StyleMap, viewportW int) (map[*dom.Node][2]float64, map[*dom.Node]image.Image) {
	return e.loadImages(ctx, doc, sm, viewportW)
}

// LoadedImage is one replaced element as LoadImageSet returns it: the two
// values LoadImages splits into its maps, plus the source the bitmap was
// decoded from — for a consumer whose output can carry those bytes as they
// are (a PDF embeds a JPEG as a DCTDecode stream without re-encoding it),
// or that picks an encoding by the source's nature (a photograph from a
// lossy source is not line art from a lossless one).
type LoadedImage struct {
	Size   [2]float64  // layout size, CSS px — LoadImages' first map
	Bitmap image.Image // decoded, CSS-sized, viewport-clamped — LoadImages' second map
	Data   []byte      // the bytes an <img> was fetched from, or an inline <svg>'s own serialisation
	Format string      // sniffed from Data — "jpeg", "png", "gif", "webp", "bmp", "svg" — or "" when unknown
	Lossy  bool        // Format is a lossy encoding: jpeg, or a webp whose bitstream is VP8 rather than VP8L
	// SourceW, SourceH is the decoded source's pixel size before any CSS or
	// viewport resize; Bitmap still has exactly the source's pixels iff its
	// bounds are this size, which is when Data can stand in for it.
	SourceW, SourceH int
}

// LoadImageSet is LoadImages keeping each image's source alongside its
// bitmap — same budgets, same fetch, same sizing, same accepted set.
func (e *Engine) LoadImageSet(ctx context.Context, doc *Document, sm css.StyleMap, viewportW int) map[*dom.Node]*LoadedImage {
	return e.loadImageSet(ctx, doc, sm, viewportW)
}

// sniffImageFormat names an image encoding from its leading bytes, and
// whether that encoding is lossy. A WebP is lossy when its bitstream chunk
// is "VP8 " (VP8L is lossless; an extended VP8X file carries one or the
// other further in, so the first 4 KB are searched).
func sniffImageFormat(data []byte) (format string, lossy bool) {
	switch {
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "jpeg", true
	case len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n":
		return "png", false
	case len(data) >= 6 && (string(data[:6]) == "GIF87a" || string(data[:6]) == "GIF89a"):
		return "gif", false
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		head := data
		if len(head) > 4096 {
			head = head[:4096]
		}
		if strings.Contains(string(head), "VP8L") {
			return "webp", false
		}
		return "webp", strings.Contains(string(head), "VP8 ")
	case len(data) >= 2 && data[0] == 'B' && data[1] == 'M':
		return "bmp", false
	}
	return "", false
}

// loadImages fetches and decodes every <img> in the document (best-effort),
// returning intrinsic sizes for layout and decoded bitmaps for paint. Images
// wider than the viewport are scaled down proportionally. Failures are skipped.
func (e *Engine) loadImages(ctx context.Context, doc *Document, sm css.StyleMap, viewportW int) (map[*dom.Node][2]float64, map[*dom.Node]image.Image) {
	set := e.loadImageSet(ctx, doc, sm, viewportW)
	sizes := make(map[*dom.Node][2]float64, len(set))
	bitmaps := make(map[*dom.Node]image.Image, len(set))
	for n, li := range set {
		sizes[n] = li.Size
		bitmaps[n] = li.Bitmap
	}
	return sizes, bitmaps
}

// loadImageSet is the loader proper: the accepted set is decided in document
// order before any fetch, the fetch+decode runs concurrently, and the map
// is filled single-threaded, so the result is the same for any scheduling.
func (e *Engine) loadImageSet(ctx context.Context, doc *Document, sm css.StyleMap, viewportW int) map[*dom.Node]*LoadedImage {
	set := map[*dom.Node]*LoadedImage{}

	// Collect replaced elements: raster/SVG <img> and inline <svg>. An inline
	// <svg> is a replaced box: it is collected and its subtree is not descended
	// into (its children are SVG primitives, not flow content). A host's
	// declarative shadow tree (n.Shadow) is walked too — e.g. a lit-based icon
	// component's SSR'd <template shadowrootmode> commonly wraps its <svg> in
	// shadow content, not light-DOM children, and without this the icon is
	// simply never discovered: no fetch, no bitmap, silently empty space at
	// paint time (found via developer.mozilla.org's nav search-button icon).
	var reps []*dom.Node
	var walk func(n *dom.Node)
	walk = func(n *dom.Node) {
		if n.Type == dom.Element && (n.Tag == "img" || n.Tag == "svg") {
			if st := sm[n]; st == nil || st.Display != css.DisplayNone {
				reps = append(reps, n)
			}
			if n.Tag == "svg" {
				return // treat the SVG subtree as an opaque replaced element
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
		if n.Shadow != nil {
			for _, c := range n.Shadow.Children {
				walk(c)
			}
		}
	}
	walk(doc.Root)

	// Separate budgets: cheap-but-plentiful vector chrome (inline <svg> and
	// <img src="*.svg">) must not starve the expensive raster content photos.
	// Each item is charged to its budget when it passes the gate, in DOCUMENT
	// ORDER, BEFORE any fetch is dispatched — so the accepted set is exactly the
	// same regardless of the concurrent scheduling below (determinism), and both
	// fetch and decode work stay bounded even on a page of only-failing images.
	raster, vector := 0, 0
	var jobs []*dom.Node
	for _, n := range reps {
		if n.Tag == "svg" {
			// Inline <svg>: no src, charged to the vector budget.
			if vector >= e.MaxVectorImages {
				continue
			}
			vector++
			jobs = append(jobs, n)
			continue
		}
		src, ok := n.Attribute("src")
		if !ok {
			continue // a src-less <img> is skipped without charging any budget
		}
		// Classify vector vs raster from the src alone so a wall of *.svg icons
		// spends only the vector budget, never the raster one.
		if srcLooksLikeSVG(src) {
			if vector >= e.MaxVectorImages {
				continue
			}
			vector++
		} else {
			if raster >= e.MaxImages {
				continue
			}
			raster++
		}
		jobs = append(jobs, n)
	}

	// Fetch+decode the accepted set CONCURRENTLY (network latency is the dominant
	// cost). Each worker writes only its own result slot; the maps are then filled
	// single-threaded in document order, so the output is byte-identical to the
	// sequential version for any fixture.
	results := make([]*LoadedImage, len(jobs))
	parallelDo(len(jobs), func(i int) {
		results[i] = e.loadOneImage(ctx, doc, sm, viewportW, jobs[i])
	})
	for i, n := range jobs {
		if results[i] != nil {
			set[n] = results[i]
		}
	}
	return set
}

// loadOneImage fetches and decodes one accepted replaced element (inline <svg>,
// raster <img>, or <img src="*.svg">), returning its intrinsic size and bitmap.
// ok=false means skip it (no maps entry) — a cancelled context, a failed fetch,
// or an undecodable payload. It is pure with respect to its node, so it is safe
// to run concurrently for distinct nodes.
func (e *Engine) loadOneImage(ctx context.Context, doc *Document, sm css.StyleMap, viewportW int, n *dom.Node) *LoadedImage {
	if ctx.Err() != nil {
		return nil // respect cancellation before any work
	}
	// Inline <svg>: serialise the subtree and rasterise it.
	if n.Tag == "svg" {
		data := []byte(serializeSVG(n, sm))
		b, w, h, ok := e.svgToBitmap(data, sm[n], attrDim(n, "width"), attrDim(n, "height"), viewportW, colorHex(sm[n]))
		if !ok {
			return nil
		}
		// Data carries the serialisation the rasteriser was just handed, so a
		// consumer needing another density than this CSS-pixel bitmap can
		// render it again — or emit it as vector. A PDF exporter is the case
		// that asked: a CSS-pixel raster is a hard 96 dpi ceiling on paper,
		// and every label inside an inline <svg> reaches the file as pixels
		// rather than as text (#229).
		return &LoadedImage{Size: [2]float64{float64(w), float64(h)}, Bitmap: b, Data: data, Format: "svg", SourceW: w, SourceH: h}
	}
	src, _ := n.Attribute("src") // presence was checked in the gate
	data, ok := e.fetchImageBytes(ctx, doc.URL, src)
	if !ok {
		return nil
	}
	// <img src="*.svg"> and data:image/svg+xml: rasterise via the SVG path.
	if looksLikeSVG(data, src) {
		b, w, h, ok := e.svgToBitmap(data, sm[n], attrDim(n, "width"), attrDim(n, "height"), viewportW, colorHex(sm[n]))
		if !ok {
			return nil
		}
		return &LoadedImage{Size: [2]float64{float64(w), float64(h)}, Bitmap: b, Data: data, Format: "svg", SourceW: w, SourceH: h}
	}
	src0, err := decodeRaster(data)
	if err != nil {
		return nil
	}
	w, h := src0.W, src0.H
	if w <= 0 || h <= 0 {
		return nil
	}
	format, lossy := sniffImageFormat(data)
	li := &LoadedImage{Data: data, Format: format, Lossy: lossy, SourceW: w, SourceH: h}
	mode := resampleMode(sm[n])
	// Apply a single-axis CSS width/height as a browser does: the specified axis
	// is used and the other is scaled by the intrinsic aspect ratio (so e.g. a
	// wide logo constrained to height:1.5rem is ~72px wide, not its full intrinsic
	// width). Both axes set uses both; neither keeps intrinsic.
	if cw, ch, ok := cssImageSize(sm[n], w, h); ok {
		if cw != w || ch != h {
			src0 = resizeRaster(src0, cw, ch, mode)
			w, h = cw, ch
		}
	}
	// A PERCENTAGE CSS width sizes the image to that fraction of the viewport,
	// scaling UP as well as down (a browser's `width:100%`). The layout engine
	// draws a replaced element at exactly the size returned here, so an image
	// meant to fill its container — most plainly a standalone image document,
	// synthesised as `<img style="width:100%">` — must be scaled to the viewport
	// here; otherwise a small image would render at its native size instead of
	// spanning the pane. cssImageSize handles only DEFINITE px sizes, so a
	// percentage is resolved separately, against the viewport width.
	if pw, ok := percentImageWidth(sm[n], viewportW); ok && pw != w {
		nh := int(float64(h) * float64(pw) / float64(w))
		if nh < 1 {
			nh = 1
		}
		src0 = resizeRaster(src0, pw, nh, mode)
		w, h = pw, nh
	}
	// Scale down to fit the viewport width, preserving aspect ratio.
	if w > viewportW && viewportW > 0 {
		nh := int(float64(h) * float64(viewportW) / float64(w))
		if nh < 1 {
			nh = 1
		}
		src0 = resizeRaster(src0, viewportW, nh, mode)
		w, h = viewportW, nh
	}
	li.Size, li.Bitmap = [2]float64{float64(w), float64(h)}, src0.ToNRGBA()
	return li
}

// loadBackgroundImages fetches and decodes every distinct CSS
// `background-image: url(...)` referenced by a styled element (best-effort),
// returning intrinsic bitmaps keyed by their raw url() token (the same key the
// paint layer stores). Gradient layers need no bitmap. Failures (fetch/decode)
// are skipped; the count is bounded by MaxImages, and each distinct URL is
// fetched at most once. A `mask-image: url(...)` shares this exact fetch
// path and the returned map — paint applies it as an alpha stencil rather
// than painting it directly (see css.Style.MaskImage), but it is the SAME
// kind of resource (a bitmap or SVG named by a url() token) with no reason to
// duplicate the fetch/decode/budget machinery for it.
func (e *Engine) loadBackgroundImages(ctx context.Context, doc *Document, sm css.StyleMap) map[string]image.Image {
	// Collect distinct raw url() tokens in document order.
	seen := map[string]bool{}
	var urls []string
	add := func(u string) {
		if u != "" && !seen[u] {
			seen[u] = true
			urls = append(urls, u)
		}
	}
	var walk func(n *dom.Node)
	walk = func(n *dom.Node) {
		if n.Type == dom.Element {
			if st := sm[n]; st != nil {
				for _, layer := range st.BackgroundImages {
					if layer.Kind == css.BgURL {
						add(layer.URL)
					}
				}
				add(st.MaskImage)
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(doc.Root)
	if len(urls) == 0 {
		return nil
	}

	// Same raster/vector budget split as loadImages, charged in document order
	// BEFORE any fetch so a wall of decorative SVG backgrounds never spends the
	// raster budget and the accepted set is independent of the concurrent
	// scheduling below (and the fetch work stays bounded even when every url fails
	// to decode). Vector backgrounds are not rasterised here (only raster formats
	// decode), but they are still gated so they cannot exhaust the raster budget.
	raster, vector := 0, 0
	var accepted []string
	for _, raw := range urls {
		if srcLooksLikeSVG(raw) {
			if vector >= e.MaxVectorImages {
				continue
			}
			vector++
		} else {
			if raster >= e.MaxImages {
				continue
			}
			raster++
		}
		accepted = append(accepted, raw)
	}

	// Fetch+decode the accepted set CONCURRENTLY; each worker writes only its own
	// result slot and the map is filled single-threaded in document order, so the
	// output is byte-identical to the sequential version.
	results := make([]image.Image, len(accepted))
	parallelDo(len(accepted), func(i int) {
		results[i] = e.loadOneBackground(ctx, doc, accepted[i])
	})
	out := map[string]image.Image{}
	for i, raw := range accepted {
		if results[i] != nil {
			out[raw] = results[i]
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// loadOneBackground fetches and decodes one accepted background-image (or
// mask-image, which shares this cache — see loadBackgroundImages) url,
// returning nil on a cancelled context, a failed fetch, or an undecodable /
// zero-size payload. Safe to run concurrently for distinct urls.
func (e *Engine) loadOneBackground(ctx context.Context, doc *Document, raw string) image.Image {
	if ctx.Err() != nil {
		return nil
	}
	data, ok := e.fetchImageBytes(ctx, doc.URL, raw)
	if !ok {
		return nil
	}
	// An SVG source (by URL extension or, since a `mask-image`/background-image
	// icon service commonly serves SVG from an extensionless query-string URL —
	// confirmed live: Wikipedia's Vector-2022 skin's whole icon set works this
	// way — sniffed content) rasterises via the same SVG path <img>/inline <svg>
	// use. There is no single consuming element here (a background/mask image is
	// cached per-URL, shared across however many elements reference it), so
	// there is no CSS size or `currentColor` to bind to: svgToBitmap's element
	// params are all zero/empty, falling back to the SVG's own intrinsic
	// viewBox/root size — paint's scaleTile resamples to whatever the actual
	// consumer draws it at.
	if looksLikeSVG(data, raw) {
		b, w, h, ok := e.svgToBitmap(data, nil, 0, 0, 0, "")
		if !ok || w <= 0 || h <= 0 {
			return nil
		}
		return b
	}
	img, err := decodeRaster(data)
	if err != nil {
		return nil
	}
	if img.W <= 0 || img.H <= 0 {
		return nil
	}
	return img.ToNRGBA()
}

// cssImageSize resolves the used pixel dimensions of an image given its style
// and intrinsic size (iw×ih). A definite (non-auto, non-percentage) CSS width
// and/or height override the intrinsic size; when only one axis is definite the
// other is derived from the intrinsic aspect ratio. It reports false when the
// style is nil or specifies neither axis definitely (keep the intrinsic size).
func cssImageSize(st *css.Style, iw, ih int) (w, h int, ok bool) {
	if st == nil || iw <= 0 || ih <= 0 {
		return 0, 0, false
	}
	definite := func(l css.Length) (float64, bool) {
		if l.Auto || l.IsPercent || l.Px <= 0 {
			return 0, false
		}
		return l.Px, true
	}
	cw, hasW := definite(st.Width)
	ch, hasH := definite(st.Height)
	switch {
	case hasW && hasH:
		return iround(cw), iround(ch), true
	case hasW:
		return iround(cw), iround(cw * float64(ih) / float64(iw)), true
	case hasH:
		return iround(ch * float64(iw) / float64(ih)), iround(ch), true
	default:
		return 0, 0, false
	}
}

// percentImageWidth resolves a PERCENTAGE CSS width on an image against the
// viewport width, returning the target pixel width (>= 1). It reports false
// when the style has no percentage width, when the percentage is non-positive,
// or when the viewport width is unknown — so only an explicit `width:N%` opts
// in and everything else keeps its intrinsic/definite sizing.
func percentImageWidth(st *css.Style, viewportW int) (int, bool) {
	if st == nil || viewportW <= 0 {
		return 0, false
	}
	if !st.Width.IsPercent || st.Width.Percent <= 0 {
		return 0, false
	}
	return iround(st.Width.Percent * float64(viewportW)), true
}

// iround rounds a non-negative float to the nearest int (>= 1).
func iround(f float64) int {
	n := int(f + 0.5)
	if n < 1 {
		n = 1
	}
	return n
}

// fetchImageBytes returns the raw bytes for an image src, handling data: URIs
// and absolute/relative http(s) URLs.
func (e *Engine) fetchImageBytes(ctx context.Context, base, src string) ([]byte, bool) {
	src = strings.TrimSpace(src)
	if strings.HasPrefix(src, "data:") {
		return decodeDataURI(src)
	}
	abs, ok := resolveURL(base, src)
	if !ok || !(strings.HasPrefix(abs, "http://") || strings.HasPrefix(abs, "https://")) {
		return nil, false
	}
	// The per-render cache (see withImgByteCache) is checked first: it is always
	// present here (renderCoreStaged installs one unconditionally) and catches a
	// URL already fetched earlier in THIS render — most commonly the same image
	// reloaded after a settle pass — with no network call and no e.ImageCache
	// dispatch either.
	render := imgByteCacheFrom(ctx)
	if render != nil {
		if data, ok := render.get(abs); ok {
			return data, true
		}
	}
	// A configured cache serves a previously-fetched image with no network, so a
	// re-render (or another page using the same asset) is instant.
	if e.ImageCache != nil {
		if data, ok := e.ImageCache.Get(abs); ok {
			if render != nil {
				render.put(abs, data)
			}
			return data, true
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, abs, nil)
	if err != nil {
		return nil, false
	}
	req.Header.Set("User-Agent", e.UserAgent)
	resp, ok := e.doWithRateLimitRetry(ctx, req)
	if !ok {
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, false
	}
	if e.ImageCache != nil {
		e.ImageCache.Put(abs, data)
	}
	if render != nil {
		render.put(abs, data)
	}
	return data, true
}

// maxRateLimitRetries bounds how many times doWithRateLimitRetry will retry a
// single request after a 429 — enough to ride out one shared rate-limit
// window (confirmed live: Wikimedia's upload.wikimedia.org CDN, hit by this
// engine's own concurrent per-page image fetching — up to imgWorkers requests
// in flight at once against the SAME host for a single image-heavy article —
// replies 429 with `Retry-After: 1` to whichever requests land outside its
// window) without turning one rate-limited page into an unbounded retry
// storm.
const maxRateLimitRetries = 3

// defaultRateLimitBackoff is used when a 429 response carries no Retry-After
// header, or one this engine cannot parse (an HTTP-date is accepted by the
// spec but not handled here — no confirmed caller has sent one; delay-seconds
// is the form actually observed live).
const defaultRateLimitBackoff = 500 * time.Millisecond

// maxRateLimitBackoff caps how long a single retry will wait, regardless of
// what a Retry-After header requests — a defensive ceiling so a misconfigured
// or hostile server cannot stall a render by naming an absurd delay.
const maxRateLimitBackoff = 5 * time.Second

// doWithRateLimitRetry runs req via e.Client, retrying up to
// maxRateLimitRetries times when the response is 429 Too Many Requests —
// honouring the server's own Retry-After (delay-seconds form) when present,
// falling back to defaultRateLimitBackoff otherwise. Any other status (or
// response, including a real success) is returned immediately on the first
// attempt: only a 429 triggers a wait-and-retry. req's body is nil (every
// caller here is a bodyless GET), so the same *http.Request is safe to reuse
// across attempts. ok is false only for a request-level error (network
// failure, context cancelled) or exhausting all retries still 429 — never
// for an ordinary non-2xx status, which the caller already handles.
func (e *Engine) doWithRateLimitRetry(ctx context.Context, req *http.Request) (resp *http.Response, ok bool) {
	for attempt := 0; ; attempt++ {
		resp, err := e.Client.Do(req)
		if err != nil {
			return nil, false
		}
		if resp.StatusCode != http.StatusTooManyRequests || attempt >= maxRateLimitRetries {
			return resp, true
		}
		wait := retryAfterDelay(resp.Header.Get("Retry-After"))
		resp.Body.Close()
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, false
		}
	}
}

// retryAfterDelay parses a Retry-After header's delay-seconds form (RFC 9110
// §10.2.3 — a non-negative integer count of seconds; the alternative
// HTTP-date form is not handled, see maxRateLimitRetries's doc comment) into
// a duration, clamped to [0, maxRateLimitBackoff]. An empty, negative, or
// unparseable value falls back to defaultRateLimitBackoff.
func retryAfterDelay(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return defaultRateLimitBackoff
	}
	secs, err := strconv.Atoi(v)
	if err != nil || secs < 0 {
		return defaultRateLimitBackoff
	}
	d := time.Duration(secs) * time.Second
	if d > maxRateLimitBackoff {
		return maxRateLimitBackoff
	}
	return d
}

// imgByteCache is an ephemeral, in-memory, per-render cache of fetched image
// bytes keyed by absolute URL. It exists purely to dedupe repeat fetches
// WITHIN one render (e.g. the settle loop reloading images after a script
// mutates the DOM) — it is created fresh per render (see renderCoreStaged)
// and never stored on Engine, which is shared and used concurrently across
// unrelated renders. Safe for concurrent use: loadImages/loadBackgroundImages
// fetch concurrently via parallelDo.
type imgByteCache struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newImgByteCache() *imgByteCache { return &imgByteCache{m: map[string][]byte{}} }

func (c *imgByteCache) get(url string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	data, ok := c.m[url]
	return data, ok
}

func (c *imgByteCache) put(url string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[url] = data
}

type imgByteCacheKey struct{}

// withImgByteCache returns a context carrying c for fetchImageBytes to find via
// imgByteCacheFrom. ctx already threads unchanged through every image-fetching
// call in a render (the initial pass, the settle loop, the meta-fallback
// path), so installing it once at the top of renderCoreStaged reaches all of
// them with no signature change at each call site.
func withImgByteCache(ctx context.Context, c *imgByteCache) context.Context {
	return context.WithValue(ctx, imgByteCacheKey{}, c)
}

// imgByteCacheFrom returns the render-scoped cache installed by
// withImgByteCache, or nil if none was (e.g. a direct unit test call).
func imgByteCacheFrom(ctx context.Context) *imgByteCache {
	c, _ := ctx.Value(imgByteCacheKey{}).(*imgByteCache)
	return c
}

// decodeDataURI decodes a base64 data: URI's payload.
func decodeDataURI(s string) ([]byte, bool) {
	comma := strings.IndexByte(s, ',')
	if comma < 0 {
		return nil, false
	}
	meta, payload := s[5:comma], s[comma+1:]
	if strings.Contains(meta, "base64") {
		b, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return nil, false
		}
		return b, true
	}
	// A non-base64 data URI payload is percent-encoded (RFC 2397); decode it so
	// e.g. `data:image/svg+xml,%3Csvg...` yields real SVG bytes. PathUnescape
	// (unlike QueryUnescape) leaves '+' literal, which SVG data needs. An
	// undecodable payload falls back to the raw bytes.
	if dec, err := url.PathUnescape(payload); err == nil {
		return []byte(dec), true
	}
	return []byte(payload), true
}
