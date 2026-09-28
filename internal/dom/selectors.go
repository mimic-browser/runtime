package dom

import "strings"

// FindClassIDs projects membership only. Class names are ASCII-whitespace
// tokens, not selectors, and quirks matching folds ASCII letters only.
func (d *Document) FindClassIDs(root int64, names string, fold bool) []int64 {
	d.mu.RLock()
	defer d.mu.RUnlock()
	normalize := func(s string) string {
		if !fold {
			return s
		}
		return strings.Map(func(r rune) rune {
			if r >= 'A' && r <= 'Z' {
				return r + ('a' - 'A')
			}
			return r
		}, s)
	}
	tokens := strings.FieldsFunc(normalize(names), func(r rune) bool { return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f' })
	result := []int64{}
	if len(tokens) == 0 {
		return result
	}
	var visit func(int64)
	visit = func(id int64) {
		n := d.nodes[id]
		if n == nil {
			return
		}
		if n.Type == "element" {
			classes, matches := normalize(n.Attributes["class"]), true
			for _, token := range tokens {
				if !hasClass(classes, token) {
					matches = false
					break
				}
			}
			if matches {
				result = append(result, id)
			}
		}
		for _, child := range n.Children {
			visit(child)
		}
	}
	if n := d.nodes[root]; n != nil {
		for _, child := range n.Children {
			visit(child)
		}
	}
	return result
}

// This restricted legacy leaf matcher is used by internal native lookups and
// the validated simple-selector fast path. Web-platform selector parsing and
// all structural matching live in the shared mature JavaScript domain layer.
func (d *Document) Matches(id int64, selector string) bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.matchesSelector(d.nodes[id], selector)
}

func (d *Document) matchesSelector(n *Node, selector string) bool {
	for {
		leaf, rest, more := strings.Cut(selector, ",")
		if d.matchesLeafSelector(n, strings.TrimSpace(leaf)) {
			return true
		}
		if !more {
			return false
		}
		selector = rest
	}
}

func (d *Document) matchesLeafSelector(n *Node, selector string) bool {
	if n == nil || n.Type != "element" || selector == "" {
		return false
	}
	if strings.ContainsAny(selector, " >+~:") {
		return false
	}
	if strings.HasPrefix(selector, "#") {
		return n.Attributes["id"] == selector[1:]
	}
	if strings.HasPrefix(selector, ".") {
		return hasClass(n.Attributes["class"], selector[1:])
	}
	if strings.HasPrefix(selector, "[") && strings.HasSuffix(selector, "]") {
		inside := strings.TrimSpace(selector[1 : len(selector)-1])
		name, value, hasValue := strings.Cut(inside, "=")
		actual, exists := n.Attributes[strings.ToLower(strings.TrimSpace(name))]
		if !hasValue {
			return exists
		}
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		return exists && actual == value
	}
	if dot := strings.IndexByte(selector, '.'); dot >= 0 {
		return strings.EqualFold(n.TagName, selector[:dot]) && hasClass(n.Attributes["class"], selector[dot+1:])
	}
	return selector == "*" || strings.EqualFold(n.TagName, selector)
}
