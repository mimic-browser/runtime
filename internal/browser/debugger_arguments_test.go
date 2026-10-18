package browser

import (
	"context"
	"testing"
)

func TestDebuggerCallArgumentsHaveRealmPrototypes(t *testing.T) {
	historyTestPages(t, func(t *testing.T, page *Page) {
		d := NewDebugger(page)
		defer d.Close()
		params := map[string]any{"arguments": []any{
			map[string]any{"value": map[string]any{"items": []any{map[string]any{"name": "first"}, "second"}}},
			map[string]any{},
		}}
		got, err := d.CallFunction(context.Background(), "", "", `function(value, missing) {
			return Object.getPrototypeOf(value) === Object.prototype &&
				Object.getPrototypeOf(value.items) === Array.prototype &&
				Object.getPrototypeOf(value.items[0]) === Object.prototype &&
				[...value.items].map(item => typeof item === 'string' ? item : item.name).join(',') === 'first,second' &&
				missing === undefined;
		}`, params, DebuggerOptions{ReturnByValue: true})
		if err != nil || got["result"].(map[string]any)["value"] != true {
			t.Fatalf("call arguments: %v, %v", got, err)
		}
	})
}
