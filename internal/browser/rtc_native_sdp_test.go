//go:build (windows || linux) && amd64

package browser

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	chrome152 "github.com/moreveal/mimic/chrome/152"
)

// This is an additional scoped assertion against unchanged frozen evidence.
// The complete RTC session oracle continues to test the full Chrome codec
// catalog and the reference machine's candidate enumeration.
func TestRTCNativeDataSDPMatchesFrozenChrome152(t *testing.T) {
	serialBrowserTest(t)
	source, err := os.ReadFile("testdata/rtc_session_oracle.js")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile("testdata/rtc_session_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	var oracle struct {
		Result struct {
			Result struct {
				Value map[string]any `json:"value"`
			} `json:"result"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &oracle); err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"ordinary", "snapshot"} {
		t.Run(mode, func(t *testing.T) {
			var page *Page
			if mode == "snapshot" {
				seed := bootstrapSnapshotPage(t)
				navigateCapabilityFixture(t, seed)
				bootstrapSnapshotWarm(t, seed)
				page, err = seed.ctx.NewPage()
				if err != nil {
					t.Fatal(err)
				}
			} else {
				browser, err := New(ordinaryOracleFactory{}, chrome152.New())
				if err != nil {
					t.Fatal(err)
				}
				ctx := browser.NewContext()
				t.Cleanup(func() { _ = ctx.Close() })
				page, err = ctx.NewPage()
				if err != nil {
					t.Fatal(err)
				}
			}
			navigateCapabilityFixture(t, page)
			value := bootstrapSnapshotEvaluate(t, page, "(async()=>JSON.stringify(await "+string(source)+"))()")
			if mode == "snapshot" && !page.Top.Realm.bootstrapRestored {
				t.Fatal("not a restored realm")
			}
			var actual map[string]any
			if err := json.Unmarshal([]byte(value.(string)), &actual); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"empty", "data"} {
				if !reflect.DeepEqual(actual[key], oracle.Result.Result.Value[key]) {
					t.Errorf("%s: got %#v; want %#v", key, actual[key], oracle.Result.Result.Value[key])
				}
			}
			for _, key := range []string{"false", "true", "both"} {
				got := actual["lifecycle"].(map[string]any)[key].(map[string]any)
				want := oracle.Result.Result.Value["lifecycle"].(map[string]any)[key].(map[string]any)
				for _, field := range []string{"log", "local", "current", "pending", "mid"} {
					if !reflect.DeepEqual(got[field], want[field]) {
						t.Errorf("lifecycle %s %s: got %#v; want %#v", key, field, got[field], want[field])
					}
				}
			}
			_ = page.Close()
		})
	}
}
