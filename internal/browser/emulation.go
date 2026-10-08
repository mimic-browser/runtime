package browser

import (
	"fmt"
	"math"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/moreveal/mimic/internal/state"
	"golang.org/x/text/language"
)

func (p *Page) SetDeviceMetrics(width, height int, scale float64, screenWidth, screenHeight int) error {
	if err := p.CheckProfileMutation(); err != nil {
		return err
	}
	if width < 0 || height < 0 || width > 10000000 || height > 10000000 || scale < 0 || math.IsInf(scale, 0) || math.IsNaN(scale) {
		return fmt.Errorf("Invalid device metrics")
	}
	base := p.ctx.Environment()
	if width == 0 {
		width = base.Window.ViewportWidth
	}
	if height == 0 {
		height = base.Window.ViewportHeight
	}
	if scale == 0 {
		scale = base.Display.DeviceScaleFactor
	}
	p.viewportObservationChange(true)
	defer p.viewportObservationChange(false)
	p.mu.Lock()
	defer p.mu.Unlock()
	// Screen dimensions are CSS pixels in CDP and do not shrink when only
	// devicePixelRatio changes. Physical dimensions are derived consistently.
	screen := base.Screen()
	if screenWidth == 0 {
		screenWidth = screen.Width
	}
	if screenHeight == 0 {
		screenHeight = screen.Height
	}
	p.env.Display.DeviceScaleFactor = scale
	p.env.Display.PhysicalWidth = int(math.Round(float64(screenWidth) * scale))
	p.env.Display.PhysicalHeight = int(math.Round(float64(screenHeight) * scale))
	// Frozen headful Chrome's metrics override exposes the entire emulated
	// screen as available, including when screen dimensions are omitted.
	p.env.Display.AvailableWidth = p.env.Display.PhysicalWidth
	p.env.Display.AvailableHeight = p.env.Display.PhysicalHeight
	p.env.Window.ViewportWidth = width
	p.env.Window.ViewportHeight = height
	return nil
}
func (p *Page) ClearDeviceMetrics() error {
	if err := p.CheckProfileMutation(); err != nil {
		return err
	}
	p.viewportObservationChange(true)
	defer p.viewportObservationChange(false)
	base := p.ctx.Environment()
	p.mu.Lock()
	p.env.Window.ViewportWidth = base.Window.ViewportWidth
	p.env.Window.ViewportHeight = base.Window.ViewportHeight
	p.env.Display = base.Display
	p.env.ScreenOrientation = base.ScreenOrientation
	p.mu.Unlock()
	return nil
}
func (p *Page) SetUserAgentOverride(o *state.UserAgentOverride) error {
	if err := p.CheckProfileMutation(); err != nil {
		return err
	}
	p.mu.Lock()
	p.env.UserAgentOverride = o
	// External callers retain no references to mutable identity metadata.
	p.env = p.env.Clone()
	p.mu.Unlock()
	return nil
}

func (p *Page) SetScreenOrientation(kind string, angle int) error {
	if err := p.CheckProfileMutation(); err != nil {
		return err
	}
	switch kind {
	case "portraitPrimary", "portraitSecondary", "landscapePrimary", "landscapeSecondary":
	default:
		return fmt.Errorf("Invalid screen orientation type")
	}
	if angle < 0 || angle > 360 {
		return fmt.Errorf("Invalid screen orientation angle")
	}
	p.viewportObservationChange(true)
	defer p.viewportObservationChange(false)
	split := "portrait"
	suffix := "primary"
	if strings.HasPrefix(kind, "landscape") {
		split = "landscape"
	}
	if strings.HasSuffix(kind, "Secondary") {
		suffix = "secondary"
	}
	p.mu.Lock()
	p.env.ScreenOrientation = &state.ScreenOrientation{Type: split + "-" + suffix, Angle: angle}
	p.mu.Unlock()
	return nil
}
func (p *Page) SetMediaPreferences(scheme string, reducedMotion *bool) error {
	if err := p.CheckProfileMutation(); err != nil {
		current := p.Environment().Preferences
		if (scheme == "" || scheme == current.ColorScheme) && (reducedMotion == nil || *reducedMotion == current.ReducedMotion) {
			return nil
		}
		return err
	}
	p.viewportObservationChange(true)
	defer p.viewportObservationChange(false)
	p.mu.Lock()
	defer p.mu.Unlock()
	if scheme != "" {
		p.env.Preferences.ColorScheme = scheme
	}
	if reducedMotion != nil {
		p.env.Preferences.ReducedMotion = *reducedMotion
	}
	return nil
}

// SetLocaleOverride changes the default ICU locale without changing
// navigator.languages or Accept-Language. Future navigation realms inherit
// the canonical Page environment.
func (p *Page) SetLocaleOverride(locale string) error {
	if err := p.CheckProfileMutation(); err != nil {
		return err
	}
	if locale == "" {
		locale = p.ctx.Environment().Locale.IntlLocale
	}
	canonical, err := language.Parse(locale)
	if err != nil || canonical == language.Und {
		return fmt.Errorf("Invalid locale")
	}
	locale = canonical.String()
	p.mu.Lock()
	p.env.Locale.IntlLocale = locale
	p.mu.Unlock()
	return nil
}

// SetTimezoneOverride changes the Page's clock projection without changing the
// process-wide Go or ICU timezone. The empty identifier restores the context.
func (p *Page) SetTimezoneOverride(timezone string) error {
	if err := p.CheckProfileMutation(); err != nil {
		return err
	}
	if timezone == "" {
		timezone = p.ctx.Environment().Locale.Timezone
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		return fmt.Errorf("Invalid timezone id")
	}
	p.mu.Lock()
	p.env.Locale.Timezone = timezone
	p.mu.Unlock()
	return nil
}

// SetWindowBounds updates the modeled native window shared by Window metrics
// and CDP. The outer bounds may be smaller than the emulated viewport: Chrome
// preserves the viewport in that case and reports the independently sized
// outer window through Browser.getWindowBounds and window.outerWidth/Height.
func (p *Page) SetWindowBounds(left, top, width, height *int) error {
	if err := p.CheckProfileMutation(); err != nil {
		return err
	}
	p.mu.Lock()
	if width != nil {
		if *width <= 0 {
			p.mu.Unlock()
			return fmt.Errorf("Invalid window width")
		}
	}
	if height != nil {
		if *height <= 0 {
			p.mu.Unlock()
			return fmt.Errorf("Invalid window height")
		}
	}
	p.mu.Unlock()
	p.viewportObservationChange(true)
	defer p.viewportObservationChange(false)
	p.mu.Lock()
	defer p.mu.Unlock()
	if width != nil {
		p.env.Window.OuterWidth = *width
	}
	if height != nil {
		p.env.Window.OuterHeight = *height
	}
	if left != nil {
		p.env.Window.X = *left
	}
	if top != nil {
		p.env.Window.Y = *top
	}
	return nil
}
