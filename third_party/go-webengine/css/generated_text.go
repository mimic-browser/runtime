// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/go-webengine/engine/dom"
)

// AddGeneratedText inserts only literal text from ::before and ::after rules.
// It runs on a renderer-owned document after script execution, so generated
// nodes never become part of the page's script-visible DOM. The supplied style
// map remains the authority for layout and paint of those synthetic nodes.
func AddGeneratedText(root *dom.Node, sm StyleMap, externalSheets []string, media Media) {
	var before, after []Rule
	add := func(r Rule) {
		if r.Container != nil { // container-dependent pseudo styling is not modelled
			return
		}
		var hasBefore, hasAfter bool
		for _, sel := range r.Selectors {
			if len(sel.parts) == 0 {
				continue
			}
			switch sel.parts[len(sel.parts)-1].TextPseudo {
			case TextPseudoBefore:
				hasBefore = true
			case TextPseudoAfter:
				hasAfter = true
			}
		}
		if hasBefore {
			before = append(before, r)
		}
		if hasAfter {
			after = append(after, r)
		}
	}
	for _, sheet := range externalSheets {
		for _, r := range ParseStylesheetMedia(sheet, media) {
			add(r)
		}
	}
	for _, r := range collectAuthorRules(root, media) {
		add(r)
	}
	if len(before) == 0 && len(after) == 0 {
		return
	}
	type insertion struct {
		origin *dom.Node
		before *dom.Node
		after  *dom.Node
	}
	var pending []insertion
	var walk func(*dom.Node)
	walk = func(n *dom.Node) {
		if n.Type != dom.Element {
			for _, child := range n.Children {
				walk(child)
			}
			return
		}
		if parent := sm[n]; parent != nil && parent.Display != DisplayNone {
			entry := insertion{origin: n}
			var style *Style
			entry.before, style = generatedTextNode(n, *parent, before, TextPseudoBefore)
			if entry.before != nil {
				sm[entry.before] = style
			}
			entry.after, style = generatedTextNode(n, *parent, after, TextPseudoAfter)
			if entry.after != nil {
				sm[entry.after] = style
			}
			if entry.before != nil || entry.after != nil {
				pending = append(pending, entry)
			}
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
	for _, entry := range pending {
		if entry.before != nil {
			entry.origin.Children = append([]*dom.Node{entry.before}, entry.origin.Children...)
		}
		if entry.after != nil {
			entry.origin.Children = append(entry.origin.Children, entry.after)
		}
	}
}

func generatedTextNode(origin *dom.Node, parent Style, rules []Rule, kind TextPseudoKind) (*dom.Node, *Style) {
	var cands []candidate
	order := 0
	for _, rule := range rules {
		spec := -1
		for _, sel := range rule.Selectors {
			if sel.MatchesTextPseudo(origin, kind) && sel.Specificity() > spec {
				spec = sel.Specificity()
			}
		}
		if spec < 0 {
			continue
		}
		for _, decl := range rule.Declarations {
			cands = append(cands, candidate{decl: decl, precedence: precAuthor, specificity: spec, order: order})
			order++
		}
	}
	if len(cands) == 0 {
		return nil, nil
	}
	sort.SliceStable(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if a.decl.Important != b.decl.Important {
			return !a.decl.Important
		}
		if a.specificity != b.specificity {
			return a.specificity < b.specificity
		}
		return a.order < b.order
	})
	style := inheritFrom(parent)
	ownProps := false
	for _, c := range cands {
		if isCustomProperty(c.decl.Property) {
			if !ownProps {
				style.CustomProps = cloneProps(parent.CustomProps)
				ownProps = true
			}
			style.CustomProps[c.decl.Property] = c.decl.Value
		}
	}
	apply := func(d Declaration, em float64) (string, bool) {
		value, ok := resolveDeclValue(d.Value, style.CustomProps)
		if ok {
			d.Value = value
			if d.Property != "content" {
				style.apply(d, em, &parent)
			}
		}
		return value, ok
	}
	for _, c := range cands {
		if c.decl.Property == "font-size" {
			apply(c.decl, parent.FontSize)
		}
	}
	var content string
	var hasContent bool
	for _, c := range cands {
		if c.decl.Property == "font-size" || isCustomProperty(c.decl.Property) {
			continue
		}
		value, ok := apply(c.decl, style.FontSize)
		if ok && c.decl.Property == "content" {
			content, hasContent = literalGeneratedContent(value)
		}
	}
	if !hasContent || content == "" || style.Display == DisplayNone {
		return nil, nil
	}
	node := &dom.Node{Type: dom.Element, Parent: origin}
	node.Children = []*dom.Node{{Type: dom.Text, Text: content, Parent: node}}
	return node, &style
}

// literalGeneratedContent reads quoted CSS strings (including CSS escapes).
// The optional slash introduces accessibility-only alternate text; it does
// not change the visible string. Counters, URLs and attr() stay unsupported.
func literalGeneratedContent(value string) (string, bool) {
	s := strings.TrimSpace(value)
	if s == "" || s == "none" || s == "normal" {
		return "", false
	}
	var out strings.Builder
	for len(s) > 0 {
		s = strings.TrimLeft(s, " \t\r\n")
		if len(s) == 0 || s[0] == '/' {
			break
		}
		quote := s[0]
		if quote != '\'' && quote != '"' {
			return "", false
		}
		s = s[1:]
		closed := false
		for len(s) > 0 {
			if s[0] == quote {
				s = s[1:]
				closed = true
				break
			}
			if s[0] == '\\' && len(s) > 1 {
				s = s[1:]
				n := 0
				for n < len(s) && n < 6 && isCSSHex(s[n]) {
					n++
				}
				if n > 0 {
					code, _ := strconv.ParseInt(s[:n], 16, 32)
					out.WriteRune(rune(code))
					s = s[n:]
					if len(s) > 0 && (s[0] == ' ' || s[0] == '\n') {
						s = s[1:]
					}
					continue
				}
				out.WriteByte(s[0])
				s = s[1:]
				continue
			}
			r, size := utf8.DecodeRuneInString(s)
			out.WriteRune(r)
			s = s[size:]
		}
		if !closed {
			return "", false
		}
	}
	return out.String(), true
}

func isCSSHex(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'f' || b >= 'A' && b <= 'F'
}
