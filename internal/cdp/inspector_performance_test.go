package cdp

import (
	"slices"
	"testing"
)

func TestInspectorWideTreeInputPreservesBindings(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `document.body.innerHTML='<input id="probe">'+Array.from({length:1000},(_,i)=>'<p><b>row '+i+'</b></p>').join('');document.querySelector('#probe').focus()`})
	w.call(t, id, "DOM.getDocument", map[string]any{"depth": -1})
	w.call(t, id, "CSS.enable", map[string]any{})
	for i := 0; i < 3; i++ {
		w.call(t, id, "Input.insertText", map[string]any{"text": "a"})
	}
	result := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "document.querySelector('#probe').value", "returnByValue": true})["result"].(map[string]any)
	if result["value"] != "aaa" {
		t.Fatalf("input lost: %v", result)
	}
	for {
		select {
		case event := <-w.events:
			if event["method"] == "DOM.childNodeRemoved" || event["method"] == "DOM.childNodeInserted" {
				t.Fatalf("typing recreated bindings: %v", event)
			}
		default:
			return
		}
	}
}

func TestInspectorChildDiffPreservesRelativeOrder(t *testing.T) {
	for _, test := range []struct {
		name                             string
		before, after, removed, inserted []int64
	}{
		{"unchanged", []int64{1, 2, 3}, []int64{1, 2, 3}, nil, nil},
		{"insert", []int64{1, 2, 3}, []int64{1, 4, 2, 3}, nil, []int64{4}},
		{"remove", []int64{1, 2, 3}, []int64{1, 3}, []int64{2}, nil},
		{"move", []int64{1, 2, 3}, []int64{3, 1, 2}, []int64{1, 2, 3}, []int64{1, 2, 3}},
		{"combined", []int64{1, 2, 3, 4}, []int64{4, 2, 5}, []int64{1, 2, 3, 4}, []int64{2, 4, 5}},
	} {
		t.Run(test.name, func(t *testing.T) {
			changes := inspectorChildrenChanges(test.before, test.after)
			keys := func(set map[int64]bool) []int64 {
				var ids []int64
				for id := range set {
					ids = append(ids, id)
				}
				slices.Sort(ids)
				return ids
			}
			if !slices.Equal(keys(changes.removed), test.removed) || !slices.Equal(keys(changes.inserted), test.inserted) {
				t.Fatalf("child diff: %#v", changes)
			}
		})
	}
}
