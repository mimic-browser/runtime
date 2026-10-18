package cdp

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"

	"github.com/moreveal/mimic/internal/dom"
)

func TestDOMWhitespaceClassification(t *testing.T) {
	s := &session{}
	for code := 0; code <= 32; code++ {
		want := !(code >= 9 && code <= 13 || code == 32)
		if got := s.inspectorVisible(dom.Node{Type: "text", Text: string(rune(code))}); got != want {
			t.Errorf("ASCII %d visible=%v, want %v", code, got, want)
		}
	}
	for _, text := range []string{"", " \t\r\n\v\f"} {
		if s.inspectorVisible(dom.Node{Type: "text", Text: text}) {
			t.Errorf("whitespace visible: %q", text)
		}
		if !s.inspectorVisible(dom.Node{Type: "comment", Text: text}) {
			t.Errorf("comment hidden: %q", text)
		}
	}
}

func whitespaceChildren(t *testing.T, node map[string]any) []string {
	t.Helper()
	children, _ := node["children"].([]any)
	if node["childNodeCount"] != float64(len(children)) {
		t.Fatalf("count does not match projected children: %v", node)
	}
	result := []string{}
	for _, child := range children {
		n := child.(map[string]any)
		result = append(result, n["nodeName"].(string)+":"+n["nodeValue"].(string))
	}
	return result
}

func TestDOMWhitespaceModes(t *testing.T) {
	fixture, err := os.ReadFile("testdata/dom_whitespace.html")
	if err != nil {
		t.Fatal(err)
	}
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	encoded, _ := json.Marshal(string(fixture))
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "document.open();document.write(" + string(encoded) + ");document.close()"})
	readJS := func() any {
		return w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `JSON.stringify([Array.from(document.documentElement.childNodes,n=>[n.nodeType,n.nodeValue]),Array.from(document.body.childNodes,n=>[n.nodeType,n.nodeValue])])`, "returnByValue": true})["result"].(map[string]any)["value"]
	}
	before := readJS()
	for _, mode := range []string{"implicit", "default", "none", "all", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			w.call(t, id, "DOM.disable", nil)
			if mode != "implicit" {
				params := map[string]any{}
				if mode != "default" {
					params["includeWhitespace"] = mode
				}
				w.call(t, id, "DOM.enable", params)
			}
			root := w.call(t, id, "DOM.getDocument", map[string]any{"depth": -1, "pierce": true})["root"]
			probe := findInspectorNode(root, "id", "probe")
			want := []string{"SPAN:"}
			if mode == "all" {
				want = []string{"#text: \t", "SPAN:", "#text:\n "}
			}
			if got := whitespaceChildren(t, probe); !reflect.DeepEqual(got, want) {
				t.Fatalf("children: %q, want %q", got, want)
			}
			for _, name := range []string{"vt", "ff"} {
				node := findInspectorNode(root, "id", name)
				count := 0
				if mode == "all" {
					count = 1
				}
				if len(whitespaceChildren(t, node)) != count {
					t.Fatalf("ASCII whitespace %s: %v", name, node)
				}
			}
			if got := whitespaceChildren(t, findInspectorNode(root, "id", "nbsp")); !reflect.DeepEqual(got, []string{"#text:\u00a0"}) {
				t.Fatalf("NBSP filtered: %q", got)
			}
			w.call(t, id, "DOM.requestChildNodes", map[string]any{"nodeId": probe["nodeId"], "depth": -1})
			children := w.event(t, "DOM.setChildNodes")["nodes"].([]any)
			if len(children) != len(want) {
				t.Fatalf("requestChildNodes: %v", children)
			}
			// Chrome ignores repeated enable calls until the domain is disabled.
			w.call(t, id, "DOM.enable", map[string]any{"includeWhitespace": "all"})
			root = w.call(t, id, "DOM.getDocument", map[string]any{"depth": -1})["root"]
			if got := whitespaceChildren(t, findInspectorNode(root, "id", "probe")); !reflect.DeepEqual(got, want) {
				t.Fatalf("repeated enable changed mode: %q", got)
			}
			if after := readJS(); after != before {
				t.Fatalf("inspector changed JavaScript childNodes: %v -> %v", before, after)
			}
		})
	}
}

func TestDOMWhitespaceMutationSessions(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	a, allID := inspectorSession(t, s, addr)
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `document.body.innerHTML='<div id="probe"> <span></span></div>';globalThis.probe=document.querySelector('#probe')`})
	a.call(t, allID, "DOM.enable", map[string]any{"includeWhitespace": "all"})
	root := w.call(t, id, "DOM.getDocument", map[string]any{"depth": -1})["root"]
	probe := findInspectorNode(root, "id", "probe")
	a.call(t, allID, "DOM.getDocument", map[string]any{"depth": -1})
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `probe.firstChild.data='x'`})
	inserted := w.event(t, "DOM.childNodeInserted")
	if inserted["parentNodeId"] != probe["nodeId"] || inserted["previousNodeId"] != float64(0) || inserted["node"].(map[string]any)["nodeValue"] != "x" {
		t.Fatalf("whitespace became text: %v", inserted)
	}
	if got := a.event(t, "DOM.characterDataModified")["characterData"]; got != "x" {
		t.Fatalf("all session text: %v", got)
	}
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `probe.firstChild.data=' '`})
	removed := w.event(t, "DOM.childNodeRemoved")
	if removed["nodeId"] != inserted["node"].(map[string]any)["nodeId"] {
		t.Fatalf("text became whitespace: %v", removed)
	}
	a.event(t, "DOM.characterDataModified")
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `probe.append(document.createTextNode(' '));probe.append(document.createComment('new'))`})
	inserted = w.event(t, "DOM.childNodeInserted")
	if inserted["node"].(map[string]any)["nodeType"] != float64(8) {
		t.Fatalf("hidden whitespace inserted: %v", inserted)
	}
	if previous := inserted["previousNodeId"]; previous != probe["children"].([]any)[0].(map[string]any)["nodeId"] {
		t.Fatalf("previous sibling includes hidden whitespace: %v", inserted)
	}
	// A reply barrier lets us check that hidden text caused no extra events.
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "0"})
	for {
		select {
		case event := <-w.events:
			if event["method"] == "DOM.childNodeInserted" || event["method"] == "DOM.characterDataModified" {
				t.Fatalf("unexpected hidden-node event: %v", event)
			}
		default:
			return
		}
	}
}

func TestDOMWhitespaceUnexpandedCounts(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `document.body.innerHTML='<div id="probe"> <span></span></div>';globalThis.probe=document.querySelector('#probe')`})
	root := w.call(t, id, "DOM.getDocument", map[string]any{"depth": 3})["root"]
	probe := findInspectorNode(root, "id", "probe")
	if probe["childNodeCount"] != float64(1) {
		t.Fatalf("unexpanded count: %v", probe)
	}
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `probe.append(document.createTextNode(' '));probe.append(document.createComment('new'))`})
	count := w.event(t, "DOM.childNodeCountUpdated")
	if count["nodeId"] != probe["nodeId"] || count["childNodeCount"] != float64(2) {
		t.Fatalf("unexpanded mutation count: %v", count)
	}
	w.call(t, id, "DOM.requestChildNodes", map[string]any{"nodeId": probe["nodeId"]})
	if children := w.event(t, "DOM.setChildNodes")["nodes"].([]any); len(children) != 2 {
		t.Fatalf("expanded count: %v", children)
	}
}

func TestDOMWhitespaceFrameShadowAndQueryProjection(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `document.body.innerHTML=' <div id="host"></div> <iframe></iframe> ';globalThis.shadow=document.querySelector('#host').attachShadow({mode:'closed'});shadow.innerHTML=' <!--keep--><span id="shadow-child"></span> ';document.querySelector('iframe').contentDocument.body.innerHTML=' <div id="frame-child"> </div> '`})
	for _, mode := range []string{"none", "all"} {
		w.call(t, id, "DOM.disable", nil)
		w.call(t, id, "DOM.enable", map[string]any{"includeWhitespace": mode})
		root := w.call(t, id, "DOM.getDocument", map[string]any{"depth": -1, "pierce": true})["root"]
		host := findInspectorNode(root, "id", "host")
		shadow := host["shadowRoots"].([]any)[0].(map[string]any)
		wantShadow, wantFrame := 2, 0
		if mode == "all" {
			wantShadow, wantFrame = 4, 1
		}
		if got := len(whitespaceChildren(t, shadow)); got != wantShadow {
			t.Fatalf("%s shadow: %v", mode, shadow)
		}
		if got := len(whitespaceChildren(t, findInspectorNode(root, "id", "frame-child"))); got != wantFrame {
			t.Fatalf("%s frame children: %d", mode, got)
		}
		w.call(t, id, "DOM.requestChildNodes", map[string]any{"nodeId": shadow["nodeId"], "depth": -1})
		if got := len(w.event(t, "DOM.setChildNodes")["nodes"].([]any)); got != wantShadow {
			t.Fatalf("%s shadow request: %d", mode, got)
		}
	}
	// Query ancestor publication shares the filter even without a prior tree.
	w.call(t, id, "DOM.disable", nil)
	w.call(t, id, "DOM.enable", nil)
	root := w.call(t, id, "DOM.getDocument", map[string]any{"depth": 1})["root"].(map[string]any)
	w.call(t, id, "DOM.querySelector", map[string]any{"nodeId": root["nodeId"], "selector": "#host"})
	for {
		select {
		case event := <-w.events:
			if event["method"] == "DOM.setChildNodes" {
				for _, child := range event["params"].(map[string]any)["nodes"].([]any) {
					node := child.(map[string]any)
					if node["nodeType"] == float64(3) && node["nodeValue"] == " " {
						t.Fatalf("query published whitespace: %v", event)
					}
				}
			}
		default:
			return
		}
	}
}

// Expectations replay the retained Chrome 152 count/detail captures. The
// frontend counter intentionally drifts from the canonical child list until
// the parent is expanded; this is observable Chrome inspector behavior.
func TestDOMWhitespaceUnexpandedCharacterCounters(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	a, allID := inspectorSession(t, s, addr)
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `document.body.innerHTML='<div id="probe"> <span></span>x</div>';globalThis.probe=document.querySelector('#probe')`})
	a.call(t, allID, "DOM.enable", map[string]any{"includeWhitespace": "all"})
	root := w.call(t, id, "DOM.getDocument", map[string]any{"depth": 3})["root"]
	probe := findInspectorNode(root, "id", "probe")
	allRoot := a.call(t, allID, "DOM.getDocument", map[string]any{"depth": 3})["root"]
	allProbe := findInspectorNode(allRoot, "id", "probe")
	assertCounts := func(w *inspectorWire, nodeID any, expected ...int) {
		t.Helper()
		got := []int{}
		for {
			select {
			case event := <-w.events:
				if event["method"] == "DOM.childNodeCountUpdated" {
					params := event["params"].(map[string]any)
					if params["nodeId"] != nodeID {
						t.Fatalf("wrong parent: %v", event)
					}
					got = append(got, int(params["childNodeCount"].(float64)))
				}
			default:
				if !reflect.DeepEqual(got, append([]int{}, expected...)) {
					t.Fatalf("counts %v, want %v", got, expected)
				}
				return
			}
		}
	}
	for _, step := range []struct {
		expression string
		none, all  []int
	}{
		{`probe.firstChild.data='x'`, []int{3}, []int{4}},
		{`probe.firstChild.data='x'`, []int{4}, []int{5}},
		{`probe.firstChild.data=' '`, nil, []int{6}},
		{`probe.firstChild.data=' '`, nil, []int{7}},
		{`probe.append(document.createTextNode(' '))`, nil, []int{8}},
		{`probe.append(document.createComment('c'))`, []int{5}, []int{9}},
		{`probe.lastChild.remove()`, []int{4}, []int{8}},
	} {
		t.Log(step.expression)
		w.call(t, id, "Runtime.evaluate", map[string]any{"expression": step.expression})
		// Each session's reply is its publication barrier.
		a.call(t, allID, "Runtime.evaluate", map[string]any{"expression": "0"})
		assertCounts(w, probe["nodeId"], step.none...)
		assertCounts(a, allProbe["nodeId"], step.all...)
	}
	for _, client := range []struct {
		w     *inspectorWire
		id    string
		node  map[string]any
		count int
	}{
		{w, id, probe, 2}, {a, allID, allProbe, 4},
	} {
		client.w.call(t, client.id, "DOM.requestChildNodes", map[string]any{"nodeId": client.node["nodeId"]})
		if children := client.w.event(t, "DOM.setChildNodes")["nodes"].([]any); len(children) != client.count {
			t.Fatalf("expansion: %v", children)
		}
	}
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `probe.firstChild.data='x'`})
	w.event(t, "DOM.childNodeInserted")
	a.event(t, "DOM.characterDataModified")
	a.call(t, allID, "Runtime.evaluate", map[string]any{"expression": "0"})
	assertCounts(w, probe["nodeId"])
	assertCounts(a, allProbe["nodeId"])
}

func TestDOMWhitespaceCharacterCounterSettersAndBurst(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	for _, setter := range []string{"data", "nodeValue", "textContent"} {
		t.Run(setter, func(t *testing.T) {
			w.call(t, id, "DOM.disable", nil)
			w.call(t, id, "DOM.enable", map[string]any{"includeWhitespace": "all"})
			w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `document.body.innerHTML='<div id="probe"> </div>';globalThis.probe=document.querySelector('#probe')`})
			root := w.call(t, id, "DOM.getDocument", map[string]any{"depth": 3})["root"]
			probe := findInspectorNode(root, "id", "probe")
			w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `probe.firstChild.` + setter + `=' ';probe.firstChild.` + setter + `='x';probe.firstChild.` + setter + `=' '`})
			for _, expected := range []float64{2, 3, 4} {
				event := w.event(t, "DOM.childNodeCountUpdated")
				if event["nodeId"] != probe["nodeId"] || event["childNodeCount"] != expected {
					t.Fatalf("counter: %v, want %v", event, expected)
				}
			}
			result := w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "probe.childNodes.length", "returnByValue": true})
			if result["result"].(map[string]any)["value"] != float64(1) {
				t.Fatalf("counter changed DOM: %v", result)
			}
		})
	}
}

func TestDOMWhitespaceCharacterCounterLifecycle(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	var session *session
	s.Page.LockCommands()
	for _, client := range s.clientSnapshot() {
		for _, candidate := range client.snapshot() {
			if candidate.id == id {
				session = candidate
			}
		}
	}
	s.Page.UnlockCommands()
	if session == nil {
		t.Fatal("missing session")
	}
	for _, reset := range []string{"disable", "document"} {
		w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `document.body.innerHTML='<div id="probe"> </div>';globalThis.probe=document.querySelector('#probe')`})
		w.call(t, id, "DOM.enable", map[string]any{"includeWhitespace": "all"})
		root := w.call(t, id, "DOM.getDocument", map[string]any{"depth": 3})["root"]
		probe := findInspectorNode(root, "id", "probe")
		w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `probe.firstChild.data=' '`})
		if event := w.event(t, "DOM.childNodeCountUpdated"); event["nodeId"] != probe["nodeId"] || event["childNodeCount"] != float64(2) {
			t.Fatalf("cold counter: %v", event)
		}
		s.Page.LockCommands()
		old := session.domInspector
		s.Page.UnlockCommands()
		if reset == "disable" {
			w.call(t, id, "DOM.disable", nil)
		} else {
			w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `document.open();document.write('<!doctype html><body></body>');document.close()`})
			w.call(t, id, "Runtime.evaluate", map[string]any{"expression": "0"})
		}
		w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `probe.firstChild.data='x'`})
		old.characterMu.Lock()
		pending := len(old.characterChanges)
		old.characterMu.Unlock()
		if pending != 0 {
			t.Fatalf("%s retained character observer", reset)
		}
	}
}

func TestDOMWhitespaceCharacterCounterFrameAndShadow(t *testing.T) {
	s, addr := runningServer(t)
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `document.body.innerHTML='<div id="host"></div><iframe></iframe>';globalThis.shadow=document.querySelector('#host').attachShadow({mode:'closed'});shadow.innerHTML=' <span></span>';document.querySelector('iframe').contentDocument.body.innerHTML='<div id="child"> </div>'`})
	w.call(t, id, "DOM.enable", map[string]any{"includeWhitespace": "all"})
	root := w.call(t, id, "DOM.getDocument", map[string]any{"depth": 3})["root"]
	shadow := findInspectorNode(root, "id", "host")["shadowRoots"].([]any)[0].(map[string]any)
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `shadow.firstChild.data=' '`})
	if event := w.event(t, "DOM.childNodeCountUpdated"); event["nodeId"] != shadow["nodeId"] || event["childNodeCount"] != float64(3) {
		t.Fatalf("shadow counter: %v", event)
	}
	var content map[string]any
	var find func(any)
	find = func(value any) {
		switch node := value.(type) {
		case map[string]any:
			if document, ok := node["contentDocument"].(map[string]any); ok {
				content = document
			}
			find(node["children"])
		case []any:
			for _, child := range node {
				find(child)
			}
		}
	}
	find(root)
	if content == nil {
		t.Fatal("missing child document")
	}
	child := w.call(t, id, "DOM.querySelector", map[string]any{"nodeId": content["nodeId"], "selector": "#child"})["nodeId"]
	w.call(t, id, "Runtime.evaluate", map[string]any{"expression": `document.querySelector('iframe').contentDocument.querySelector('#child').firstChild.data=' '`})
	if event := w.event(t, "DOM.childNodeCountUpdated"); event["nodeId"] != child || event["childNodeCount"] != float64(2) {
		t.Fatalf("frame counter: %v", event)
	}
}
