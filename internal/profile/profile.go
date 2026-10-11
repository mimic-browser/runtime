// Package profile owns the versioned external environment contract. It does not
// own browser state: normalized values are construction inputs and projections.
package profile

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"golang.org/x/text/language"
	"io"
	"math"
	"net/url"
	"reflect"
	"strings"
	"time"
	_ "time/tzdata"
	"unicode"

	"github.com/moreveal/mimic/internal/state"
)

type Error struct {
	Path    string `json:"path"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

func (e *Error) Error() string     { return e.Path + ": " + e.Reason + ": " + e.Message }
func (e *Error) ProtocolCode() int { return -32602 }
func (e *Error) ProtocolData() any { return e }
func Limitations() []string {
	return []string{"Custom timezone/Intl locale requires the native Intl backend. Generated GPU/font recipes are supported; arbitrary manual graphics/font/media backends remain unavailable.", "Audio output supports modeled 44.1/48 kHz stereo recipes. Graphics readbacks remain stateful approximations, not physical-GPU or Chrome pixel equivalence.", "Proxy routes resource-loader HTTP(S) traffic only; ICE metadata does not route WebRTC or change the public IP.", "Changing identity does not change the installed Chrome implementation; proxy transport disables HTTP/3."}
}
func failure(path, reason, message string) error { return &Error{path, reason, message} }

type Proxy struct {
	Server   string
	Username string
	Password string
}

func (p Proxy) URL() string {
	if p.Server == "" {
		return ""
	}
	u, _ := url.Parse(p.Server)
	if p.Username != "" || p.Password != "" {
		u.User = url.UserPassword(p.Username, p.Password)
	}
	return u.String()
}

func ParseProxy(raw []byte) (Proxy, error) {
	var p Proxy
	if err := strictObject(raw, &p, "server", "username", "password"); err != nil {
		return Proxy{}, err
	}
	return p, nil
}

type Identity struct {
	UserAgent, Platform string
	Metadata            state.UserAgentMetadata
}
type Display struct {
	Width, Height, AvailableWidth, AvailableHeight int
	DeviceScaleFactor                              float64
	ColorDepth                                     int
	Orientation                                    state.ScreenOrientation
}
type Timing struct{ ExecutionScale, NavigationScale, NetworkScale float64 }
type Network struct {
	SaveData, Online        bool
	EffectiveType           string
	DownlinkMbps, RTTMillis float64
	CookiesEnabled          bool
	WireProfile             string
	ICE                     state.ICEProfile
	Proxy                   Proxy
}
type Document struct {
	SchemaVersion int
	BaseProfile   string
	Identity      Identity
	Display       Display
	Window        state.Window
	Hardware      state.Hardware
	Locale        state.Locale
	Graphics      state.Graphics
	Audio         state.Audio
	Fonts         state.Fonts
	Preferences   state.Preferences
	Network       Network
	Permissions   state.Permissions
	Capabilities  state.Capabilities
	Features      map[string]bool
	Timing        Timing
}

// Wire names are independent of Go's default exported-field JSON encoding.
func name(s string) string {
	if s == "WoW64" {
		return "wow64"
	}
	// Use conventional initialisms: cpuPerformance, rttMillis, ice, rtp.
	for i, r := range s {
		if i > 0 && unicode.IsLower(r) {
			if i == 1 {
				return strings.ToLower(s[:1]) + s[1:]
			}
			return strings.ToLower(s[:i-1]) + s[i-1:]
		}
	}
	return strings.ToLower(s)
}
func wire(v reflect.Value) any {
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		return wire(v.Elem())
	}
	switch v.Kind() {
	case reflect.Struct:
		m := map[string]any{}
		for i := 0; i < v.NumField(); i++ {
			m[name(v.Type().Field(i).Name)] = wire(v.Field(i))
		}
		return m
	case reflect.Map:
		m := map[string]any{}
		it := v.MapRange()
		for it.Next() {
			m[it.Key().String()] = wire(it.Value())
		}
		return m
	case reflect.Slice:
		a := []any{}
		for i := 0; i < v.Len(); i++ {
			a = append(a, wire(v.Index(i)))
		}
		return a
	default:
		return v.Interface()
	}
}
func (d Document) Object() map[string]any { return wire(reflect.ValueOf(d)).(map[string]any) }
func (d Document) Public() map[string]any {
	m := d.Object()
	p := m["network"].(map[string]any)["proxy"].(map[string]any)
	delete(p, "username")
	delete(p, "password")
	return m
}

// Imported JSON and the installed bundle may represent the same empty
// collection as []/{} and nil. Canonicalize once so typed profile hashes agree
// across generated and manual modes without reflecting each field into a map.
func canonicalizeEmptyCollections(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			canonicalizeEmptyCollections(v.Elem())
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if field := v.Field(i); field.CanSet() {
				canonicalizeEmptyCollections(field)
			}
		}
	case reflect.Slice:
		if v.Len() == 0 {
			v.SetZero()
			return
		}
		for i := 0; i < v.Len(); i++ {
			canonicalizeEmptyCollections(v.Index(i))
		}
	case reflect.Map:
		if v.Len() == 0 {
			v.SetZero()
			return
		}
		iter := v.MapRange()
		for iter.Next() {
			value := reflect.New(v.Type().Elem()).Elem()
			value.Set(iter.Value())
			canonicalizeEmptyCollections(value)
			v.SetMapIndex(iter.Key(), value)
		}
	}
}

func FromEnvironment(e state.Environment, base string, proxy Proxy) Document {
	e = e.Clone()
	s := e.Screen()
	n := e.Navigator()
	metadata := e.UserAgentData()
	if e.UserAgentOverride != nil && len(e.UserAgentOverride.Languages) > 0 {
		e.Locale.Languages = append([]string(nil), e.UserAgentOverride.Languages...)
	}
	orientation := state.ScreenOrientation{Type: "landscape-primary"}
	if e.ScreenOrientation != nil {
		orientation = *e.ScreenOrientation
	}
	d := Document{1, base, Identity{n.UserAgent, n.Platform, metadata}, Display{s.Width, s.Height, s.AvailWidth, s.AvailHeight, e.Display.DeviceScaleFactor, e.Display.ColorDepth, orientation}, e.Window, e.Hardware, e.Locale, e.Graphics, e.Audio, e.Fonts, e.Preferences, Network{e.Network.SaveData, e.Network.Online, e.Network.EffectiveType, e.Network.DownlinkMbps, e.Network.RTTMillis, e.Network.CookiesEnabled, e.Network.WireProfile, e.Network.ICE, proxy}, e.Permissions, e.Capabilities, e.Features, Timing{e.Time.ExecutionScale, e.Time.NavigationScale, e.Time.NetworkScale}}
	canonicalizeEmptyCollections(reflect.ValueOf(&d).Elem())
	return d
}

func (d Document) fingerprintID() string {
	identity := d
	// Proxy routing belongs to Context transport ownership, not the observable
	// environment or its bootstrap cache key. Credentials never enter this hash.
	identity.Network.Proxy = Proxy{}
	encoded, _ := json.Marshal(identity)
	return fmt.Sprintf("%x", sha256.Sum256(encoded))
}

// Apply constructs an independent environment while preserving the bundle's
// semantic version and live clocks. All external inputs pass Normalize first.
func (d Document) Apply(base state.Environment) state.Environment {
	return d.ApplyOwned(base).Clone()
}

// ApplyOwned transfers the already-normalized Document's nested values to the
// result. Browser construction uses it when no caller retains or mutates d;
// public environment getters still return defensive clones.
func (d Document) ApplyOwned(base state.Environment) state.Environment {
	e := base
	e.ProfileID = "mimic-profile-" + d.fingerprintID()
	e.UserAgentOverride = &state.UserAgentOverride{UserAgent: d.Identity.UserAgent, Platform: d.Identity.Platform, Metadata: &d.Identity.Metadata}
	e.Display = state.Display{PhysicalWidth: int(math.Round(float64(d.Display.Width) * d.Display.DeviceScaleFactor)), PhysicalHeight: int(math.Round(float64(d.Display.Height) * d.Display.DeviceScaleFactor)), AvailableWidth: int(math.Round(float64(d.Display.AvailableWidth) * d.Display.DeviceScaleFactor)), AvailableHeight: int(math.Round(float64(d.Display.AvailableHeight) * d.Display.DeviceScaleFactor)), DeviceScaleFactor: d.Display.DeviceScaleFactor, ColorDepth: d.Display.ColorDepth}
	e.ScreenOrientation = &d.Display.Orientation
	e.Window = d.Window
	e.Hardware = d.Hardware
	e.Locale = d.Locale
	e.Graphics = d.Graphics
	e.Audio = d.Audio
	e.Fonts = d.Fonts
	e.Preferences = d.Preferences
	e.Permissions = d.Permissions
	e.Capabilities = d.Capabilities
	e.Features = d.Features
	e.Network = state.Network{SaveData: d.Network.SaveData, Online: d.Network.Online, EffectiveType: d.Network.EffectiveType, DownlinkMbps: d.Network.DownlinkMbps, RTTMillis: d.Network.RTTMillis, CookiesEnabled: d.Network.CookiesEnabled, WireProfile: d.Network.WireProfile, ICE: d.Network.ICE}
	e.Time.ExecutionScale = d.Timing.ExecutionScale
	e.Time.NavigationScale = d.Timing.NavigationScale
	e.Time.NetworkScale = d.Timing.NetworkScale
	return e
}

func Decode(raw []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var value map[string]any
	if err := dec.Decode(&value); err != nil || value == nil {
		return nil, failure("$", "invalidType", "expected a JSON object")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, failure("$", "invalidJSON", "expected one JSON object")
	}
	return value, nil
}
func dynamic(path string) bool {
	return path == "identity" || strings.HasPrefix(path, "identity.") || path == "window" || strings.HasPrefix(path, "window.") || path == "display" || strings.HasPrefix(path, "display.") && path != "display.colorDepth" || path == "locale.languages" || path == "preferences.colorScheme" || path == "preferences.reducedMotion"
}

// Fields requiring new resource backends are explicit
// boundaries. Reading their measured baseline remains supported.
func boundary(path string) string {
	for _, p := range []string{"fonts", "graphics.webGLCapabilitiesJSON", "graphics.webGPU", "capabilities.devices", "capabilities.media"} {
		if path == p || strings.HasPrefix(path, p+".") {
			return "custom values are not yet validated by the available engine/backend"
		}
	}
	return ""
}
func merge(dst map[string]any, src map[string]any, typ reflect.Type, path string, patch bool) error {
	for key, v := range src {
		p := key
		if path != "" {
			p = path + "." + key
		}
		if v == nil {
			return failure(p, "invalidType", "null is not accepted")
		}
		var ft reflect.Type
		if typ.Kind() == reflect.Map {
			ft = typ.Elem()
		} else {
			for i := 0; i < typ.NumField(); i++ {
				if name(typ.Field(i).Name) == key {
					ft = typ.Field(i).Type
					break
				}
			}
		}
		if ft == nil {
			return failure(p, "unknownField", "unknown profile field")
		}
		if child, ok := v.(map[string]any); ok && (ft.Kind() == reflect.Struct || ft.Kind() == reflect.Map) {
			target, _ := dst[key].(map[string]any)
			if target == nil {
				target = map[string]any{}
			}
			if err := merge(target, child, ft, p, patch); err != nil {
				return err
			}
			dst[key] = target
			continue
		}
		if patch && !dynamic(p) {
			return failure(p, "requiresNewContext", "field cannot change on an existing Page")
		}
		if why := boundary(p); why != "" {
			a, _ := json.Marshal(dst[key])
			b, _ := json.Marshal(v)
			if string(a) != string(b) {
				return failure(p, "unsupported", why)
			}
		}
		if err := checkType(v, ft, p); err != nil {
			return err
		}
		dst[key] = v
	}
	return nil
}
func checkType(v any, t reflect.Type, path string) error {
	switch t.Kind() {
	case reflect.Struct, reflect.Map:
		return failure(path, "invalidType", "expected object")
	case reflect.Slice:
		a, ok := v.([]any)
		if !ok {
			return failure(path, "invalidType", "expected array")
		}
		for i, x := range a {
			if x == nil {
				return failure(path, "invalidType", "null array member")
			}
			if t.Elem().Kind() == reflect.Struct {
				m, ok := x.(map[string]any)
				if !ok {
					return failure(path, "invalidType", "expected object member")
				}
				if err := merge(map[string]any{}, m, t.Elem(), fmt.Sprintf("%s[%d]", path, i), false); err != nil {
					return err
				}
			} else if err := checkType(x, t.Elem(), path); err != nil {
				return err
			}
		}
	case reflect.Bool:
		if _, ok := v.(bool); !ok {
			return failure(path, "invalidType", "expected boolean")
		}
	case reflect.String:
		if _, ok := v.(string); !ok {
			return failure(path, "invalidType", "expected string")
		}
	default:
		n, ok := v.(json.Number)
		if !ok {
			return failure(path, "invalidType", "expected number")
		}
		if t.Kind() == reflect.Float64 {
			if _, err := n.Float64(); err != nil {
				return failure(path, "invalidValue", "invalid number")
			}
		} else {
			if _, err := n.Int64(); err != nil {
				return failure(path, "invalidType", "expected integer")
			}
		}
	}
	return nil
}
func Normalize(raw []byte, base state.Environment, current *Document) (Document, error) {
	src, err := Decode(raw)
	if err != nil {
		return Document{}, err
	}
	d := FromEnvironment(base, base.ProfileID, Proxy{})
	patch := current != nil
	if patch {
		d = *current
	} else {
		if src["schemaVersion"] != json.Number("1") {
			return d, failure("schemaVersion", "unsupportedVersion", "expected 1")
		}
		if src["baseProfile"] != base.ProfileID {
			return d, failure("baseProfile", "unsupported", "select an installed base profile")
		}
	}
	obj := d.Object()
	if err = merge(obj, src, reflect.TypeOf(d), "", patch); err != nil {
		return d, err
	}
	encoded, _ := json.Marshal(obj)
	d = Document{}
	if err = json.Unmarshal(encoded, &d); err != nil {
		return d, failure("$", "invalidValue", "value exceeds supported range")
	}
	canonicalizeEmptyCollections(reflect.ValueOf(&d).Elem())
	validation := d
	if patch {
		// Standard CDP deliberately permits identities/geometries outside the
		// full-profile creation contract. Unrelated Mimic patches preserve them.
		baseline := FromEnvironment(base, base.ProfileID, Proxy{})
		v, original := reflect.ValueOf(&validation).Elem(), reflect.ValueOf(baseline)
		for i := 0; i < v.NumField(); i++ {
			if _, changed := src[name(v.Type().Field(i).Name)]; !changed {
				v.Field(i).Set(original.Field(i))
			}
		}
	}
	if err = validation.Validate(base); err != nil {
		return d, err
	}
	return d, nil
}
func (d Document) Validate(base state.Environment) error {
	if d.SchemaVersion != 1 || d.BaseProfile != base.ProfileID {
		return failure("baseProfile", "unsupported", "profile must use the installed Mimic contract and base profile")
	}
	if d.Locale.Timezone == "" || d.Locale.Timezone == "Local" {
		return failure("locale.timezone", "invalidValue", "expected an explicit IANA time zone")
	}
	if _, err := time.LoadLocation(d.Locale.Timezone); err != nil {
		return failure("locale.timezone", "invalidValue", "unknown IANA time zone")
	}
	if _, err := language.Parse(d.Locale.IntlLocale); err != nil || d.Locale.IntlLocale == "" || strings.Contains(d.Locale.IntlLocale, "_") {
		return failure("locale.intlLocale", "invalidValue", "expected a BCP 47 locale")
	}
	if d.Display.Width < 1 || d.Display.Height < 1 || d.Display.Width > 10000000 || d.Display.Height > 10000000 || d.Display.DeviceScaleFactor <= 0 || d.Display.DeviceScaleFactor > 100 {
		return failure("display", "invalidValue", "invalid dimensions or scale")
	}
	if d.Display.AvailableWidth <= 0 || d.Display.AvailableWidth > d.Display.Width || d.Display.AvailableHeight <= 0 || d.Display.AvailableHeight > d.Display.Height {
		return failure("display", "incoherent", "available area must fit screen")
	}
	if d.Display.ColorDepth != 24 && d.Display.ColorDepth != 30 && d.Display.ColorDepth != 32 {
		return failure("display.colorDepth", "unsupported", "supported depths: 24, 30, 32")
	}
	if d.Window.ViewportWidth < 1 || d.Window.ViewportHeight < 1 || d.Window.OuterWidth < d.Window.ViewportWidth || d.Window.OuterHeight < d.Window.ViewportHeight {
		return failure("window", "incoherent", "viewport must fit outer window")
	}
	switch d.Display.Orientation.Type {
	case "landscape-primary", "landscape-secondary", "portrait-primary", "portrait-secondary":
	default:
		return failure("display.orientation.type", "invalidValue", "invalid orientation")
	}
	if d.Display.Orientation.Angle != 0 && d.Display.Orientation.Angle != 90 && d.Display.Orientation.Angle != 180 && d.Display.Orientation.Angle != 270 {
		return failure("display.orientation.angle", "invalidValue", "expected 0, 90, 180 or 270")
	}
	if d.Hardware.LogicalProcessors < 1 || d.Hardware.LogicalProcessors > 1024 {
		return failure("hardware.logicalProcessors", "invalidValue", "expected 1..1024")
	}
	switch d.Hardware.DeviceMemoryGB {
	case .25, .5, 1, 2, 4, 8, 16, 32:
	default:
		return failure("hardware.deviceMemoryGB", "invalidValue", "expected browser device-memory bucket")
	}
	for _, s := range append([]string{d.Identity.UserAgent, d.Identity.Platform}, d.Locale.Languages...) {
		if s == "" || strings.ContainsAny(s, "\r\n\x00") {
			return failure("identity", "invalidValue", "empty value or header control character")
		}
	}
	for _, tag := range d.Locale.Languages {
		if _, err := language.Parse(tag); err != nil {
			return failure("locale.languages", "invalidValue", "expected BCP 47 language tags")
		}
	}
	if d.Hardware.CPUPerformanceKnown && (d.Hardware.CPUPerformance < 0 || d.Hardware.CPUPerformance > 4) {
		return failure("hardware.cpuPerformance", "invalidValue", "expected a Chrome CPU class 0..4")
	}
	if d.Graphics.MaxTextureSize < 1 || d.Graphics.MaxTextureSize > 65536 || d.Graphics.MaxTextureSize&(d.Graphics.MaxTextureSize-1) != 0 {
		return failure("graphics.maxTextureSize", "invalidValue", "expected a power of two up to 65536")
	}
	if d.Audio.SampleRate != 44100 && d.Audio.SampleRate != 48000 {
		return failure("audio.sampleRate", "unsupported", "supported output-device sample rates are 44100 and 48000 Hz")
	}
	if d.Audio.Channels != 2 || d.Audio.BufferDuration != 0.01 || d.Audio.MaxBufferFrames != 7680 {
		return failure("audio", "unsupported", "this release models stereo output with the installed latency recipe")
	}
	m := d.Identity.Metadata
	if m.Mobile || m.Platform != "Windows" || d.Identity.Platform != "Win32" || m.FullVersion != base.Product.FullVersion {
		return failure("identity", "incoherent", "identity must retain the selected desktop Chrome implementation")
	}
	if d.Network.WireProfile != base.Network.WireProfile {
		return failure("network.wireProfile", "unsupported", "wire profile must match the selected bundle")
	}
	if d.Network.DownlinkMbps < 0 || d.Network.RTTMillis < 0 {
		return failure("network", "invalidValue", "negative network metrics")
	}
	switch d.Network.EffectiveType {
	case "slow-2g", "2g", "3g", "4g":
	default:
		return failure("network.effectiveType", "invalidValue", "invalid effective connection type")
	}
	for k, v := range d.Permissions {
		if _, ok := base.Permissions[k]; !ok {
			return failure("permissions."+k, "unsupported", "unknown baseline permission")
		}
		if v != "prompt" && v != "default" && v != "granted" && v != "denied" {
			return failure("permissions."+k, "invalidValue", "invalid permission state")
		}
	}
	for k, v := range d.Features {
		b, ok := base.Features[k]
		if !ok || v && !b {
			return failure("features."+k, "unsupported", "cannot enable an uncaptured feature")
		}
	}
	p := d.Network.Proxy
	if p.Server != "" {
		u, err := url.Parse(p.Server)
		if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" {
			return failure("network.proxy.server", "invalidValue", "expected http, https or socks5 authority without credentials or path")
		}
	} else if p.Username != "" || p.Password != "" {
		return failure("network.proxy", "invalidValue", "credentials require server")
	}
	if err := d.ApplyOwned(base).Validate(); err != nil {
		return failure("$", "incoherent", err.Error())
	}
	return nil
}

func Schema(base state.Environment) map[string]any {
	s := schemaType(reflect.TypeOf(Document{}), "")
	s["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	s["required"] = []string{"schemaVersion", "baseProfile"}
	props := s["properties"].(map[string]any)
	props["schemaVersion"].(map[string]any)["const"] = 1
	props["baseProfile"].(map[string]any)["enum"] = []string{base.ProfileID}
	return s
}
func schemaType(t reflect.Type, path string) map[string]any {
	s := map[string]any{}
	mode := "create"
	if dynamic(path) {
		mode = "dynamic"
	}
	if why := boundary(path); why != "" {
		mode = "unsupported"
		s["description"] = why
	}
	s["x-mimic-mutability"] = mode
	switch t.Kind() {
	case reflect.Struct:
		s["type"] = "object"
		s["additionalProperties"] = false
		p := map[string]any{}
		for i := 0; i < t.NumField(); i++ {
			k := name(t.Field(i).Name)
			q := k
			if path != "" {
				q = path + "." + k
			}
			p[k] = schemaType(t.Field(i).Type, q)
		}
		s["properties"] = p
	case reflect.Map:
		s["type"] = "object"
		s["additionalProperties"] = schemaType(t.Elem(), path+".*")
	case reflect.Slice:
		s["type"] = "array"
		s["items"] = schemaType(t.Elem(), path+"[]")
	case reflect.String:
		s["type"] = "string"
	case reflect.Bool:
		s["type"] = "boolean"
	case reflect.Float64:
		s["type"] = "number"
	default:
		s["type"] = "integer"
	}
	return s
}
