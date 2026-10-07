package browser

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"sync"

	"github.com/google/uuid"
	"github.com/moreveal/mimic/compatibility"
	"github.com/moreveal/mimic/internal/camera"
	"github.com/moreveal/mimic/internal/engine"
	"github.com/moreveal/mimic/internal/microphone"
	"github.com/moreveal/mimic/internal/network"
	"github.com/moreveal/mimic/internal/profile"
	"github.com/moreveal/mimic/internal/speech"
	"github.com/moreveal/mimic/internal/state"
	"github.com/moreveal/mimic/internal/textmetrics"
	"github.com/moreveal/mimic/internal/workload"
)

type Browser struct {
	executionProfile       *workload.Profile
	profileNote            func(string)
	transportWrapper       func(network.Transport) network.Transport
	classicScriptAdmission func(url, source string, external bool) bool
	classicScriptSkipped   func(url, source string, external bool)
	defaultResourcePolicy  *network.ResourcePolicy
	devPreview             bool
	speechProvider         speech.Provider
	cameraProvider         camera.Provider
	microphoneProvider     microphone.Provider
	mu                     sync.RWMutex
	lifetime               context.Context
	cancel                 context.CancelFunc
	closed                 bool
	closeDone              chan struct{}
	closeErr               error
	factory                engine.Factory
	env                    state.Environment
	compat                 compatibility.Bundle
	contexts               map[string]*Context
	bootstrapSnapshots     bootstrapSnapshotCache
	profileBootstrapMu     sync.Mutex
	profileBootstraps      map[[32]byte]*profileBootstrapAttempt
}

type Options struct {
	ExecutionProfile *workload.Profile
	ProfileNote      func(string)
	// TransportWrapper and ClassicScriptAdmission are private experiment hooks.
	// A wrapper is created once per Context, before any Page can issue requests.
	TransportWrapper       func(network.Transport) network.Transport
	ClassicScriptAdmission func(url, source string, external bool) bool
	ClassicScriptSkipped   func(url, source string, external bool)
	// ResourcePolicy supplies an opt-in template for newly created Contexts.
	ResourcePolicyJSON []byte
	// DevPreview enables private debug observations and the CDP preview routes.
	DevPreview bool
	// SpeechProvider is an optional portable synthesis driver. Nil selects the
	// system provider. It creates document-owned resources only on first use.
	SpeechProvider speech.Provider
	// CameraProvider selects document-owned portable capture. Nil uses the bundled system driver.
	CameraProvider camera.Provider
	// MicrophoneProvider selects lazy document-owned PCM capture.
	MicrophoneProvider microphone.Provider
}

func New(factory engine.Factory, bundle compatibility.Bundle) (*Browser, error) {
	return NewWithOptions(factory, bundle, Options{})
}
func NewWithOptions(factory engine.Factory, bundle compatibility.Bundle, options Options) (*Browser, error) {
	if bundle == nil || bundle.Environment() == nil {
		return nil, fmt.Errorf("compatibility bundle is required")
	}
	env := bundle.Environment().State
	if err := env.Validate(); err != nil {
		return nil, err
	}
	provider := options.SpeechProvider
	if provider == nil {
		provider = speech.Open
	}
	// Installed font metadata is browser-wide immutable input. Resolve it once
	// during browser construction, never in a synchronous DOM/layout read.
	textmetrics.WarmSystemCatalog()
	lifetime, cancel := context.WithCancel(context.Background())
	b := &Browser{devPreview: options.DevPreview, speechProvider: provider, factory: factory, env: env.Clone(), compat: bundle, contexts: map[string]*Context{}, lifetime: lifetime, cancel: cancel}
	b.cameraProvider = options.CameraProvider
	if b.cameraProvider == nil {
		b.cameraProvider = camera.System()
	}
	b.microphoneProvider = options.MicrophoneProvider
	if b.microphoneProvider == nil {
		b.microphoneProvider = microphone.System()
	}
	b.transportWrapper = options.TransportWrapper
	if options.ExecutionProfile != nil {
		if err := options.ExecutionProfile.Validate(); err != nil {
			cancel()
			return nil, err
		}
		owned := options.ExecutionProfile.Clone()
		b.executionProfile = &owned
	}
	b.profileNote = options.ProfileNote
	b.classicScriptAdmission = options.ClassicScriptAdmission
	b.classicScriptSkipped = options.ClassicScriptSkipped
	if len(options.ResourcePolicyJSON) != 0 {
		policy, err := network.ParseResourcePolicy(options.ResourcePolicyJSON)
		if err != nil {
			return nil, fmt.Errorf("resource policy: %w", err)
		}
		b.defaultResourcePolicy = &policy
	}
	return b, nil
}
func (b *Browser) NewContext() *Context { return b.newContext(nil) }

func (b *Browser) newContext(d *profile.Document) *Context {
	return b.newConfiguredContext(d, nil, false)
}

func (b *Browser) newConfiguredContext(d *profile.Document, policy *network.ResourcePolicy, locked bool) *Context {
	b.mu.Lock()
	defer b.mu.Unlock()
	lifetime, cancel := context.WithCancel(context.Background())
	c := &Context{lifetime: lifetime, cancel: cancel, ID: uuid.NewString(), browser: b, cookies: network.NewCookieStore(), network: network.NewSessionState(), storage: map[string]map[string]string{}, pages: map[string]*Page{}}
	c.resourcePolicy = &network.ResourcePolicyState{}
	c.profileLocked = locked
	if b.defaultResourcePolicy != nil {
		_, _ = c.resourcePolicy.Update(*b.defaultResourcePolicy)
	}
	if policy != nil {
		_, _ = c.resourcePolicy.Update(*policy)
	}
	if b.executionProfile != nil && len(b.executionProfile.Plan.Resources) > 0 {
		_ = c.resourcePolicy.InstallSpecialization(b.executionProfile.Plan.Resources)
	}
	if b.closed {
		cancel()
		return c
	}
	if d != nil {
		c.env = d.ApplyOwned(b.env)
		c.mediaSeed = c.env.ProfileID
		c.proxy = d.Network.Proxy
	} else {
		c.env = b.env.Clone()
	}
	b.contexts[c.ID] = c
	return c
}
func (b *Browser) Environment() state.Environment {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.env.Clone()
}
func (b *Browser) Compatibility() compatibility.Bundle { return b.compat }

type Context struct {
	bootstrapPreparation bool // Private seed capture, never a user Context mode.
	profileLocked        bool
	resourcePolicy       *network.ResourcePolicyState
	env                  state.Environment
	proxy                profile.Proxy
	files                map[string]*opfsStore
	indexedDatabases     map[string]map[string]*indexedDatabase
	indexedSequence      uint64
	cacheNames           map[string]map[string]*cacheBucket
	cacheSequence        uint64
	storageMu            sync.Mutex
	permissionRealms     map[*Realm]struct{}
	lifetime             context.Context
	keepaliveMu          sync.Mutex
	keepaliveWG          sync.WaitGroup
	cancel               context.CancelFunc
	mu                   sync.RWMutex
	ID                   string
	browser              *Browser
	cookies              *network.CookieStore
	network              *network.SessionState
	transport            network.Transport
	storage              map[string]map[string]string
	capabilities         map[string]*originCapabilities
	pages                map[string]*Page

	permissionDefaults        map[string]string // Context-wide CDP overrides; protected by mu.
	permissionDefaultFallback string
	mediaSeed                 string
	mediaProfile              *MediaProfile
	mediaCaptures             int
	mediaDiagnostics          map[string]string
}

func (c *Context) NewPage() (*Page, error) {
	c.mu.Lock()
	if err := c.lifetime.Err(); err != nil {
		c.mu.Unlock()
		return nil, err
	}
	if c.transport == nil {
		profile := c.browser.Compatibility().Environment()
		if profile.NewTransport != nil {
			var transport network.Transport
			var err error
			if c.proxy.Server != "" {
				if profile.NewProxyTransport == nil {
					err = fmt.Errorf("proxy transport is unsupported")
				} else {
					transport, err = profile.NewProxyTransport(c.proxy.URL())
				}
			} else {
				transport, err = profile.NewTransport()
			}
			if err != nil {
				c.mu.Unlock()
				return nil, fmt.Errorf("create %s network transport: %w", c.browser.Environment().Network.WireProfile, err)
			}
			if c.browser.transportWrapper != nil {
				transport = c.browser.transportWrapper(transport)
			}
			c.transport = transport
		}
		if c.transport == nil && c.browser.transportWrapper != nil {
			c.mu.Unlock()
			return nil, fmt.Errorf("workload experiment requires an explicit Context transport")
		}
	}
	p, err := newPage(c)
	c.mu.Unlock()
	if err != nil {
		return nil, err
	}
	if err := p.initBlank(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	if err := c.lifetime.Err(); err != nil {
		c.mu.Unlock()
		_ = p.Close()
		return nil, err
	}
	c.pages[p.ID] = p
	c.mu.Unlock()
	return p, nil
}
func (c *Context) Pages() []*Page {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]*Page, 0, len(c.pages))
	for _, p := range c.pages {
		out = append(out, p)
	}
	return out
}
func (c *Context) Page(id string) (*Page, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	p, ok := c.pages[id]
	return p, ok
}
func (c *Context) ClosePage(id string) bool {
	c.mu.Lock()
	p, ok := c.pages[id]
	if ok {
		delete(c.pages, id)
	}
	c.mu.Unlock()
	if ok {
		_ = p.Close()
	}
	return ok
}
func (c *Context) Cookies() *network.CookieStore { return c.cookies }
func (c *Context) store(origin string) map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.storage[origin] == nil {
		c.storage[origin] = map[string]string{}
	}
	return c.storage[origin]
}

// Auxiliary top windows share their opener's agent/event loop, but are not
// DOM descendants and have their own document, WindowProxy and viewport.
type Frame struct {
	auxiliaryOwner     *Realm
	auxiliaryBase      *url.URL
	auxiliaryOpener    *Frame
	auxiliaryWidth     int
	auxiliaryHeight    int
	windowClosing      bool
	windowName         string
	ID                 string
	page               *Page
	parent             *Frame
	elementID          int64
	children           map[string]*Frame
	Realm              *Realm
	navigationStarted  bool
	navigationPending  bool
	navigationCancel   context.CancelFunc
	navigationSequence uint64
	loaderID           string
	pendingMessages    []frameMessage
	loadBlockers       map[uint64]string
}

// ExecutionAgent is the browser-observable owner of a realm and event loop.
// Frame is the first implementation; dedicated/shared workers can implement
// the same boundary without pretending to be frames or sharing globals.
type ExecutionAgent interface {
	Page() *Page
	ContextID() string
	AgentType() string
}

func (f *Frame) Page() *Page       { return f.page }
func (f *Frame) ContextID() string { return f.ID }
func (f *Frame) AgentType() string { return "frame" }
func (f *Frame) Parent() *Frame    { return f.parent }
func (f *Frame) URL() string {
	if f == nil || f.Realm == nil || f.Realm.documentURL() == nil {
		return ""
	}
	return f.Realm.documentURL().String()
}
func (f *Frame) LoaderID() string {
	if f == nil {
		return ""
	}
	return f.loaderID
}
func (f *Frame) RealmID() string {
	if f == nil || f.Realm == nil {
		return ""
	}
	return f.Realm.ID
}
func (f *Frame) ReadyState() string {
	if f == nil || f.Realm == nil {
		return ""
	}
	return f.Realm.readyState
}
func (f *Frame) Children() []*Frame {
	if f == nil {
		return nil
	}
	children := make([]*Frame, 0, len(f.children))
	for _, child := range f.children {
		children = append(children, child)
	}
	sort.Slice(children, func(i, j int) bool { return children[i].elementID < children[j].elementID })
	return children
}
func (f *Frame) Top() *Frame {
	for f.parent != nil {
		f = f.parent
	}
	return f
}

type WindowProxy struct{ frame *Frame }

func (w *WindowProxy) Realm() *Realm { return w.frame.Realm }

func (b *Browser) String() string { return fmt.Sprintf("Mimic/%s", b.env.Product.FullVersion) }

// Close releases all pages before their shared transport pool.
func (c *Context) Close() error {
	c.Cancel()
	c.keepaliveWG.Wait()
	for _, p := range c.Pages() {
		c.ClosePage(p.ID)
	}
	c.network.Close()
	c.storageMu.Lock()
	c.files = nil
	c.storageMu.Unlock()
	c.mu.Lock()
	if closer, ok := c.transport.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
	c.mu.Unlock()
	c.browser.mu.Lock()
	delete(c.browser.contexts, c.ID)
	c.browser.mu.Unlock()
	return nil
}

// Cancel stops external work without touching realm-owned JavaScript state.
// Shutdown can call it before joining active browser command runners.
func (c *Context) Cancel() {
	c.keepaliveMu.Lock()
	c.cancel()
	c.keepaliveMu.Unlock()
}
