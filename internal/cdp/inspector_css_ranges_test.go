package cdp

import "testing"

func TestInspectorCSSSelectorRangesAndIdentity(t *testing.T) {
	s, addr := runningServer(t)
	_, err := evaluatePageFixture(s.Page, `document.head.innerHTML='<style>.before { color: red; }\n@media (min-width: 1px) {.before {background: blue}}</style>';document.body.innerHTML='<div id="probe" class="before">Ω</div>';globalThis.savedRule=document.styleSheets[0].cssRules[0];globalThis.savedStyle=savedRule.style`)
	if err != nil {
		t.Fatal(err)
	}
	w, id := inspectorSession(t, s, addr)
	root := w.call(t, id, "DOM.getDocument", map[string]any{"depth": -1})["root"]
	node := findInspectorNode(root, "id", "probe")
	w.call(t, id, "CSS.enable", map[string]any{})
	matched := w.call(t, id, "CSS.getMatchedStylesForNode", map[string]any{"nodeId": node["nodeId"]})
	rules := matched["matchedCSSRules"].([]any)
	if len(rules) != 2 {
		t.Fatalf("conditional matches: %v", matched)
	}
	rule := rules[0].(map[string]any)["rule"].(map[string]any)
	selector := rule["selectorList"].(map[string]any)
	w.call(t, id, "CSS.setRuleSelector", map[string]any{"styleSheetId": rule["styleSheetId"], "range": selector["range"], "selector": "#probe"})
	value, err := evaluatePageFixture(s.Page, `savedRule===document.styleSheets[0].cssRules[0]&&savedStyle===savedRule.style&&savedRule.selectorText==='#probe'`)
	if err != nil || value != true {
		t.Fatalf("selector edit replaced canonical CSS objects: %v %v", value, err)
	}
	text := w.call(t, id, "CSS.getStyleSheetText", map[string]any{"styleSheetId": rule["styleSheetId"]})
	if text["text"] == "" {
		t.Fatal("edited source lost")
	}
	before := text["text"]
	reply := w.request(t, id, "CSS.setStyleTexts", map[string]any{"edits": []any{map[string]any{"styleSheetId": rule["styleSheetId"], "range": map[string]any{"startLine": 999, "startColumn": 0, "endLine": 999, "endColumn": 1}, "text": "color: green"}}})
	if reply["error"] == nil {
		t.Fatal("invalid source range accepted")
	}
	if after := w.call(t, id, "CSS.getStyleSheetText", map[string]any{"styleSheetId": rule["styleSheetId"]})["text"]; after != before {
		t.Fatal("invalid edit mutated source")
	}
	w.call(t, id, "CSS.disable", map[string]any{})
	if reply := w.request(t, id, "CSS.getMatchedStylesForNode", map[string]any{"nodeId": node["nodeId"]}); reply["error"] == nil {
		t.Fatal("disabled CSS agent served retained source")
	}
}

func TestInspectorDOMSearchAndRelease(t *testing.T) {
	s, addr := runningServer(t)
	_, err := evaluatePageFixture(s.Page, `document.body.innerHTML='<div id="probe">unique Ω text</div><iframe></iframe>';document.querySelector('iframe').contentDocument.body.innerHTML='<span id="child">unique Ω text</span>'`)
	if err != nil {
		t.Fatal(err)
	}
	w, id := inspectorSession(t, s, addr)
	w.call(t, id, "DOM.getDocument", map[string]any{"depth": 0})
	for _, query := range []string{"#probe", "unique Ω text"} {
		search := w.call(t, id, "DOM.performSearch", map[string]any{"query": query})
		count := int(coordinateValue(search["resultCount"]))
		if query == "#probe" && count != 1 || query == "unique Ω text" && count != 2 {
			t.Fatalf("search %s: %v", query, search)
		}
		result := w.call(t, id, "DOM.getSearchResults", map[string]any{"searchId": search["searchId"], "fromIndex": 0, "toIndex": count})
		for _, nodeID := range result["nodeIds"].([]any) {
			w.call(t, id, "DOM.describeNode", map[string]any{"nodeId": nodeID})
		}
		w.call(t, id, "DOM.discardSearchResults", map[string]any{"searchId": search["searchId"]})
		if reply := w.request(t, id, "DOM.getSearchResults", map[string]any{"searchId": search["searchId"], "fromIndex": 0, "toIndex": 1}); reply["error"] == nil {
			t.Fatal("discarded search retained results")
		}
	}
}
