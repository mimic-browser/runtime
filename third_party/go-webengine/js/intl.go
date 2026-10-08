// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package js

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/dop251/goja"
)

// The ECMAScript Intl (Internationalization) API.
//
// goja implements no Intl object at all (verified: `Intl is not defined`), yet
// every react-intl / FormatJS app — Mastodon included — references
// Intl.NumberFormat / DateTimeFormat / PluralRules / RelativeTimeFormat the
// moment it boots, so the reference throws before any component renders. This is
// an ECMAScript-engine gap, not a DOM gap, but it sits squarely on the SPA
// hydration path, so the binding provides a pragmatic, en-leaning Intl: real
// enough that formatting produces sensible text (grouped numbers, formatted
// dates, correct English plural category) and, above all, never throws. It is
// deliberately not a full CLDR implementation.

// installIntl wires the Intl global with the formatter constructors hydration
// code constructs at boot.
func (b *binder) installIntl(g *goja.Object) {
	intl := b.vm.NewObject()

	// setCtor installs a formatter constructor and attaches the static
	// supportedLocalesOf every FormatJS boot path calls before constructing.
	setCtor := func(name string, ctor func(goja.ConstructorCall) *goja.Object) {
		intl.Set(name, ctor)
		if c, ok := intl.Get(name).(*goja.Object); ok {
			c.Set("supportedLocalesOf", func(call goja.FunctionCall) goja.Value {
				return b.vm.NewArray(localeList(call.Argument(0))...)
			})
		}
	}
	setCtor("NumberFormat", b.intlNumberFormat())
	setCtor("DateTimeFormat", b.intlDateTimeFormat())
	setCtor("PluralRules", b.intlPluralRules())
	setCtor("RelativeTimeFormat", b.intlRelativeTimeFormat())
	setCtor("ListFormat", b.intlListFormat())
	setCtor("Collator", b.intlCollator())
	setCtor("Segmenter", b.intlSegmenter())
	setCtor("DisplayNames", b.intlDisplayNames())
	setCtor("Locale", b.intlLocale())
	intl.Set("getCanonicalLocales", func(call goja.FunctionCall) goja.Value {
		return b.vm.NewArray(localeList(call.Argument(0))...)
	})
	intl.Set("supportedValuesOf", func(goja.FunctionCall) goja.Value { return b.vm.NewArray() })

	g.Set("Intl", intl)
}

// firstLocale returns the first requested locale (a string), defaulting to en.
func firstLocale(v goja.Value) string {
	for _, l := range localeStrings(v) {
		if l != "" {
			return l
		}
	}
	return "en"
}

// localeStrings coerces a locale argument (string | string[] | undefined) to a
// slice of locale tags.
func localeStrings(v goja.Value) []string {
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil
	}
	if obj, ok := v.(*goja.Object); ok {
		if obj.ClassName() == "Array" {
			n := int(obj.Get("length").ToInteger())
			out := make([]string, 0, n)
			for i := 0; i < n; i++ {
				out = append(out, obj.Get(strconv.Itoa(i)).String())
			}
			return out
		}
	}
	return []string{v.String()}
}

func localeList(v goja.Value) []interface{} {
	ls := localeStrings(v)
	out := make([]interface{}, len(ls))
	for i, l := range ls {
		out[i] = l
	}
	return out
}

// resolvedOptions builds a minimal resolvedOptions() object carrying the locale.
func (b *binder) resolvedOptions(locale string, extra map[string]interface{}) func(goja.FunctionCall) goja.Value {
	return func(goja.FunctionCall) goja.Value {
		o := b.vm.NewObject()
		o.Set("locale", locale)
		o.Set("numberingSystem", "latn")
		o.Set("calendar", "gregory")
		o.Set("timeZone", "UTC")
		for k, v := range extra {
			o.Set(k, v)
		}
		return o
	}
}

// intlNumberFormat implements Intl.NumberFormat with grouped integer/decimal
// output and basic minimum/maximumFractionDigits handling.
func (b *binder) intlNumberFormat() func(goja.ConstructorCall) *goja.Object {
	return func(call goja.ConstructorCall) *goja.Object {
		locale := firstLocale(call.Argument(0))
		opts := call.Argument(1)
		minFrac, maxFrac := 0, 3
		useGrouping := true
		percent := false
		if o, ok := opts.(*goja.Object); ok {
			if v := o.Get("minimumFractionDigits"); v != nil && !goja.IsUndefined(v) {
				minFrac = int(v.ToInteger())
			}
			if v := o.Get("maximumFractionDigits"); v != nil && !goja.IsUndefined(v) {
				maxFrac = int(v.ToInteger())
			}
			if maxFrac < minFrac {
				maxFrac = minFrac
			}
			if v := o.Get("useGrouping"); v != nil && !goja.IsUndefined(v) {
				useGrouping = v.ToBoolean()
			}
			if s := o.Get("style"); s != nil && s.String() == "percent" {
				percent = true
			}
		}
		o := call.This
		if o == nil {
			o = b.vm.NewObject()
		}
		render := func(v float64) string {
			if percent {
				return formatNumber(v*100, minFrac, maxFrac, useGrouping) + "%"
			}
			return formatNumber(v, minFrac, maxFrac, useGrouping)
		}
		format := func(call goja.FunctionCall) goja.Value {
			return b.vm.ToValue(render(call.Argument(0).ToFloat()))
		}
		o.Set("format", format)
		o.Set("formatToParts", func(call goja.FunctionCall) goja.Value {
			part := b.vm.NewObject()
			part.Set("type", "literal")
			part.Set("value", render(call.Argument(0).ToFloat()))
			return b.vm.NewArray(part)
		})
		o.Set("resolvedOptions", b.resolvedOptions(locale, map[string]interface{}{"style": "decimal"}))
		return o
	}
}

// formatNumber renders f with grouping and the fraction-digit bounds.
func formatNumber(f float64, minFrac, maxFrac int, group bool) string {
	if math.IsNaN(f) {
		return "NaN"
	}
	if math.IsInf(f, 1) {
		return "∞"
	}
	if math.IsInf(f, -1) {
		return "-∞"
	}
	neg := math.Signbit(f)
	f = math.Abs(f)
	s := strconv.FormatFloat(f, 'f', maxFrac, 64)
	intPart, fracPart := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, fracPart = s[:i], s[i+1:]
	}
	// Trim trailing zeros down to minFrac.
	for len(fracPart) > minFrac && strings.HasSuffix(fracPart, "0") {
		fracPart = fracPart[:len(fracPart)-1]
	}
	if group {
		intPart = groupThousands(intPart)
	}
	out := intPart
	if len(fracPart) > 0 {
		out += "." + fracPart
	}
	if neg && out != "0" {
		out = "-" + out
	}
	return out
}

// groupThousands inserts commas every three digits from the right.
func groupThousands(s string) string {
	n := len(s)
	if n <= 3 {
		return s
	}
	var sb strings.Builder
	pre := n % 3
	if pre > 0 {
		sb.WriteString(s[:pre])
		if n > pre {
			sb.WriteByte(',')
		}
	}
	for i := pre; i < n; i += 3 {
		sb.WriteString(s[i : i+3])
		if i+3 < n {
			sb.WriteByte(',')
		}
	}
	return sb.String()
}

// intlDateTimeFormat implements Intl.DateTimeFormat with a compact date/time
// rendering driven by the common option keys.
func (b *binder) intlDateTimeFormat() func(goja.ConstructorCall) *goja.Object {
	return func(call goja.ConstructorCall) *goja.Object {
		locale := firstLocale(call.Argument(0))
		opts := call.Argument(1)
		layout := "Jan 2, 2006"
		if o, ok := opts.(*goja.Object); ok {
			layout = dateLayout(o)
		}
		o := call.This
		if o == nil {
			o = b.vm.NewObject()
		}
		format := func(call goja.FunctionCall) goja.Value {
			return b.vm.ToValue(b.toTime(call.Argument(0)).Format(layout))
		}
		o.Set("format", format)
		o.Set("formatToParts", func(call goja.FunctionCall) goja.Value {
			part := b.vm.NewObject()
			part.Set("type", "literal")
			part.Set("value", b.toTime(call.Argument(0)).Format(layout))
			return b.vm.NewArray(part)
		})
		o.Set("formatRange", func(call goja.FunctionCall) goja.Value {
			a := b.toTime(call.Argument(0)).Format(layout)
			c := b.toTime(call.Argument(1)).Format(layout)
			return b.vm.ToValue(a + " – " + c)
		})
		o.Set("resolvedOptions", b.resolvedOptions(locale, nil))
		return o
	}
}

// dateLayout derives a Go time layout from DateTimeFormat options (a pragmatic
// subset: the common date/time-style and component combinations).
func dateLayout(o *goja.Object) string {
	get := func(k string) string {
		v := o.Get(k)
		if v == nil || goja.IsUndefined(v) {
			return ""
		}
		return v.String()
	}
	hasDate := get("year") != "" || get("month") != "" || get("day") != "" || get("dateStyle") != ""
	hasTime := get("hour") != "" || get("minute") != "" || get("second") != "" || get("timeStyle") != ""
	date := ""
	if hasDate {
		month := "Jan"
		switch get("month") {
		case "2-digit", "numeric":
			month = "1"
		case "long":
			month = "January"
		}
		date = month + " 2, 2006"
		if get("weekday") == "long" {
			date = "Monday, " + date
		}
	}
	tm := ""
	if hasTime {
		tm = "3:04 PM"
		if get("second") != "" {
			tm = "3:04:05 PM"
		}
	}
	switch {
	case date != "" && tm != "":
		return date + ", " + tm
	case tm != "":
		return tm
	case date != "":
		return date
	default:
		return "Jan 2, 2006"
	}
}

// toTime coerces a Date object / epoch-ms number / date string to a time.Time.
func (b *binder) toTime(v goja.Value) time.Time {
	if v == nil || goja.IsUndefined(v) {
		return time.Now().UTC()
	}
	if obj, ok := v.(*goja.Object); ok {
		if fn, ok := goja.AssertFunction(obj.Get("getTime")); ok {
			if res, err := fn(obj); err == nil {
				return time.UnixMilli(res.ToInteger()).UTC()
			}
		}
	}
	// Numeric epoch ms.
	f := v.ToFloat()
	if !math.IsNaN(f) {
		return time.UnixMilli(int64(f)).UTC()
	}
	return time.Now().UTC()
}

// intlPluralRules implements Intl.PluralRules with the English plural categories
// (the overwhelmingly common case for these SPAs' default locale).
func (b *binder) intlPluralRules() func(goja.ConstructorCall) *goja.Object {
	return func(call goja.ConstructorCall) *goja.Object {
		locale := firstLocale(call.Argument(0))
		o := call.This
		if o == nil {
			o = b.vm.NewObject()
		}
		o.Set("select", func(call goja.FunctionCall) goja.Value {
			return b.vm.ToValue(englishPlural(call.Argument(0).ToFloat()))
		})
		o.Set("resolvedOptions", b.resolvedOptions(locale, map[string]interface{}{
			"pluralCategories": b.vm.NewArray("one", "other"),
			"type":             "cardinal",
		}))
		return o
	}
}

// englishPlural returns the English plural category for n.
func englishPlural(n float64) string {
	if n == 1 {
		return "one"
	}
	return "other"
}

// intlRelativeTimeFormat implements Intl.RelativeTimeFormat with plain English
// "in N units" / "N units ago" phrasing.
func (b *binder) intlRelativeTimeFormat() func(goja.ConstructorCall) *goja.Object {
	return func(call goja.ConstructorCall) *goja.Object {
		locale := firstLocale(call.Argument(0))
		o := call.This
		if o == nil {
			o = b.vm.NewObject()
		}
		format := func(call goja.FunctionCall) goja.Value {
			return b.vm.ToValue(relativeTime(call.Argument(0).ToFloat(), call.Argument(1).String()))
		}
		o.Set("format", format)
		o.Set("formatToParts", func(call goja.FunctionCall) goja.Value {
			part := b.vm.NewObject()
			part.Set("type", "literal")
			part.Set("value", relativeTime(call.Argument(0).ToFloat(), call.Argument(1).String()))
			return b.vm.NewArray(part)
		})
		o.Set("resolvedOptions", b.resolvedOptions(locale, nil))
		return o
	}
}

// relativeTime renders a relative value/unit pair in English.
func relativeTime(value float64, unit string) string {
	unit = strings.TrimSuffix(unit, "s")
	n := int64(math.Abs(value))
	plural := unit
	if n != 1 {
		plural = unit + "s"
	}
	if value == 0 {
		return "this " + unit
	}
	if value > 0 {
		return "in " + strconv.FormatInt(n, 10) + " " + plural
	}
	return strconv.FormatInt(n, 10) + " " + plural + " ago"
}

// intlListFormat implements Intl.ListFormat with English conjunction joining.
func (b *binder) intlListFormat() func(goja.ConstructorCall) *goja.Object {
	return func(call goja.ConstructorCall) *goja.Object {
		locale := firstLocale(call.Argument(0))
		conj := "and"
		if o, ok := call.Argument(1).(*goja.Object); ok {
			if t := o.Get("type"); t != nil && t.String() == "disjunction" {
				conj = "or"
			}
		}
		o := call.This
		if o == nil {
			o = b.vm.NewObject()
		}
		join := func(call goja.FunctionCall) goja.Value {
			return b.vm.ToValue(joinList(b.stringArray(call.Argument(0)), conj))
		}
		o.Set("format", join)
		o.Set("formatToParts", func(call goja.FunctionCall) goja.Value {
			part := b.vm.NewObject()
			part.Set("type", "element")
			part.Set("value", joinList(b.stringArray(call.Argument(0)), conj))
			return b.vm.NewArray(part)
		})
		o.Set("resolvedOptions", b.resolvedOptions(locale, nil))
		return o
	}
}

// stringArray coerces a JS array-like into a []string.
func (b *binder) stringArray(v goja.Value) []string {
	obj, ok := v.(*goja.Object)
	if !ok {
		return nil
	}
	lv := obj.Get("length")
	if lv == nil || goja.IsUndefined(lv) || goja.IsNull(lv) {
		return nil
	}
	n := int(lv.ToInteger())
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, obj.Get(strconv.Itoa(i)).String())
	}
	return out
}

// joinList joins items in English list style with the given conjunction.
func joinList(items []string, conj string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " " + conj + " " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + ", " + conj + " " + items[len(items)-1]
	}
}

// intlCollator implements Intl.Collator with a byte-wise compare.
func (b *binder) intlCollator() func(goja.ConstructorCall) *goja.Object {
	return func(call goja.ConstructorCall) *goja.Object {
		locale := firstLocale(call.Argument(0))
		o := call.This
		if o == nil {
			o = b.vm.NewObject()
		}
		o.Set("compare", func(call goja.FunctionCall) goja.Value {
			return b.vm.ToValue(strings.Compare(call.Argument(0).String(), call.Argument(1).String()))
		})
		o.Set("resolvedOptions", b.resolvedOptions(locale, nil))
		return o
	}
}

// intlSegmenter implements a minimal Intl.Segmenter (whole-string segment).
func (b *binder) intlSegmenter() func(goja.ConstructorCall) *goja.Object {
	return func(call goja.ConstructorCall) *goja.Object {
		locale := firstLocale(call.Argument(0))
		o := call.This
		if o == nil {
			o = b.vm.NewObject()
		}
		o.Set("segment", func(call goja.FunctionCall) goja.Value {
			s := call.Argument(0).String()
			seg := b.vm.NewObject()
			seg.Set("segment", s)
			seg.Set("index", 0)
			seg.Set("input", s)
			arr := b.vm.NewArray(seg)
			// Return an iterable array (react rarely needs true grapheme splitting).
			return arr
		})
		o.Set("resolvedOptions", b.resolvedOptions(locale, map[string]interface{}{"granularity": "grapheme"}))
		return o
	}
}

// intlDisplayNames implements Intl.DisplayNames (identity passthrough of the code).
func (b *binder) intlDisplayNames() func(goja.ConstructorCall) *goja.Object {
	return func(call goja.ConstructorCall) *goja.Object {
		locale := firstLocale(call.Argument(0))
		o := call.This
		if o == nil {
			o = b.vm.NewObject()
		}
		o.Set("of", func(call goja.FunctionCall) goja.Value { return b.vm.ToValue(call.Argument(0).String()) })
		o.Set("resolvedOptions", b.resolvedOptions(locale, nil))
		return o
	}
}

// intlLocale implements a minimal Intl.Locale carrying the base tag.
func (b *binder) intlLocale() func(goja.ConstructorCall) *goja.Object {
	return func(call goja.ConstructorCall) *goja.Object {
		tag := firstLocale(call.Argument(0))
		o := call.This
		if o == nil {
			o = b.vm.NewObject()
		}
		o.Set("baseName", tag)
		lang := tag
		if i := strings.IndexByte(tag, '-'); i >= 0 {
			lang = tag[:i]
		}
		o.Set("language", lang)
		o.Set("toString", func(goja.FunctionCall) goja.Value { return b.vm.ToValue(tag) })
		o.Set("maximize", func(goja.FunctionCall) goja.Value { return o })
		o.Set("minimize", func(goja.FunctionCall) goja.Value { return o })
		return o
	}
}
