package network

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/http2"
)

type priorityReplayListener struct{ net.Listener }

func (l priorityReplayListener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e != nil {
		return nil, e
	}
	return priorityReplayConn{c}, nil
}

type priorityReplayConn struct{ net.Conn }

func (c priorityReplayConn) Write(b []byte) (int, error) {
	time.Sleep(3 * time.Millisecond)
	return c.Conn.Write(b)
}
func TestHTTP2PriorityPageWorkload(t *testing.T) {
	script := os.Getenv("MIMIC_PRIORITY_PAGE_SCRIPT")
	if script == "" {
		t.Skip("optional SDK performance workload; set MIMIC_PRIORITY_PAGE_SCRIPT")
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		switch {
		case r.URL.Path == "/":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<!doctype html>
<script>
  window.asyncDone = 0;
</script>`)
			for i := 0; i < 16; i++ {
				fmt.Fprintf(w, "<script async src='/async/%d.js'></script>", i)
			}
			fmt.Fprint(w, "<link rel='stylesheet' href='/critical.css'><div id='probe'></div><script src='/probe.js'></script>")
		case r.URL.Path == "/critical.css":
			w.Header().Set("Content-Type", "text/css")
			fmt.Fprint(w, "#probe { margin-left: 7px; } /*"+strings.Repeat("x", 32768)+"*/")
		case r.URL.Path == "/probe.js":
			w.Header().Set("Content-Type", "text/javascript")
			fmt.Fprint(w, `window.answer =
  getComputedStyle(document.getElementById('probe')).marginLeft === '7px'
    ? 'ready'
    : 'early';`)
		case strings.HasPrefix(r.URL.Path, "/async/"):
			w.Header().Set("Content-Type", "text/javascript")
			fmt.Fprint(w, "/*"+strings.Repeat("x", 512*1024)+"*/\nwindow.asyncDone++;\n")
		default:
			w.WriteHeader(404)
		}
	}))
	server.Listener = priorityReplayListener{server.Listener}
	if err := http2.ConfigureServer(server.Config, &http2.Server{MaxConcurrentStreams: 100, NewWriteScheduler: func() http2.WriteScheduler { return http2.NewPriorityWriteScheduler(nil) }}); err != nil {
		t.Fatal(err)
	}
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	command := exec.Command("node", script, server.URL)
	command.Stdout = os.Stdout
	command.Stderr = os.Stderr
	if err := command.Run(); err != nil {
		t.Fatal(err)
	}
}
