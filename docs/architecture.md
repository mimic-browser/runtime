# Architecture

The dependency direction is intentionally one-way:

```text
CDP / future APIs -> browser commands -> canonical state machine
                                      -> scheduler
                                      -> resource loader -> interceptors -> transport
                                      -> Web API semantics -> engine interface
```

Concrete Chrome versions live under `chrome/<milestone>`. The `chrome`
registry returns a version-neutral `compatibility.Bundle` containing the exact
environment profile, generated Web API surface, generated CDP schema and
expectations. Browser realms and the CDP adapter consume that selected bundle;
the runtime core never imports `chrome/152` (or any future version) directly.

`internal/engine` is the only contract visible to browser code. The V8, QuickJS and goja
adapters live below it. Production browser code does not import a concrete engine.

## State

`Environment` stores canonical facts only: product, platform, hardware,
display, graphics, locale, clock, network and permissions. JavaScript-visible
values and request headers are derived projections. Construction validates
cross-field invariants; mutation occurs through commands on `Page`.
Presentation mode and the Environment profile ID are first-class state. A complete
headful or headless profile is selected before realm construction; runtime code
must not spoof individual properties to conceal a headless identity. See the
[oracle policy](oracle-policy.md).

Environment permissions are initial defaults. Live permission decisions and
changes, clipboard text, login state, bucket metadata and lock queues belong to
the Context's origin capability store. PermissionStatus notifications enter each
receiving realm through Scheduler tasks. User activation is realm state granted
by the trusted browser input path and expires against the Scheduler clock;
synthetic DOM events do not grant it. Machine media, keyboard and device
capabilities remain Environment projections. See the
[capability matrix and explicit limits](navigator-capabilities.md).

Each page owns a top frame and one V8 execution owner. Each frame owns an
independent realm adapter, native context, intrinsic objects and global object.
Synchronous calls between connected frame realms stay on their Page's owner,
preserving native author stack continuity. Context security tokens project the
browser's origin admission into native access and stack capture; opaque origins
retain creator identity. Independent Pages use independent owners by default.
WindowProxy is represented as the
stable frame-facing identity which delegates to the current realm.

## Scheduling

Go concurrency may perform implementation work, but never invokes JavaScript.
Connected V8 realms use a shared execution owner with explicit per-realm
teardown; closing a parent adapter does not dispose a live child context. The
last connected realm releases the original owner. Navigation retirement shuts
down the old native context's microtask admission at an outer execution
boundary, while preserving retained objects and the active contexts' common
queue. Frame removal deliberately keeps retained language promise jobs.
Bootstrap artifacts retain a bare default context and an additional platform seed context. A matching frame
restores that seed on its Page's owner; a different exposure graph starts from
the bare context. Preparation of a previously unseen graph is synchronous
before its first ordinary Page executes author code, so future matching frames
can restore without moving the Page's existing author objects to another isolate.
The first ordinary Page still executes the cold surface in a bare context on
that prepared owner; later cache admissions restore the seed. Child realms
participate in capture and admission even when their owner already exists.
Isolate-wide module callbacks dispatch by the actual native context, preserving
per-realm loaders, module identity and import.meta state.

All callbacks enter through Scheduler tasks. A microtask checkpoint is made
after every task. Timers use a monotonic virtual clock and deterministic
sequence numbers.

## Networking

Beacon and Window Fetch keepalive share the initiating document's 64 KiB
outstanding upload budget. Accepted requests capture their URL, security policy,
credentials, body and timing owner before entering the shared loader. They may
outlive navigation and Page closure; their Context owns cancellation and joins
pending transport work before closing its network pool. Transport completion
does not enter a retired JavaScript realm. A successful Beacon return value
means queue acceptance, not server delivery.

Applied CSS background URLs are discovered from the canonical computed
declarations at document task checkpoints. Their requests use the existing
document network and load-blocker boundary; no drawing backend or separate
cascade model is involved. Unmatched selectors and `display:none` descendants
do not cause speculative background downloads.

Navigation, fetch, XHR, external scripts and dynamically inserted scripts call
the same ResourceLoader. Interceptors receive immutable request/response views
and return explicit decisions. Cookie/header derivation is performed around
that common path.

## Web APIs

`internal/webapi/surface.js` contains handwritten semantic bindings. It is
composed per realm with the selected bundle's generated Blink WebIDL surface.
Generated bindings describe known shape and route operations to semantic hosts;
they never own independent browser state.

DOMException name, message and legacy code share private state in Window and
Worker bindings. Its current native Error backing preserves Error.isError but
still exposes a different Object.prototype.toString tag after prototype removal.
See the measured [exception branding boundary](compatibility/domexception-state-2026-09-11.md).
Host-record conversion creates own data properties; inherited setters must not
intercept fields transported into V8. This does not establish complete structured
clone semantics or preserve ordering lost in a Go map.

### Graphics observations without rendering

Mimic does not render a display and must not require a GPU or an embedded graphics
engine. Canvas and graphics support models script-observable state and queries,
rather than reconstructing an image for its own sake. Capability values remain
projections of the selected Environment, not properties of the host's GPU.

Readback is not an independent random or precomputed fingerprint. It must derive
from the same state as dimensions, context settings, metrics and operations.
Identical operations in identical state must produce identical observations;
local changes must preserve unaffected regions, and overlapping reads, explicit
pixel writes, copies, resets and transfers must agree. For example, repeating
`AAAA` must agree with its previous result, while replacing its final character
with `B` must not perturb unrelated regions. Exact glyph pixels are not required
by that relational contract.

Approximate observations must be identified as approximations and checked for
these relationships. They must not be presented as measured Chrome pixel
equivalence. Unsupported behavior remains a documented boundary; implementation
must not inspect website identity or recognize fingerprinting scripts to choose
answers. The same observable model applies to ordinary application code.

## Ownership and execution invariants

- Browser environment is a validated construction template. Page environment is
  its command-owned snapshot; Navigator, Screen, viewport and graphics capability
  bindings derive from it. Returned profiles/environments are read-only to callers.
- Page current URL/history is authoritative for the top document. Realm documentURL
  delegates to it; child document URLs and Worker script URLs are realm-owned.
  Realm origin is the committed security origin, not an independently mutable URL.
- Context owns cookies, origin storage, network/session settings and the transport
  pool. Loader cache/body records and trace data are derived observations. Transport
  connection/TLS/session state is never replaced by a guessed browser identity.
- Documents in a Page share a node arena while retaining independent document
  roots. New frame documents join before wrappers or parser state retain IDs;
  adoption changes ownership without copying nodes or changing their IDs.
  Independent Pages never share this arena. JavaScript wrappers and cross-realm
  value registries preserve identity; they do not own a second DOM. WebIDL Node
  checks use private brands, including synthetic Attr objects, rather than the
  calling realm's `instanceof`. Removed frames can retain a realm for existing
  WindowProxy references until the owning realm closes. The detached-frame
  lookup is not a reachability root: unobserved removed browsing contexts and
  their descendant Page-tree entries are released after the current JS turn.
  Exported Window/object references still participate in Page-local tracing.
  Arena nodes remain
  retained until Page teardown; incremental collection is not implemented.
- Page clock establishes navigation epochs; scheduler time is authoritative during
  a turn, including microtasks. Worker schedulers have their own agent clock.
  Resource/Navigation Performance entries derive from loader/lifecycle records;
  user marks/measures are authored within their own realm.
- CDP sessions serialize commands and complete event-loop turns per Page; independent
  Pages run concurrently. Context registry locking excludes realm bootstrap. Protocol interception continuation
  remains out of the Page command mutex so a paused network request can be resumed. Library
  users must serialize Page/DOM command access too; a Page is not a concurrently
  callable JavaScript engine. Worker JS may run on its separate agent, never on
  the Window runtime. Cross-agent delivery always queues a receiving-agent task.
- Scheduler runMu serializes a task and its complete checkpoint. V8 uses explicit
  microtasks on its thread-affine owner. Do not restore V8 automatic checkpoints.
- Network goroutines perform external work and enqueue completion; fetch/XHR and
  resource work is cancelled/joined when the realm closes. Cancellation guards
  suppress late Worker delivery. Closing a Context closes pages then its pool.
- V8 cancellation watchers are joined before the next isolate operation; persistent
  values and modules are released on the isolate thread at Close. Values currently
  remain rooted until realm teardown; long-lived realm memory is technical debt.
- Window bootstrap snapshots are immutable, bounded artifacts owned by the Browser.
  V8 trusted platform bindings compile with non-user Script provenance before
  parsing. Nested implementation functions retain that provenance through code
  caches and snapshots, so platform implementation frames never enter author
  `Error.stack` or structured `Error.prepareStackTrace` call sites. Author
  callbacks remain user code, and their exception identity and frames survive
  platform calls. Resource names do not grant platform provenance. Ordinary
  author evaluation and compilation never use the trusted entry point.
  Repeated compatible profiles admit asynchronous construction; restoration
  creates an independent isolate and rebinds native callbacks and realm state before
  user script. Page teardown releases consumer copies; Browser teardown cancels and
  joins builders and releases the cache. Workers retain ordinary initialization.
  Snapshot failure falls back to ordinary bootstrap with diagnostics. See
  [bootstrap snapshot measurements](performance/bootstrap-snapshot-20260910.md)
  for admission, profiling bypass and the live-memory tradeoff. The cache key
  uses bootstrap exposure-shaping state, not the complete Context
  environment. Per-Context locale, device, graphics, audio and font observations
  come from native callbacks rebound to the consuming Page after restoration;
  distinct generated identities can therefore reuse one immutable JS artifact.
  Native snapshot identity also includes the SHA-256 of the loaded shim artifact.
  An unchanged upstream V8 version does not make different embedder callbacks or
  engine patches snapshot-compatible. The loader captures the artifact identity
  once and rejects replacement during loading; later override-path changes cannot
  relabel an already loaded engine.
  Managed profile Pages prepare the exact exposure graph on first use under
  Browser-owned singleflight; a burst of navigations waits for compilation.
  Ordinary Pages retain lazy preparation.
  Committed navigation documents create canonical DOM, policy and parser state
  before materializing JavaScript bindings. Parser-blocking script transport can
  start while the language runtime remains unobserved, in both top and child
  documents. Script execution or lifecycle/resource event dispatch initializes
  bindings on the owning Page turn; this does not postpone document commit or
  move JavaScript to a network worker. Profile preparation admits its captured
  seed directly rather than bootstrapping another temporary Page.
  The pinned native engine must keep the stock shared read-only heap sealed
  (`--no-extensible-ro-snapshot` before initialization). Custom snapshot objects
  remain in private serialized heaps. Independent custom read-only layouts can
  corrupt restoration; concurrent late read-only finalization can write to an
  already protected page. This requirement preserves concurrent builders and
  Pages without a runtime lock. See the
  [serialization regression](compatibility/snapshot-serialization-2026-09-11.md).
- Keep runtime behavior target-blind. Domain names, vendor tokens, challenge
  patterns and captured payload structures must never select runtime semantics.

Do not remove retained realms, cross-value registries, explicit checkpoints,
transport session pooling or timer source ordering as apparent duplication.
Any change to these invariants needs a local regression demonstrating the defect.

## Service ownership and portability

Web service semantics are platform neutral. Compatibility means observable
values, descriptors, errors, identity, state transitions, event ordering and
Promise/realm/lifecycle behavior; it does not require Chromium's internal
mechanisms. Prefer a synthetic Go model when it can reproduce those observations.
Each service has one authoritative state model; JavaScript objects project it
and perform Web IDL conversion rather than keep a second mutable service store.

Cache Storage and IndexedDB belong to the browser Context and origin. Cookie
Store observes the existing network cookie jar. Navigation uses the Frame's
existing history. Scheduler priorities and task context belong to the Page
scheduler. Crash annotations, speech utterances and service lifecycle belong to
their document realm. Document picture-in-picture creates an auxiliary browsing
context on the existing Page event loop. Launch delivery uses the Page's queue.
Snapshots contain bindings, never live provider resources or shared service state.

System data and effects cross narrow provider interfaces. Speech uses
`internal/speech.Provider` through `browser.Options.SpeechProvider`; browser core
owns utterance properties, queue identities, cancellation and event projection.
The Windows SAPI implementation is one replaceable provider, isolated behind build
tags. Its COM thread, handles and installed voice discovery never enter browser
core. Completion comes from provider events, not estimated-duration timers.
Other platforms can inject their own provider; without one, synthesis reports
`synthesis-unavailable` rather than claiming to produce speech. Platform-dependent
voice catalogs are discovered, never copied from the capture or current machine.
Provider callbacks enqueue work on the owning Page; teardown closes the provider
and suppresses late delivery. Regression coverage exercises the same browser
model with a platform-independent controlled provider.

`navigator.webdriver` is an explicit Mimic invariant: it always returns false,
including CDP/debug configurations. Debug transport configuration must never
change this value.

## Host portability

Windows and Linux amd64 use the same browser state, V8 adapter and native shim.
Only native library loading/calling conventions, kernel thread identity, CPU
counters, scheduling hints, font discovery and OS service providers vary by host.
The gov8 `internal/native` boundary uses Windows trampolines or Linux System V
calls; Linux normalizes the legacy floating-point exports and supports wide
pointer-word calls without changing the browser execution model. Both retain
one owning OS thread per V8 owner and concurrent independent Page event loops.
Native library initialization remains process-wide; Page execution is not.

The build host does not select a different Chrome environment profile. Captured
Windows Chrome observations and benchmark provenance remain labeled Windows.
Host font files and speech providers are explicit resources, not synthesized
platform compatibility. See [setup and host requirements](getting-started.md).

## Layout ownership

Mimic remains authoritative for CSS parsing, cascade, computed values, DOM and
intrinsic text/control measurements. Flex and Grid formatting contexts are
projected once per observable style/DOM epoch into a normalized numeric tree.
The native `internal/layouttaffy` library passes that tree through one C ABI call
to Rust/Taffy and returns a flat array of absolute layout boxes. There are no
per-node native calls and no CSS strings cross the C boundary.

Taffy owns final Block/Flex/Grid sizing and placement inside those snapshots,
including nested flex, basis/grow/shrink, min/max constraints, percentages,
gaps, alignment, fixed-count grid tracks and numeric grid placement. Geometry
APIs consume the returned boxes first; the previous Mimic layout remains a
fallback for formatting contexts Taffy does not model here, notably inline text,
tables, replaced/shadow-specific geometry, transforms and document flow outside
a Flex/Grid snapshot. Unsupported inline subtrees are measured by Mimic and
enter Taffy as leaves rather than being interpreted as block layout.

The first production implementation intentionally rebuilds a snapshot after an
epoch invalidation. All reads in the same epoch reuse its flat boxes. A
persistent cross-epoch Taffy tree is deferred until profiling demonstrates that
its extra ownership and invalidation complexity is justified.
