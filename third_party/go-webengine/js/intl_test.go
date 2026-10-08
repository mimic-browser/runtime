// Copyright (c) the go-webengine/engine authors.
// SPDX-License-Identifier: BSD-3-Clause

package js

import "testing"

func TestIntlNumberFormat(t *testing.T) {
	logs := evalLog(t, `
		console.log('plain=' + new Intl.NumberFormat('en').format(1234567));
		console.log('frac=' + new Intl.NumberFormat('en', { minimumFractionDigits: 2 }).format(3.5));
		console.log('nogroup=' + new Intl.NumberFormat('en', { useGrouping: false }).format(1234));
		console.log('neg=' + new Intl.NumberFormat('en').format(-1000));
		console.log('supported=' + Intl.NumberFormat.supportedLocalesOf(['en']).length);
	`)
	wantLog(t, logs, "plain=1,234,567")
	wantLog(t, logs, "frac=3.50")
	wantLog(t, logs, "nogroup=1234")
	wantLog(t, logs, "neg=-1,000")
	wantLog(t, logs, "supported=1")
}

func TestIntlPluralRules(t *testing.T) {
	logs := evalLog(t, `
		var p = new Intl.PluralRules('en');
		console.log('one=' + p.select(1));
		console.log('other=' + p.select(5));
		console.log('zero=' + p.select(0));
	`)
	wantLog(t, logs, "one=one")
	wantLog(t, logs, "other=other")
	wantLog(t, logs, "zero=other")
}

func TestIntlListFormat(t *testing.T) {
	logs := evalLog(t, `
		var l = new Intl.ListFormat('en', { type: 'conjunction' });
		console.log('two=' + l.format(['a', 'b']));
		console.log('three=' + l.format(['a', 'b', 'c']));
		var d = new Intl.ListFormat('en', { type: 'disjunction' });
		console.log('or=' + d.format(['x', 'y']));
	`)
	wantLog(t, logs, "two=a and b")
	wantLog(t, logs, "three=a, b, and c")
	wantLog(t, logs, "or=x or y")
}

func TestIntlRelativeTimeFormat(t *testing.T) {
	logs := evalLog(t, `
		var r = new Intl.RelativeTimeFormat('en');
		console.log('future=' + r.format(3, 'day'));
		console.log('past=' + r.format(-2, 'hour'));
		console.log('one=' + r.format(1, 'minute'));
	`)
	wantLog(t, logs, "future=in 3 days")
	wantLog(t, logs, "past=2 hours ago")
	wantLog(t, logs, "one=in 1 minute")
}

func TestIntlDateTimeFormat(t *testing.T) {
	// Fixed epoch: 2021-09-09T00:00:00Z.
	logs := evalLog(t, `
		var ts = Date.UTC(2021, 8, 9, 12, 30, 0);
		var f = new Intl.DateTimeFormat('en', { year: 'numeric', month: 'short', day: 'numeric' });
		console.log('date=' + f.format(new Date(ts)));
		var opts = new Intl.DateTimeFormat('en').resolvedOptions();
		console.log('hasLocale=' + (opts.locale === 'en'));
	`)
	wantLog(t, logs, "date=Sep 9, 2021")
	wantLog(t, logs, "hasLocale=true")
}

func TestIntlCollatorAndMisc(t *testing.T) {
	logs := evalLog(t, `
		var c = new Intl.Collator('en');
		console.log('cmp=' + (c.compare('a', 'b') < 0));
		console.log('canon=' + Intl.getCanonicalLocales('en').length);
		console.log('display=' + new Intl.DisplayNames(['en'], { type: 'language' }).of('fr'));
		console.log('locale=' + new Intl.Locale('en-US').language);
	`)
	wantLog(t, logs, "cmp=true")
	wantLog(t, logs, "canon=1")
	wantLog(t, logs, "display=fr")
	wantLog(t, logs, "locale=en")
}

func TestIntlEdgeCases(t *testing.T) {
	logs := evalLog(t, `
		console.log('nan=' + new Intl.NumberFormat('en').format(NaN));
		console.log('inf=' + new Intl.NumberFormat('en').format(Infinity));
		console.log('ninf=' + new Intl.NumberFormat('en').format(-Infinity));
		console.log('pct=' + new Intl.NumberFormat('en', { style: 'percent' }).format(0.5));
		console.log('nfParts=' + new Intl.NumberFormat('en').formatToParts(12).length);
		console.log('small=' + new Intl.NumberFormat('en').format(42));
		console.log('rtZero=' + new Intl.RelativeTimeFormat('en').format(0, 'day'));
		console.log('rtParts=' + new Intl.RelativeTimeFormat('en').formatToParts(1, 'day').length);
		console.log('lfEmpty=' + (new Intl.ListFormat('en').format([]) === ''));
		console.log('lfOne=' + new Intl.ListFormat('en').format(['solo']));
		console.log('lfParts=' + new Intl.ListFormat('en').formatToParts(['a','b']).length);
		console.log('seg=' + new Intl.Segmenter('en').segment('hello').length);
		console.log('canonArr=' + Intl.getCanonicalLocales(['en','fr']).length);
		console.log('supValues=' + Intl.supportedValuesOf('currency').length);
		console.log('locMax=' + (new Intl.Locale('en').maximize().toString()));
		console.log('locMin=' + (new Intl.Locale('en').minimize().baseName));
		console.log('dtParts=' + new Intl.DateTimeFormat('en').formatToParts(new Date(0)).length);
		console.log('dtNow=' + (new Intl.DateTimeFormat('en').format() !== ''));
		var f = new Intl.DateTimeFormat('en', { hour: 'numeric', minute: 'numeric' });
		console.log('timeOnly=' + (f.format(new Date(Date.UTC(2021,0,1,9,5,0))) !== ''));
		var g = new Intl.DateTimeFormat('en', { weekday: 'long', year: 'numeric', month: 'long', day: 'numeric', second: 'numeric', hour: 'numeric', minute: 'numeric' });
		console.log('full=' + (g.format(new Date(Date.UTC(2021,0,1,9,5,7))) !== ''));
		console.log('range=' + (new Intl.DateTimeFormat('en').formatRange(new Date(0), new Date(86400000)).indexOf('–') >= 0));
	`)
	for _, k := range []string{
		"nan=NaN", "inf=∞", "ninf=-∞", "pct=50%", "nfParts=1", "small=42",
		"rtZero=this day", "rtParts=1", "lfEmpty=true", "lfOne=solo", "lfParts=1",
		"seg=1", "canonArr=2", "supValues=0", "locMax=en", "locMin=en",
		"dtParts=1", "dtNow=true", "timeOnly=true", "full=true", "range=true",
	} {
		wantLog(t, logs, k)
	}
}

func TestIntlToTimeCoercions(t *testing.T) {
	// A bare number is treated as epoch ms; a non-date value falls back to now.
	logs := evalLog(t, `
		var f = new Intl.DateTimeFormat('en', { year: 'numeric', month: 'short', day: 'numeric' });
		console.log('epochNum=' + f.format(Date.UTC(2020, 0, 15)));
		console.log('nowFallback=' + (f.format('not-a-date') !== ''));
	`)
	wantLog(t, logs, "epochNum=Jan 15, 2020")
	wantLog(t, logs, "nowFallback=true")
}

func TestIntlLocaleCoercions(t *testing.T) {
	logs := evalLog(t, `
		// No locale -> default en; array with empty first -> skip to en.
		console.log('none=' + new Intl.NumberFormat().format(1000));
		console.log('emptyArr=' + new Intl.PluralRules(['']).select(1));
		// ListFormat.format on a non-array is coerced to empty.
		console.log('nonArr=' + (new Intl.ListFormat('en').format({}) === ''));
		// Numeric and long month layouts.
		var num = new Intl.DateTimeFormat('en', { month: 'numeric', day: 'numeric', year: 'numeric' });
		console.log('numMonth=' + (num.format(new Date(Date.UTC(2021,2,4))) !== ''));
		var lng = new Intl.DateTimeFormat('en', { month: 'long', day: 'numeric', year: 'numeric' });
		console.log('longMonth=' + lng.format(new Date(Date.UTC(2021,2,4))));
	`)
	wantLog(t, logs, "none=1,000")
	wantLog(t, logs, "emptyArr=one")
	wantLog(t, logs, "nonArr=true")
	wantLog(t, logs, "numMonth=true")
	wantLog(t, logs, "longMonth=March 4, 2021")
}
