// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import "testing"

// text-transform's four keywords, and `inherit` — the property is nearly
// always set on a heading or a label class with the text in a child, so the
// inherited path is the one that carries it.
func TestTextTransformParsing(t *testing.T) {
	var s Style
	apply := func(v string) { s.apply(Declaration{Property: "text-transform", Value: v}, 16, nil) }
	for _, c := range []struct {
		value string
		want  TextTransform
	}{
		{"uppercase", TTUppercase},
		{"lowercase", TTLowercase},
		{"capitalize", TTCapitalize},
		{"none", TTNone},
		{"UPPERCASE", TTUppercase}, // keywords are case-insensitive
	} {
		s.TextTransform = TTCapitalize // a value none of the cases would leave behind
		if c.want == TTCapitalize {
			s.TextTransform = TTNone
		}
		apply(c.value)
		if s.TextTransform != c.want {
			t.Errorf("text-transform: %s gave %v, want %v", c.value, s.TextTransform, c.want)
		}
	}
	// An unknown keyword leaves the cascaded value alone rather than resetting it.
	s.TextTransform = TTUppercase
	apply("full-width")
	if s.TextTransform != TTUppercase {
		t.Errorf("an unimplemented keyword must not reset the value, got %v", s.TextTransform)
	}
}

func TestTextTransformInherits(t *testing.T) {
	parent := Style{TextTransform: TTUppercase}
	// Automatic inheritance, the path every child takes.
	if child := inheritFrom(parent); child.TextTransform != TTUppercase {
		t.Errorf("inheritFrom gave %v, want TTUppercase", child.TextTransform)
	}
	// And the explicit `inherit` keyword.
	child := Style{TextTransform: TTLowercase}
	child.apply(Declaration{Property: "text-transform", Value: "inherit"}, 16, &parent)
	if child.TextTransform != TTUppercase {
		t.Errorf("text-transform: inherit gave %v, want TTUppercase", child.TextTransform)
	}
}

// white-space: nowrap, the one branch of the switch next door that no test
// reached — noticed while covering the case added beside it.
func TestWhiteSpaceNowrapParses(t *testing.T) {
	var s Style
	s.apply(Declaration{Property: "white-space", Value: "nowrap"}, 16, nil)
	if s.WhiteSpace != WSNoWrap {
		t.Errorf("white-space: nowrap gave %v, want WSNoWrap", s.WhiteSpace)
	}
}
