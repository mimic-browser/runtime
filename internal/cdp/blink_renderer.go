package cdp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/moreveal/mimic/internal/browser"
)

// Blink owns only an inert presentation of a Page. Fetch interception serves
// immutable Mimic resources and rejects every other request. No source script,
// cookie jar or independent network execution is delegated to the renderer.
type blinkRenderer struct {
	conn                    *websocket.Conn
	process                 *exec.Cmd
	profile                 string
	writeMu                 sync.Mutex
	presentationMu          sync.Mutex // renderer transactions; never acquired under a Page turn
	mu                      sync.Mutex
	next                    int64
	pending                 map[int64]chan map[string]any
	files                   map[string][]byte
	done                    chan struct{}
	processDone             chan struct{}
	closeOnce               sync.Once
	frameID                 string
	presentationInitialized bool
}

func rendererExecutable(configured string) (string, error) {
	if configured == "" {
		configured = os.Getenv("MIMIC_DEVTOOLS_CHROME")
	}
	if configured != "" {
		resolved, err := exec.LookPath(configured)
		if err != nil {
			return "", fmt.Errorf("DevTools Blink executable: %w", err)
		}
		return resolved, nil
	}
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "chrome", "msedge"} {
		if resolved, err := exec.LookPath(name); err == nil {
			return resolved, nil
		}
	}
	if runtime.GOOS == "windows" {
		for _, base := range []string{os.Getenv("PROGRAMFILES"), os.Getenv("PROGRAMFILES(X86)"), os.Getenv("LOCALAPPDATA")} {
			for _, relative := range []string{"Google/Chrome/Application/chrome.exe", "Microsoft/Edge/Application/msedge.exe"} {
				candidate := filepath.Join(base, filepath.FromSlash(relative))
				if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
					return candidate, nil
				}
			}
		}
	}
	return "", fmt.Errorf("DevTools page viewing requires an external Chrome/Chromium executable; set -devtools-chrome or MIMIC_DEVTOOLS_CHROME")
}

func newBlinkRenderer(ctx context.Context, executable string, width, height int) (*blinkRenderer, error) {
	executable, err := rendererExecutable(executable)
	if err != nil {
		return nil, err
	}
	profile, err := os.MkdirTemp("", "mimic-devtools-blink-")
	if err != nil {
		return nil, err
	}
	// Reserve a concrete local port. This is an explicitly headless presentation
	// helper, not a normal-browser compatibility oracle.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		os.RemoveAll(profile)
		return nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	command := exec.Command(executable,
		"--headless=new", "--disable-gpu", "--disable-background-networking", "--disable-component-update",
		"--disable-renderer-backgrounding", "--disable-background-timer-throttling", "--disable-backgrounding-occluded-windows",
		"--remote-debugging-address=127.0.0.1", "--remote-debugging-port="+strconv.Itoa(port),
		"--user-data-dir="+profile, "--no-first-run", "--no-default-browser-check",
		"--window-size="+strconv.Itoa(width)+","+strconv.Itoa(height), "about:blank")
	configureRendererProcess(command)
	if err = command.Start(); err != nil {
		os.RemoveAll(profile)
		return nil, err
	}
	r := &blinkRenderer{process: command, profile: profile, pending: make(map[int64]chan map[string]any), done: make(chan struct{}), processDone: make(chan struct{})}
	go func() { _ = command.Wait(); close(r.processDone) }()
	failed := true
	defer func() {
		if failed {
			r.Close()
		}
	}()
	// Use the screencast startup budget rather than failing discovery five
	// seconds before it expires on a busy host. Cancellation remains immediate.
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	endpoint := "http://127.0.0.1:" + strconv.Itoa(port) + "/json/list"
	client := &http.Client{Timeout: time.Second}
	for r.conn == nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-deadline.C:
			return nil, fmt.Errorf("Blink renderer did not expose a page target")
		case <-r.processDone:
			return nil, fmt.Errorf("Blink renderer exited during startup")
		case <-ticker.C:
		}
		response, err := client.Get(endpoint)
		if err != nil {
			continue
		}
		var targets []struct {
			Type string `json:"type"`
			URL  string `json:"webSocketDebuggerUrl"`
		}
		err = json.NewDecoder(response.Body).Decode(&targets)
		response.Body.Close()
		if err != nil {
			continue
		}
		for _, target := range targets {
			if target.Type == "page" {
				r.conn, _, err = websocket.DefaultDialer.DialContext(ctx, target.URL, nil)
				break
			}
		}
	}
	go r.read()
	if _, err = r.call(ctx, "Page.enable", nil); err != nil {
		return nil, err
	}
	// Readiness awaits compositor frames. The presentation target must remain
	// active even on hosts without an interactive foreground desktop.
	if _, err = r.call(ctx, "Page.bringToFront", nil); err != nil {
		return nil, err
	}
	if _, err = r.call(ctx, "Fetch.enable", map[string]any{"patterns": []any{map[string]any{"urlPattern": "*"}}}); err != nil {
		return nil, err
	}
	if _, err = r.call(ctx, "Emulation.setDeviceMetricsOverride", map[string]any{"width": width, "height": height, "deviceScaleFactor": 1, "mobile": false}); err != nil {
		return nil, err
	}
	result, err := r.call(ctx, "Page.getFrameTree", nil)
	if err != nil {
		return nil, err
	}
	r.frameID = stringValue(result["frameTree"].(map[string]any)["frame"].(map[string]any)["id"])
	failed = false
	return r, nil
}

func (r *blinkRenderer) send(message any) error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	if r.conn == nil {
		return fmt.Errorf("Blink renderer is not connected")
	}
	_ = r.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	return r.conn.WriteJSON(message)
}

func (r *blinkRenderer) read() {
	defer close(r.done)
	for {
		var message map[string]any
		if err := r.conn.ReadJSON(&message); err != nil {
			return
		}
		if id := int64(coordinateValue(message["id"])); id != 0 {
			r.mu.Lock()
			response := r.pending[id]
			delete(r.pending, id)
			r.mu.Unlock()
			if response != nil {
				response <- message
			}
			continue
		}
		if message["method"] == "Fetch.requestPaused" {
			p := message["params"].(map[string]any)
			request := p["request"].(map[string]any)
			u, _ := url.Parse(stringValue(request["url"]))
			r.mu.Lock()
			body, present := r.files[strings.TrimPrefix(path.Clean(u.Path), "/")]
			r.next++
			id := r.next
			r.mu.Unlock()
			method := "Fetch.failRequest"
			params := map[string]any{"requestId": p["requestId"], "errorReason": "BlockedByClient"}
			if u.Host == "mimic-render.invalid" && present {
				contentType := mime.TypeByExtension(path.Ext(u.Path))
				if contentType == "" {
					contentType = "application/octet-stream"
				}
				method = "Fetch.fulfillRequest"
				params = map[string]any{"requestId": p["requestId"], "responseCode": 200, "responseHeaders": []any{map[string]any{"name": "Content-Type", "value": contentType}}, "body": base64.StdEncoding.EncodeToString(body)}
			}
			if err := r.send(map[string]any{"id": id, "method": method, "params": params}); err != nil {
				return
			}
		}
	}
}

func (r *blinkRenderer) call(ctx context.Context, method string, params any) (map[string]any, error) {
	r.mu.Lock()
	r.next++
	id := r.next
	response := make(chan map[string]any, 1)
	r.pending[id] = response
	r.mu.Unlock()
	defer func() { r.mu.Lock(); delete(r.pending, id); r.mu.Unlock() }()
	if params == nil {
		params = map[string]any{}
	}
	if err := r.send(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("Blink %s: %w", method, ctx.Err())
	case <-r.done:
		return nil, fmt.Errorf("Blink renderer disconnected")
	case reply := <-response:
		if failure, ok := reply["error"].(map[string]any); ok {
			return nil, fmt.Errorf("Blink %s: %s", method, failure["message"])
		}
		result, _ := reply["result"].(map[string]any)
		return result, nil
	}
}

func (r *blinkRenderer) render(ctx context.Context, snapshot *browser.Snapshot, format string, quality int, clip map[string]any, highlight *presentationHighlight) (string, error) {
	r.presentationMu.Lock()
	defer r.presentationMu.Unlock()
	r.mu.Lock()
	r.files = snapshot.Files
	r.mu.Unlock()
	// Keep one presentation target; each immutable generation replaces only its
	// document. Request interception remains enabled through every replacement.
	if !r.presentationInitialized {
		navigation, err := r.call(ctx, "Page.navigate", map[string]any{"url": "http://mimic-render.invalid/index.html"})
		if err != nil {
			return "", err
		}
		if message := stringValue(navigation["errorText"]); message != "" {
			return "", fmt.Errorf("Presentation navigation: %s", message)
		}
		for {
			tree, err := r.call(ctx, "Page.getFrameTree", nil)
			if err != nil {
				return "", err
			}
			frame := tree["frameTree"].(map[string]any)["frame"].(map[string]any)
			if frame["loaderId"] == navigation["loaderId"] {
				r.presentationInitialized = true
				break
			}
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(25 * time.Millisecond):
			}
		}
	}
	if _, err := r.call(ctx, "Page.setDocumentContent", map[string]any{"frameId": r.frameID, "html": string(snapshot.Files["index.html"])}); err != nil {
		return "", err
	}
	// The only executed code is this trusted presentation readiness probe.
	// Author scripts have been removed and CSP blocks their execution.
	if _, err := r.call(ctx, "Runtime.evaluate", map[string]any{"expression": "Promise.race([Promise.all([document.fonts.ready,...Array.from(document.images,image=>image.complete?Promise.resolve():new Promise(resolve=>{image.onload=image.onerror=resolve}))]),new Promise(resolve=>setTimeout(resolve,3000))])", "awaitPromise": true}); err != nil {
		return "", err
	}
	encoded, err := json.Marshal(snapshot.InspectorFrames)
	if err != nil {
		return "", err
	}
	if _, err := r.call(ctx, "Runtime.evaluate", map[string]any{"expression": "(" + presentationScrollSource + ")(" + string(encoded) + ")", "awaitPromise": true}); err != nil {
		return "", err
	}
	params := map[string]any{"format": format, "fromSurface": true}
	if err := r.applyHighlight(ctx, highlight); err != nil {
		return "", err
	}
	if clip != nil {
		// Screenshot clips are document coordinates, not viewport coordinates.
		metrics, err := r.call(ctx, "Page.getLayoutMetrics", nil)
		if err != nil {
			return "", err
		}
		viewport := metrics["cssVisualViewport"].(map[string]any)
		clip["x"], clip["y"] = viewport["pageX"], viewport["pageY"]
		params["clip"] = clip
	}
	if format == "jpeg" {
		params["quality"] = quality
	}
	result, err := r.call(ctx, "Page.captureScreenshot", params)
	if err != nil {
		return "", err
	}
	return stringValue(result["data"]), nil
}

func (r *blinkRenderer) hit(ctx context.Context, x, y float64) (string, int64, float64, float64, error) {
	r.presentationMu.Lock()
	defer r.presentationMu.Unlock()
	location, err := r.call(ctx, "DOM.getNodeForLocation", map[string]any{"x": int(x), "y": int(y), "includeUserAgentShadowDOM": true})
	if err != nil {
		return "", 0, 0, 0, err
	}
	resolved, err := r.call(ctx, "DOM.resolveNode", map[string]any{"backendNodeId": location["backendNodeId"], "objectGroup": "mimic-presentation-hit"})
	if err != nil {
		return "", 0, 0, 0, err
	}
	defer r.call(ctx, "Runtime.releaseObjectGroup", map[string]any{"objectGroup": "mimic-presentation-hit"})
	object := resolved["object"].(map[string]any)
	result, err := r.call(ctx, "Runtime.callFunctionOn", map[string]any{"objectId": object["objectId"], "functionDeclaration": `function(x,y){let node=this;while(node&&!node.getAttribute?.('data-mimic-preview-node'))node=node.parentNode||node.getRootNode()?.host;let view=this.ownerDocument.defaultView;while(view.frameElement){const rect=view.frameElement.getBoundingClientRect();x-=rect.left+view.frameElement.clientLeft;y-=rect.top+view.frameElement.clientTop;view=view.parent;}return {id:node?.getAttribute('data-mimic-preview-node')||'',x,y};}`, "arguments": []any{map[string]any{"value": x}, map[string]any{"value": y}}, "returnByValue": true})
	if err != nil {
		return "", 0, 0, 0, err
	}
	remote := result["result"].(map[string]any)
	value, ok := remote["value"].(map[string]any)
	if !ok {
		return "", 0, 0, 0, fmt.Errorf("Presentation hit could not be mapped")
	}
	id := stringValue(value["id"])
	separator := strings.LastIndex(id, ":")
	if separator < 0 {
		return "", 0, 0, 0, fmt.Errorf("Presentation hit has no canonical node")
	}
	nodeID, err := strconv.ParseInt(id[separator+1:], 10, 64)
	return id[:separator], nodeID, coordinateValue(value["x"]), coordinateValue(value["y"]), err
}

func (r *blinkRenderer) Close() {
	r.closeOnce.Do(func() {
		if r.conn != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_, _ = r.call(ctx, "Browser.close", nil)
			cancel()
			_ = r.conn.Close()
		}
		if r.process != nil && r.process.Process != nil {
			select {
			case <-r.processDone:
			case <-time.After(2 * time.Second):
				_ = r.process.Process.Kill()
				<-r.processDone
			}
		}
		// Chrome's child processes can briefly retain Windows profile handles
		// after the browser process exits. Teardown owns this dedicated directory.
		deadline := time.Now().Add(3 * time.Second)
		for {
			if err := os.RemoveAll(r.profile); err == nil {
				break
			} else if time.Now().After(deadline) {
				fmt.Fprintf(os.Stderr, "Mimic DevTools renderer profile cleanup: %v\n", err)
				break
			}
			time.Sleep(25 * time.Millisecond)
		}
	})
}

// Only this trusted projection code runs in Blink; source scripts are inert.
const presentationScrollSource = `async function (states) {
  await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
  const visit = root => {
    for (const element of root.querySelectorAll('*')) {
      const key = element.getAttribute('data-mimic-preview-node');
      if (key) {
        const separator = key.lastIndexOf(':');
        const realm = key.slice(0, separator);
        const local = key.slice(separator + 1);
        const state = states[realm];
        if (state) {
          const position = state.positions?.[local];
          if (position) element.scrollTo({left: position[0], top: position[1], behavior: 'instant'});
          if (element === element.ownerDocument.documentElement) {
            element.ownerDocument.defaultView.scrollTo({left: state.scroll[0], top: state.scroll[1], behavior: 'instant'});
          }
        }
      }
      if (element.shadowRoot) visit(element.shadowRoot);
      if (element.contentDocument) visit(element.contentDocument);
    }
  };
  visit(document);
}`
