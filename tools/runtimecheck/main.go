// Command runtimecheck verifies a built Mimic executable against local fixtures.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	binary := flag.String("binary", "", "Mimic executable to verify")
	engine := flag.String("engine", "v8", "engine to verify: v8, quickjs, goja")
	flag.Parse()
	if *binary == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := check(*binary, *engine); err != nil {
		fmt.Fprintln(os.Stderr, "runtimecheck:", err)
		os.Exit(1)
	}
	fmt.Printf("PASS %s/%s: fresh-cache startup, CDP navigation, DOM, Promise, canvas text, mouse input, target teardown and orderly shutdown\n", runtime.GOOS, *engine)
}

type remote struct {
	conn   *websocket.Conn
	serial int
}

func (r *remote) call(method string, params any, result any) error {
	r.serial++
	if err := r.conn.SetReadDeadline(time.Now().Add(15 * time.Second)); err != nil {
		return err
	}
	if err := r.conn.SetWriteDeadline(time.Now().Add(15 * time.Second)); err != nil {
		return err
	}
	if err := r.conn.WriteJSON(map[string]any{"id": r.serial, "method": method, "params": params}); err != nil {
		return err
	}
	for {
		var reply struct {
			ID     int             `json:"id"`
			Error  json.RawMessage `json:"error"`
			Result json.RawMessage `json:"result"`
		}
		if err := r.conn.ReadJSON(&reply); err != nil {
			return err
		}
		if reply.ID != r.serial {
			continue
		}
		if len(reply.Error) > 0 {
			return fmt.Errorf("%s: %s", method, reply.Error)
		}
		if result != nil {
			return json.Unmarshal(reply.Result, result)
		}
		return nil
	}
}
func (r *remote) eval(expression string, out any) error {
	var result struct {
		Exception json.RawMessage `json:"exceptionDetails"`
		Result    struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
	}
	if err := r.call("Runtime.evaluate", map[string]any{"expression": expression, "returnByValue": true}, &result); err != nil {
		return err
	}
	if len(result.Exception) > 0 {
		return fmt.Errorf("JavaScript: %s", result.Exception)
	}
	return json.Unmarshal(result.Result.Value, out)
}
func check(binary, engine string) error {
	binary, err := filepath.Abs(binary)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "mimic-runtimecheck-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--browser-mode", "headless", "-listen", "127.0.0.1:0", "-engine", engine)
	cmd.Dir = dir
	// No repository-relative engine paths or previously extracted native asset.
	cmd.Env = append(os.Environ(), "XDG_CACHE_HOME="+dir, "LOCALAPPDATA="+dir, "GOV8_SHIM_LIBRARY=", "GOV8_SHIM_DLL=")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		return err
	}
	finished := false
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		cancel()
		_ = cmd.Process.Kill()
		if !finished {
			<-done
		}
	}()
	scan := bufio.NewScanner(stdout)
	if !scan.Scan() {
		return fmt.Errorf("server did not report readiness: %v", scan.Err())
	}
	address := strings.TrimPrefix(scan.Text(), "Mimic listening on ")
	if !strings.HasPrefix(address, "http://127.0.0.1:") {
		return fmt.Errorf("unexpected readiness line: %s", scan.Text())
	}
	client := http.Client{Timeout: 15 * time.Second}
	request, err := http.NewRequest(http.MethodPut, address+"/json/new", nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	var target struct {
		ID  string `json:"id"`
		URL string `json:"webSocketDebuggerUrl"`
	}
	err = json.NewDecoder(response.Body).Decode(&target)
	response.Body.Close()
	if err != nil {
		return err
	}
	conn, _, err := websocket.DefaultDialer.Dial(target.URL, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	remote := remote{conn: conn}
	fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, `<!doctype html><title>Portable Mimic</title><button id="button" style="font-family:sans-serif" onclick="globalThis.clicked=true">Run</button><p id="result">pending</p><script>Promise.resolve(1.25).then(v=>{document.getElementById('result').textContent=String(v+2.5);globalThis.ready=true})</script>`)
	}))
	defer fixture.Close()
	if err = remote.call("Page.navigate", map[string]any{"url": fixture.URL}, nil); err != nil {
		return err
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		var ready bool
		if err = remote.eval("globalThis.ready===true", &ready); err != nil {
			return err
		}
		if ready {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("navigation timeout")
		}
		time.Sleep(25 * time.Millisecond)
	}
	var valid bool
	if err = remote.eval(`(()=>{const c=document.createElement('canvas').getContext('2d');c.font='16px sans-serif';return document.title==='Portable Mimic'&&document.getElementById('result').textContent==='3.75'&&c.measureText('Mimic').width>0})()`, &valid); err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("DOM, Promise or real-font measurement failed")
	}
	var box struct{ X, Y, Width, Height float64 }
	if err = remote.eval(`(()=>{const r=document.getElementById('button').getBoundingClientRect();return {X:r.x,Y:r.y,Width:r.width,Height:r.height}})()`, &box); err != nil {
		return err
	}
	if box.Width <= 0 || box.Height <= 0 {
		return fmt.Errorf("button has no usable geometry: %+v", box)
	}
	for _, kind := range []string{"mousePressed", "mouseReleased"} {
		if err = remote.call("Input.dispatchMouseEvent", map[string]any{"type": kind, "x": box.X + box.Width/2, "y": box.Y + box.Height/2, "button": "left", "clickCount": 1}, nil); err != nil {
			return err
		}
	}
	if err = remote.eval("globalThis.clicked===true", &valid); err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("mouse input did not activate the button")
	}
	conn.Close()
	response, err = client.Get(address + "/json/close/" + target.ID)
	if err != nil {
		return err
	}
	response.Body.Close()
	response, err = client.Get(address + "/json/list")
	if err != nil {
		return err
	}
	var remaining []struct {
		ID string `json:"id"`
	}
	err = json.NewDecoder(response.Body).Decode(&remaining)
	response.Body.Close()
	if err != nil {
		return err
	}
	for _, other := range remaining {
		if other.ID == target.ID {
			return fmt.Errorf("closed target still appears in discovery")
		}
	}
	if runtime.GOOS == "linux" {
		if err = cmd.Process.Signal(syscall.SIGTERM); err != nil {
			return err
		}
	} else {
		response, err = client.Get(address + "/json/version")
		if err != nil {
			return err
		}
		var version struct {
			URL string `json:"webSocketDebuggerUrl"`
		}
		err = json.NewDecoder(response.Body).Decode(&version)
		response.Body.Close()
		if err != nil {
			return err
		}
		browser, _, err := websocket.DefaultDialer.Dial(version.URL, nil)
		if err != nil {
			return err
		}
		defer browser.Close()
		if err = browser.WriteJSON(map[string]any{"id": 1, "method": "Browser.close", "params": map[string]any{}}); err != nil {
			return err
		}
	}
	select {
	case err = <-done:
		finished = true
		return err
	case <-time.After(10 * time.Second):
		return fmt.Errorf("orderly shutdown timed out")
	}
}
