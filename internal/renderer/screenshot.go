// Package renderer turns a static Page snapshot into an approximate PNG.
// It owns no browser state and never fetches resources outside the snapshot.
package renderer

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"

	webengine "github.com/go-webengine/engine/static"
)

const snapshotOrigin = "https://mimic-render.invalid"

type snapshotTransport struct {
	files map[string][]byte
}

func (t snapshotTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "https" || request.URL.Host != "mimic-render.invalid" {
		return nil, fmt.Errorf("approximate renderer cannot fetch %s", request.URL.Redacted())
	}
	name := strings.TrimPrefix(path.Clean(request.URL.Path), "/")
	if name == "." || strings.HasPrefix(name, "../") {
		return nil, fmt.Errorf("invalid snapshot resource %q", name)
	}
	body, ok := t.files[name]
	if !ok && !strings.Contains(name, "/") {
		// Snapshot CSS lives under assets/, and its relative url() entries
		// resolve there. The renderer currently resolves them against the
		// document URL, so serve the same immutable asset at that alias.
		body, ok = t.files["assets/"+name]
	}
	if !ok {
		return nil, fmt.Errorf("snapshot resource unavailable: %s", name)
	}
	mediaType := mime.TypeByExtension(path.Ext(name))
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	return &http.Response{
		StatusCode:    http.StatusOK,
		Status:        "200 OK",
		Header:        http.Header{"Content-Type": []string{mediaType}},
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
		Request:       request,
	}, nil
}

// Render uses the third-party CSS/layout/paint pipeline only after the Page has
// exported an immutable snapshot. Its independent layout can disagree with Mimic.
func Render(ctx context.Context, files map[string][]byte, width, height int) (result *image.RGBA, err error) {
	// Keep an unexpected third-party paint failure local to this CDP command
	// and report it explicitly instead of terminating the Page process.
	defer func() {
		if failure := recover(); failure != nil {
			result = nil
			err = fmt.Errorf("approximate renderer failed: %v", failure)
		}
	}()
	if width <= 0 || height <= 0 || width > 4096 || height > 4096 {
		return nil, fmt.Errorf("approximate screenshot viewport must be 1..4096 pixels on each axis")
	}
	html, ok := files["index.html"]
	if !ok {
		return nil, fmt.Errorf("snapshot has no index.html")
	}
	engine := webengine.New()
	engine.Client = &http.Client{Transport: snapshotTransport{files: files}}
	result, err = engine.RenderHTML(ctx, string(html), snapshotOrigin+"/index.html", image.Rect(0, 0, width, height))
	if err != nil {
		return nil, err
	}
	return result, nil
}
