package browser

import (
	"context"
	"strings"
	"testing"
)

func TestBlitzDeviceScaleAdmissionInvalidatesAndRepairs(t *testing.T) {
	parallelBrowserTest(t)
	p := blitzStandardsPage(t)
	for _, scale := range []float64{1, 2, 1} {
		p.env.Display.DeviceScaleFactor = scale
		_, err := p.Evaluate(context.Background(), `document.body.getBoundingClientRect().width`)
		if err != nil {
			t.Fatal(err)
		}
		if scale == 1 {
			assertBlitzOwnerActive(t, p)
		} else if p.Top.Realm.blitz.document.Owner != nil || !strings.Contains(p.Top.Realm.blitz.fallback, "device-scale") {
			t.Fatal("device scale change did not invalidate native admission")
		}
	}
}

func TestBlitzStaticFallbackSkipsUnusedInputProjection(t *testing.T) {
	parallelBrowserTest(t)
	for _, kind := range []string{"device-scale", "reduced-motion"} {
		t.Run(kind, func(t *testing.T) {
			p := blitzStandardsPage(t)
			if kind == "device-scale" {
				p.env.Display.DeviceScaleFactor = 2
			} else {
				p.env.Preferences.ReducedMotion = true
			}
			// Static admission is decided before the JS CSSOM projection. This
			// projection cannot contribute to a native owner in these profiles.
			p.Top.Realm.blitzInputs = nil
			if _, err := p.Evaluate(context.Background(), `document.body.getBoundingClientRect().width`); err != nil {
				t.Fatal(err)
			}
			if p.Top.Realm.blitz.document.Owner != nil || !strings.Contains(p.Top.Realm.blitz.fallback, kind) {
				t.Fatalf("incorrect fallback: %q", p.Top.Realm.blitz.fallback)
			}
		})
	}
}

func TestBlitzCanonicalColorSchemeInvalidatesNativeStyles(t *testing.T) {
	parallelBrowserTest(t)
	p := blitzStandardsPage(t)
	_, err := p.Evaluate(context.Background(), `document.head.innerHTML='<style>div{width:10px;height:5px}@media(prefers-color-scheme:dark){div{width:30px}}</style>';document.body.innerHTML='<div></div>'`)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		scheme string
		width  string
	}{{"light", "10"}, {"dark", "30"}, {"light", "10"}} {
		p.env.Preferences.ColorScheme = step.scheme
		value, err := p.Evaluate(context.Background(), `String(document.querySelector('div').getBoundingClientRect().width)`)
		if err != nil || value != step.width {
			t.Fatalf("scheme %s: width=%v error=%v", step.scheme, value, err)
		}
		assertBlitzOwnerActive(t, p)
	}
}
