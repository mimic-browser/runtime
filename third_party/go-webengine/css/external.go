// Copyright (c) the go-webengine/engine authors
//
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"strings"

	"github.com/go-webengine/engine/dom"
)

// LinkRef is a reference to an external stylesheet declared by a
// <link rel="stylesheet" href="..."> element, with its media attribute.
type LinkRef struct {
	Href  string // the raw href attribute value (unresolved)
	Media string // the raw media attribute value ("" when absent)
}

// StylesheetLinks walks the DOM in document order and returns every
// <link rel="stylesheet"> href, preserving order (which fixes cascade
// precedence). Links whose rel does not contain the "stylesheet" token, or
// that carry the "alternate" token, or that have an empty href, are skipped.
func StylesheetLinks(root *dom.Node) []LinkRef {
	var refs []LinkRef
	var walk func(n *dom.Node)
	walk = func(n *dom.Node) {
		if n.Type == dom.Element && n.Tag == "link" {
			if ref, ok := linkRef(n); ok {
				refs = append(refs, ref)
			}
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(root)
	return refs
}

// linkRef extracts a stylesheet LinkRef from a <link> element, reporting false
// when the element is not a (non-alternate) stylesheet link with a href.
func linkRef(n *dom.Node) (LinkRef, bool) {
	rel, _ := n.Attribute("rel")
	var isSheet, isAlt bool
	for _, tok := range strings.Fields(strings.ToLower(rel)) {
		switch tok {
		case "stylesheet":
			isSheet = true
		case "alternate":
			isAlt = true
		}
	}
	if !isSheet || isAlt {
		return LinkRef{}, false
	}
	href, _ := n.Attribute("href")
	if strings.TrimSpace(href) == "" {
		return LinkRef{}, false
	}
	media, _ := n.Attribute("media")
	return LinkRef{Href: strings.TrimSpace(href), Media: strings.TrimSpace(media)}, true
}

// MediaApplies reports whether a media query (from a <link media> attribute or
// an @import) applies at viewport width vw. It reuses the same simplified
// evaluation as @media blocks: an empty query always applies, "print" never
// does, and min-width/max-width pixel features are honoured against vw. A
// comma-separated media list matches if ANY component matches.
func MediaApplies(media string, vw float64) bool {
	return MediaAppliesTo(media, Media{Width: vw})
}

// MediaAppliesTo is MediaApplies against a Media: a <link media="print">
// applies under Media{Type: Print} and nowhere else, "screen" the reverse.
func MediaAppliesTo(media string, m Media) bool {
	return mediaMatchesOn(media, m)
}

// ImportURLs extracts the targets of leading @import at-rules from a stylesheet,
// in order. Per the CSS spec @import rules must precede all other rules (except
// @charset), so only the leading run is honoured; the first non-@import,
// non-@charset, non-comment token stops the scan. Both @import "url" and
// @import url(...) forms are supported, with an optional trailing media query
// (returned via the paired media slice, "" when absent).
func ImportURLs(sheet string) (urls []string, medias []string) {
	s := sheet
	for {
		s = skipLeadingTrivia(s)
		if !strings.HasPrefix(strings.ToLower(s), "@import") {
			return urls, medias
		}
		rest := s[len("@import"):]
		semi := strings.IndexByte(rest, ';')
		if semi < 0 {
			return urls, medias
		}
		clause := strings.TrimSpace(rest[:semi])
		if u, m, ok := parseImportClause(clause); ok {
			urls = append(urls, u)
			medias = append(medias, m)
		}
		s = rest[semi+1:]
	}
}

// skipLeadingTrivia advances past any run of leading whitespace, /* */ comments
// and a leading @charset rule, so the caller sees the first significant at-rule.
// An unterminated comment or @charset consumes the rest (returns "").
func skipLeadingTrivia(s string) string {
	for {
		s = strings.TrimLeft(s, " \t\r\n\f")
		switch {
		case strings.HasPrefix(s, "/*"):
			end := strings.Index(s, "*/")
			if end < 0 {
				return ""
			}
			s = s[end+2:]
		case strings.HasPrefix(strings.ToLower(s), "@charset"):
			semi := strings.IndexByte(s, ';')
			if semi < 0 {
				return ""
			}
			s = s[semi+1:]
		default:
			return s
		}
	}
}

// parseImportClause parses the body of an @import (everything between "@import"
// and ";"): a url() or quoted string, then an optional media query.
func parseImportClause(clause string) (url, media string, ok bool) {
	clause = strings.TrimSpace(clause)
	var rest string
	switch {
	case strings.HasPrefix(strings.ToLower(clause), "url("):
		close := strings.IndexByte(clause, ')')
		if close < 0 {
			return "", "", false
		}
		url = strings.Trim(clause[4:close], "'\" \t")
		rest = clause[close+1:]
	case strings.HasPrefix(clause, "\"") || strings.HasPrefix(clause, "'"):
		q := clause[0]
		end := strings.IndexByte(clause[1:], q)
		if end < 0 {
			return "", "", false
		}
		url = clause[1 : 1+end]
		rest = clause[1+end+1:]
	default:
		return "", "", false
	}
	if url == "" {
		return "", "", false
	}
	return url, strings.TrimSpace(rest), true
}
