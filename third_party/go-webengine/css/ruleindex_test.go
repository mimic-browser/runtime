// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"reflect"
	"testing"

	"github.com/go-webengine/engine/dom"
)

func indexOf(t *testing.T, src string) []Rule {
	t.Helper()
	return ParseStylesheet(src)
}

func TestRuleIndexBucketsBySubjectKey(t *testing.T) {
	rules := indexOf(t, `
		#main { color: red }
		.card { color: green }
		div { color: blue }
		* { color: black }
		:root { color: white }
		.card p { color: gray }
		.a.b { color: pink }
	`)
	ix := buildRuleIndex(rules)
	if !reflect.DeepEqual(ix.byID["main"], []int{0}) {
		t.Errorf("byID[main] = %v, want [0]", ix.byID["main"])
	}
	if !reflect.DeepEqual(ix.byClass["card"], []int{1}) {
		t.Errorf("byClass[card] = %v, want [1] (`.card p` keys on its subject p, not .card)", ix.byClass["card"])
	}
	if !reflect.DeepEqual(ix.byTag["div"], []int{2}) {
		t.Errorf("byTag[div] = %v, want [2]", ix.byTag["div"])
	}
	// `*` and `:root` have no key, so they must be tested for every element.
	if !reflect.DeepEqual(ix.always, []int{3, 4}) {
		t.Errorf("always = %v, want [3 4] (universal and :root subjects)", ix.always)
	}
	if !reflect.DeepEqual(ix.byTag["p"], []int{5}) {
		t.Errorf("byTag[p] = %v, want [5]: `.card p` keys on its subject p, not its ancestor .card", ix.byTag["p"])
	}
}

func TestRuleIndexCandidatesAreSortedAndDeduped(t *testing.T) {
	rules := indexOf(t, `
		.x, .y { color: red }
		div.x { color: blue }
		* { color: black }
	`)
	ix := buildRuleIndex(rules)
	n := &dom.Node{Type: dom.Element, Tag: "div", Attr: map[string]string{"class": "x y", "id": ""}}
	got := ix.candidates(n)
	if !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Errorf("candidates = %v, want [0 1 2] (sorted, the .x,.y rule once, universal included)", got)
	}
}

func TestRuleIndexCandidatesIncludeIDAndTag(t *testing.T) {
	rules := indexOf(t, `
		#hero { color: red }
		section { color: blue }
		.other { color: green }
	`)
	ix := buildRuleIndex(rules)
	n := &dom.Node{Type: dom.Element, Tag: "section", Attr: map[string]string{"id": "hero"}}
	if got := ix.candidates(n); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Errorf("candidates = %v, want [0 1]", got)
	}
}

func TestRuleIndexEmptySelectorAndHostSubjectAreAlways(t *testing.T) {
	ix := &ruleIndex{byID: map[string][]int{}, byClass: map[string][]int{}, byTag: map[string][]int{}}
	ix.add(7, Selector{})
	ix.add(8, Selector{parts: []compound{{Host: true}}})
	ix.add(9, Selector{parts: []compound{{Part: "x"}}})
	if !reflect.DeepEqual(ix.always, []int{7, 8, 9}) {
		t.Errorf("always = %v, want [7 8 9]", ix.always)
	}
}
