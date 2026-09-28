package cdp

import (
	"testing"

	"github.com/gorilla/websocket"
)

func TestTraceCaptureRequiresExplicitStart(t *testing.T) {
	s, addr := runningServer(t)
	c, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/devtools/page/"+s.Page.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	call := func(id int, method string) map[string]any {
		t.Helper()
		if err := c.WriteJSON(map[string]any{"id": id, "method": method}); err != nil {
			t.Fatal(err)
		}
		response := readReply(t, c, float64(id))
		if response["error"] != nil {
			t.Fatal(response)
		}
		return response
	}
	if got := call(1, "Mimic.getTrace")["result"].(map[string]any)["events"].([]any); len(got) != 0 {
		t.Fatalf("default capture retained %d events", len(got))
	}
	call(2, "Mimic.startTrace")
	call(3, "Mimic.getStatus")
	if got := call(4, "Mimic.getTrace")["result"].(map[string]any)["events"].([]any); len(got) == 0 {
		t.Fatal("explicit capture recorded no events")
	}
	call(5, "Mimic.stopTrace")
	count := len(call(6, "Mimic.getTrace")["result"].(map[string]any)["events"].([]any))
	call(7, "Mimic.getStatus")
	if got := len(call(8, "Mimic.getTrace")["result"].(map[string]any)["events"].([]any)); got != count {
		t.Fatalf("stopped capture changed from %d to %d events", count, got)
	}
}
