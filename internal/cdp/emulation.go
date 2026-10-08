package cdp

import (
	"fmt"
	"strings"

	"github.com/moreveal/mimic/internal/browser"
	"github.com/moreveal/mimic/internal/state"
)

func (s *session) handleEmulation(method string, p map[string]any) (any, bool, error) {
	empty := map[string]any{}
	switch method {
	case "Emulation.setGeolocationOverride", "Page.setGeolocationOverride":
		location := &browser.GeolocationOverride{}
		for name, field := range map[string]**float64{
			"latitude": &location.Latitude, "longitude": &location.Longitude,
			"accuracy": &location.Accuracy, "altitude": &location.Altitude,
			"altitudeAccuracy": &location.AltitudeAccuracy, "heading": &location.Heading,
			"speed": &location.Speed,
		} {
			if raw, exists := p[name]; exists {
				value, ok := raw.(float64)
				if !ok {
					return nil, true, fmt.Errorf("Invalid geolocation")
				}
				*field = &value
			}
		}
		return empty, true, s.page.SetGeolocationOverride(location)
	case "Emulation.clearGeolocationOverride", "Page.clearGeolocationOverride":
		return empty, true, s.page.SetGeolocationOverride(nil)
	case "Emulation.setDeviceMetricsOverride":
		if mobile, _ := p["mobile"].(bool); mobile {
			return nil, true, fmt.Errorf("Mobile viewport emulation is not supported")
		}
		for _, key := range []string{"viewport", "displayFeature", "devicePosture"} {
			if _, set := p[key]; set {
				return nil, true, fmt.Errorf("%s is not supported", key)
			}
		}
		orientation, _ := p["screenOrientation"].(map[string]any)
		if orientation != nil {
			switch stringValue(orientation["type"]) {
			case "portraitPrimary", "portraitSecondary", "landscapePrimary", "landscapeSecondary":
			default:
				return nil, true, fmt.Errorf("Invalid screen orientation type")
			}
			if angle := intValue(orientation["angle"], 0); angle < 0 || angle > 360 {
				return nil, true, fmt.Errorf("Invalid screen orientation angle")
			}
		}
		scale, _ := p["deviceScaleFactor"].(float64)
		if err := s.page.SetDeviceMetrics(intValue(p["width"], 0), intValue(p["height"], 0), scale, intValue(p["screenWidth"], 0), intValue(p["screenHeight"], 0)); err != nil {
			return nil, true, err
		}
		if orientation != nil {
			return empty, true, s.page.SetScreenOrientation(stringValue(orientation["type"]), intValue(orientation["angle"], 0))
		}
		return empty, true, nil
	case "Emulation.clearDeviceMetricsOverride":
		return empty, true, s.page.ClearDeviceMetrics()
	case "Emulation.setFocusEmulationEnabled":
		enabled, _ := p["enabled"].(bool)
		s.page.SetFocusEmulationEnabled(enabled)
		return empty, true, nil
	case "Emulation.setLocaleOverride":
		return empty, true, s.page.SetLocaleOverride(stringValue(p["locale"]))
	case "Emulation.setTimezoneOverride":
		if err := s.page.SetTimezoneOverride(stringValue(p["timezoneId"])); err != nil {
			return nil, true, err
		}
		_, err := s.page.EvaluateCommand(s.ctx, "", `globalThis[Symbol.for('__mimicRefreshIntlEnvironment')]()`)
		return empty, true, err
	case "Network.setUserAgentOverride", "Emulation.setUserAgentOverride":
		o := &state.UserAgentOverride{UserAgent: stringValue(p["userAgent"]), Platform: stringValue(p["platform"])}
		for _, lang := range strings.Split(stringValue(p["acceptLanguage"]), ",") {
			lang, _, _ = strings.Cut(strings.TrimSpace(lang), ";")
			if lang != "" {
				o.Languages = append(o.Languages, lang)
			}
		}
		if raw, ok := p["userAgentMetadata"].(map[string]any); ok {
			m := &state.UserAgentMetadata{FullVersion: stringValue(raw["fullVersion"]), Platform: stringValue(raw["platform"]), PlatformVersion: stringValue(raw["platformVersion"]), Architecture: stringValue(raw["architecture"]), Model: stringValue(raw["model"]), Bitness: stringValue(raw["bitness"])}
			m.Mobile, _ = raw["mobile"].(bool)
			m.WoW64, _ = raw["wow64"].(bool)
			if factors, ok := raw["formFactors"].([]any); ok {
				m.FormFactors = []string{}
				for _, factor := range factors {
					m.FormFactors = append(m.FormFactors, stringValue(factor))
				}
			}
			brands := func(value any) []state.UserAgentBrand {
				out := []state.UserAgentBrand{}
				items, _ := value.([]any)
				for _, item := range items {
					b, _ := item.(map[string]any)
					out = append(out, state.UserAgentBrand{Brand: stringValue(b["brand"]), Version: stringValue(b["version"]), FullVersion: stringValue(b["version"])})
				}
				return out
			}
			m.Brands = brands(raw["brands"])
			m.FullVersionList = brands(raw["fullVersionList"])
			o.Metadata = m
		}
		if o.UserAgent == "" && o.Platform == "" && len(o.Languages) == 0 {
			o = nil
		}
		return empty, true, s.page.SetUserAgentOverride(o)
	case "Emulation.setEmulatedMedia":
		if media := stringValue(p["media"]); media != "" && media != "screen" {
			return nil, true, fmt.Errorf("Media type %s is not supported", media)
		}
		base := s.page.BaseEnvironment().Preferences
		scheme := base.ColorScheme
		reduced := &base.ReducedMotion
		if features, ok := p["features"].([]any); ok {
			for _, item := range features {
				f, _ := item.(map[string]any)
				switch stringValue(f["name"]) {
				case "prefers-color-scheme":
					scheme = stringValue(f["value"])
					if scheme != "dark" && scheme != "light" {
						scheme = s.page.BaseEnvironment().Preferences.ColorScheme
					}
				case "prefers-reduced-motion":
					value := stringValue(f["value"])
					v := base.ReducedMotion
					if value != "" {
						v = value == "reduce"
					}
					reduced = &v
				case "forced-colors", "prefers-contrast":
				default:
					return nil, true, fmt.Errorf("Media feature %s is not supported", stringValue(f["name"]))
				}
			}
		}
		return empty, true, s.page.SetMediaPreferences(scheme, reduced)
	}
	return nil, false, nil
}
