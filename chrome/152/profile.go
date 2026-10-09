package chrome152

import (
	"fmt"
	"net/http"
	"strings"
	"sync"

	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	"github.com/moreveal/mimic/chrome/152/generated"
	"github.com/moreveal/mimic/compatibility"
	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/state"
)

const (
	Milestone        = 152
	Version          = "152.0.7977.82"
	ChromiumCommit   = "d04cdb24d67b081f6cf80200ffc5233f44b61109"
	ChromiumRevision = 1669021
)

// Environment returns the canonical Windows x64 Stable profile for this
// compatibility bundle. Runtime state such as the wall clock is captured by
// the base constructor, while product identity is pinned here.
func environment(mode state.BrowserMode) state.Environment {
	environment := state.ChromeDesktopWindows(state.Product{Name: "Chrome", Version: "152.0.0.0", FullVersion: Version, UserAgentProduct: "Chrome", UserAgentBrands: []state.UserAgentBrand{{Brand: "Not?A_Brand", Version: "24", FullVersion: "24.0.0.0"}, {Brand: "Chromium", Version: "152", FullVersion: Version}}})
	environment.ProfileID = "chrome-152-windows-x64-headful-controlled-v1"
	environment.Presentation.Mode = mode
	environment.Platform.Architecture = "x86_64"
	environment.Fonts.Fallback = []string{"Segoe UI Emoji", "Tahoma", "SimSun", "Segoe UI Symbol", "Segoe UI", "Arial"}
	for _, name := range strings.Fields("background-fetch background-sync accelerometer gyroscope magnetometer screen-wake-lock clipboard-write payment-handler storage-access pointer-lock") {
		environment.Permissions[name] = "granted"
	}
	environment.Permissions["periodic-background-sync"] = "denied"
	environment.Permissions["fullscreen"] = "denied"
	environment.Features = map[string]bool{"GenericSensorExtraClasses": false, "WebNFC": false, "SystemWakeLock": false, "SpeakerSelection": false, "WebAppInstallation": false, "ApproximateGeolocationPermission": false}
	environment.Capabilities = state.Capabilities{
		StorageQuotaBytes: 10 * 1024 * 1024 * 1024,
		Devices:           state.DeviceCapabilities{Posture: "continuous"},
		Media: state.MediaCapabilities{
			Formats:              mediaFormats(),
			RTP:                  rtpCatalog(),
			Kinds:                []string{"audioinput", "videoinput", "audiooutput"},
			SupportedConstraints: strings.Fields("aspectRatio autoGainControl brightness channelCount colorTemperature contrast deviceId displaySurface echoCancellation exposureCompensation exposureMode exposureTime facingMode focusDistance focusMode frameRate groupId height iso latency noiseSuppression pan pointsOfInterest resizeMode restrictOwnAudio sampleRate sampleSize saturation sharpness suppressLocalAudioPlayback tilt torch voiceIsolation whiteBalanceMode width zoom"),
		},
	}
	environment.Capabilities.KeyboardLayout = map[string]string{
		"Backquote": "`", "Minus": "-", "Equal": "=", "BracketLeft": "[", "BracketRight": "]",
		"Backslash": "\\", "IntlBackslash": "\\", "Semicolon": ";", "Quote": "'", "Comma": ",", "Period": ".", "Slash": "/",
	}
	for letter := 'A'; letter <= 'Z'; letter++ {
		environment.Capabilities.KeyboardLayout["Key"+string(letter)] = strings.ToLower(string(letter))
	}
	for digit := '0'; digit <= '9'; digit++ {
		environment.Capabilities.KeyboardLayout["Digit"+string(digit)] = string(digit)
	}
	environment.Platform.OSVersion = "19.0.0"
	// Frozen Chrome 152 local oracle on this profile reports CPU performance class 4.
	environment.Hardware.CPUPerformance = 4
	environment.Hardware.CPUPerformanceKnown = true
	environment.Hardware.LogicalProcessors = 28
	environment.Hardware.DeviceMemoryGB = 32
	environment.Display.PhysicalWidth = 2560
	environment.Display.PhysicalHeight = 1440
	environment.Display.AvailableWidth = 2560
	environment.Display.AvailableHeight = 1392
	environment.Window.OuterWidth = 1280
	environment.Window.OuterHeight = 800
	environment.Window.ViewportWidth = 1272
	environment.Window.ViewportHeight = 653
	environment.Locale.Languages = []string{"ru-RU", "ru", "en-US", "en"}
	environment.Locale.ReduceAcceptLanguage = true
	environment.Locale.IntlLocale = "ru"
	environment.Locale.Timezone = "Asia/Tbilisi"
	// Live transport phases are measured, not rescaled to match one capture.
	// Browser-visible Resource Timing, CDP timing and scheduler delivery all use
	// those same measurements. Machine-dependent synthetic latency belongs in a
	// separately selected environment profile, never in Chrome-version data.
	environment.Time.ExecutionScale = 1
	environment.Time.NavigationScale = 1
	environment.Time.NetworkScale = 1
	environment.Graphics.WebGPU.Vendor = "nvidia"
	environment.Graphics.WebGPU.Architecture = "blackwell"
	environment.Graphics.WebGPU.SubgroupMinSize = 32
	environment.Graphics.WebGPU.SubgroupMaxSize = 32
	// Versioned language capability set; iteration order is not a machine fingerprint.
	environment.Graphics.WebGPU.WGSLLanguageFeatures = strings.Fields("packed_4x8_integer_dot_product subgroup_uniformity immediate_address_space linear_indexing subgroup_id readonly_and_readwrite_storage_textures unrestricted_pointer_parameters pointer_composite_access texture_and_sampler_let uniform_buffer_standard_layout")
	// Adapter discovery timing is selected by this environment profile rather
	// than treated as a Chrome-version semantic.
	environment.Graphics.WebGPU.InitializationDelayMillis = 205
	environment.Network.WireProfile = "chrome-152-windows-x64"
	// These are properties of the pinned differential-test machine/network,
	// not of a target site. They remain canonical and externally configurable
	// state, while the Chrome 152 bundle supplies the reproducible default.
	environment.Network.ICE.HostCandidateCount = 3
	environment.Network.ICE.PortOffsets = []int{0, 2, 7}
	// Host and server-reflexive counts are independent environment facts;
	// neither is a Chrome-version constant. This reproducible profile selects
	// the three mappings observed in its baseline capture even though live
	// differential runs may see fewer transient STUN results.
	environment.Network.ICE.ReflexiveCandidateCount = 3
	environment.Network.ICE.ReflexivePortOffsets = []int{0, 7, 2}
	environment.Network.ICE.PublicAddress = "94.43.38.7"
	environment.Network.ICE.NetworkCost = 999
	environment.Network.ICE.HostDelayMillis = 21
	environment.Network.ICE.ReflexiveDelayMillis = 30
	environment.Network.ICE.EndDelayMillis = 130
	if mode == state.BrowserModeHeadless {
		// This is retained only as an explicit regression environment. It is not
		// the default Chrome 152 oracle and cannot supply generic expectations.
		// Headless and headful intentionally share the same display/window geometry.
		environment.ProfileID = "chrome-152-windows-x64-headless-controlled-v1"
		environment.Product.UserAgentProduct = "HeadlessChrome"
		environment.Graphics.WebGPU.InitializationDelayMillis = 250
		environment.Permissions["geolocation"] = "denied"
		environment.Permissions["camera"] = "denied"
		environment.Permissions["clipboard-read"] = "denied"
		environment.Permissions["keyboard-lock"] = "denied"
	}
	return environment
}

func newTransport() (http.RoundTripper, error) {
	// The PSK-capable profile omits extension 41 on a cold connection and emits
	// a real pre_shared_key only after this session's cache has received a TLS
	// ticket. This preserves Chrome's cold fingerprint while enabling genuine
	// resumption on a later connection; it is not a declared/faked fingerprint.
	return network.NewTLSClientTransport(profiles.Chrome_152_PSK,
		tls_client.WithNotFollowRedirects(),
		tls_client.WithTimeoutSeconds(30),
		tls_client.WithRandomTLSExtensionOrder(),
		tls_client.WithProtocolRacing(),
		tls_client.WithTransportOptions(&tls_client.TransportOptions{DisableCompression: true}),
	)
}

func newProxyTransport(proxyURL string) (http.RoundTripper, error) {
	return network.NewProxyTLSClientTransport(profiles.Chrome_152_PSK, proxyURL,
		tls_client.WithNotFollowRedirects(), tls_client.WithTimeoutSeconds(30),
		tls_client.WithRandomTLSExtensionOrder(), tls_client.WithTransportOptions(&tls_client.TransportOptions{DisableCompression: true}))
}

type Bundle struct{ mode state.BrowserMode }

func New() *Bundle { return &Bundle{mode: state.BrowserModeHeadful} }

// NewForMode selects a complete presentation/runtime environment. Headless is
// available for explicitly mode-scoped regression probes only.
func NewForMode(mode state.BrowserMode) (*Bundle, error) {
	if mode != state.BrowserModeHeadful && mode != state.BrowserModeHeadless {
		return nil, fmt.Errorf("unsupported Chrome oracle mode %q", mode)
	}
	return &Bundle{mode: mode}, nil
}

func (*Bundle) Version() compatibility.ChromeVersion {
	return compatibility.ChromeVersion{Milestone: Milestone, Version: Version, ChromiumCommit: ChromiumCommit, ChromiumRevision: ChromiumRevision}
}

var immutableSurface = sync.OnceValue(func() *compatibility.WebAPISurface {
	return &compatibility.WebAPISurface{
		GeneratedJavaScript:        generated.Surface,
		GeneratedCatalogJSON:       generated.SurfaceCatalog,
		TrustedTypeEventAttributes: generated.BootstrapEventAttributes,
		BootstrapMetadata:          generated.BootstrapMetadata,
		LookupExposure: func(name string) (compatibility.RealmExposure, bool) {
			switch name {
			case "window.insecure.non-isolated":
				return generated.InsecureWindowExposure(), true
			case "window.secure.non-isolated":
				return generated.SecureWindowExposure(), true
			case "window.secure.isolated":
				return generated.SecureIsolatedWindowExposure(), true
			case "worker.secure.non-isolated":
				return generated.SecureWorkerExposure(), true
			default:
				return compatibility.RealmExposure{}, false
			}
		},
	}
})

func (*Bundle) Surface() *compatibility.WebAPISurface { return immutableSurface() }
func (*Bundle) CDP() *compatibility.ProtocolSchema {
	return &compatibility.ProtocolSchema{Methods: generated.ProtocolMethods(), Events: generated.ProtocolEvents()}
}
func (b *Bundle) Environment() *compatibility.EnvironmentProfile {
	e := environment(b.mode)
	return &compatibility.EnvironmentProfile{ID: e.ProfileID, Mode: e.Presentation.Mode, State: e, NewTransport: newTransport, NewProxyTransport: newProxyTransport}
}
func (*Bundle) Expectations() *compatibility.CompatExpectations {
	return &compatibility.CompatExpectations{
		Platform: "windows-x64", Channel: "stable",
		PrimaryOracle: compatibility.OracleDescriptor{ChromeVersion: Version, ChromiumRevision: ChromiumRevision, V8Version: "15.2.124.21", Platform: "windows-x64", Mode: state.BrowserModeHeadful, EnvironmentProfileID: "chrome-152-windows-x64-headful-controlled-v1", Authoritative: true},
		Sources: map[state.BrowserMode]compatibility.OracleExpectationSource{
			state.BrowserModeHeadful:  {Mode: state.BrowserModeHeadful, CapturePath: "compatibility/captures/navigator-chrome152.json", Authoritative: true},
			state.BrowserModeHeadless: {Mode: state.BrowserModeHeadless, CapturePath: "compatibility/captures/navigator-chrome152-headless.json", Authoritative: false},
		},
	}
}
