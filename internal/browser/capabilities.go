package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/scheduler"
)

// Origin capability state is owned by the context, shared by its same-origin
// pages, and discarded with it. JS wrappers never own a second permission store.
type originCapabilities struct {
	permissionFallback string
	permissions        map[string]string
	clipboard          string
	preventSilent      bool
	login              string
	buckets            map[string]*storageBucket
	locks              []*capabilityLock
	sequence           int64
}

type storageBucket struct {
	Name       string   `json:"name"`
	Persisted  bool     `json:"persisted"`
	Durability string   `json:"durability"`
	Expires    *float64 `json:"expires"`
	Quota      int64    `json:"quota"`
	Usage      int64    `json:"usage"`
}

type capabilityLock struct {
	ID                   int64
	Name, Mode, ClientID string
	Held                 bool
}

func (c *Context) originCapabilities(origin string) *originCapabilities {
	if c.capabilities == nil {
		c.capabilities = map[string]*originCapabilities{}
	}
	s := c.capabilities[origin]
	if s == nil {
		s = &originCapabilities{permissions: map[string]string{}, permissionFallback: c.permissionDefaultFallback}
		for name, value := range c.env.Permissions {
			if value == "default" {
				value = "prompt"
			}
			s.permissions[name] = value
		}
		for name, value := range c.permissionDefaults {
			s.permissions[name] = value
		}
		c.capabilities[origin] = s
	}
	return s
}

func (c *Context) SetPermission(origin, name, value string) error {
	if value != "granted" && value != "denied" && value != "prompt" {
		return fmt.Errorf("invalid permission state %q", value)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if origin == "" {
		if c.permissionDefaults == nil {
			c.permissionDefaults = map[string]string{}
		}
		c.permissionDefaults[name] = value
		for existing := range c.capabilities {
			c.setPermissionLocked(existing, name, value)
		}
		return nil
	}
	c.setPermissionLocked(origin, name, value)
	return nil
}

// ResetPermissions restores the selected environment's permission defaults.
func (c *Context) ResetPermissions() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.permissionDefaults = nil
	c.permissionDefaultFallback = ""
	for origin, state := range c.capabilities {
		state.permissionFallback = ""
		for name := range state.permissions {
			value := c.env.Permissions[name]
			if value == "" || value == "default" {
				value = "prompt"
			}
			c.setPermissionLocked(origin, name, value)
		}
	}
}

// GrantPermissions replaces the allowlist and denies other permission names.
func (c *Context) GrantPermissions(origin string, permissions []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	grants := map[string]bool{}
	for _, name := range permissions {
		grants[name] = true
	}
	apply := func(origin string) {
		state := c.originCapabilities(origin)
		state.permissionFallback = "denied"
		for name := range state.permissions {
			if !grants[name] {
				c.setPermissionLocked(origin, name, "denied")
			}
		}
		for name := range grants {
			c.setPermissionLocked(origin, name, "granted")
		}
	}
	if origin != "" {
		apply(origin)
		return
	}
	c.permissionDefaultFallback = "denied"
	c.permissionDefaults = map[string]string{}
	for name := range c.env.Permissions {
		c.permissionDefaults[name] = "denied"
	}
	for name := range grants {
		c.permissionDefaults[name] = "granted"
	}
	for existing := range c.capabilities {
		apply(existing)
	}
}

func (c *Context) setPermissionLocked(origin, name, value string) {
	s := c.originCapabilities(origin)
	if s.permissions[name] == value {
		return
	}
	s.permissions[name] = value
	for r := range c.permissionRealms {
		if r == nil || r.origin != origin || r.permissionNotifier == nil || r.resourceContext.Err() != nil {
			continue
		}
		r.scheduler.Post(scheduler.Control, 0, func(ctx context.Context) error {
			if (name == "camera" || name == "microphone") && value != "granted" {
				for _, track := range r.cameraTracks {
					permission := "camera"
					if track.kind == "audio" {
						permission = "microphone"
					}
					if name == permission && !track.remote && !track.stopped {
						r.stopCameraTrack(track)
						if r.cameraNotifier != nil {
							if _, err := r.runtime.Call(ctx, r.cameraNotifier, nil, r.val(track.id), r.val("ended")); err != nil {
								return err
							}
						}
					}
				}
			}
			_, err := r.runtime.Call(ctx, r.permissionNotifier, nil, r.val(name))
			return err
		})
	}
}

// navigatorWebDriver is a product invariant, independent of CDP attachment,
// debugging ports, headless flags, and environment overrides.
const navigatorWebDriver = false

func addCapabilityHosts(r *Realm, h map[string]any) {
	p := r.agent.Page()
	h["rtpCapabilities"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		return r.val(p.environmentView().Capabilities.Media.RTP.Capabilities(strarg(a, 0), strarg(a, 1))), nil
	})
	h["rtpMedia"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		m := p.environmentView().Capabilities.Media.RTP.Media(strarg(a, 0), strarg(a, 1))
		return r.val(map[string]any{"payloads": m.Payloads, "extensions": m.Extensions, "attributes": m.Attributes}), nil
	})
	h["mediaTypeSupport"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		m := p.environmentView().Capabilities.Media.Formats.Support(strarg(a, 0))
		return r.val(map[string]any{"play": m.Play, "mediaSource": m.MediaSource, "precise": m.Precise, "efficient": m.Efficient, "kind": m.Kind}), nil
	})
	h["capabilityState"] = r.fn(func(_ engine.Value, a []engine.Value) (engine.Value, error) {
		e := p.environmentView()
		switch strarg(a, 0) {
		case "geolocationPolicy":
			return r.val(r.selfPolicyAllows("geolocation")), nil
		case "geolocation":
			p.mu.RLock()
			location := p.geolocationOverride
			p.mu.RUnlock()
			if location == nil {
				return r.val(nil), nil
			}
			values := map[string]any{}
			for name, value := range map[string]*float64{
				"latitude": location.Latitude, "longitude": location.Longitude, "accuracy": location.Accuracy,
				"altitude": location.Altitude, "altitudeAccuracy": location.AltitudeAccuracy,
				"heading": location.Heading, "speed": location.Speed,
			} {
				values[name] = nil
				if value != nil {
					values[name] = *value
				}
			}
			return r.val(values), nil
		case "ai":
			// The selected runtime has no model service. Exposure is independent
			// of backend availability; creation still respects document policy.
			return r.val(map[string]any{"available": false, "policyAllowed": r.selfPolicyAllows(strarg(a, 1))}), nil
		case "feature":
			enabled, ok := e.Features[strarg(a, 1)]
			return r.val(!ok || enabled), nil
		case "identity":
			var dnt any
			if e.Preferences.DoNotTrack {
				dnt = "1"
			}
			return r.val(map[string]any{"vendorSub": "", "productSub": "20030107", "appCodeName": "Mozilla", "doNotTrack": dnt}), nil
		case "activation":
			return r.val(map[string]any{"isActive": r.navigationActivated(), "hasBeenActive": !r.activationAt.IsZero(), "pending": r.scheduler.HasPendingInput()}), nil
		case "presentation":
			return r.val(map[string]any{"mode": string(e.Presentation.Mode)}), nil
		case "network":
			return r.val(map[string]any{"effectiveType": e.Network.EffectiveType, "downlink": e.Network.DownlinkMbps, "rtt": e.Network.RTTMillis, "saveData": e.Network.SaveData}), nil
		case "devices":
			return r.val(map[string]any{"bluetooth": e.Capabilities.Devices.BluetoothAvailable, "posture": e.Capabilities.Devices.Posture}), nil
		case "battery":
			// No battery backend is connected. Blink's unavailable-device state
			// represents a fully charged supply, with no discharge deadline.
			return r.val(map[string]any{"charging": true, "level": 1, "chargingTime": 0, "dischargingTime": nil}), nil
		case "deviceList":
			// No transport backend is installed. All device API families share
			// this registry boundary and cannot fabricate authorized devices.
			return r.val([]map[string]any{}), nil
		case "keyboard":
			return r.val(e.Capabilities.KeyboardLayout), nil
		case "media":
			kinds := e.Capabilities.Media.Kinds
			if kinds == nil {
				kinds = []string{}
			}
			return r.val(map[string]any{"kinds": kinds, "constraints": e.Capabilities.Media.SupportedConstraints}), nil
		case "quota":
			if r.origin == "null" {
				return r.val(nil), nil
			}
			// Web Storage is excluded from StorageManager usage in Blink. Future
			// quota clients must account bytes here, alongside the same budget.
			return r.val(map[string]any{"quota": e.Capabilities.StorageQuotaBytes, "usage": 0, "usageDetails": map[string]any{}}), nil
		}
		p.ctx.mu.Lock()
		defer p.ctx.mu.Unlock()
		s := p.ctx.originCapabilities(r.origin)
		switch strarg(a, 0) {
		case "bucket":
			if s.buckets == nil {
				s.buckets = map[string]*storageBucket{}
			}
			op, name := strarg(a, 1), strarg(a, 2)
			for key, b := range s.buckets {
				if b.Expires != nil && *b.Expires <= float64(r.scheduler.Now().UnixMilli()) {
					delete(s.buckets, key)
				}
			}
			if op == "keys" {
				keys := []string{}
				for key := range s.buckets {
					keys = append(keys, key)
				}
				sort.Strings(keys)
				return r.val(keys), nil
			}
			if op == "delete" {
				delete(s.buckets, name)
				return nil, nil
			}
			b := s.buckets[name]
			if op == "open" && b == nil {
				b = &storageBucket{Name: name, Durability: "relaxed", Quota: e.Capabilities.StorageQuotaBytes}
				var options struct {
					Persisted  bool
					Durability string
					Quota      int64
					Expires    *float64
				}
				_ = json.Unmarshal([]byte(strarg(a, 3)), &options)
				b.Persisted = options.Persisted && s.permissions["persistent-storage"] == "granted"
				if options.Durability != "" {
					b.Durability = options.Durability
				}
				if options.Quota > 0 && options.Quota < b.Quota {
					b.Quota = options.Quota
				}
				b.Expires = options.Expires
				s.buckets[name] = b
			}
			if b == nil {
				return r.val(nil), nil
			}
			if op == "setExpires" {
				value := numarg(a, 3)
				b.Expires = &value
			}
			if op == "persist" {
				b.Persisted = s.permissions["persistent-storage"] == "granted"
			}
			encoded, _ := json.Marshal(b)
			var result map[string]any
			_ = json.Unmarshal(encoded, &result)
			return r.val(result), nil
		case "lock":
			op := strarg(a, 1)
			if op == "query" {
				held, pending := []map[string]any{}, []map[string]any{}
				for _, l := range s.locks {
					v := map[string]any{"name": l.Name, "mode": l.Mode, "clientId": l.ClientID}
					if l.Held {
						held = append(held, v)
					} else {
						pending = append(pending, v)
					}
				}
				return r.val(map[string]any{"held": held, "pending": pending}), nil
			}
			if op == "enqueue" {
				s.sequence++
				s.locks = append(s.locks, &capabilityLock{ID: s.sequence, Name: strarg(a, 2), Mode: strarg(a, 3), ClientID: r.ID})
				return r.val(s.sequence), nil
			}
			id := int64(numarg(a, 2))
			if op == "release" {
				for i, l := range s.locks {
					if l.ID == id {
						s.locks = append(s.locks[:i], s.locks[i+1:]...)
						break
					}
				}
				return nil, nil
			}
			var current *capabilityLock
			for _, l := range s.locks {
				if l.ID == id {
					current = l
					break
				}
			}
			if current == nil {
				return r.val(false), nil
			}
			for _, l := range s.locks {
				if l == current {
					break
				}
				if l.Name == current.Name && (l.Mode == "exclusive" || current.Mode == "exclusive") {
					return r.val(false), nil
				}
			}
			current.Held = true
			return r.val(true), nil
		case "permission":
			if r.origin == "null" {
				name := strarg(a, 1)
				if name == "storage-access" || name == "local-network" || name == "loopback-network" {
					return r.val("prompt"), nil
				}
				return r.val("denied"), nil
			}
			value := s.permissions[strarg(a, 1)]
			if value == "" {
				value = s.permissionFallback
			}
			if value == "" {
				value = "prompt"
			}
			return r.val(value), nil
		case "permissionSet":
			p.ctx.setPermissionLocked(r.origin, strarg(a, 1), strarg(a, 2))
		case "clipboard":
			return r.val(s.clipboard), nil
		case "clipboardWrite":
			s.clipboard = strarg(a, 1)
		case "preventSilent":
			s.preventSilent = true
		case "login":
			s.login = strarg(a, 1)
		}
		return nil, nil
	})
}

// DispatchInput is the trusted browser input path. Script-created dispatchEvent
// and HTMLElement.click never call it and cannot grant user activation.
// Callers supply a DOM target selected by the browser's input routing layer.
func (p *Page) DispatchInput(ctx context.Context, nodeID int64, eventType string) error {
	r := p.Top.Realm
	if eventType != "mousedown" && eventType != "keydown" && eventType != "touchend" {
		return fmt.Errorf("unsupported input event %q", eventType)
	}
	if r.inputDispatcher == nil {
		return fmt.Errorf("input dispatcher unavailable")
	}
	r.scheduler.Post(scheduler.UserInteraction, 0, func(ctx context.Context) error {
		r.activationAt = r.scheduler.Now()
		r.activationConsumed = false
		_, err := r.runtime.Call(ctx, r.inputDispatcher, nil, r.val(nodeID), r.val(eventType))
		return err
	})
	return r.scheduler.RunReady(ctx, 1000)
}
