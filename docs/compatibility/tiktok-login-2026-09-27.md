# Browser compatibility findings from TikTok

Frozen Chrome 152 is the behavioral reference. The fixes apply to browser
mechanisms shared by all sites; no TikTok-specific branches, response
substitutions, fingerprint overrides or account data are included.

## Implemented behavior

- Correct Performance measure options and Window/Worker observations.
- Preserve dataset prototype behavior, HTML direction and link metadata without
  acquiring resources merely to inspect metadata.
- Model XHR constants, event handlers and upload lifecycle observations.
- Route XHR through the shared Fetch CORS checks, including author-header and
  method preflights, upload-listener preflights, credentialed origin checks and
  exposed response headers. Forbidden cookie response fields are removed before
  entering JavaScript state, while the canonical cookie jar and inspector
  evidence retain their normal ownership. Frozen Chrome 152 fixtures cover the
  same-origin, allowed, denied, preflight and credential-wildcard boundaries.
- Compare nodes from canonical DOM state and preserve UTF-16 character data
  through creation, mutation, textContent aggregation and browser projections.
  Parent textContent uses the same code-unit transport as CharacterData; the
  shared replace-all implementation retains normal ownership and notifications.
- Match trusted keyboard activation behavior measured in Chrome 152.
- Preserve object URL ownership across Page and Worker boundaries.
- Model MediaSource attachment, lifecycle, buffered/played/seekable ranges and
  bounded MP4 initialization metadata. This does not decode or render video.
- Keep media metadata acquisition and retention consistent with resource policy.
- Preserve cross-realm integrity operations using the canonical owner, with local
  Proxy invariant materialization. Frozen Chrome fixtures cover platform objects;
  focused ownership tests cover owner-initiated changes, arrays and callables.
- Preserve the active geometry observation when a reduced scroll range clamps
  an ancestor offset during focus. Invalidate viewport projections without
  discarding the style scope still used by the ancestor walk. A focused V8/goja
  regression reproduces the previous exception; a local Chrome 152 control
  completes the same focus operation without throwing.
- Leave unnamed inspector/Page evaluations unnamed and invoke inspector
  functions directly through the engine after resolving their realm-owned
  arguments. Debugger implementation functions never enter the author call
  stack; explicit sourceURL directives remain observable. Frozen Chrome probes
  verify the original stacks without string rewriting.
- Use native V8 property observations and receiver dispatch to reduce JavaScript
  diagnostic wrappers. Preserve native library identity and snapshot ownership.
- Invoke Window and Worker timer callbacks directly through the engine, with
  the owning global as receiver and retained arguments. String timers evaluate
  without an artificial resource name. Existing realm error reporting preserves
  the original thrown value, interval continuation and self-cancellation.
  Minimal frozen Chrome 152 probes match the unmodified callback stacks. Two
  retained, instrumented SDK initialization runs now consume the same stack as
  their saved Chrome controls; this is not live authentication evidence.

Focused fixtures and their oracle programs are retained in
`internal/browser/testdata/`; native implementation provenance is retained in
`third_party/gov8/patches/`. These are regression evidence, not live account
captures. See [architecture](../architecture.md) and
[resource policy](../resource-policy.md) for the observation boundary.

## Observed outcome and remaining limitations

The current-source Mimic run opened the main page, login modal, password form
and QR authentication flow. The QR canvas readback matched the authentication
URL returned by the service. Account authentication did not complete: the direct
password request received HTTP 200 with application error code 7 and the
maximum-attempts message. An ordinary user Chrome session also returned the
maximum-attempts message after phone confirmation. This does not establish the
cause of the server decision or demonstrate that login compatibility is fixed.

A subsequent continuous diagnostic capture received application status 0 and
item lists from anonymous feed endpoints. Those requests used tokens previously
issued within that capture. This excludes missing token continuity in those
observed feed requests; it does not establish authentication acceptance or
explain the account endpoint's error code. The XHR CORS correction is independently
verified against Chrome 152, not claimed as the cause of a successful login.

A proxy control failed to connect to required application CDN hosts. An
independent SOCKS client reproduced that refusal; it was not resolved by changing
Mimic behavior.

Native observation work is incomplete. Remaining receiver error paths and
alternate-engine limitations require further measured work. The existing goja
nonconfigurable-accessor descriptor boundary remains explicitly unsupported.
The focus/scroll style-cache exception is fixed; it has not been established as
the cause of the authentication refusal. Unbounded diagnostic trace retention remains a separate deferred memory
issue, described in [the performance report](../performance/report.md).

The application repeatedly invokes `Node.isEqualNode`. Its undefined result
was corrected with comparison of canonical node state, independent of public
getters, attribute insertion order, template contents and shadow trees. A frozen
Chrome 152 probe covers node types, prefixes, text code units and argument
validation on both V8 and goja. CharacterData creation and reads now preserve
lone UTF-16 surrogates through the canonical JSON transport. A subsequent fresh,
direct Mimic run submitted the password form once and still displayed the
maximum-attempts message. These DOM corrections do not establish the cause of
the authentication refusal. Shared CORS still lacks preflight caching and the
aggregate request-header safelist limit, as documented beside the loader.

Investigation follows [the gate methodology](antibot-gate-methodology.md) and
[the oracle policy](../oracle-policy.md). Live account captures are private ignored artifacts and are excluded from the
public changes; public fixtures and native patch provenance are retained. No successful sign-in or complete elimination of diagnostic stack
frames is claimed.


## Connection CSP admission and SDK retry observations

A retained headful Chrome 152 probe with `default-src 'none'` blocks Fetch and
XHR before transport. Fetch rejects with `TypeError`; XHR reports ready states
1 and 4, then `error` and `loadend`, with status 0. Mimic previously allowed the
connection. Enforced `connect-src` (or its `default-src` fallback) now participates
in request admission before preload reuse, CORS preflight, interception and
transport, and remains attached to the initiating realm across redirects.
Worker Fetch uses the worker's policy rather than the creating document's policy;
blob/data workers use their inherited security URL for source matching.
Report-only policies do not block connections.

The same Chrome probe family shows that a CSP-rejected WebSocket is CLOSED
immediately after construction and emits an asynchronous `error` without a
`close` event. Admission now prevents dialing and preserves that sequence.
An ordinary failed handshake instead reaches CLOSED before its error and close
events. Focused tests cover these paths, Fetch/XHR transport suppression,
redirect destinations, policy intersection and worker ownership. This does not
claim complete CSP source-expression support or violation-reporting support.

Two subsequent instrumented SDK initializations recover the same XHR failure
and retry tail as the two retained Chrome runs: DONE, status 0 and the retry
counter/interval updates. The common read-occurrence comparison still contains
network-estimate profile state and random decimal-string length differences.
Numeric table accesses and a few additional graphics/string reads remain under
investigation; occurrence matching alone cannot prove full execution identity.
This correction is browser compatibility evidence, not a causal explanation of
application error 7 or proof of successful authentication.


## Image CSP and the additional graphics branch

A second minimal frozen Chrome 152 probe sets an otherwise valid one-pixel data
GIF under `default-src 'none'`. Chrome emits `error`, reports completion and zero
natural width; Mimic previously emitted `load` with natural width one. Image
source-list admission now uses the same retained policy/source/directive state
as connections, with `img-src` and its default fallback. It runs before the
available-image cache as well as preload reuse and transport; `img-src data:`
continues to permit the image. Both engines have focused blocked/allowed tests.

After this correction, the extra SDK `drawImage`, `getImageData` and image-data
reads disappear in both Mimic initialization runs. All four recorded initial consumer
stacks remain equal to the retained Chrome stacks. Stable common-read differences
are the selected network RTT estimate and the length of a randomly generated
decimal string. A bounded one-second capture can include another timer iteration
in one run; those additional reads must retain their timing classification.
None of these findings establish universal browser identity or server-policy
acceptance.


## Origin-agent-cluster observations

The extended SDK diagnostic reads `originAgentCluster` after initialization.
Frozen Chrome 152 defaults to true in secure contexts, accepts `?0` as an opt-out,
and ignores invalid header values. In insecure contexts all measured header
cases report false. Mimic previously defaulted to false and accepted `?1` in an
insecure context. Response admission now follows the measured secure-context
rules.

Within a document tree, the first decision for an origin persists across child
frame replacement and removal; another origin has an independent decision.
A top-level navigation can start a new decision group, as measured by sequential
navigations with changing headers. This state belongs to retained realm trees,
not a global runtime registry, and isolated worlds use their document's state.
Focused tests cover response defaults, header admission, navigation and same-
and cross-origin iframe decisions on both engines. This models the observed
cluster projection; it does not introduce a renderer-process allocator.

A longer diagnostic retained roughly forty thousand reads without reaching the
new fifty-thousand-read cap. It exposes additional receiver-error messages that
include the authored source of JS-backed platform functions, unlike Chrome's
native-function representation. The initial Navigator receiver stack remains
matched; those later function-receiver paths remain unresolved. No complete
stack or browser-identity claim follows from the initialization comparison.


## Native platform callable entry

JS-backed platform operations are now published through the existing native
receiver dispatcher where the engine supports it. Their semantic implementation
remains realm-owned JS; the public callable is a nonconstructible native function
with the operation's name and arity. One callable per implementation preserves
aliases, and genuine engine intrinsics retain their original identities. The
native callback passes the raw receiver, arguments, result and thrown value;
there is no replacement error message or reconstructed stack. Window operations,
interface methods/configurable accessors and Worker-global operations use this
boundary. The dispatcher is already part of snapshot external references and
requires no new native binary build.

Minimal frozen Chrome 152 measurements cover Date receiver failures on
`setTimeout`, `Storage.getItem`, `Document.createElement` and Array's intrinsic
iterator. The new public callables match the native source representation and
first error frame. Focused tests also retain arbitrary author-function source,
argument-conversion exception identity, intrinsic aliases, callable metadata,
Worker timers, callback-root release and restored snapshot behavior.

The extended SDK capture contains 36 stack observations. Function-receiver
messages and stacks now match the retained Chrome trace; two canvas-object
receiver observations still differ in their object description. That remaining
object-allocation/brand boundary is independent of native callable entry and is
not resolved by modifying error strings. Alternate engines without the native
dispatcher retain their explicit JS implementation boundary.


## Generated constructor object descriptions

The remaining two SDK stacks were Date receiver errors on canvas objects.
Generated constructors had correct public names but shared anonymous V8 function
metadata. Objects and even `Object.create(HTMLCanvasElement.prototype)` therefore
had a different engine error description. Renaming the shared record was rejected:
it incorrectly changed sibling constructors. The generator now emits independent
literal named constructor factories, retaining receiver, arguments, new-target,
lazy construction and custom-element behavior. No error text or stack is rewritten.

Both fresh Mimic diagnostic runs now match all 36 retained Chrome 152 SDK stack
observations exactly. Focused tests cover ordinary and restored-snapshot object
brands and constructor identity. This proves the measured instrumented scenario;
it does not establish every possible author stack, full browser identity or a
cause for the server's login rejection.


## Matched diagnostic environment and remaining stack boundary

A fresh diagnostic Context per run imports the ordinary Mimic profile through
validated CDP profile import, selecting the retained Chrome window position
(593, 328) and network RTT estimate (450 ms). These are environment observations,
not universal defaults. Both runs match all 36 SDK stack values and have no
repeatable differences in common named scalar reads. The remaining named-read
count difference is one `charCodeAt` read: the recorded random decimal input has
ten characters in Chrome and nine in Mimic. Random samples and capture timing
are not literal compatibility targets.

At that checkpoint, a separate minimal author probe revealed a broader boundary.
Errors created by JS-backed platform implementations included bootstrap frames:
illegal canvas construction, Storage and Document receivers, and timer argument
conversion. Chrome's corresponding stacks contain only author frames. Native
public callables alone do not change the implementation script's ownership.
This required a trusted platform-code compilation boundary, preserving arbitrary
author source (including misleading sourceURL names), author callbacks and
original exception identity. Stack strings must not be sanitized. The same probe
also found that `Document.createElement('<bad>')` incorrectly succeeds.

## CI load-gate attribution

The retained failing Windows CI stack shows the lazy-image test deadline inside
goja bootstrap execution, before parser image scheduling. The test now separates
bootstrap admission from its unchanged two-second load-delay gate, starting that
gate once the deliberately blocked image reaches transport. A load event before
transport is also accepted, and independent image scheduling retains its own
two-second gate. An eager-image negative control fails the load-delay gate as
required. This is test phase attribution, not a production image-loading fix or
proof of a startup performance improvement.


A subsequent Windows run passes the lazy-image gate and reveals two fixture
issues. The XHR fixture recorded every non-document request in unsynchronized
strings, so browser-owned favicon discovery could overwrite the completed POST
capture. The fixture now captures only its explicit POST endpoint through a
channel, with unchanged header/body/event assertions. The origin-cluster fixture
previously timed eight iframe bootstraps as one evaluation; it now bounds each
navigation individually and checks both every choice and the original aggregate
sequence in the same parent decision group. Focused production-engine tests pass.
These changes neither suppress favicon acquisition nor change cluster semantics.


## Current platform stack and lifecycle audit

Trusted platform compilation now establishes provenance before parsing, rather
than classifying resource names or rewriting captured stack strings. Native
DOMException state preserves lazy stack hooks and current exception name/message
observations; ordinary author-created DOMException objects keep their distinct
stack contract. Platform promises retain their realm intrinsics even when author
code replaces the global constructor or static methods.

Connected frame contexts share their Page's V8 owner and canonical origin tokens.
Measured author getter/method stacks retain parent frames without frame-reflection
or transaction frames. Date coercion runs inside native Date construction, and
CDP return-by-value serialization leaves only the author getter frame observed in
Chrome. Focused stack tests explicitly exercise both cold and actually restored
parent contexts on Windows and Linux. Navigation retirement disables the old
native context's promise jobs without disposing retained objects; removing a
frame keeps retained language promise jobs, as the saved Chrome lifecycle probes
require. Isolate-wide module callbacks dispatch by the originating context.

Two fresh instrumented SDK runs again consume all 36 retained Chrome stack values
exactly and contain no repeatable differences in named scalar value sets. They
record 39,814 and 39,815 reads against the retained Chrome run's 39,802. Read counts
and ordering are not identical: another timer registration, randomized table
accesses and asynchronous block ordering still require classification. These
instrumented, network-blocked diagnostics neither prove passive page identity nor
explain the account endpoint's error 7. The historical login outcomes above
remain unchanged; no new successful sign-in or permanent server acceptance is
claimed.

### Permission completion ordering

An explicitly seeded-random diagnostic, run in alternating Chrome/Mimic/Mimic/
Chrome order, produces identical counts of random calls and timer registrations.
This control replaces `Math.random` openly and is not a normal-browser oracle.
It removes the previous extra timer difference, but does not establish identical
execution paths: asynchronous blocks still move, and numeric-index reads differ.

A separate minimal probe without the SDK or platform overrides identifies a
general permissions defect. Both headful Chrome controls reject invalid queries
before ordinary microtasks and fulfill successful queries after them. Mimic had
already-fulfilled successful promises. Descriptor conversion remains synchronous;
successful fulfillment now runs through the private Page scheduler in a later
task. The private scheduler avoids depending on author replacements of timers.
No fixed latency or ordering against unrelated task sources is inferred from
Chrome's permission-service transport.

The minimal completion sequence now matches all four controls. Focused descriptor,
permission-state, alias and completion-order tests pass on Windows and Linux.
Repeated SDK controls retain all 36 exact stack values and 448 random calls in
each run; total reads remain 39,813 in Chrome and 39,816/39,815 in Mimic. These
results prove the measured permission boundary and preserved stack observations,
not full SDK identity or a causal explanation of login error 7.

### Window frame index publication

The remaining two numeric reads were traced to an own-property-name scan of
Window: Mimic retained index `0` after its iframe was disconnected, while indexed
access already returned undefined. The initial minimal probe accidentally read
`window.length` first, refreshing the properties and hiding the defect. Controls
which enumerate properties before reading length reproduce the missing index
after insertion and stale index after removal on both Goja and V8. Frozen Chrome
updates both observations immediately.

Window index publication now follows changes to the canonical connected frame
tree, including removal of an ancestor container. That removal detaches its
embedded browsing contexts rather than leaving them in the active Page tree.
Initial language projection also publishes an existing tree. Retained detached
Windows remain observable after resource cancellation; publication does not use
the canceled resource context or start a resource operation.

Direct-frame and container-removal controls match the retained Chrome observations
without a length read. Focused frame identity, deferred initialization, retained
promise, snapshot and stack tests pass on Windows and Linux. Both SDK repeats
retain all 36 exact stacks and no longer read the stale undefined Window index.
One records the same 39,813 reads as Chrome; the other records 39,586 and 447
random calls at the timed snapshot instead of 448, so asynchronous completion
still needs classification. Equal counts in one run do not establish full
identity or explain the account endpoint's error 7.

### Parser transport and runtime admission

Committed top and child navigation documents now retain canonical state while
deferring JS bindings until execution or event dispatch needs them. Previously,
cold bootstrap delayed the first external parser-script request, and a short
navigation deadline could expire before that request reached transport. A
focused creation-count regression reproduces the eager runtime and now verifies
that parser-suspended script transport starts without creating one. Existing
navigation realm, cancellation, timing, currentScript, unsuccessful-resource and
restored-document tests pass on Windows and Linux. Resource events initialize
their dispatcher before reading its cached function handle.

The original CDP navigation-deadline test reaches the held-script barrier after
this change without modifying its deadline or assertions. A separate one-second
first-evaluation response gate remains sensitive to cold admission under load;
repeated combined checks can still time out there. It is not reported as fixed.

With the retained controls' actual window coordinates and network RTT selected,
two further SDK repeats each record 39,813 reads and 448 random calls. All 36
stacks and counts of named read operations match both Chrome controls. The only
repeatable named scalar differences are reads of the diagnostic's own random-call
counter, taken at different points in asynchronous execution. Earlier controls
used coordinates and RTT from an older capture; those differences are selected
environment state, not browser constants. Timing, uninstrumented execution and
server decisions remain outside this diagnostic's identity proof.
## Shared-owner frame call admission, 2026-09-28

A retained diagnostic timer control localized the earlier reordered SDK blocks
to execution cost. One callback took 23–24 ms in Chrome and 778–1,259 ms in
Mimic; the latter crossed an already scheduled three-second timer deadline.
This does not justify changing task ordering to reproduce a faster run.

A separate probe without SDK execution attributed most repeated function-source
observation cost to reading the child Window property. Three thousand repeated
child reads took 202–236 ms; observing an already retained child function took
about 3 ms. The frame bridge unnecessarily used a cooperating goroutine and
mailbox pump even when both contexts shared the same V8 execution owner.

Realm call admission now runs directly for a shared owner, preserves the active
caller's cancellation, and retains nested dispatch for independent owners.
An initial measured candidate reduced repeated child reads to 67–69 ms without
changing identity or source checksums. The focused regression fails on queued
shared-owner admission and covers direct thread affinity, independent-owner
callbacks and active cancellation. Stack, live-trap, revoked-proxy, origin and
identity checks pass on Windows and Linux.

Two fresh SDK runs on the final implementation retain 39,813 reads, 448 random
calls, all 36 exact reference stacks and identical named-operation counts.
The first ordered-operation mismatch occurs at index 32,634 in one repeat and
39,770 in the other; the prior implementation differed at 32,367 in both.
These instrumented results show improved admission cost, not complete ordering
identity or a demonstrated cause of login error 7. Cold artifact preparation
and the currently failing Windows CI deadlines remain separate work.
## Snapshot builder thread admission, 2026-09-28

Focused repeat measurements found that cold snapshot execution and serialization
slowed within one Windows process while the seed source and V8 heap sizes stayed
comparable. Go GC CPU fraction remained below 0.3%. These observations do not
establish a memory leak or a server-side login cause.

The Page execution owner already requests latency-sensitive Windows thread QoS,
honoring any explicitly inherited policy. Snapshot construction lacked the same
admission even though the Page synchronously awaited its result. The builder now
uses that policy and retains an outer OS-thread pin until restoration finishes;
SnapshotCreator consumes its own pin during serialization. Success and failure
paths restore the caller's policy, and restoration errors remain visible.

The unchanged suspended-navigation deadline test passes three consecutive
document/script repetitions after the change; prior repeat controls failed the
one-second first-evaluation deadline. Focused snapshot lifetime, cancellation,
serialization, restored callback and author-stack checks pass on Windows and
Linux. This is a bounded local responsiveness result, not a broad performance
comparison or a claim that GitHub CI has passed.

The consolidated commit still failed Windows CI: the suspended-navigation
first evaluation exceeded one second, and visible focus in a large document
exceeded five seconds. The execution owner's QoS admission previously occurred
after isolate construction. It now covers construction and snapshot restoration
as well, with an outer thread pin preserving restoration ownership even on
constructor failure. Failed post-construction initialization also closes the
isolate and releases its snapshot instead of abandoning native ownership.
Both unchanged deadline tests pass three local repetitions; hosted CI remains
the acceptance check. No assertion deadlines or reference expectations changed.

The next hosted run passed the visible-focus gate but exposed a child-navigation
clock defect: its projected response completion did not advance the Page clock
before parsing, unlike a top-level navigation. Resource Timing could therefore
report responseEnd after domInteractive. Both commit paths now share clock
admission. A controlled response with a five-second visible completion phase
reproduces the ordering defect in both engines without a real five-second wait;
the regression also checks child and parent performance.now against completion.
The retained Chrome redirect/lifecycle expectations remain unchanged.

The lazy-image test's unchanged two-second load gate ran concurrently with
unrelated cold Goja and V8 bootstraps. Its failure stack remained in parser
JavaScript initialization after lazy-image transport had started. This elapsed
deadline check now runs in the serial test lane, retaining the blocked image,
both original deadlines and all load-state assertions. Focused Windows and Linux
checks pass; broad acceptance remains the hosted CI run.

The suspended-navigation CDP response gate still timed out when run late in a
long-lived Windows test process, although its unchanged document/script cases
passed focused local repeats. Windows CI now executes all 85 CDP root tests and
their subtests in ten-root process batches, retaining the existing one-second
response assertion. The batch partition has a coverage test; this changes only
process lifetime, not the browser implementation or expected responses.
