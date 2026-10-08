// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"regexp"
	"strings"
)

// supportsConditionHolds evaluates only feature tests backed by this engine.
// Unknown tests remain false. In particular, grid and URL masks are supported
// here, so sites that gate their visible navigation behind those tests retain
// it in the approximate render.
//
// Light-dark() remains supported for its native color path. Unrecognised
// feature tests, including selector() queries, remain false. This is a bounded
// capability check, not a general CSS feature-query implementation.
func supportsConditionHolds(cond string) bool {
	cond = strings.TrimSpace(strings.ToLower(cond))
	if rest, ok := cutLeadingWord(cond, "not"); ok {
		return !supportsConditionHolds(rest)
	}
	if inner, ok := unwrapSupportsCondition(cond); ok {
		cond = inner
	}
	if parts := splitSupportsCondition(cond, " or "); len(parts) > 1 {
		for _, part := range parts {
			if supportsConditionHolds(part) {
				return true
			}
		}
		return false
	}
	if parts := splitSupportsCondition(cond, " and "); len(parts) > 1 {
		for _, part := range parts {
			if !supportsConditionHolds(part) {
				return false
			}
		}
		return true
	}
	if supportsLightDarkRe.MatchString("(" + cond + ")") {
		return true
	}
	parts := strings.SplitN(cond, ":", 2)
	if len(parts) != 2 {
		return false
	}
	property, value := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	switch property {
	case "display":
		return value == "grid"
	case "mask-image", "-webkit-mask-image":
		return value == "none" || strings.HasPrefix(value, "url(")
	}
	return false
}

func unwrapSupportsCondition(cond string) (string, bool) {
	if len(cond) < 2 || cond[0] != '(' || cond[len(cond)-1] != ')' {
		return "", false
	}
	depth := 0
	for i := range cond {
		switch cond[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && i != len(cond)-1 {
				return "", false
			}
		}
		if depth < 0 {
			return "", false
		}
	}
	if depth != 0 {
		return "", false
	}
	return strings.TrimSpace(cond[1 : len(cond)-1]), true
}

func splitSupportsCondition(cond, separator string) []string {
	depth := 0
	start := 0
	var parts []string
	for i := 0; i < len(cond); i++ {
		switch cond[i] {
		case '(':
			depth++
		case ')':
			depth--
		}
		if depth == 0 && strings.HasPrefix(cond[i:], separator) {
			parts = append(parts, strings.TrimSpace(cond[start:i]))
			i += len(separator) - 1
			start = i + 1
		}
	}
	if len(parts) == 0 {
		return nil
	}
	return append(parts, strings.TrimSpace(cond[start:]))
}

// supportsLightDarkRe matches "(color: light-dark(...))" — the feature under
// test is light-dark() itself, so the specific probe colours used (real CSS
// varies them: "red,red", "tan,tan", ...) and internal whitespace don't
// matter, only that a light-dark() call appears as color's value.
var supportsLightDarkRe = regexp.MustCompile(`^\(\s*color\s*:\s*light-dark\(`)
