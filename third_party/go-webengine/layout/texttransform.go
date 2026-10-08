// Copyright (c) the go-webengine authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

package layout

import (
	"strings"
	"unicode"

	"github.com/go-webengine/engine/css"
)

// applyTextTransform returns text as `text-transform` says to RENDER it,
// leaving the document's own characters alone.
//
// It runs where a text node becomes inline items, which is the one place
// that makes measurement and painting agree: a label written "Partners" and
// styled uppercase has to be MEASURED as "PARTNERS", which is wider, or the
// line it sits on is wrong before anything is drawn.
//
// Case mapping is Unicode's, so it is right for the accented capitals French
// and the other Latin orthographies need ("é" -> "É"); Turkish dotless i and
// the other locale-dependent mappings are not distinguished, matching what
// the rest of this engine does with language.
func applyTextTransform(text string, st *css.Style) string {
	if st == nil || text == "" {
		return text
	}
	switch st.TextTransform {
	case css.TTUppercase:
		return strings.ToUpper(text)
	case css.TTLowercase:
		return strings.ToLower(text)
	case css.TTCapitalize:
		return capitalizeWords(text)
	}
	return text
}

// capitalizeWords upper-cases the first letter of each word, leaving the rest
// of the word as the document wrote it — CSS capitalizes, it does not
// lower-case what follows, so "iPhone" stays "IPhone" rather than becoming
// "Iphone". A word starts after anything that is not a letter, a digit or an
// apostrophe, so "l'été" capitalises once, not twice, and "3rd" is left
// alone (its first letter unit is a digit, which has no upper case).
func capitalizeWords(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	atWordStart := true
	for _, r := range text {
		switch {
		case unicode.IsLetter(r):
			if atWordStart {
				b.WriteRune(unicode.ToUpper(r))
			} else {
				b.WriteRune(r)
			}
			atWordStart = false
		case unicode.IsDigit(r) || r == '\'' || r == '’':
			b.WriteRune(r)
			atWordStart = false
		default:
			b.WriteRune(r)
			atWordStart = true
		}
	}
	return b.String()
}
