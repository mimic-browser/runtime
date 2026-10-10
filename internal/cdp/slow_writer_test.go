package cdp

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/moreveal/mimic/internal/trace"
)

// Gate the socket write itself: TCP buffer/window sizes on Windows loopback do
// not reliably make a peer that stops reading exert backpressure.
type stalledWriteConn struct {
	net.Conn
	mu        sync.Mutex
	enabled   bool
	entered   chan struct{}
	stopped   chan struct{}
	enterOnce sync.Once
	closeOnce sync.Once
}

func (c *stalledWriteConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	enabled := c.enabled
	c.mu.Unlock()
	if enabled {
		c.enterOnce.Do(func() { close(c.entered) })
		<-c.stopped
		return 0, net.ErrClosed
	}
	return c.Conn.Write(p)
}
func (c *stalledWriteConn) Close() error {
	c.closeOnce.Do(func() { close(c.stopped) })
	return c.Conn.Close()
}

type stalledWriteListener struct {
	net.Listener
	accepted chan *stalledWriteConn
}

func (l *stalledWriteListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	wrapped := &stalledWriteConn{Conn: conn, entered: make(chan struct{}), stopped: make(chan struct{})}
	l.accepted <- wrapped
	return wrapped, nil
}
func TestUnreadRuntimeResponseDoesNotBlockAnotherClient(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	wrapped := &stalledWriteListener{Listener: listener, accepted: make(chan *stalledWriteConn, 2)}
	s, addr := runningServerOnListener(t, wrapped)
	first, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+s.Page.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	socket := <-wrapped.accepted
	wireCall(t, first, 1, "Runtime.evaluate", map[string]any{"expression": "1"})
	blocked := s.clientSnapshot()[0]
	second, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+s.Page.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	wireCall(t, second, 1, "Runtime.evaluate", map[string]any{"expression": "1"})
	socket.mu.Lock()
	socket.enabled = true
	socket.mu.Unlock()
	if err := first.WriteJSON(map[string]any{"id": 2, "method": "Runtime.evaluate", "params": map[string]any{"expression": "({value:1})", "returnByValue": true}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-socket.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("socket write did not enter")
	}
	_ = second.SetReadDeadline(time.Now().Add(time.Second))
	result := wireCall(t, second, 2, "Runtime.evaluate", map[string]any{"expression": "6*7", "returnByValue": true})
	if result["result"].(map[string]any)["value"] != float64(42) {
		t.Fatal(result)
	}
	_ = first.Close()
	select {
	case <-blocked.writerDone:
	case <-time.After(3 * time.Second):
		t.Fatal("disconnect retained writer")
	}
	blocked.outMu.Lock()
	defer blocked.outMu.Unlock()
	if blocked.outBytes != 0 || len(blocked.outbox) != 0 {
		t.Fatal("closed writer retained payloads")
	}
}
func TestOutboundOverflowClosesConnectionAndReleasesQueue(t *testing.T) {
	for _, limit := range []string{"bytes", "messages"} {
		t.Run(limit, func(t *testing.T) {
			s, addr := runningServer(t)
			wire := browserConnection(t, addr)
			wireCall(t, wire, 1, "Runtime.evaluate", map[string]any{"expression": "1"})
			c := s.clientSnapshot()[0]
			c.writeMu.Lock()
			if limit == "bytes" {
				c.enqueueWire(make([]byte, maxQueuedWireBytes), nil)
				c.enqueueWire([]byte("x"), nil)
			} else {
				for i := 0; i < 258; i++ {
					c.enqueueWire([]byte("x"), nil)
				}
			}
			c.writeMu.Unlock()
			select {
			case <-c.ctx.Done():
			case <-time.After(time.Second):
				t.Fatal("overflow did not cancel transport")
			}
			select {
			case <-c.writerDone:
			case <-time.After(time.Second):
				t.Fatal("overflow retained writer")
			}
			c.outMu.Lock()
			defer c.outMu.Unlock()
			if c.outBytes != 0 || len(c.outbox) != 0 {
				t.Fatal("overflow retained payloads")
			}
		})
	}
}

func TestBlockedResponseWriterDoesNotOwnPage(t *testing.T) {
	t.Setenv("MIMIC_PROFILE_CDP", "1")
	s, addr := runningServer(t)
	s.Page.Trace().Start()
	dial := func() *websocket.Conn {
		c, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+s.Page.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		wireCall(t, c, 1, "Runtime.evaluate", map[string]any{"expression": "1"})
		return c
	}
	first := dial()
	var blocked *connection
	for _, c := range s.clientSnapshot() {
		blocked = c
	}
	second := dial()
	blocked.writeMu.Lock()
	defer blocked.writeMu.Unlock()
	finished := make(chan struct{})
	var once sync.Once
	unsubscribe := s.Page.Trace().SubscribeKinds([]trace.Kind{trace.CDP}, func(e trace.Event) {
		if e.Name == "evaluatePhase" && e.Data["callID"] == "evaluate::2" && e.Data["phase"] == "finish" {
			once.Do(func() { close(finished) })
		}
	})
	defer unsubscribe()
	if err := first.WriteJSON(map[string]any{"id": 2, "method": "Runtime.evaluate", "params": map[string]any{"expression": "globalThis.writerMarker=42;({value:1})", "returnByValue": true}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("evaluation did not finish")
	}
	_ = second.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	result := wireCall(t, second, 3, "Runtime.evaluate", map[string]any{"expression": "writerMarker", "returnByValue": true})
	if result["result"].(map[string]any)["value"] != float64(42) {
		t.Fatal(result)
	}
}
