package optimize

import (
	"encoding/json"
	"github.com/gorilla/websocket"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestProcessHelper(t *testing.T) {
	if os.Getenv("MIMIC_PROCESS_HELPER") != "1" {
		return
	}
	mode := os.Args[len(os.Args)-2]
	path := os.Args[len(os.Args)-1]
	if mode == "parent" {
		c := exec.Command(os.Args[0], "-test.run=TestProcessHelper", "--", "child", path)
		c.Env = os.Environ()
		if c.Start() != nil {
			os.Exit(2)
		}
		for {
			time.Sleep(time.Second)
		}
	}
	for {
		_ = os.WriteFile(path, []byte(time.Now().String()), 0600)
		time.Sleep(20 * time.Millisecond)
	}
}
func TestOwnedProcessTreeStopsDescendants(t *testing.T) {
	path := filepath.Join(t.TempDir(), "heartbeat")
	c := exec.Command(os.Args[0], "-test.run=TestProcessHelper", "--", "parent", path)
	c.Env = append(os.Environ(), "MIMIC_PROCESS_HELPER=1")
	p, err := startProcess(c)
	if err != nil {
		t.Fatal(err)
	}
	defer p.stop()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(path); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("descendant did not start")
		}
		time.Sleep(20 * time.Millisecond)
	}
	p.stop()
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Fatal("descendant survived process-tree cleanup")
	}
	p.stop() // ownership teardown is idempotent
}
func TestExternalOracleHelper(t *testing.T) {
	if os.Getenv("MIMIC_ORACLE_HELPER") != "1" {
		return
	}
	if os.Args[len(os.Args)-1] != "misdirected" {
		response, err := http.Get(os.Getenv("MIMIC_CDP_URL") + "/json/version")
		if err != nil {
			os.Exit(3)
		}
		var version struct {
			URL string `json:"webSocketDebuggerUrl"`
		}
		err = json.NewDecoder(response.Body).Decode(&version)
		response.Body.Close()
		if err != nil {
			os.Exit(3)
		}
		socket, _, err := websocket.DefaultDialer.Dial(version.URL, nil)
		if err != nil {
			os.Exit(3)
		}
		err = socket.WriteJSON(map[string]any{"id": 1, "method": "Browser.getVersion"})
		if err != nil {
			os.Exit(3)
		}
		_, _, err = socket.ReadMessage()
		socket.Close()
		if err != nil {
			os.Exit(3)
		}
	}
	switch os.Args[len(os.Args)-1] {
	case "misdirected":
		os.Exit(0)
	case "pass":
		os.Exit(0)
	case "fail":
		os.Exit(7)
	case "timeout":
		time.Sleep(10 * time.Second)
		os.Exit(0)
	case "result-good", "result-changed":
		body := `{"price":12,"items":["verified"]}`
		if os.Args[len(os.Args)-1] == "result-changed" {
			body = `{"price":99,"items":["verified"]}`
		}
		if err := os.WriteFile(os.Getenv("WORKLOAD_RESULT"), []byte(body), 0600); err != nil {
			os.Exit(3)
		}
		os.Exit(0)
	case "result-state":
		body, _ := json.Marshal(map[string]string{"input": os.Getenv("ARTICLE")})
		if err := os.WriteFile(os.Getenv("WORKLOAD_RESULT"), body, 0600); err != nil {
			os.Exit(3)
		}
		os.Exit(0)
	}
}
