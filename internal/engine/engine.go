package engine

import (
	"context"
	"time"
)

// Value is deliberately engine-neutral. Browser semantics must not depend on
// the representation used by the selected ECMAScript implementation.
type Value interface {
	Export() any
	String() string
}

// ValueReleaser releases an embedder-owned root after its last use. It never
// destroys the JavaScript object while that object is reachable from script.
// Values retained by another Go owner must not be released through this API.
type ValueReleaser interface {
	ReleaseValue(Value)
}

// ValueRetainer creates an independent native root for a value borrowed from a
// synchronous host callback. The new owner must release it after its last use.
// Releasing it never invalidates an independently retained handle or JS reference.
type ValueRetainer interface {
	RetainValue(Value) Value
}

// HostValueReturner consumes one owned root and lends its value until the
// current synchronous host callback returns it to JavaScript. The returned
// value must not be retained by Go or used after leaving that callback.
type HostValueReturner interface {
	ReturnValueAndRelease(Value) Value
}

// ThrownValue preserves the identity and type of a JavaScript exception.
// The caller may release this root after handling the exception.
type ThrownValue interface {
	ThrownValue() Value
}

// ScriptOriginRuntime compiles parser scripts at their document source
// coordinates. Ordinary eval and dynamically inserted scripts use zero offsets.
type ScriptOriginRuntime interface {
	EvalWithOrigin(context.Context, string, string, int32, int32) (Value, error)
}

type Function func(this Value, args []Value) (Value, error)

type Promise struct {
	Value   Value
	Resolve func(any) error
	Reject  func(any) error
}

// HostPromiseRuntime lends Value to the current synchronous host callback,
// which must return it to JavaScript before leaving that callback. The native
// resolvers own pending settlement; they release their roots after the first
// Resolve/Reject. Code retaining Value in Go must use Runtime.NewPromise.
type HostPromiseRuntime interface {
	NewHostPromise() Promise
}

type Runtime interface {
	Eval(context.Context, string, string) (Value, error)
	Set(string, any) error
	Get(string) Value
	Value(any) Value
	GetProperty(Value, string) Value
	SetProperty(Value, string, any) error
	TypeOf(Value) string
	StrictEqual(Value, Value) bool
	Call(context.Context, Value, Value, ...Value) (Value, error)
	Function(Function) any
	NewPromise() Promise
	Await(Value) (Value, bool, error)
	SetTimeSource(func() time.Time)
	MicrotaskCheckpoint() error
	SetGlobalAccessObserver(func(name string, supported bool))
	Close() error
}

// ReentrantRuntime permits a synchronous call into another actor-owned realm
// while servicing calls back into this realm on its original actor thread.
// The operation must use the supplied context and return before RunNested
// returns; it must not dispose the calling runtime from inside the operation.
type ReentrantRuntime interface {
	RunNested(context.Context, func(context.Context) error) error
}

// OwnerRuntime groups synchronous engine operations on their owning thread.
// It does not pump tasks or perform a microtask checkpoint. The operation must
// not close the runtime or retain thread-local state after returning.
type OwnerRuntime interface {
	RunOnOwner(context.Context, func(context.Context) error) error
}

// RealmCallRuntime admits a synchronous operation on another realm, directly
// when it shares the execution owner and through nested dispatch otherwise.
// Admission preserves the active calling operation's cancellation context.
type RealmCallRuntime interface {
	RunRealmCall(context.Context, Runtime, func(context.Context) error) error
}

// StringCallRuntime executes a private serialization function with native
// Values or primitive arguments. The result must already be a string; no
// author coercion or checkpoint is performed. Temporary arguments/results do
// not become persistent runtime roots.
type StringCallRuntime interface {
	CallString(context.Context, Value, ...any) (string, error)
}

// BootstrapRuntime may reuse a compiled embedder function body across runtimes.
// Only immutable code is shared; execution and all objects remain realm-local.
// The body must install state explicitly on globalThis (no global var bindings).
// Page scripts must use Eval, even if they choose an internal-looking filename.
type BootstrapRuntime interface {
	EvalBootstrap(context.Context, string, string) (Value, error)
}

// RealmRuntimeFactory creates an independent realm on the same execution owner.
// A Page uses this for synchronously connected frame realms; unrelated Pages
// continue to obtain independent owners from the engine Factory.
type RealmRuntimeFactory interface {
	NewRealmRuntime(useBootstrap bool) (Runtime, bool, error)
}

type ConnectedRealmFactory interface{ ConnectedRealms() bool }

// RealmSecurityRuntime projects the browser's origin admission into native
// context access and stack capture. The key is private browser-owned state.
type RealmSecurityRuntime interface{ SetSecurityOrigin(string) error }

// ModuleLoader resolves one static module request. referrer is the canonical
// resource name supplied for the importing module; resourceName becomes the
// identity and base URL of the returned source.
type ModuleLoader func(specifier, referrer string) (source, resourceName string, err error)

// ModuleRuntime is implemented by engines which can compile, link, and
// evaluate ECMAScript SourceTextModule graphs.
type ModuleRuntime interface {
	EvalModule(context.Context, string, string, ModuleLoader) (Value, error)
}

// PreparedModuleRuntime separates native parsing from linking, allowing browser
// network waits between Page tasks. Completion must be invoked on that Page's
// event loop, after the native dynamic-import callback has returned.
// Its error is borrowed: a handler may retain a cached graph's ThrownValue and
// pass it to multiple completions. Completion must not release that owner.
type DynamicModuleCompletion func(context.Context, string, string, ModuleLoader, error) error
type DynamicModuleHandler func(string, string, DynamicModuleCompletion)
type PreparedModuleRuntime interface {
	PrepareModule(context.Context, string, string) ([]string, error)
	SetDynamicModuleHandler(DynamicModuleHandler)
}

// ImportMetaRuntime lets the browser supply its URL resolution semantics without
// fetching a module. The factory receives the immutable module resource name and
// returns a realm-owned resolve function; changing import.meta.url cannot rebase it.
type ImportMetaRuntime interface {
	SetImportMetaResolveFactory(Value) error
}

// EvalSourceRuntime lets the browser recognize branded code objects without
// replacing native eval (which would destroy direct eval's lexical scope).
// The realm-owned resolver returns a source string for a recognized object,
// or undefined to preserve native eval's non-string identity behavior.
type EvalSourceRuntime interface {
	SetEvalSourceResolver(Value) error
}

// DebuggerEvalRuntime scopes the inspector's CSP unsafe-eval exception to one
// synchronous call. It must not leak into queued tasks, other realms or Pages.
type DebuggerEvalRuntime interface {
	RunWithUnsafeEval(context.Context, func(context.Context) error) error
}

type Factory interface{ New() Runtime }

// NativeIntlFactory certifies an engine's real ECMA-402 implementation, required
// for custom locale profiles. Shape-only Intl fallbacks are not sufficient.
type NativeIntlFactory interface{ NativeIntl() bool }

// UndetectableRuntime creates a native callable object with HTMLDDA operator
// semantics. Property operations are delegated to realm-owned JS handlers;
// an ordinary JS Proxy cannot preserve the native undetectable flag.
type UndetectableRuntime interface {
	NewUndetectableObject(handlers Value) (Value, error)
}

// InterceptedObjectRuntime supplies detectable, noncallable exotic objects.
// Unlike JS Proxy targets, their descriptors can change when a Window navigates.
type InterceptedObjectRuntime interface {
	NewInterceptedObject(handlers Value) (Value, error)
}

// PropertyObservationRuntime supplies a realm-owned native factory whose
// callback completes before normal property resolution continues. It observes
// without wrapping object identity or adding frames to the resolved operation.
// Factory and callback data must survive bootstrap snapshots without Go roots.
type PropertyObservationRuntime interface {
	PropertyObservationFactory() Value
}

// SharedRevisionRuntime exposes an internal atomic revision word to JavaScript.
// The counter is native memory, not a retained Go pointer. Its publisher must
// be unsubscribed from canonical state before Close releases its native owner.
type SharedRevisionRuntime interface {
	NewSharedRevision() (Value, SharedRevision, error)
}

type SharedRevision interface {
	Publish(uint64)
	Close() error
}

// GeneratedLazyRuntime supplies one snapshot-portable native installer per
// realm. Generated WebAPI member IDs are immutable catalog data; the callable
// values and any later mutations remain local to the current V8 context.
type GeneratedLazyRuntime interface {
	GeneratedLazyInstaller() Value
}

// GeneratedConstructorRuntime provides snapshot-portable native interface
// constructors while their dispatch state stays in the current realm.
type GeneratedConstructorRuntime interface {
	GeneratedConstructorFactory() Value
}

// ReceiverDispatchRuntime supplies a realm-owned native factory taking a
// native receiver validator, original intrinsic, semantic implementation, name
// and length. It preserves raw receivers and lets the original intrinsic
// produce brand failures without running a JavaScript adapter on that path.
// Factory and callback data must preserve ownership through bootstrap snapshots.
// ExceptionStateRuntime supplies a native initializer for platform exception
// state. It does not format stacks or replace author-visible error fields.
type ExceptionStateRuntime interface {
	ExceptionStateFactory() Value
}

type ReceiverDispatchRuntime interface {
	ReceiverDispatchFactory() Value
}

// ArrayBufferDetacher supplies real backing-store detachment where the engine
// does not expose ArrayBuffer.prototype.transfer. Call only on the realm actor.
type ArrayBufferDetacher interface {
	DetachArrayBuffer(Value) error
}

// NativeFunctionSourceRuntime installs an engine-owned Function#toString. The
// resolver only describes explicitly registered browser functions; the engine
// remains authoritative for arbitrary author functions and callable proxies.
type NativeFunctionSourceRuntime interface {
	InstallNativeFunctionToString(resolver Value, original Value) error
}

// PromiseJobLifecycleRuntime retires runnable work while preserving a realm's
// retained language objects. Frame removal may deliberately keep promise jobs;
// document replacement shuts them down.
type PromiseJobLifecycleRuntime interface {
	DeactivatePromiseJobs() error
}
