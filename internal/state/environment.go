package state

import (
	"fmt"
	"math"
	"strings"
	"time"
)

type UserAgentBrand struct{ Brand, Version, FullVersion string }
type Product struct {
	Name, Version, FullVersion string
	// UserAgentProduct is the product token exposed consistently in the
	// legacy user agent and HTTP User-Agent header. Empty means Chrome.
	UserAgentProduct string
	UserAgentBrands  []UserAgentBrand
}
type Platform struct {
	OS, OSVersion, Architecture string
	Mobile                      bool
}
type Hardware struct {
	// CPUPerformance is the measured Chrome performance class of the selected machine profile.
	CPUPerformance      int
	CPUPerformanceKnown bool
	LogicalProcessors   int
	DeviceMemoryGB      float64
}
type Display struct {
	PhysicalWidth, PhysicalHeight   int
	AvailableWidth, AvailableHeight int
	DeviceScaleFactor               float64
	ColorDepth                      int
}
type Window struct{ X, Y, OuterWidth, OuterHeight, ViewportWidth, ViewportHeight int }
type Graphics struct {
	Vendor, Renderer string
	MaxTextureSize   int
	// Immutable serialized query profile; empty selects the measured default.
	WebGLCapabilitiesJSON string
	WebGPU                GPUAdapter
	// WebGPUAdapters optionally selects coherent alternative adapters by
	// "low-power", "high-performance", or "fallback" request policy.
	WebGPUAdapters map[string]GPUAdapter
}

// Audio describes the selected output device's observable configuration. PCM
// processing remains Page-owned and never opens an operating-system device.
type Audio struct {
	SampleRate      float64
	Channels        int
	BufferDuration  float64
	MaxBufferFrames int
}
type GPUAdapter struct {
	Vendor, Architecture, Device, Description string
	SubgroupMinSize, SubgroupMaxSize          uint32
	IsFallbackAdapter                         bool
	Features                                  []string
	Limits                                    map[string]uint64
	WGSLLanguageFeatures                      []string
	// InitializationDelayMillis is selected by the machine/environment profile.
	// Adapter discovery is asynchronous in Chrome and depends on the graphics
	// process, driver and host state; it is not a browser-version constant.
	InitializationDelayMillis float64
}

// Fonts selects resource families, never captured glyph metrics. Empty names use
// the normal desktop defaults. The selected resources must exist in the font engine.
type Fonts struct {
	Serif, SansSerif, Monospace, SystemUI string
	System                                map[string]string
	Fallback                              []string
}

type Locale struct {
	// Languages retains the complete ordered preference list. Reduction affects
	// navigator observations and document requests, but not Worker fetch headers.
	ReduceAcceptLanguage bool
	Languages            []string
	IntlLocale           string
	Timezone             string
}
type Preferences struct {
	ColorScheme   string
	SystemColors  map[string]map[string]string
	ReducedMotion bool
	DoNotTrack    bool
}
type Time struct {
	WallOrigin      time.Time
	MonotonicOrigin time.Duration
	// The virtual browser clock is independent of host implementation cost.
	// These factors map host execution and transport durations into the pinned
	// compatibility profile's observable timeline.
	ExecutionScale  float64
	NavigationScale float64
	NetworkScale    float64
}
type Network struct {
	SaveData                bool
	Online                  bool
	EffectiveType           string
	DownlinkMbps, RTTMillis float64
	CookiesEnabled          bool
	// WireProfile identifies the low-level TLS/HTTP behavior which must agree
	// with Product. Its implementation is supplied by the compatibility bundle.
	WireProfile string
	// ICE is canonical network state observed by WebRTC. Candidate generation
	// must project this state instead of inventing an unrelated network view in
	// the JavaScript binding.
	ICE ICEProfile
}
type ICEProfile struct {
	HostCandidateCount      int
	ReflexiveCandidateCount int
	// PortOffsets describes socket allocation relative to the first host
	// candidate and therefore belongs to canonical network state.
	PortOffsets []int
	// ReflexivePortOffsets preserves the host/network stack's observable STUN
	// completion order. It may differ from local candidate enumeration order.
	ReflexivePortOffsets []int
	PublicAddress        string
	NetworkCost          int
	// Candidate discovery timing is machine/network state. It is kept apart
	// from browser-version rules and reproduced only by the selected profile.
	HostDelayMillis      float64
	ReflexiveDelayMillis float64
	EndDelayMillis       float64
}
type Permissions map[string]string

type BrowserMode string

const (
	BrowserModeHeadful  BrowserMode = "headful"
	BrowserModeHeadless BrowserMode = "headless"
)

type Presentation struct {
	// Mode is part of the selected environment, never inferred from individual
	// Navigator or Window properties.
	Mode BrowserMode
}

type Environment struct {
	UserAgentOverride *UserAgentOverride
	ScreenOrientation *ScreenOrientation
	ProfileID         string
	Presentation      Presentation
	Product           Product
	Platform          Platform
	Hardware          Hardware
	Display           Display
	Window            Window
	Graphics          Graphics
	Audio             Audio
	Fonts             Fonts
	Locale            Locale
	Preferences       Preferences
	Time              Time
	Network           Network
	Permissions       Permissions
	Capabilities      Capabilities
	// Explicit feature overrides restrict the bundle's captured exposure.
	Features map[string]bool
}

func ChromeDesktopWindows(product Product) Environment {
	// The product profile is versioned, but wall-clock time is execution state,
	// not the Chrome release date. Capture it once so Date, Performance and the
	// scheduler all project the same current clock.
	now := time.Now().UTC()
	if len(product.UserAgentBrands) == 0 {
		major := strings.SplitN(product.Version, ".", 2)[0]
		product.UserAgentBrands = []UserAgentBrand{{"Not?A_Brand", "24", "24.0.0.0"}, {"Chromium", major, product.FullVersion}, {"Google Chrome", major, product.FullVersion}}
	}
	if product.UserAgentProduct == "" {
		product.UserAgentProduct = "Chrome"
	}
	return Environment{
		ProfileID:    "chrome-desktop-windows-headful-default",
		Presentation: Presentation{Mode: BrowserModeHeadful},
		Product:      product,
		Platform:     Platform{"Windows", "10.0.0", "x86", false},
		Hardware:     Hardware{LogicalProcessors: 8, DeviceMemoryGB: 8},
		Display:      Display{PhysicalWidth: 1920, PhysicalHeight: 1080, AvailableWidth: 1920, AvailableHeight: 1040, DeviceScaleFactor: 1, ColorDepth: 24},
		Window:       Window{0, 0, 1280, 800, 1280, 720},
		Graphics: Graphics{
			Vendor: "Google Inc. (Intel)", Renderer: "ANGLE (Intel, Intel(R) UHD Graphics Direct3D11 vs_5_0 ps_5_0, D3D11)", MaxTextureSize: 16384,
			WebGPU: GPUAdapter{Vendor: "intel", Architecture: "gen-12lp", Features: []string{"bgra8unorm-storage", "clip-distances", "core-features-and-limits", "depth-clip-control", "depth32float-stencil8", "dual-source-blending", "float32-blendable", "float32-filterable", "indirect-first-instance", "primitive-index", "rg11b10ufloat-renderable", "shader-f16", "subgroup-size-control", "subgroups", "texture-component-swizzle", "texture-compression-bc", "texture-compression-bc-sliced-3d", "texture-formats-tier1", "texture-formats-tier2", "timestamp-query"}},
		},
		Audio:       Audio{SampleRate: 48000, Channels: 2, BufferDuration: 0.01, MaxBufferFrames: 7680},
		Locale:      Locale{Languages: []string{"en-US", "en"}, IntlLocale: "en-US", Timezone: "UTC"},
		Preferences: Preferences{ColorScheme: "dark"},
		Time:        Time{WallOrigin: now, ExecutionScale: 1, NavigationScale: 1, NetworkScale: 1},
		Network:     Network{Online: true, EffectiveType: "4g", DownlinkMbps: 10, RTTMillis: 50, CookiesEnabled: true, ICE: ICEProfile{HostCandidateCount: 1, PortOffsets: []int{0}, NetworkCost: 999}},
		Permissions: Permissions{"geolocation": "prompt", "notifications": "default"},
	}
}

func (e Environment) Validate() error {
	if e.ProfileID == "" {
		return fmt.Errorf("environment profile ID is required")
	}
	if e.Presentation.Mode != BrowserModeHeadful && e.Presentation.Mode != BrowserModeHeadless {
		return fmt.Errorf("browser presentation mode must be headful or headless")
	}
	if e.Product.Name != "Chrome" || e.Product.Version == "" {
		return fmt.Errorf("product must name a concrete Chrome version")
	}
	if e.Display.PhysicalWidth <= 0 || e.Display.PhysicalHeight <= 0 || e.Display.DeviceScaleFactor <= 0 {
		return fmt.Errorf("invalid display")
	}
	if e.Window.ViewportWidth <= 0 || e.Window.ViewportHeight <= 0 || e.Window.OuterWidth < e.Window.ViewportWidth || e.Window.OuterHeight < e.Window.ViewportHeight {
		return fmt.Errorf("incoherent window and viewport")
	}
	if e.Hardware.LogicalProcessors < 1 || len(e.Locale.Languages) == 0 {
		return fmt.Errorf("incomplete hardware or locale")
	}
	if _, err := time.LoadLocation(e.Locale.Timezone); err != nil {
		return fmt.Errorf("timezone: %w", err)
	}
	if e.Preferences.ColorScheme != "light" && e.Preferences.ColorScheme != "dark" {
		return fmt.Errorf("color scheme must be light or dark")
	}
	if e.Time.WallOrigin.IsZero() {
		return fmt.Errorf("wall clock origin is required")
	}
	if e.Time.ExecutionScale <= 0 || e.Time.NavigationScale <= 0 || e.Time.NetworkScale <= 0 {
		return fmt.Errorf("clock scales must be positive")
	}
	if e.Network.ICE.HostCandidateCount < 0 || e.Network.ICE.ReflexiveCandidateCount < 0 || e.Network.ICE.NetworkCost < 0 {
		return fmt.Errorf("invalid ICE profile")
	}
	if e.Graphics.WebGPU.InitializationDelayMillis < 0 {
		return fmt.Errorf("invalid WebGPU initialization delay")
	}
	if e.Network.RTTMillis < 0 {
		return fmt.Errorf("invalid network timing profile")
	}
	if e.Network.ICE.HostDelayMillis < 0 || e.Network.ICE.ReflexiveDelayMillis < e.Network.ICE.HostDelayMillis || e.Network.ICE.EndDelayMillis < e.Network.ICE.ReflexiveDelayMillis {
		return fmt.Errorf("invalid ICE candidate timing profile")
	}
	if len(e.Network.ICE.PortOffsets) > 0 && len(e.Network.ICE.PortOffsets) != e.Network.ICE.HostCandidateCount {
		return fmt.Errorf("ICE port offsets must match host candidate count")
	}
	if len(e.Network.ICE.ReflexivePortOffsets) > 0 && len(e.Network.ICE.ReflexivePortOffsets) != e.Network.ICE.ReflexiveCandidateCount {
		return fmt.Errorf("ICE reflexive port offsets must match reflexive candidate count")
	}
	return nil
}

type NavigatorView struct {
	UserAgent, Platform   string
	Languages             []string
	HardwareConcurrency   int
	DeviceMemory          float64
	Online, CookieEnabled bool
}

func (e Environment) Navigator() NavigatorView {
	languages := e.Locale.Languages
	if e.Locale.ReduceAcceptLanguage && len(languages) > 1 {
		languages = languages[:1]
	}
	os := "Windows NT 10.0; Win64; x64"
	n := NavigatorView{
		UserAgent: fmt.Sprintf("Mozilla/5.0 (%s) AppleWebKit/537.36 (KHTML, like Gecko) %s/%s Safari/537.36", os, e.Product.UserAgentProduct, e.Product.Version),
		Platform:  "Win32", Languages: append([]string(nil), languages...), HardwareConcurrency: e.Hardware.LogicalProcessors, DeviceMemory: e.Hardware.DeviceMemoryGB, Online: e.Network.Online, CookieEnabled: e.Network.CookiesEnabled,
	}
	if o := e.UserAgentOverride; o != nil {
		if o.UserAgent != "" {
			n.UserAgent = o.UserAgent
		}
		if o.Platform != "" {
			n.Platform = o.Platform
		}
		if len(o.Languages) > 0 {
			n.Languages = append([]string(nil), o.Languages...)
		}
	}
	return n
}

type ScreenView struct {
	Width, Height, AvailWidth, AvailHeight, ColorDepth, PixelDepth int
	DevicePixelRatio                                               float64
}

func (e Environment) Screen() ScreenView {
	d := e.Display.DeviceScaleFactor
	availableWidth, availableHeight := e.Display.AvailableWidth, e.Display.AvailableHeight
	if availableWidth <= 0 {
		availableWidth = e.Display.PhysicalWidth
	}
	if availableHeight <= 0 {
		availableHeight = e.Display.PhysicalHeight
	}
	return ScreenView{int(math.Round(float64(e.Display.PhysicalWidth) / d)), int(math.Round(float64(e.Display.PhysicalHeight) / d)), int(math.Round(float64(availableWidth) / d)), int(math.Round(float64(availableHeight) / d)), e.Display.ColorDepth, e.Display.ColorDepth, d}
}
func (e Environment) RequestHeaders() map[string]string {
	return e.requestHeaders(e.Locale.ReduceAcceptLanguage)
}

func (e Environment) WorkerRequestHeaders() map[string]string {
	return e.requestHeaders(false)
}

// Browser-created CORS preflights use the complete language preference even
// when document requests expose Chrome's reduced Accept-Language value.
func (e Environment) PreflightRequestHeaders() map[string]string {
	return e.requestHeaders(false)
}

func (e Environment) requestHeaders(reduce bool) map[string]string {
	selectedLanguages := e.Locale.Languages
	if reduce && len(selectedLanguages) > 1 {
		selectedLanguages = selectedLanguages[:1]
	}
	n := e.Navigator()
	if e.UserAgentOverride != nil && len(e.UserAgentOverride.Languages) > 0 {
		selectedLanguages = e.UserAgentOverride.Languages
	}
	languages := ""
	for index, language := range selectedLanguages {
		if index == 0 {
			languages = language
		} else {
			languages += fmt.Sprintf(",%s;q=%.1f", language, math.Max(.1, 1-float64(index)/10))
		}
	}
	if len(selectedLanguages) == 1 {
		if base, _, found := strings.Cut(selectedLanguages[0], "-"); found {
			languages += "," + base + ";q=0.9"
		}
	}
	brands := make([]string, 0, len(e.Product.UserAgentBrands))
	for _, brand := range e.Product.UserAgentBrands {
		brands = append(brands, fmt.Sprintf(`%q;v=%q`, brand.Brand, brand.Version))
	}
	headers := map[string]string{"User-Agent": n.UserAgent, "Accept-Language": languages, "Sec-CH-UA": strings.Join(brands, ", "), "Sec-CH-UA-Mobile": "?0", "Sec-CH-UA-Platform": `"Windows"`}
	if o := e.UserAgentOverride; o != nil && o.UserAgent != "" {
		delete(headers, "Sec-CH-UA")
		delete(headers, "Sec-CH-UA-Mobile")
		delete(headers, "Sec-CH-UA-Platform")
		if m := o.Metadata; m != nil {
			brands = nil
			for _, b := range m.Brands {
				brands = append(brands, fmt.Sprintf(`%q;v=%q`, b.Brand, b.Version))
			}
			headers["Sec-CH-UA"] = strings.Join(brands, ", ")
			headers["Sec-CH-UA-Platform"] = fmt.Sprintf("%q", m.Platform)
			headers["Sec-CH-UA-Mobile"] = "?0"
			if m.Mobile {
				headers["Sec-CH-UA-Mobile"] = "?1"
			}
		}
	}
	return headers
}

// ClientHintHeaders projects only hints accepted by the origin. This keeps
// network-visible identity tied to the same Product and Platform as Navigator.
func (e Environment) ClientHintHeaders(accepted map[string]bool) map[string]string {
	if o := e.UserAgentOverride; o != nil && o.UserAgent != "" {
		return overrideHintHeaders(o.Metadata, accepted)
	}
	metadata := e.UserAgentData()
	return overrideHintHeaders(&metadata, accepted)
}
