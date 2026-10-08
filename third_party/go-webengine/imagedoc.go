// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

// This file adds an IMAGE-DOCUMENT path to Fetch. A URL whose response is a
// raster image (Content-Type image/png|jpeg|gif|webp|bmp|…, i.e. raw image
// bytes rather than an HTML page — think a direct https://i.redd.it/abc.jpg
// link) has no markup to parse: feeding its bytes to the HTML parser yields a
// garbage/empty DOM and nothing renders. A browser instead wraps a bare image
// in a synthetic document that centres/sizes the image; the engine does the
// analogue here, synthesising `<img style="width:100%">` markup so the image
// flows through the exact same decode + layout + paint path as any other
// <img>, spanning the full viewport width (upscaled if it is smaller, so a
// standalone image is always at least the pane width). SVG is deliberately
// excluded: a fetched SVG document parses as inline <svg> and already renders
// through the SVG path, so it is left untouched.
package engine

import (
	"encoding/base64"
	"html"
	"mime"
	"net/url"
	"path"
	"strings"
)

// maxImageDocDataURI bounds how many image bytes are inlined as a base64 data:
// URI in a synthesised image document. Below it the already-fetched bytes are
// embedded so the document renders offline with no second request; above it the
// document points <img src> at the final URL instead (the decoder re-fetches),
// keeping a pathologically large image out of an even larger base64 string.
const maxImageDocDataURI = 16 << 20 // 16 MB

// imageContentType reports whether a Content-Type identifies a RASTER image the
// engine should present as a standalone image document, returning the bare
// media type (e.g. "image/png"). SVG is excluded (svg==true) because a fetched
// SVG document already renders through the inline-<svg> path; html/xml/text and
// anything non-image return ok==false so the normal HTML parse path runs.
func imageContentType(contentType string) (mediaType string, ok bool, svg bool) {
	mt, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		// Tolerate a bare, parameter-less type the strict parser rejects.
		mt = strings.ToLower(strings.TrimSpace(contentType))
		if i := strings.IndexByte(mt, ';'); i >= 0 {
			mt = strings.TrimSpace(mt[:i])
		}
	}
	mt = strings.ToLower(mt)
	if !strings.HasPrefix(mt, "image/") {
		return "", false, false
	}
	if mt == "image/svg+xml" || strings.Contains(mt, "svg") {
		return mt, false, true
	}
	return mt, true, false
}

// imageDocumentHTML synthesises the markup for a standalone-image document: the
// image sized to the full viewport width (height auto by aspect ratio), on a
// zero-margin body. When the fetched bytes fit the data-URI budget they are
// embedded as `data:<mediaType>;base64,…` so the document is self-contained and
// renders offline (deterministic tests, no re-fetch); otherwise the final URL
// is used as the src. title becomes the document <title>.
func imageDocumentHTML(mediaType string, body []byte, finalURL, title string) string {
	var src string
	if len(body) <= maxImageDocDataURI {
		src = "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(body)
	} else {
		src = finalURL
	}
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><title>`)
	b.WriteString(html.EscapeString(title))
	b.WriteString(`</title></head><body style="margin:0"><img src="`)
	b.WriteString(html.EscapeString(src))
	b.WriteString(`" style="display:block;width:100%;height:auto"></body></html>`)
	return b.String()
}

// imageDocTitle derives a human title for an image document from its URL: the
// last non-empty path segment (the file's basename), or the host, or the raw
// URL as a last resort.
func imageDocTitle(rawurl string) string {
	if u, err := url.Parse(rawurl); err == nil {
		if base := path.Base(u.Path); base != "" && base != "/" && base != "." {
			return base
		}
		if u.Host != "" {
			return u.Host
		}
	}
	return rawurl
}
