package profile

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"strings"

	"github.com/moreveal/mimic/internal/state"
)

// Generation selects a complete GPU/font observation recipe, then varies only
// independently modeled window, preference and audio-output settings. Chrome
// identity and the network wire recipe remain those of the installed bundle.
type Generation struct {
	Browser  string `json:"browser,omitempty"`
	Version  int    `json:"version,omitempty"`
	Platform string `json:"platform,omitempty"`
	Seed     string `json:"seed,omitempty"`
}

// Descriptor is portable data, not an API-version selector. ProfileID commits
// to the resolved environment: a changed bundle or generator fails restoration
// instead of silently changing the identity of a saved profile.
type Descriptor struct {
	Mode        string         `json:"mode"`
	BaseProfile string         `json:"baseProfile"`
	Seed        string         `json:"seed,omitempty"`
	ProfileID   string         `json:"profileId"`
	Environment map[string]any `json:"environment,omitempty"`
}

func strictObject(raw []byte, target any, fields ...string) error {
	obj, err := Decode(raw)
	if err != nil {
		return err
	}
	allowed := map[string]bool{}
	for _, field := range fields {
		allowed[field] = true
	}
	for key, value := range obj {
		if !allowed[key] {
			return failure(key, "unknownField", "unknown profile parameter")
		}
		if value == nil {
			return failure(key, "invalidType", "null is not accepted")
		}
	}
	if err := json.Unmarshal(raw, target); err != nil {
		return failure("$", "invalidType", "invalid profile parameter type")
	}
	return nil
}

func Generate(raw []byte, base state.Environment) (Document, Descriptor, error) {
	if base.Product.Name != "Chrome" || !strings.HasPrefix(base.Product.Version, "152.") || base.Platform.OS != "Windows" {
		return Document{}, Descriptor{}, failure("generate", "unsupported", "generator requires the installed Chrome 152 Windows bundle")
	}
	var options Generation
	if err := strictObject(raw, &options, "browser", "version", "platform", "seed"); err != nil {
		return Document{}, Descriptor{}, err
	}
	obj, _ := Decode(raw)
	if (options.Browser != "" && options.Browser != "chrome") || (options.Platform != "" && options.Platform != "windows") || (options.Version != 0 && (options.Version != 152 || !strings.HasPrefix(base.Product.Version, "152."))) {
		return Document{}, Descriptor{}, failure("generate", "unsupported", "select the installed Chrome 152 Windows bundle")
	}
	for _, key := range []string{"browser", "platform"} {
		if obj[key] == "" {
			return Document{}, Descriptor{}, failure(key, "invalidValue", "empty selector")
		}
	}
	if _, exists := obj["version"]; exists && options.Version == 0 {
		return Document{}, Descriptor{}, failure("version", "unsupported", "select the installed milestone")
	}
	if _, exists := obj["seed"]; exists && options.Seed == "" {
		return Document{}, Descriptor{}, failure("seed", "invalidValue", "seed must be nonempty")
	}
	if len(options.Seed) > 1024 {
		return Document{}, Descriptor{}, failure("seed", "invalidValue", "seed exceeds 1024 bytes")
	}
	if options.Seed == "" {
		var random [32]byte
		if _, err := rand.Read(random[:]); err != nil {
			return Document{}, Descriptor{}, err
		}
		options.Seed = hex.EncodeToString(random[:])
	}
	d := FromEnvironment(base, base.ProfileID, Proxy{})
	// Preserve the measured window chrome insets and screen/device recipe.
	dx := d.Window.OuterWidth - d.Window.ViewportWidth
	dy := d.Window.OuterHeight - d.Window.ViewportHeight
	maxWidth, maxHeight := d.Display.AvailableWidth-dx, d.Display.AvailableHeight-dy
	if maxWidth < 800 || maxHeight < 480 {
		return Document{}, Descriptor{}, failure("display", "unsupported", "baseline does not support the desktop window recipe")
	}
	choose := func(group string, count int) int {
		h := sha256.Sum256([]byte("mimic-profile\x00" + base.ProfileID + "\x00" + options.Seed + "\x00" + group))
		return int(binary.LittleEndian.Uint64(h[:8]) % uint64(count))
	}
	if selected := choose("gpu-font-recipe", len(gpuFontRecipes)+1); selected != 0 {
		gpuFontRecipes[selected-1].apply(&d)
	}
	d.Window.ViewportWidth = 800 + choose("width", maxWidth-800+1)
	d.Window.ViewportHeight = 480 + choose("height", maxHeight-480+1)
	d.Window.OuterWidth = d.Window.ViewportWidth + dx
	d.Window.OuterHeight = d.Window.ViewportHeight + dy
	d.Window.X = choose("window-x", d.Display.AvailableWidth-d.Window.OuterWidth+1)
	d.Window.Y = choose("window-y", d.Display.AvailableHeight-d.Window.OuterHeight+1)
	d.Preferences.ColorScheme = []string{"light", "dark"}[choose("theme", 2)]
	d.Preferences.ReducedMotion = choose("motion", 2) == 1
	d.Audio.SampleRate = []float64{44100, 48000}[choose("audio-output-rate", 2)]
	if err := d.Validate(base); err != nil {
		return Document{}, Descriptor{}, err
	}
	return d, Descriptor{Mode: "generated", BaseProfile: base.ProfileID, Seed: options.Seed, ProfileID: d.fingerprintID()}, nil
}

func Restore(raw []byte, base state.Environment) (Document, Descriptor, error) {
	var saved Descriptor
	if err := strictObject(raw, &saved, "mode", "baseProfile", "seed", "profileId", "environment"); err != nil {
		return Document{}, Descriptor{}, err
	}
	if saved.BaseProfile != base.ProfileID || saved.ProfileID == "" {
		return Document{}, Descriptor{}, failure("profile", "incompatibleProfile", "incomplete descriptor or unavailable base profile")
	}
	if saved.Mode == "manual" {
		if saved.Seed != "" {
			return Document{}, Descriptor{}, failure("seed", "invalidValue", "manual profiles have no seed")
		}
		input, _ := json.Marshal(saved.Environment)
		d, resolved, err := ImportManual(input, base)
		if err != nil {
			return Document{}, Descriptor{}, err
		}
		if resolved.ProfileID != saved.ProfileID {
			return Document{}, Descriptor{}, failure("profile", "incompatibleProfile", "manual profile hash differs")
		}
		return d, resolved, nil
	}
	if saved.Mode != "generated" || saved.Seed == "" || saved.Environment != nil {
		return Document{}, Descriptor{}, failure("profile", "invalidValue", "expected a generated descriptor")
	}
	input, _ := json.Marshal(Generation{Seed: saved.Seed})
	d, resolved, err := Generate(input, base)
	if err != nil {
		return Document{}, Descriptor{}, err
	}
	if resolved.ProfileID != saved.ProfileID {
		return Document{}, Descriptor{}, failure("profile", "incompatibleProfile", "resolved profile differs; regenerate explicitly for this Mimic release")
	}
	return d, resolved, nil
}

func (d Descriptor) Token() string {
	raw, _ := json.Marshal(d)
	return "mimic:" + base64.RawURLEncoding.EncodeToString(raw)
}

func ResolveToken(token string, base state.Environment) (Document, Descriptor, error) {
	if len(token) > 1<<20 || !strings.HasPrefix(token, "mimic:") {
		return Document{}, Descriptor{}, failure("profile", "invalidValue", "expected a Mimic profile token (maximum 1 MiB)")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(token, "mimic:"))
	if err != nil {
		return Document{}, Descriptor{}, failure("profile", "invalidValue", "invalid profile token")
	}
	return Restore(raw, base)
}

// ImportManual accepts the installed baseline or an exact complete generated
// GPU/font pair. Individual graphics/font edits remain unsupported.
func ImportManual(raw []byte, base state.Environment) (Document, Descriptor, error) {
	obj, err := Decode(raw)
	if err != nil {
		return Document{}, Descriptor{}, err
	}
	for _, key := range []string{"schemaVersion", "baseProfile"} {
		if _, exists := obj[key]; exists {
			return Document{}, Descriptor{}, failure(key, "unknownField", "profile format follows the installed Mimic release")
		}
	}
	if network, ok := obj["network"].(map[string]any); ok {
		if _, exists := network["proxy"]; exists {
			return Document{}, Descriptor{}, failure("network.proxy", "unknownField", "set proxy on createContext")
		}
	}
	obj["schemaVersion"], obj["baseProfile"] = 1, base.ProfileID
	encoded, _ := json.Marshal(obj)
	baseline := FromEnvironment(base, base.ProfileID, Proxy{})
	d, err := Normalize(encoded, base, nil)
	if err != nil && obj["graphics"] != nil && obj["fonts"] != nil {
		// A manual user can pin one of the curated whole-machine observations,
		// but cannot splice an arbitrary GPU or font override into another.
		for _, recipe := range gpuFontRecipes {
			candidate := base.Clone()
			candidateDoc := FromEnvironment(candidate, base.ProfileID, Proxy{})
			recipe.apply(&candidateDoc)
			candidate.Graphics, candidate.Fonts = candidateDoc.Graphics, candidateDoc.Fonts
			candidateFields := FromEnvironment(candidate, base.ProfileID, Proxy{}).Object()
			graphicsInput, _ := json.Marshal(obj["graphics"])
			graphicsRecipe, _ := json.Marshal(candidateFields["graphics"])
			fontsInput, _ := json.Marshal(obj["fonts"])
			fontsRecipe, _ := json.Marshal(candidateFields["fonts"])
			if string(graphicsInput) != string(graphicsRecipe) || string(fontsInput) != string(fontsRecipe) {
				continue
			}
			d, err = Normalize(encoded, candidate, nil)
			if err == nil {
				break
			}
		}
	}
	if err != nil {
		return Document{}, Descriptor{}, err
	}
	if !reflect.DeepEqual(d.Graphics, baseline.Graphics) || !reflect.DeepEqual(d.Fonts, baseline.Fonts) {
		matched := false
		for _, recipe := range gpuFontRecipes {
			candidate := baseline
			recipe.apply(&candidate)
			if reflect.DeepEqual(d.Graphics, candidate.Graphics) && reflect.DeepEqual(d.Fonts, candidate.Fonts) {
				matched = true
				break
			}
		}
		if !matched {
			return Document{}, Descriptor{}, failure("graphics", "unsupported", "use a complete supported GPU/font recipe")
		}
	}
	// The UA string is a caller-selected observation, not a backend selector.
	identity := d.Identity
	identity.UserAgent = baseline.Identity.UserAgent
	if !reflect.DeepEqual(identity, baseline.Identity) {
		return Document{}, Descriptor{}, failure("identity", "unsupported", "platform and client hints must remain the installed Chrome 152 recipe")
	}
	environment := d.Public()
	delete(environment, "schemaVersion")
	delete(environment, "baseProfile")
	delete(environment["network"].(map[string]any), "proxy")
	return d, Descriptor{Mode: "manual", BaseProfile: base.ProfileID, ProfileID: d.fingerprintID(), Environment: environment}, nil
}

func Warnings(mode string) []string {
	if mode == "manual" {
		return []string{"Manual mode: do not combine inconsistent surfaces. Known invalid combinations are rejected, but unmodeled cross-surface relationships cannot yet be certified.", "Manual userAgent strings are caller-selected and do not change the engine, platform, Client Hints or wire behavior. Manual graphics and fonts accept only the installed baseline or a complete supported GPU/font pair. A Context proxy does not route WebRTC or define locale/timezone."}
	}
	return []string{"Generated profiles select one supported GPU/font observation recipe while retaining Chrome 152 identity and wire behavior. They also vary window geometry, preferences and audio output rate. Readbacks are stable modeled operations, not captured pixel hashes; this does not certify physical-GPU equivalence or route WebRTC through the Context proxy."}
}

func ManualSchema(base state.Environment) map[string]any {
	schema := Schema(base)
	delete(schema, "required")
	properties := schema["properties"].(map[string]any)
	delete(properties, "schemaVersion")
	delete(properties, "baseProfile")
	delete(properties["network"].(map[string]any)["properties"].(map[string]any), "proxy")
	properties["identity"].(map[string]any)["description"] = "userAgent accepts any nonempty header-safe string; platform and Client Hints retain the installed Chrome 152 recipe."
	for _, key := range []string{"graphics", "fonts"} {
		properties[key].(map[string]any)["description"] = "Only the installed baseline or an exact complete generated GPU/font pair is accepted; individual field edits are rejected."
	}
	properties["audio"].(map[string]any)["description"] = "Modeled stereo output device: 44100 or 48000 Hz; latency fields retain the installed recipe."
	schema["description"] = "Explicit manual import fields. Generated/imported Contexts are immutable; mutability annotations describe ordinary Page emulation only."
	return schema
}
