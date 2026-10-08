package browser

import (
	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/profile"
	"github.com/moreveal/mimic/internal/state"
)

// NewProfileContext validates every input before publishing a Context. Tokens
// are stateless and contain no proxy credentials or mutable browsing state.
func (b *Browser) NewProfileContext(token string, proxyJSON []byte, policy *network.ResourcePolicy) (*Context, error) {
	d, _, err := profile.ResolveToken(token, b.env)
	if err != nil {
		return nil, err
	}
	return b.NewResolvedProfileContext(d, proxyJSON, policy)
}

// NewResolvedProfileContext consumes a validated generated/imported document.
// CDP already resolved it to return the portable token, so avoid resolving the
// same seed or manual export a second time before context construction.
func (b *Browser) NewResolvedProfileContext(d profile.Document, proxyJSON []byte, policy *network.ResourcePolicy) (*Context, error) {
	if len(proxyJSON) != 0 {
		proxy, err := profile.ParseProxy(proxyJSON)
		if err != nil {
			return nil, err
		}
		d.Network.Proxy = proxy
	}
	if err := b.ValidateResolvedProfile(d); err != nil {
		return nil, err
	}
	if d.Network.Proxy.Server != "" && b.compat.Environment().NewProxyTransport == nil {
		return nil, &profile.Error{Path: "proxy", Reason: "unsupported", Message: "bundle has no proxy transport"}
	}
	if policy != nil {
		if err := policy.Validate(); err != nil {
			return nil, err
		}
	}
	c := b.newConfiguredContext(&d, policy, true)
	if err := c.lifetime.Err(); err != nil {
		return nil, err
	}
	return c, nil
}

// CheckProfileMutation is shared by direct Go and CDP command entry points.
// Managed profiles are immutable; ordinary Target contexts retain CDP emulation.
func (p *Page) CheckProfileMutation() error {
	if p.ctx.profileLocked {
		return &profile.Error{Path: "profile", Reason: "profileLocked", Message: "create a new context to change an imported or generated profile"}
	}
	return nil
}

func (b *Browser) ValidateProfile(raw []byte) (profile.Document, error) {
	d, err := profile.Normalize(raw, b.env, nil)
	if err != nil {
		return d, err
	}
	return d, b.validateProfileBackend(d)
}
func (b *Browser) ValidateResolvedProfile(d profile.Document) error {
	if err := d.Validate(b.env); err != nil {
		return err
	}
	return b.validateProfileBackend(d)
}
func (b *Browser) validateProfileBackend(d profile.Document) error {
	capability, ok := b.factory.(engine.NativeIntlFactory)
	if !ok || !capability.NativeIntl() {
		for field, changed := range map[string]bool{"timezone": d.Locale.Timezone != b.env.Locale.Timezone, "intlLocale": d.Locale.IntlLocale != b.env.Locale.IntlLocale} {
			if changed {
				return &profile.Error{Path: "locale." + field, Reason: "unsupported", Message: "custom locale profiles require a native Intl engine"}
			}
		}
	}
	return nil
}
func (b *Browser) NewContextWithProfile(raw []byte) (*Context, error) {
	d, err := b.ValidateProfile(raw)
	if err != nil {
		return nil, err
	}
	if d.Network.Proxy.Server != "" && b.compat.Environment().NewProxyTransport == nil {
		return nil, &profile.Error{Path: "network.proxy", Reason: "unsupported", Message: "bundle has no proxy transport"}
	}
	return b.newContext(&d), nil
}
func (c *Context) Environment() state.Environment {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.env.Clone()
}
func (p *Page) BaseEnvironment() state.Environment { return p.ctx.Environment() }

// Internal projections only read this snapshot. Mutable subobjects are replaced,
// never edited in place; public consumers use Environment's defensive copy.
func (p *Page) environmentView() state.Environment { p.mu.RLock(); defer p.mu.RUnlock(); return p.env }
func (c *Context) Profile() profile.Document {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return profile.FromEnvironment(c.env, c.browser.env.ProfileID, c.proxy)
}

// ConfigureProfile installs a managed identity into an empty ordinary Context.
// Framework adapters can close their owned about:blank identity probe and call
// this before exposing the Context to user code. No live or initializing Page
// can observe a partially replaced environment. A managed Context stays immutable.
func (c *Context) ConfigureProfile(d profile.Document, proxyJSON []byte, policy *network.ResourcePolicy, mediaJSON []byte) (*MediaProfile, error) {
	if len(proxyJSON) != 0 {
		proxy, err := profile.ParseProxy(proxyJSON)
		if err != nil {
			return nil, err
		}
		d.Network.Proxy = proxy
	}
	if err := c.browser.ValidateResolvedProfile(d); err != nil {
		return nil, err
	}
	if d.Network.Proxy.Server != "" && c.browser.compat.Environment().NewProxyTransport == nil {
		return nil, &profile.Error{Path: "proxy", Reason: "unsupported", Message: "bundle has no proxy transport"}
	}
	if policy != nil {
		if err := policy.Validate(); err != nil {
			return nil, err
		}
	}
	var media *MediaProfile
	if len(mediaJSON) != 0 {
		validated, err := c.ValidateMediaProfileJSON(mediaJSON)
		if err != nil {
			return nil, err
		}
		media = &validated
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.lifetime.Err(); err != nil {
		return nil, err
	}
	if c.profileLocked {
		return nil, &profile.Error{Path: "browserContextId", Reason: "profileLocked", Message: "managed Context profiles are immutable"}
	}
	if len(c.pages) != 0 || c.creatingPages != 0 || c.closingPages != 0 || c.activeKeepalives.Load() != 0 || c.mediaCaptures != 0 {
		return nil, &profile.Error{Path: "browserContextId", Reason: "contextInUse", Message: "configure requires an empty Context with no live Page, upload or capture workers"}
	}
	// Policy validation above and serialization here happen before any identity
	// mutation. Existing loaders cannot exist because the Context has no Pages.
	if policy != nil {
		if _, err := c.resourcePolicy.Update(*policy); err != nil {
			return nil, err
		}
	}
	if closer, ok := c.transport.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
	c.transport = nil
	c.env = d.ApplyOwned(c.browser.env)
	c.proxy = d.Network.Proxy
	c.profileLocked = true
	c.mediaSeed = c.env.ProfileID
	if media != nil {
		c.mediaProfile, c.mediaSeed = media, media.Seed
	}
	if media != nil {
		snapshot := cloneMediaProfile(*media)
		return &snapshot, nil
	}
	return nil, nil
}
func (p *Page) Profile() profile.Document {
	return profile.FromEnvironment(p.Environment(), p.ctx.browser.env.ProfileID, p.ctx.proxy)
}

// Caller owns the Page command boundary, shared with all CDP sessions.
func (p *Page) UpdateProfile(raw []byte) (profile.Document, error) {
	current := p.Profile()
	if err := p.CheckProfileMutation(); err != nil {
		return current, err
	}
	d, err := profile.Normalize(raw, p.ctx.browser.env, &current)
	if err != nil {
		return current, err
	}
	p.applyProfileEnvironment(d.ApplyOwned(p.ctx.env))
	return p.Profile(), nil
}
func (p *Page) ResetProfileOverrides() profile.Document {
	if p.CheckProfileMutation() != nil {
		return p.Profile()
	}
	p.applyProfileEnvironment(p.ctx.env.Clone())
	return p.Profile()
}
func (p *Page) applyProfileEnvironment(e state.Environment) {
	p.viewportObservationChange(true)
	p.mu.Lock()
	p.env = e
	p.mu.Unlock()
	p.viewportObservationChange(false)
}
