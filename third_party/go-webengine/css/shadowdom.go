// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import "github.com/go-webengine/engine/dom"

// shadowStylesheet parses a shadow tree's own <style> elements into rules,
// exactly like collectAuthorRules but scoped to sr's content instead of the
// whole document — the CSS Shadow-DOM scoping rule that a shadow tree's own
// stylesheet applies only within that tree (plus, via ":host"/":host()", to
// the host element itself; see filterHostSelectors) and is never seen by, or
// leaked into, the surrounding document's cascade.
func shadowStylesheet(sr *dom.ShadowRoot, m Media) []Rule {
	return collectAuthorRulesFrom(sr.Children, m)
}

// filterHostSelectors returns a copy of rules keeping, for each rule, only
// the selectors whose key (rightmost) compound is ":host"/":host(...)" — the
// one part of a shadow tree's own stylesheet that targets the host element
// itself rather than the shadow tree's content. A rule with no such selector
// is dropped entirely.
//
// This is how a shadow tree's ":host" declarations reach the host: the host
// is cascaded with the document's ordinary rules (it is a light-DOM element,
// not a member of its own shadow tree), so its own shadow's :host-keyed rules
// are folded in as an extra rule source for that one element's computeElement
// call — see cascade.go. Filtering out the shadow's ORDINARY (non-host) rules
// first is essential, not an optimisation: a shadow rule like `div{color:red}`
// must never apply to the host even when the host itself happens to be a
// <div> — only ":host"/":host(...)" is a sanctioned way for a shadow tree's
// stylesheet to reach outside itself.
func filterHostSelectors(rules []Rule) []Rule {
	var out []Rule
	for _, r := range rules {
		var sels []Selector
		for _, s := range r.Selectors {
			if len(s.parts) > 0 && s.parts[len(s.parts)-1].Host {
				sels = append(sels, s)
			}
		}
		if len(sels) > 0 {
			out = append(out, Rule{Selectors: sels, Declarations: r.Declarations, Container: r.Container})
		}
	}
	return out
}

// filterPartSelectors is filterHostSelectors' mirror image: it returns a copy
// of rules keeping only the selectors whose key compound is "::part(name)" —
// the one sanctioned way an OUTER stylesheet reaches INTO a shadow tree (see
// compound.Part's own doc comment), rather than a shadow's own stylesheet
// reaching out via ":host". rules here is the AMBIENT scope the host element
// itself was cascaded in (an outer document's rules, or an outer shadow
// tree's own scoped rules if the host is itself shadow-tree content) — see
// cascade.go's walk, which appends this filtered set onto the shadow tree's
// own scoped rules before descending into it, so a ::part() declaration's
// cascade precedence relative to the shadow's own internal rules is decided
// by ordinary specificity, exactly as two author rules from any two
// stylesheets would be.
func filterPartSelectors(rules []Rule) []Rule {
	var out []Rule
	for _, r := range rules {
		var sels []Selector
		for _, s := range r.Selectors {
			if len(s.parts) > 0 && s.parts[len(s.parts)-1].Part != "" {
				sels = append(sels, s)
			}
		}
		if len(sels) > 0 {
			out = append(out, Rule{Selectors: sels, Declarations: r.Declarations, Container: r.Container})
		}
	}
	return out
}
