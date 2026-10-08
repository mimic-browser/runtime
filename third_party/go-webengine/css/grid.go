// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package css

import (
	"strconv"
	"strings"
)

// TrackKind is the sizing function of a single grid track.
type TrackKind uint8

const (
	// TrackAuto sizes the track to its content (initial for implicit tracks).
	TrackAuto TrackKind = iota
	// TrackPx is a fixed pixel size.
	TrackPx
	// TrackPercent is a percentage of the container's content size on that axis.
	TrackPercent
	// TrackFr is a flexible fraction of the leftover space.
	TrackFr
	// TrackMinMax is a minmax(min,max) range; Min and Max are non-nil.
	TrackMinMax
)

// TrackSize is one column or row sizing function of a grid template.
type TrackSize struct {
	Kind     TrackKind
	Px       float64    // TrackPx
	Percent  float64    // TrackPercent (0..1)
	Fr       float64    // TrackFr
	Min, Max *TrackSize // TrackMinMax bounds
}

// GridFlow is the auto-placement direction of a grid container.
type GridFlow uint8

const (
	// GridFlowRow fills each row before moving to the next (initial).
	GridFlowRow GridFlow = iota
	// GridFlowColumn fills each column before moving to the next.
	GridFlowColumn
)

// GridLine is one endpoint of a grid item's placement. Auto means the endpoint
// is chosen by auto-placement; Span means the endpoint is "span N" tracks from
// the opposite edge; otherwise N is a 1-based grid line number.
type GridLine struct {
	Auto bool
	Span bool
	N    int
}

// splitTopLevel splits s on whitespace, keeping parenthesised groups (minmax(),
// repeat(), fit-content()) and [line-name] brackets as single tokens.
func splitTopLevel(s string) []string {
	var out []string
	depth := 0
	start := -1
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '(', '[':
			depth++
		case ')', ']':
			if depth > 0 {
				depth--
			}
		}
		isSpace := (c == ' ' || c == '\t' || c == '\n' || c == '\r') && depth == 0
		if isSpace {
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

// splitTopLevelComma splits s on commas that are not nested in parentheses.
func splitTopLevelComma(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	out = append(out, strings.TrimSpace(s[start:]))
	return out
}

// parseTrackList parses a grid-template-columns/rows value into a flat track
// list, expanding repeat() with an integer count. Line-name [..] tokens are
// dropped. Returns false when a token cannot be understood (auto-fill/auto-fit
// repeat, fit-content, subgrid, ...), leaving the property unset.
func parseTrackList(v string, emRef float64) ([]TrackSize, bool) {
	lv := strings.ToLower(strings.TrimSpace(v))
	if lv == "none" || lv == "" {
		return nil, false
	}
	var out []TrackSize
	for _, tok := range splitTopLevel(v) {
		lt := strings.ToLower(tok)
		if strings.HasPrefix(lt, "[") {
			continue // line names are not modelled
		}
		if strings.HasPrefix(lt, "repeat(") && strings.HasSuffix(lt, ")") {
			reps, ok := parseRepeat(tok, emRef)
			if !ok {
				return nil, false
			}
			out = append(out, reps...)
			continue
		}
		ts, ok := parseTrackSize(tok, emRef)
		if !ok {
			return nil, false
		}
		out = append(out, ts)
	}
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// parseRepeat expands repeat(<int>, <tracklist>). Auto-fill/auto-fit counts are
// unsupported (they need the resolved container size) and report false.
func parseRepeat(tok string, emRef float64) ([]TrackSize, bool) {
	inner := tok[len("repeat(") : len(tok)-1]
	comma := strings.IndexByte(inner, ',')
	if comma < 0 {
		return nil, false
	}
	countTok := strings.TrimSpace(inner[:comma])
	n, err := strconv.Atoi(countTok)
	if err != nil || n <= 0 || n > 1000 {
		return nil, false
	}
	body, ok := parseTrackList(strings.TrimSpace(inner[comma+1:]), emRef)
	if !ok {
		return nil, false
	}
	out := make([]TrackSize, 0, n*len(body))
	for i := 0; i < n; i++ {
		out = append(out, body...)
	}
	return out, true
}

// parseTrackSize parses one track sizing function.
func parseTrackSize(tok string, emRef float64) (TrackSize, bool) {
	lt := strings.ToLower(strings.TrimSpace(tok))
	switch lt {
	case "auto", "min-content", "max-content":
		return TrackSize{Kind: TrackAuto}, true
	}
	if strings.HasPrefix(lt, "minmax(") && strings.HasSuffix(lt, ")") {
		inner := tok[len("minmax(") : len(tok)-1]
		parts := splitTopLevelComma(inner)
		if len(parts) != 2 {
			return TrackSize{}, false
		}
		mn, ok1 := parseTrackSize(parts[0], emRef)
		mx, ok2 := parseTrackSize(parts[1], emRef)
		if !ok1 || !ok2 {
			return TrackSize{}, false
		}
		return TrackSize{Kind: TrackMinMax, Min: &mn, Max: &mx}, true
	}
	if strings.HasSuffix(lt, "fr") {
		f, err := strconv.ParseFloat(strings.TrimSpace(lt[:len(lt)-2]), 64)
		if err != nil || f < 0 {
			return TrackSize{}, false
		}
		return TrackSize{Kind: TrackFr, Fr: f}, true
	}
	if l, ok := parseLength(tok, emRef); ok && !l.Auto {
		if l.IsPercent {
			return TrackSize{Kind: TrackPercent, Percent: l.Percent}, true
		}
		return TrackSize{Kind: TrackPx, Px: l.Px}, true
	}
	return TrackSize{}, false
}

// parseGridLine parses a single grid line: auto, a line number, or "span N".
// Named lines are not modelled and resolve to auto.
func parseGridLine(v string) GridLine {
	lv := strings.ToLower(strings.TrimSpace(v))
	if lv == "" || lv == "auto" {
		return GridLine{Auto: true}
	}
	if strings.HasPrefix(lv, "span") {
		rest := strings.TrimSpace(strings.TrimPrefix(lv, "span"))
		if rest == "" {
			return GridLine{Span: true, N: 1}
		}
		if n, err := strconv.Atoi(rest); err == nil && n > 0 {
			return GridLine{Span: true, N: n}
		}
		return GridLine{Span: true, N: 1}
	}
	if n, err := strconv.Atoi(lv); err == nil && n != 0 {
		return GridLine{N: n}
	}
	return GridLine{Auto: true} // named line: unsupported
}

// parseGridPlacement parses a grid-column/grid-row shorthand ("<start> / <end>")
// into its two endpoints. A missing end defaults to auto.
func parseGridPlacement(v string) (start, end GridLine) {
	parts := strings.SplitN(v, "/", 2)
	start = parseGridLine(parts[0])
	if len(parts) == 2 {
		end = parseGridLine(parts[1])
	} else {
		end = GridLine{Auto: true}
	}
	return start, end
}

// parseGridTemplate handles the common rows / columns shorthand, including
// quoted area rows with an optional track size after each row. Invalid values
// leave all three longhands unchanged.
func parseGridTemplate(v string, emRef float64) (rows, columns []TrackSize, areas [][]string, ok bool) {
	parts := splitTopLevelSep(v, '/')
	if len(parts) != 2 {
		return nil, nil, nil, false
	}
	columns, ok = parseTrackList(strings.TrimSpace(parts[1]), emRef)
	if !ok {
		return nil, nil, nil, false
	}
	left := strings.TrimSpace(parts[0])
	if !strings.ContainsAny(left, `"'`) {
		rows, ok = parseTrackList(left, emRef)
		return rows, columns, nil, ok
	}
	for left != "" {
		if left[0] != '\'' && left[0] != '"' {
			return nil, nil, nil, false
		}
		quote := left[0]
		end := strings.IndexByte(left[1:], quote)
		if end < 0 {
			return nil, nil, nil, false
		}
		names := strings.Fields(left[1 : end+1])
		if len(names) == 0 || (len(areas) > 0 && len(names) != len(areas[0])) {
			return nil, nil, nil, false
		}
		for i, name := range names {
			if name == "." {
				names[i] = ""
			}
		}
		areas = append(areas, names)
		left = strings.TrimSpace(left[end+2:])
		row := TrackSize{Kind: TrackAuto}
		if left != "" && left[0] != '\'' && left[0] != '"' {
			parts := splitTopLevel(left)
			if len(parts) == 0 {
				return nil, nil, nil, false
			}
			var valid bool
			row, valid = parseTrackSize(parts[0], emRef)
			if !valid {
				return nil, nil, nil, false
			}
			left = strings.TrimSpace(left[len(parts[0]):])
		}
		rows = append(rows, row)
	}
	if len(areas) == 0 || len(areas[0]) != len(columns) {
		return nil, nil, nil, false
	}
	return rows, columns, areas, true
}

// parseGridTemplateAreas parses a grid-template-areas value (a sequence of
// quoted strings) into a rectangular row-major grid of area names. A "." token
// is an empty cell (stored as ""). Returns false if the rows are ragged or no
// rows are present.
func parseGridTemplateAreas(v string) ([][]string, bool) {
	var rows [][]string
	cols := -1
	for len(v) > 0 {
		q := strings.IndexAny(v, `"'`)
		if q < 0 {
			break
		}
		quote := v[q]
		end := strings.IndexByte(v[q+1:], quote)
		if end < 0 {
			return nil, false
		}
		content := v[q+1 : q+1+end]
		v = v[q+1+end+1:]
		names := strings.Fields(content)
		for i, n := range names {
			if n == "." {
				names[i] = ""
			}
		}
		if cols < 0 {
			cols = len(names)
		} else if len(names) != cols {
			return nil, false
		}
		rows = append(rows, names)
	}
	if len(rows) == 0 || cols == 0 {
		return nil, false
	}
	return rows, true
}

// applyGap sets row-gap and column-gap from a `gap` shorthand (1 or 2 values).
func applyGap(s *Style, v string, emRef float64) {
	fields := strings.Fields(v)
	switch len(fields) {
	case 1:
		if l, ok := parseLength(fields[0], emRef); ok && !l.Auto {
			s.RowGap, s.ColumnGap = l, l
		}
	case 2:
		r, ok1 := parseLength(fields[0], emRef)
		c, ok2 := parseLength(fields[1], emRef)
		if ok1 && ok2 && !r.Auto && !c.Auto {
			s.RowGap, s.ColumnGap = r, c
		}
	}
}

// applyPlaceItems parses the place-items shorthand (<align-items>
// [<justify-items>]); a single value applies to both axes.
func applyPlaceItems(s *Style, v string) {
	fields := strings.Fields(strings.ToLower(v))
	if len(fields) == 0 {
		return
	}
	if a, ok := parseAlignItems(fields[0]); ok {
		s.AlignItems = a
	}
	j := fields[0]
	if len(fields) > 1 {
		j = fields[1]
	}
	if a, ok := parseAlignItems(j); ok {
		s.JustifyItems = a
	}
}

// applyPlaceContent parses the place-content shorthand (<align-content>
// [<justify-content>]); a single value applies to both axes.
func applyPlaceContent(s *Style, lv string) {
	fields := strings.Fields(lv)
	if len(fields) == 0 {
		return
	}
	if a, ok := parseAlignContent(fields[0]); ok {
		s.AlignContent = a
	}
	j := fields[0]
	if len(fields) > 1 {
		j = fields[1]
	}
	if jc, ok := parseJustify(j); ok {
		s.JustifyContent = jc
	}
}

// applyPlaceSelf parses the place-self shorthand (<align-self>
// [<justify-self>]); a single value applies to both axes.
func applyPlaceSelf(s *Style, v string) {
	fields := strings.Fields(strings.ToLower(v))
	if len(fields) == 0 {
		return
	}
	if a, ok := parseAlignSelf(fields[0]); ok {
		s.AlignSelf = a
	}
	j := fields[0]
	if len(fields) > 1 {
		j = fields[1]
	}
	if a, ok := parseAlignSelf(j); ok {
		s.JustifySelf = a
	}
}

// applyGridArea parses grid-area: either a named area, or a 2-to-4 slash list
// of grid lines (row-start / column-start / row-end / column-end).
func applyGridArea(s *Style, v string) {
	if !strings.Contains(v, "/") {
		name := strings.TrimSpace(v)
		if name != "" && name != "auto" {
			s.GridArea = name
		}
		return
	}
	parts := strings.Split(v, "/")
	get := func(i int) GridLine {
		if i < len(parts) {
			return parseGridLine(parts[i])
		}
		return GridLine{Auto: true}
	}
	s.GridRowStart = get(0)
	s.GridColumnStart = get(1)
	s.GridRowEnd = get(2)
	s.GridColumnEnd = get(3)
}
