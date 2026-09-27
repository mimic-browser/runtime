package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"testing"
)

func TestKeyboardActivationMatchesChrome152(t *testing.T) {
	parallelBrowserTest(t)
	raw, err := os.ReadFile("testdata/keyboard_activation_chrome152.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Results []struct {
			Tag, Text, Setup string
			Cancel           bool
			Events           any
		}
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	historyTestPages(t, func(t *testing.T, page *Page) {
		navigateCapabilityFixture(t, page)
		for _, scenario := range fixture.Results {
			t.Run(fmt.Sprintf("%s/text=%t/cancel=%t", scenario.Tag, scenario.Text != "", scenario.Cancel), func(t *testing.T) {
				ctx := context.Background()
				if _, err := page.Evaluate(ctx, scenario.Setup); err != nil {
					t.Fatal(err)
				}
				for _, kind := range []string{"keyDown", "keyUp"} {
					params := map[string]any{"type": kind, "key": "Enter", "code": "Enter", "windowsVirtualKeyCode": 13}
					if kind == "keyDown" && scenario.Text != "" {
						params["text"] = scenario.Text
					}
					if err := page.DispatchProtocolInput(ctx, "Input.dispatchKeyEvent", params); err != nil {
						t.Fatal(err)
					}
				}
				value, err := page.Evaluate(ctx, `JSON.stringify(events)`)
				if err != nil {
					t.Fatal(err)
				}
				var actual any
				if err := json.Unmarshal([]byte(value.(string)), &actual); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(actual, scenario.Events) {
					t.Fatalf("activation differs from Chrome 152: got %v, want %v", actual, scenario.Events)
				}
			})
		}
	})
}
