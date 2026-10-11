package profile

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestGeneratedProfileRoundtripAndDiversity(t *testing.T) {
	base := testBase()
	if len(gpuFontRecipes) < 40 {
		t.Fatalf("GPU/font catalog unexpectedly small: %d", len(gpuFontRecipes))
	}
	seen := map[string]bool{}
	seenGPU := map[string]bool{}
	seenFont := map[string]bool{}
	for i := 0; i < 1000; i++ {
		raw := []byte(fmt.Sprintf(`{"seed":"account-%d","browser":"chrome","version":152,"platform":"windows"}`, i))
		d, descriptor, err := Generate(raw, base)
		if err != nil {
			t.Fatal(err)
		}
		next, restored, err := ResolveToken(descriptor.Token(), base)
		if err != nil || !reflect.DeepEqual(next, d) || restored.ProfileID != descriptor.ProfileID {
			t.Fatalf("roundtrip: %v", err)
		}
		if seen[descriptor.ProfileID] {
			t.Fatal("duplicate in fixed 1000-profile corpus", i)
		}
		seen[descriptor.ProfileID] = true
		if !reflect.DeepEqual(d.Hardware, base.Hardware) || !reflect.DeepEqual(d.Identity, FromEnvironment(base, base.ProfileID, Proxy{}).Identity) || d.Network.WireProfile != base.Network.WireProfile {
			t.Fatal("machine recipe changed Chrome identity, hardware or wire behavior")
		}
		if !reflect.DeepEqual(d.Graphics, base.Graphics) || !reflect.DeepEqual(d.Fonts, base.Fonts) {
			matched := false
			for _, recipe := range gpuFontRecipes {
				candidate := FromEnvironment(base, base.ProfileID, Proxy{})
				recipe.apply(&candidate)
				if reflect.DeepEqual(d.Graphics, candidate.Graphics) && reflect.DeepEqual(d.Fonts, candidate.Fonts) {
					matched = true
					seenGPU[d.Graphics.Renderer] = true
					seenFont[d.Fonts.System["menu"]] = true
					break
				}
			}
			if !matched {
				t.Fatal("graphics and fonts were spliced across recipes")
			}
		}
		if d.Window.OuterWidth > d.Display.AvailableWidth || d.Window.OuterHeight > d.Display.AvailableHeight {
			t.Fatal("window exceeds recipe bounds")
		}
		if d.Window.X < 0 || d.Window.Y < 0 || d.Window.X+d.Window.OuterWidth > d.Display.AvailableWidth || d.Window.Y+d.Window.OuterHeight > d.Display.AvailableHeight {
			t.Fatal("window position exceeds available display")
		}
		if d.Audio.SampleRate != 44100 && d.Audio.SampleRate != 48000 {
			t.Fatal("unsupported audio device rate")
		}
	}
	if len(seenGPU) < 30 || len(seenFont) < 3 {
		t.Fatalf("generated corpus lacks GPU/font diversity: %d GPUs, %d font recipes", len(seenGPU), len(seenFont))
	}
	_, a, err := Generate([]byte(`{}`), base)
	if err != nil {
		t.Fatal(err)
	}
	_, b, err := Generate([]byte(`{}`), base)
	if err != nil {
		t.Fatal(err)
	}
	if a.Seed == b.Seed || len(a.Seed) != 64 {
		t.Fatal("random seed generation")
	}
}

func TestGPUFontRecipeCatalog(t *testing.T) {
	base := testBase()
	ids := map[string]bool{}
	renderers := map[string]bool{}
	for _, recipe := range gpuFontRecipes {
		if ids[recipe.ID] {
			t.Fatalf("duplicate recipe ID %q", recipe.ID)
		}
		ids[recipe.ID] = true
		d := FromEnvironment(base, base.ProfileID, Proxy{})
		recipe.apply(&d)
		if err := d.Validate(base); err != nil {
			t.Fatalf("%s: %v", recipe.ID, err)
		}
		renderers[d.Graphics.Renderer] = true
	}
	if len(ids) != 47 || len(renderers) != 40 {
		t.Fatalf("unexpected recipe coverage: %d recipes, %d renderers", len(ids), len(renderers))
	}
}

func TestProfileGenerationRejectsInvalidInputs(t *testing.T) {
	for _, raw := range []string{`{"seed":""}`, `{"seed":null}`, `{"version":0}`, `{"version":151}`, `{"version":"152"}`, `{"platform":"linux"}`, `{"browser":""}`, `{"browser":"firefox"}`, `{"extra":1}`} {
		if _, _, err := Generate([]byte(raw), testBase()); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	_, descriptor, _ := Generate([]byte(`{"seed":"fixed"}`), testBase())
	descriptor.ProfileID = "tampered"
	if _, _, err := ResolveToken(descriptor.Token(), testBase()); err == nil {
		t.Fatal("accepted tampering")
	}
}

func TestManualImportChecksAndRoundtrip(t *testing.T) {
	base := testBase()
	d, descriptor, err := ImportManual([]byte(`{"hardware":{"logicalProcessors":4},"locale":{"languages":["fr-FR","en"]}}`), base)
	if err != nil {
		t.Fatal(err)
	}
	roundtrip, _, err := ResolveToken(descriptor.Token(), base)
	if err != nil || !reflect.DeepEqual(d, roundtrip) {
		t.Fatalf("manual roundtrip: %v", err)
	}
	for _, raw := range []string{`{"window":{"viewportWidth":99999}}`, `{"graphics":{"renderer":"fake"}}`, `{"graphics":{"webGPU":{"vendor":"fake"}}}`, `{"fonts":{"sansSerif":"fake"}}`, `{"identity":{"platform":"Linux"}}`, `{"network":{"proxy":{}}}`, `{"schemaVersion":1}`} {
		if _, _, err := ImportManual([]byte(raw), base); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	descriptor.Environment["hardware"].(map[string]any)["logicalProcessors"] = 7
	raw, _ := json.Marshal(descriptor)
	if _, _, err := Restore(raw, base); err == nil {
		t.Fatal("accepted modified export without explicit manual import")
	}
}

func TestManualImportAllowsArbitraryUserAgent(t *testing.T) {
	base := testBase()
	for _, ua := range []string{"Chrome/151.0.0.0", "Firefox/140.0", "CustomClient/1", base.Navigator().UserAgent + " Custom/1"} {
		raw, _ := json.Marshal(map[string]any{"identity": map[string]any{"userAgent": ua}})
		d, descriptor, err := ImportManual(raw, base)
		if err != nil {
			t.Fatalf("import %q: %v", ua, err)
		}
		restored, _, err := ResolveToken(descriptor.Token(), base)
		if err != nil || restored.Identity.UserAgent != ua {
			t.Fatalf("roundtrip %q: %v", ua, err)
		}
		e := d.Apply(base)
		if e.Navigator().UserAgent != ua || e.RequestHeaders()["User-Agent"] != ua {
			t.Fatalf("UA projection %q", ua)
		}
	}
	for _, ua := range []string{"", "x\r", "x\n", "x\x00"} {
		raw, _ := json.Marshal(map[string]any{"identity": map[string]any{"userAgent": ua}})
		if _, _, err := ImportManual(raw, base); err == nil {
			t.Fatalf("accepted invalid UA %q", ua)
		}
	}
	if _, _, err := ImportManual([]byte(`{"identity":{"platform":"Linux"}}`), base); err == nil {
		t.Fatal("accepted platform change")
	}
	if _, _, err := ImportManual([]byte(`{"identity":{"metadata":{"architecture":"arm"}}}`), base); err == nil {
		t.Fatal("accepted Client Hints change")
	}
}

func TestManualAudioUsesModeledOutputDevice(t *testing.T) {
	base := testBase()
	d, descriptor, err := ImportManual([]byte(`{"audio":{"sampleRate":44100}}`), base)
	if err != nil || d.Audio.SampleRate != 44100 || d.Apply(base).Audio.SampleRate != 44100 {
		t.Fatalf("manual audio output: %v", err)
	}
	restored, _, err := ResolveToken(descriptor.Token(), base)
	if err != nil || restored.Audio != d.Audio {
		t.Fatalf("audio descriptor roundtrip: %v", err)
	}
	for _, raw := range []string{`{"audio":{"sampleRate":96000}}`, `{"audio":{"channels":8}}`, `{"audio":{"bufferDuration":0}}`} {
		if _, _, err := ImportManual([]byte(raw), base); err == nil {
			t.Fatal("accepted unsupported audio recipe", raw)
		}
	}
}

func TestProxyDoesNotChangeEnvironmentIdentity(t *testing.T) {
	base := testBase()
	d, _, err := Generate([]byte(`{"seed":"same-device"}`), base)
	if err != nil {
		t.Fatal(err)
	}
	want := d.Apply(base).ProfileID
	d.Network.Proxy = Proxy{Server: "socks5://localhost:1080", Username: "private", Password: "secret"}
	if got := d.Apply(base).ProfileID; got != want {
		t.Fatal("routing changed environment identity")
	}
}

func TestGenerationModeDoesNotChangeProfileIdentity(t *testing.T) {
	base := testBase()
	d, generated, err := Generate([]byte(`{"seed":"same-device"}`), base)
	if err != nil {
		t.Fatal(err)
	}
	fields := d.Public()
	delete(fields, "schemaVersion")
	delete(fields, "baseProfile")
	delete(fields["network"].(map[string]any), "proxy")
	raw, _ := json.Marshal(fields)
	manualDoc, manual, err := ImportManual(raw, base)
	if err != nil || generated.ProfileID != manual.ProfileID || !reflect.DeepEqual(d, manualDoc) {
		t.Fatalf("mode changed profile identity: %v", err)
	}
}

func TestManualRejectsMixedGPUFontRecipes(t *testing.T) {
	base := testBase()
	var first, second Document
	for i := 0; i < 100 && second.Graphics.Renderer == ""; i++ {
		d, _, err := Generate([]byte(fmt.Sprintf(`{"seed":"manual-pair-%d"}`, i)), base)
		if err != nil {
			t.Fatal(err)
		}
		if d.Graphics.WebGLCapabilitiesJSON == "" {
			continue
		}
		if first.Graphics.Renderer == "" {
			first = d
		} else if d.Graphics.Renderer != first.Graphics.Renderer && !reflect.DeepEqual(d.Fonts, first.Fonts) {
			second = d
		}
	}
	if second.Graphics.Renderer == "" {
		t.Fatal("fixed seeds did not select two different graphics/font pairs")
	}
	fields := first.Public()
	delete(fields, "schemaVersion")
	delete(fields, "baseProfile")
	delete(fields["network"].(map[string]any), "proxy")
	fields["fonts"] = second.Public()["fonts"]
	raw, _ := json.Marshal(fields)
	if _, _, err := ImportManual(raw, base); err == nil {
		t.Fatal("accepted graphics and fonts from different recipes")
	}
}
