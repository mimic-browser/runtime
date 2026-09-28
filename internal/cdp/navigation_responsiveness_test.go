package cdp

import (
	"encoding/base64"
	"fmt"
	"github.com/gorilla/websocket"
	"github.com/moreveal/mimic/internal/trace"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// The server barrier, not a timer, proves the command runs before the pending
// resource completes. These commands all address the navigating Page itself.
func TestPageCommandsDuringParserResourceWait(t *testing.T) {
	for _, kind := range []string{"document", "location", "form", "script", "stylesheet", "module-entry", "iframe-stylesheet"} {
		t.Run(kind, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/held":
					once.Do(func() { close(entered) })
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
					if kind == "stylesheet" || kind == "iframe-stylesheet" {
						w.Header().Set("Content-Type", "text/css")
						fmt.Fprint(w, "body{color:rgb(1,2,3)}")
					} else {
						w.Header().Set("Content-Type", "text/javascript")
						fmt.Fprint(w, "globalThis.loaded=true")
					}
				case "/child":
					fmt.Fprint(w, `<p id=before>child</p><link rel=stylesheet href=/held><script>globalThis.loaded=true</script><p id=tail>tail</p>`)
				case "/":
					if kind == "document" || kind == "location" || kind == "form" {
						once.Do(func() { close(entered) })
						select {
						case <-release:
						case <-r.Context().Done():
							return
						}
						fmt.Fprint(w, `<p id=before>after</p>`)
						return
					}
					switch kind {
					case "script":
						fmt.Fprint(w, `<p id=before>before</p><script src=/held></script><p id=tail>tail</p>`)
					case "module-entry":
						fmt.Fprint(w, `<p id=before>before</p><script type=module src=/held></script><p id=tail>tail</p>`)
					case "stylesheet":
						fmt.Fprint(w, `<p id=before>before</p><link rel=stylesheet href=/held><script>globalThis.loaded=true</script><p id=tail>tail</p>`)
					case "iframe-stylesheet":
						fmt.Fprint(w, `<p id=before>before</p><iframe src=/child></iframe>`)
					}
				default:
					http.NotFound(w, r)
				}
			}))
			defer fixture.Close()
			var released sync.Once
			unblock := func() { released.Do(func() { close(release) }) }
			defer unblock()
			s, addr := runningServer(t)
			c, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+s.Page.ID, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if kind == "document" || kind == "location" || kind == "form" {
				c.WriteJSON(map[string]any{"id": 1000, "method": "Runtime.evaluate", "params": map[string]any{"expression": "document.body.innerHTML='<p id=before>before</p>'"}})
				readReply(t, c, 1000)
			}
			if kind == "location" || kind == "form" {
				expression := fmt.Sprintf("location.href=%q", fixture.URL)
				if kind == "form" {
					expression = fmt.Sprintf("var f=document.createElement('form');f.action=%q;document.body.append(f);f.submit()", fixture.URL)
				}
				c.WriteJSON(map[string]any{"id": 1, "method": "Runtime.evaluate", "params": map[string]any{"expression": expression}})
			} else {
				c.WriteJSON(map[string]any{"id": 1, "method": "Page.navigate", "params": map[string]any{"url": fixture.URL}})
			}
			// Page.navigate replies at commit. A pending main response must still
			// permit explicitly multiplexed commands before that reply arrives.
			if kind != "document" {
				readReply(t, c, 1)
			}
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("resource barrier not reached")
			}
			elapsed := make([]time.Duration, 0, 30)
			for i := 0; i < 31; i++ {
				c.SetReadDeadline(time.Now().Add(2 * time.Second))
				start := time.Now()
				c.WriteJSON(map[string]any{"id": i + 2, "method": "Runtime.evaluate", "params": map[string]any{"expression": "document.getElementById('before').textContent"}})
				reply := readReply(t, c, float64(i+2))
				if reply["error"] != nil {
					t.Fatal(reply)
				}
				value := reply["result"].(map[string]any)["result"].(map[string]any)["value"]
				if value != "before" {
					t.Fatal(reply)
				}
				if i > 0 {
					elapsed = append(elapsed, time.Since(start))
				}
			}
			if kind == "module-entry" {
				// Chrome 152 parses the tail and becomes interactive while the
				// deferred module fetch is held, before DOMContentLoaded.
				c.WriteJSON(map[string]any{"id": 2000, "method": "Runtime.evaluate", "params": map[string]any{"expression": "!!document.getElementById('tail') && document.readyState==='interactive' && typeof loaded==='undefined'"}})
				reply := readReply(t, c, 2000)
				if reply["result"].(map[string]any)["result"].(map[string]any)["value"] != true {
					t.Fatal(reply)
				}
			}
			sort.Slice(elapsed, func(i, j int) bool { return elapsed[i] < elapsed[j] })
			t.Logf("30 reads while resource blocked: median=%s p95=%s", elapsed[15], elapsed[28])
			unblock()
			if kind == "document" {
				if reply := readReply(t, c, 1); reply["error"] != nil || reply["result"].(map[string]any)["errorText"] != nil {
					t.Fatal(reply)
				}
			}
			for i := 40; i < 140; i++ {
				c.WriteJSON(map[string]any{"id": i, "method": "Runtime.evaluate", "params": map[string]any{"expression": fmt.Sprintf("document.readyState==='complete' && location.href.startsWith(%q)", fixture.URL)}})
				reply := readReply(t, c, float64(i))
				if reply["error"] != nil {
					t.Fatal(reply)
				}
				if reply["result"].(map[string]any)["result"].(map[string]any)["value"] == true {
					return
				}
				time.Sleep(10 * time.Millisecond)
			}
			t.Fatal("load did not complete after releasing resource")
		})
	}
}

func TestSuspendedNavigationCannotMutateReplacement(t *testing.T) {
	for _, kind := range []string{"document", "script", "stylesheet", "stop-script"} {
		t.Run(kind, func(t *testing.T) {
			entered, release := make(chan struct{}), make(chan struct{})
			var enteredOnce, releaseOnce sync.Once
			unblock := func() { releaseOnce.Do(func() { close(release) }) }
			fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/new" {
					fmt.Fprint(w, `<p id=new>replacement</p>`)
					return
				}
				if r.URL.Path == "/held" || (kind == "document" && r.URL.Path == "/") {
					enteredOnce.Do(func() { close(entered) })
					select {
					case <-release:
					case <-r.Context().Done():
						return
					}
					if kind == "document" {
						fmt.Fprint(w, `<script>globalThis.stale=true</script><p>stale</p>`)
					} else if kind == "stylesheet" {
						w.Header().Set("Content-Type", "text/css")
						fmt.Fprint(w, `body{color:red}`)
					} else {
						w.Header().Set("Content-Type", "text/javascript")
						fmt.Fprint(w, `globalThis.stale=true`)
					}
					return
				}
				if r.URL.Path != "/" {
					http.NotFound(w, r)
					return
				}
				if kind == "stylesheet" {
					fmt.Fprint(w, `<link rel=stylesheet href=/held><script>globalThis.stale=true</script>`)
				} else {
					fmt.Fprint(w, `<p>initial</p><script src=/held></script>`)
				}
			}))
			defer fixture.Close()
			defer unblock()
			s, addr := runningServer(t)
			c, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+s.Page.ID, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			c.WriteJSON(map[string]any{"id": 1, "method": "Page.navigate", "params": map[string]any{"url": fixture.URL}})
			if kind != "document" {
				readReply(t, c, 1)
			}
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("barrier not reached")
			}
			c.SetReadDeadline(time.Now().Add(3 * time.Second))
			if kind == "stop-script" {
				c.WriteJSON(map[string]any{"id": 2, "method": "Page.stopLoading"})
			} else {
				c.WriteJSON(map[string]any{"id": 2, "method": "Page.navigate", "params": map[string]any{"url": fixture.URL + "/new"}})
			}
			readReply(t, c, 2)
			if kind != "stop-script" {
				ready := false
				for id := 3; id < 100; id++ {
					c.WriteJSON(map[string]any{"id": id, "method": "Runtime.evaluate", "params": map[string]any{"expression": "!!document.getElementById('new')"}})
					reply := readReply(t, c, float64(id))
					if reply["result"].(map[string]any)["result"].(map[string]any)["value"] == true {
						ready = true
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				if !ready {
					t.Fatal("replacement did not commit")
				}
			}
			unblock()
			// A timer gives any mistakenly queued old continuation a chance to run.
			c.WriteJSON(map[string]any{"id": 200, "method": "Runtime.evaluate", "params": map[string]any{"expression": "new Promise(r=>setTimeout(()=>r(typeof stale==='undefined'),50))", "awaitPromise": true}})
			reply := readReply(t, c, 200)
			if reply["result"].(map[string]any)["result"].(map[string]any)["value"] != true {
				t.Fatal(reply)
			}
		})
	}
}

func TestSuspendedNavigationDeadlineAndTeardown(t *testing.T) {
	for _, kind := range []string{"document", "script"} {
		t.Run(kind, func(t *testing.T) {
			entered, terminated, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/held" || kind == "document" {
					once.Do(func() { close(entered) })
					select {
					case <-r.Context().Done():
						close(terminated)
					case <-release:
					}
					return
				}
				if r.URL.Path == "/" {
					fmt.Fprint(w, `<p id=before>before</p><script src=/held></script><p id=tail>tail</p>`)
				} else {
					http.NotFound(w, r)
				}
			}))
			defer fixture.Close()
			defer close(release)
			s, addr := runningServer(t)
			s.SetNavigationTimeout(600 * time.Millisecond)
			s.Page.Trace().Start()
			c, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+s.Page.ID, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			c.WriteJSON(map[string]any{"id": 1, "method": "Page.navigate", "params": map[string]any{"url": fixture.URL}})
			readReply(t, c, 1)
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("barrier not reached")
			}
			deadline := time.Now().Add(3 * time.Second)
			found := false
			for time.Now().Before(deadline) {
				for _, event := range s.Page.Trace().Events() {
					if event.Name == "navigation" && event.Data["error"] == "context deadline exceeded" {
						found = true
					}
					if event.Name == "DOMContentLoaded" || event.Name == "load" {
						t.Fatalf("deadline fabricated lifecycle: %s", event.Name)
					}
				}
				if found {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !found {
				t.Fatal("missing navigation deadline diagnostic")
			}
			c.SetReadDeadline(time.Now().Add(time.Second))
			c.WriteJSON(map[string]any{"id": 2, "method": "Runtime.evaluate", "params": map[string]any{"expression": "6*7"}})
			reply := readReply(t, c, 2)
			if reply["result"].(map[string]any)["result"].(map[string]any)["value"] != float64(42) {
				t.Fatal(reply)
			}
			closed := make(chan struct{})
			go func() { s.closePage(s.Page); close(closed) }()
			select {
			case <-closed:
			case <-time.After(2 * time.Second):
				t.Fatal("Page close waited on held resource")
			}
			select {
			case <-terminated:
			case <-time.After(time.Second):
				t.Fatal("held request was not canceled")
			}
		})
	}
}

func TestSnapshotInterruptsNavigationContinuation(t *testing.T) {
	for _, external := range []bool{false, true} {
		t.Run(fmt.Sprint("external=", external), func(t *testing.T) {
			fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				const busy = `document.getElementById('before').textContent='running parser script';console.log('parser entered');while(true){}`
				if r.URL.Path == "/busy.js" {
					w.Header().Set("Content-Type", "text/javascript")
					fmt.Fprint(w, busy)
					return
				}
				if r.URL.Path != "/" {
					http.NotFound(w, r)
					return
				}
				if external {
					fmt.Fprint(w, `<p id=before>before</p><script src=/busy.js></script>`)
				} else {
					fmt.Fprint(w, `<p id=before>before</p><script>`+busy+`</script>`)
				}
			}))
			defer fixture.Close()
			s, addr := runningServer(t)
			entered := make(chan struct{}, 1)
			unsubscribe := s.Page.Trace().Subscribe(func(event trace.Event) {
				// The fixture logs only after committing the DOM update. scriptStart
				// precedes compilation, and elapsed wall time cannot prove execution.
				if event.Kind == trace.Console {
					select {
					case entered <- struct{}{}:
					default:
					}
				}
			})
			defer unsubscribe()
			c, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+s.Page.ID, nil)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			defer s.stopPump(s.Page)
			c.WriteJSON(map[string]any{"id": 1, "method": "Page.navigate", "params": map[string]any{"url": fixture.URL}})
			readReply(t, c, 1)
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("parser script did not reach its DOM update")
			}
			c.SetReadDeadline(time.Now().Add(3 * time.Second))
			c.WriteJSON(map[string]any{"id": 2, "method": "Mimic.captureSnapshot", "params": map[string]any{"interrupt": true}})
			reply := readReply(t, c, 2)
			if reply["error"] != nil {
				t.Fatal(reply)
			}
			files := reply["result"].(map[string]any)["files"].(map[string]any)
			html, err := base64.StdEncoding.DecodeString(files["index.html"].(string))
			if err != nil || !strings.Contains(string(html), "running parser script") {
				t.Fatalf("missing interrupted parser state: %s, %v", html, err)
			}
		})
	}
}

func TestInterruptAndCloseChildParserContinuation(t *testing.T) {
	for _, external := range []bool{false, true} {
		for _, closeTarget := range []bool{false, true} {
			t.Run(fmt.Sprintf("external=%v/close=%v", external, closeTarget), func(t *testing.T) {
				fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					const busy = `document.getElementById('before').textContent='running child parser script';console.log('child parser entered');while(true){}`
					switch r.URL.Path {
					case "/":
						fmt.Fprint(w, `<p>parent</p><iframe src=/child></iframe>`)
					case "/busy.js":
						w.Header().Set("Content-Type", "text/javascript")
						fmt.Fprint(w, busy)
					case "/child":
						if external {
							fmt.Fprint(w, `<p id=before>before</p><script src=/busy.js></script>`)
						} else {
							fmt.Fprint(w, `<p id=before>before</p><script>`+busy+`</script>`)
						}
					default:
						http.NotFound(w, r)
					}
				}))
				defer fixture.Close()
				s, addr := runningServer(t)
				entered := make(chan struct{}, 1)
				unsubscribe := s.Page.Trace().Subscribe(func(event trace.Event) {
					// The fixture's only console call occurs after the DOM update.
					// scriptStart precedes compilation, so elapsed time is not a barrier.
					if event.Kind == trace.Console {
						select {
						case entered <- struct{}{}:
						default:
						}
					}
				})
				defer unsubscribe()
				c, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+s.Page.ID, nil)
				if err != nil {
					t.Fatal(err)
				}
				defer c.Close()
				defer s.stopPump(s.Page)
				c.WriteJSON(map[string]any{"id": 1, "method": "Page.navigate", "params": map[string]any{"url": fixture.URL}})
				readReply(t, c, 1)
				select {
				case <-entered:
				case <-time.After(3 * time.Second):
					t.Fatal("child parser script did not reach its DOM update")
				}
				c.SetReadDeadline(time.Now().Add(3 * time.Second))
				if closeTarget {
					c.WriteJSON(map[string]any{"id": 2, "method": "Target.closeTarget", "params": map[string]any{"targetId": s.Page.ID}})
				} else {
					c.WriteJSON(map[string]any{"id": 2, "method": "Mimic.captureSnapshot", "params": map[string]any{"interrupt": true}})
				}
				reply := readReply(t, c, 2)
				if reply["error"] != nil {
					t.Fatal(reply)
				}
				if closeTarget {
					if reply["result"].(map[string]any)["success"] != true {
						t.Fatal(reply)
					}
					return
				}
				files := reply["result"].(map[string]any)["files"].(map[string]any)
				found := false
				for _, encoded := range files {
					data, err := base64.StdEncoding.DecodeString(encoded.(string))
					if err == nil && strings.Contains(string(data), "running child parser script") {
						found = true
					}
				}
				if !found {
					t.Fatal("snapshot lost interrupted child parser state")
				}
			})
		}
	}
}
