package browser

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestWebCryptoURLSelectorsChromeOracle(t *testing.T) {
	checkCompatibilityOracle(t, "webcrypto_url_selectors", []string{"urls", "selectors", "errors", "hmacResult", "aes"})
}

func TestCSSDOMObservationsChromeOracle(t *testing.T) {
	checkCompatibilityOracle(t, "css_dom_observations", []string{"pseudo", "registered", "errors", "typed", "shadow", "rangeResult"})
}

func TestEncodingStorageCryptoChromeOracle(t *testing.T) {
	checkCompatibilityOracle(t, "encoding_storage_crypto", []string{"encodings", "decoded", "malformed", "storage", "cryptography"})
}

func TestWorkerCryptoEncodingChromeOracle(t *testing.T) {
	checkCompatibilityOracle(t, "worker_crypto_encoding", []string{"encodings", "decoded", "malformed", "cryptography"})
}

func checkCompatibilityOracle(t *testing.T, name string, observations []string) {
	t.Helper()
	parallelBrowserTest(t)
	fixture, err := os.ReadFile("testdata/" + name + "_oracle.js")
	if err != nil {
		t.Fatal(err)
	}
	reference, err := os.ReadFile("testdata/" + name + "_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	var oracle struct {
		Result   map[string]json.RawMessage `json:"result"`
		Metadata struct {
			FixtureSHA256 string `json:"fixtureSHA256"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal(reference, &oracle); err != nil {
		t.Fatal(err)
	}
	fixtureHash := sha256.Sum256([]byte(strings.ReplaceAll(string(fixture), "\r\n", "\n")))
	if hex.EncodeToString(fixtureHash[:]) != oracle.Metadata.FixtureSHA256 {
		t.Fatal("fixture does not match the saved Chrome capture")
	}
	historyTestPages(t, func(t *testing.T, p *Page) {
		navigateCapabilityFixture(t, p)
		script := "(" + string(fixture) + ").then(value=>JSON.stringify(value))"
		if strings.HasPrefix(name, "worker_") {
			workerSource, _ := json.Marshal("(" + string(fixture) + ").then(value=>postMessage(value),error=>postMessage({error:String(error)}))")
			script = `new Promise((resolve,reject)=>{const url=URL.createObjectURL(new Blob([` + string(workerSource) + `],{type:'text/javascript'})),worker=new Worker(url);const cleanup=()=>{worker.terminate();URL.revokeObjectURL(url)};worker.onmessage=event=>{cleanup();event.data.error?reject(new Error(event.data.error)):resolve(JSON.stringify(event.data))};worker.onerror=event=>{cleanup();reject(new Error(event.message))}})`
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		value, err := p.Evaluate(ctx, script)
		if err != nil {
			t.Fatal(err)
		}
		var got map[string]json.RawMessage
		if err := json.Unmarshal([]byte(value.(string)), &got); err != nil {
			t.Fatal(err)
		}
		for _, name := range observations {
			var actual, expected any
			if err := json.Unmarshal(got[name], &actual); err != nil {
				t.Fatalf("invalid %s observation: %v", name, err)
			}
			if err := json.Unmarshal(oracle.Result[name], &expected); err != nil {
				t.Fatalf("invalid %s reference: %v", name, err)
			}
			if name == "storage" {
				// Preserve event order within each storage area. Local and session
				// notification paths need not arrive in a shared cross-area order.
				for _, observation := range []any{actual, expected} {
					rows := observation.(map[string]any)["rows"].([]any)
					sort.SliceStable(rows, func(i, j int) bool { return rows[i].([]any)[4].(string) < rows[j].([]any)[4].(string) })
				}
			}
			actualJSON, _ := json.Marshal(actual)
			expectedJSON, _ := json.Marshal(expected)
			if string(actualJSON) != string(expectedJSON) {
				t.Errorf("%s: got %s want %s", name, actualJSON, expectedJSON)
			}
		}
	})
}
