package chrome152

import (
	"strings"
	"testing"

	"github.com/moreveal/mimic/internal/state"
)

func TestDefaultOracleEnvironmentIsHeadful(t *testing.T) {
	bundle := New()
	profile := bundle.Environment()
	if profile.ID != "chrome-152-windows-x64-headful-controlled-v1" || profile.Mode != state.BrowserModeHeadful {
		t.Fatalf("unexpected default oracle profile: %q %q", profile.ID, profile.Mode)
	}
	if strings.Contains(profile.State.Navigator().UserAgent, "HeadlessChrome") {
		t.Fatal("headful default leaked the explicit headless environment identity")
	}
	if brands := profile.State.Product.UserAgentBrands; len(brands) != 2 || brands[1].Brand != "Chromium" {
		t.Fatalf("unexpected Chrome-for-Testing UA-CH brands: %+v", brands)
	}
	if profile.State.Network.RTTMillis != 100 {
		t.Fatalf("unexpected controlled-oracle network RTT: %v", profile.State.Network.RTTMillis)
	}
	if !bundle.Expectations().PrimaryOracle.Authoritative || bundle.Expectations().PrimaryOracle.Mode != state.BrowserModeHeadful {
		t.Fatal("headful Chrome must be the authoritative compatibility oracle")
	}
}

func TestHeadlessOracleEnvironmentRequiresExplicitSelection(t *testing.T) {
	bundle, err := NewForMode(state.BrowserModeHeadless)
	if err != nil {
		t.Fatal(err)
	}
	profile := bundle.Environment()
	if profile.Mode != state.BrowserModeHeadless || !strings.Contains(profile.State.Navigator().UserAgent, "HeadlessChrome") {
		t.Fatalf("incoherent explicit headless profile: %#v", profile)
	}
	if bundle.Expectations().Sources[state.BrowserModeHeadless].Authoritative {
		t.Fatal("headless observations must not become generic Chrome expectations")
	}

	headful := New().Environment().State
	headless := profile.State
	if headless.Display != headful.Display {
		t.Fatalf("headless display geometry differs from headful: headless=%+v headful=%+v", headless.Display, headful.Display)
	}
	if headless.Window != headful.Window {
		t.Fatalf("headless window geometry differs from headful: headless=%+v headful=%+v", headless.Window, headful.Window)
	}
}
