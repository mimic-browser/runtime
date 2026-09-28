# Performance architecture pass

## 2026-09-28: callback-scope size experiment rejected

The ten-execution DOM profile attributed about 123 MiB of Go allocations to
the per-invocation `gov8` callback record. A removable experiment eliminated
one duplicated isolate pointer from `CallbackScope`, reducing the combined
invocation struct from 64 to 56 bytes. Both sizes occupy the same 64-byte Go
allocation class, and the measured site remained **123.01 → 122.51 MiB** in
unpaired sampled profiles. The ten-active-Page fast gate and focused callback,
snapshot, DOM and cross-realm tests passed, but the change was reverted because
it produced no material allocation benefit. The native callback record must
reach a smaller allocation class, or avoid some invocations entirely, to make
this approach useful. Receipts: `.build/profile-callback-scope-20260928/` and
`.build/fast-gate-callback-scope-20260928/raw.json`.

## 2026-09-28: keep packed string host arguments unboxed

The next ten-execution DOM profile attributed 10 MiB to putting decoded
packed strings into an interface field before browser hosts read them. Packed
callbacks now keep decoded strings in their reusable invocation frame and the
browser's string argument helper reads them directly. Explicit `Export()`,
Unicode/NUL round-trips, returned arguments and post-callback invalidation
remain covered. The corresponding boxing allocation site disappeared; UTF-16
decoding still allocates the actual string. Whole-profile sampled Go allocation
was **435.13 → 409.91 MiB** in one unpaired diagnostic run, so its total delta
must not be assigned entirely to this change.

Focused packed-call, DOM, geometry and Canvas tests passed. The fresh-build
ten-active-Page fast gate passed every validity row. Warm DOM/static/React
completion medians were **427.34/44.51/105.60 ms**, versus
**421.35/39.24/117.81 ms** in the preceding short run; no page-latency gain
is established. Receipts: `.build/profile-packed-string-20260928/` and
`.build/fast-gate-packed-string-20260928/raw.json`.

## 2026-09-28: keep packed numeric host arguments unboxed

The DOM allocation profile attributed 13.0 MiB across ten executions to
boxing packed numeric host arguments before the browser read node IDs and
coordinates. Packed callbacks now keep those numbers in their existing
reusable invocation frame, and the browser's numeric argument helper reads
them directly. Explicit `Export()` still returns a float64; returned arguments,
negative zero, nested callbacks and invalidation after callback exit retain
their behavior. The same profile site has no numeric boxing allocation after
the change. Whole-profile sampled Go allocation was **445.14 → 435.13 MiB**
in one unpaired diagnostic run; other sites varied, so that total is not a
precise estimate of the isolated saving.

Focused packed-call, DOM, geometry, Canvas and cross-realm tests passed. The
fresh-build ten-active-Page fast gate passed every validity row; warm
DOM/static/React completion medians were **421.35/39.24/117.81 ms**, versus
**424.64/40.53/107.61 ms** at the preceding checkpoint. This short unpaired
gate does not establish a page-latency improvement or React regression.
Receipts: `.build/profile-next-b-20260928/`,
`.build/profile-packed-number-20260928/` and
`.build/fast-gate-packed-number-20260928/raw.json`.

## 2026-09-28: avoid copying bootstrap source for cache key

The current ten-Page allocation profile attributed 4.28 MiB to converting
immutable bootstrap source strings into byte slices solely to hash cache keys.
The synchronous SHA-256 calculation now borrows the string's read-only bytes;
the cache key and script text are unchanged. A focused test compares hashes
against the previous copied form for empty, short and large sources, and
verifies zero allocations for a large source. Source-cache isolation tests
also pass. This removes the identified copy; it does not address V8's larger
native isolate graph.

The fresh-build ten-active-Page fast gate passed every validity row. Warm
DOM/static/React completion medians were **424.64/40.53/107.61 ms**, versus
**437.15/40.27/119.05 ms** at the preceding catalog checkpoint. This short,
unpaired comparison is not evidence of a page-latency gain. Receipt:
`.build/fast-gate-hash-borrow-20260928/raw.json`.

## 2026-09-28: decode only selected catalog shape fields

Catalog selection previously decoded generated IDL specifications and members
into temporary JSON maps, then encoded and decoded filtered members again.
It now decodes only the fields needed for selection and builds the same compact
shape catalog directly. Ten repeated selections of the frozen Chrome 152
window catalog produced identical 379,667-byte output (SHA-256
`9fec735c6a9bbdf919b2934783a4f8481a5d0dc3efafe7e8f4809b58ce33c53e`).
The focused probe measured **298.26 → 193.79 MiB** cumulative Go allocations
(-35.0%) and **332.74 → 211.22 ms** elapsed (-36.5%). This preparation is
normally performed once per exposure, so the gain is startup allocation and
CPU, not a per-Page runtime or ten-live-Page memory reduction.

Focused catalog selection and independent-realm tests passed. The fresh-build
ten-active-Page fast gate passed all DOM, static and React validity rows;
warm completion medians were **437.15/40.27/119.05 ms**, versus the prior
checkpoint's **438.48/40.14/107.95 ms**. These unpaired short runs do not
establish a page-latency gain or React regression. The temporary probe was
removed. Gate receipt: `.build/fast-gate-catalog-20260928/raw.json`.

## 2026-09-28: ten-live-Page memory attribution after allocation fixes

The current fresh-build density probe kept ten independent Pages live for
each static, CPU and React workload. At the ten-Page barrier, process RSS was
**495.8/578.0/574.2 MiB** respectively, while Go heap was only
**32.7/42.7/46.8 MiB**. The ten V8 isolates together reported used heap of
**209.4/257.2/260.7 MiB**. Diagnostic full V8 collection on each live Page
reduced RSS to **470.9/474.3/524.1 MiB** and used isolate heap to
**199.1/200.3/240.6 MiB**; median collection time per Page was
**6.0/8.0/9.6 ms**. Collection is an attribution intervention, not a proposed
production policy.

After all ten Pages closed, RSS at 250 ms was **150.5/152.4/158.2 MiB**;
explicit Go scavenging reduced it to **117.3/120.3/123.9 MiB**. The active
memory cost is thus primarily native isolate/realm state, and Page teardown
does release most of it. The prior shared-isolate experiment regressed E2E and
would compromise independent Page execution; forced collection does not remove
the persistent live graph. No production code changed in this attribution
step. Fresh-build receipts and per-isolate diagnostics are under
`.build/profile-density-current-20260928/` and
`.build/profile-density-gc-20260928/`.

The startup blob remains a native copy per isolate: `HoldBlob` owns its own
buffer until that isolate closes, even though Go consumers share immutable
source bytes. Sharing the native buffer could reduce live density, but it
requires a reference-counted shim lifetime change and matching rebuilt Windows
and Linux artifacts. This workspace has the packaged binaries and a stock
Windows V8 archive, not the matching patched V8 source/archive required by the
repository's native build verification. No unverified native sharing change
was shipped or included in the measurements above.

## 2026-09-28: defer geometry-revision read until needed

`invalidateRetainedGeometry` read the canonical observation revision before
checking whether a mutation was connected. Its connected and no-target paths
discard the retained geometry without using that value. The read now occurs
only for detached mutations, where it is still needed to advance the retained
revision. The source of truth, dirty-ancestor walk, invalidation behavior and
observable geometry are unchanged. Focused mutation, cache and
IntersectionObserver tests passed, as did the ten-live-Page fast gate.

The unchanged ten-execution DOM profile allocated **449.24 → 428.61 MiB** of
Go memory and measured **410.31 → 380.77 ms** median execution in one
unpaired comparison. The fast gate did not reproduce a clear page-latency
improvement: warm DOM/static/React completion medians were
**438.48/40.14/107.95 ms**. Three alternating fresh-process pairs of the
unchanged Wikipedia E2E all passed; control/candidate wall seconds were
**12.375/12.378**, **11.539/11.821**, and **11.925/11.798**. The paired
differences change sign and do not establish an E2E gain. The change remains
as a pure, measured allocation-pressure reduction, not as a Wikipedia
optimization. Exact source and binary hashes are in
`.build/wiki-geometry-build.json`; workload receipts are
`.build/wikipedia-geometry-pairs-20260928.json`,
`.build/profile-geometry-invalidation-20260928/` and
`.build/fast-gate-geometry-invalidation-20260928/`.

## 2026-09-28: Page-owned immutable host primitives

Go-origin `true`, `false` and `null` results now reuse three immutable V8
host-value wrappers owned by their adapter. Other values remain individually
allocated and may survive their creating callback. No wrapper or mutable
state is shared between Pages. Focused V8 and race tests passed.

The unchanged ten-execution DOM profile allocated **473.96 → 449.24 MiB** of
Go memory (about **5.2% less**); the sampled `adapter.Value` allocation site
fell from about 43 to 19 MiB. The sequential React profile measured
**215.25 → 213.90 MiB** and essentially equal execution medians
(**55.70 → 55.81 ms**). The original ten-live-Page gate passed, but its React
timings were slower in a noisy period. Two alternating fresh-build React
control/candidate pairs then measured warm medians **109.40/109.09 ms** for
control and **106.52/111.78 ms** for candidate, with ten-live-Page wave
completion **777.9/776.0 ms** versus **779.3/729.2 ms**. Each pair ran all six
semantic workloads. These results support the allocation reduction and do
not establish a general latency gain or a repeatable regression. The control
used a Go overlay of the committed `runtime.go`; binaries were hash-checked
before every launch. Receipts are under `.build/profile-host-primitives-20260928/`,
`.build/profile-host-primitives-react-20260928/`,
`.build/profile-host-primitives-react-control-20260928/`,
`.build/fast-gate-host-primitives-20260928/`, and
`.build/react-pair-{c1,a1,a2,c2}-20260928/`.

## 2026-09-28: allocation-free class membership scan

The restricted native class matcher built a token slice for each tested node.
It now scans the existing attribute string directly, recognizing the same five
ASCII whitespace characters and leaving non-ASCII whitespace within a token.
Focused tests cover leading/trailing separators, substring mismatches,
Unicode tokens and non-breaking space. Existing browser class and token tests,
the DOM race check and the ten-live-Page fast gate passed.

The unchanged ten-execution DOM profile allocated **478.04 MiB control** and
**473.58/473.96 MiB** in two candidate runs (about 4 MiB less). Candidate
median execution was **400.82/392.56 ms** against one **387.16 ms** control;
those short samples are too noisy to establish either a gain or a regression.
The ten-live-Page gate's warm DOM/static/React completion medians were
**430.11/37.14/100.32 ms**, near the preceding gate. The change is retained
for its measured allocation reduction, not a claimed page-speed gain. Raw
receipts are `.build/profile-selector-scan-20260928/`,
`.build/profile-class-scan-20260928/`,
`.build/profile-class-scan-repeat-20260928/`, and
`.build/fast-gate-class-scan-20260928/`.

## 2026-09-28: scan legacy selector lists without splitting

The restricted native leaf-selector matcher split a comma-separated selector
into a new slice for every candidate DOM node. It now scans the same leaves
with `strings.Cut`, preserving order, trimming and empty-leaf behavior.
A focused regression covers single, grouped, whitespace and empty selectors;
existing selector-journal and browser tests passed.

On the unchanged ten-execution DOM profile, Go allocation fell **487.67 →
478.04 MiB** against the direct-predicate binary (about **2.0%**). Median
execution was **386.24 → 387.16 ms**, effectively unchanged. The fresh-build
ten-live-Page fast gate passed all six workloads and its three throughput
waves; warm DOM/static/React completion medians were
**432.58/36.71/99.76 ms**. This is an allocation reduction, not an
established page-latency gain. Raw receipts are
`.build/profile-predicate-20260928/`,
`.build/profile-selector-scan-20260928/`, and
`.build/fast-gate-selector-scan-20260928/`.

## 2026-09-28: direct native V8 predicates

Gov8's `Value.predicate` used a variadic native procedure call for every
type check. It now uses the same fixed-argument syscall boundary already used
by other hot gov8 operations, with the same native function, arguments,
result and error handling. Focused V8, gov8 HTMLDDA and race tests passed.

On the unchanged ten-execution DOM profile, Go allocation fell **501.71 →
487.67 MiB** against the preceding receiver-value binary (about **2.8%**),
and the sampled predicate allocation site disappeared. Short-run median
execution was **392.07 → 386.24 ms**, but the ten-live-Page gate was mixed:
warm DOM/static/React completion medians were **432.65/37.40/99.40 ms**.
The result is an allocation reduction, not a proven general latency gain.
All six fast-gate workloads passed. Receipts are
`.build/profile-this-value-20260928/`, `.build/profile-predicate-20260928/`,
and `.build/fast-gate-predicate-20260928/`.

## 2026-09-28: avoid transient receiver wrapper in V8 host calls

The ordinary V8 host callback path retrieved `this` through gov8's `This`,
which allocated an Object wrapper before Mimic immediately extracted its
underlying Value. Gov8 now also exposes `ThisValue` with the same frame checks
and borrowed-scope lifetime; Mimic uses it only where the Object wrapper was
discarded. The existing `This` method retains its contract. A focused test
checks the receiver in both `.call(...)` and constructor invocations.

On the unchanged ten-execution DOM profile, Go allocation fell **520.36 →
501.71 MiB** against the preceding borrowed-scope binary (about **3.6%**).
Sampled allocation from `FunctionCallbackArguments.This` disappeared from the
Mimic path. Short-run median execution was **401.05 → 392.07 ms**, but the
ten-live-Page gate did not confirm a latency gain: its warm DOM/static/React
completion medians were **436.91/36.64/105.37 ms**. The result is therefore
an allocation-pressure improvement. Focused V8 and browser tests, V8 race
checks, and all six fast-gate workloads passed. Raw receipts are
`.build/profile-gov8-scope-20260928/`,
`.build/profile-this-value-20260928/`, and
`.build/fast-gate-this-value-20260928/`.

## 2026-09-28: compact borrowed V8 callback scope

The local gov8 callback dispatcher deliberately allocates one unique scope
record per V8-to-Go callback so a retained scope cannot become valid again
through reuse. Its scope carried a 24-byte slice header for the seldom-used
JavaScript execution-guard stack on every call. The header is now allocated
only when the first guard is opened; the callback's unique scope and close
invalidation remain unchanged. A focused regression checks nested guard LIFO
rules and refusal to close their owning scope early.

On the unchanged ten-execution DOM profile, Go allocation fell **553.74 →
520.36 MiB** against the compact-host-value binary (about **6.0%**). Sampled
flat allocation at `hostCallbackDispatch` fell from about **183.5 to 122.5
MiB**. Median execution was **400.81 → 401.05 ms**, effectively unchanged.
The profile receipts are `.build/profile-host-value-20260928/` and
`.build/profile-gov8-scope-20260928/`. Focused gov8 and browser tests, the
guard race test, and a fresh-build ten-live-Page fast gate passed. Its warm
DOM/static/React completion medians were **435.17/36.84/98.72 ms**; noisy
static throughput waves do not establish a speed gain. The gate receipt is
under `.build/fast-gate-gov8-scope-20260928/`.

## 2026-09-28: compact host-value wrappers

The V8 DOM allocation profile still attributed about 146 MiB across ten
executions to `adapter.Value`: every Go-origin value allocated a full native
handle wrapper even though it carried only a runtime pointer and host payload.
Host values now have a compact immutable wrapper. It remains individually
allocated because values may be retained across callbacks; a focused regression
checks export, string conversion, type, and return from a later callback.
Packed and ordinary host returns use the same payload conversion rules, while
native-handle values keep their existing representation.

The unchanged ten-execution DOM profile allocated **661.59 → 553.74 MiB** of
Go memory after the preceding callback-frame change, a further **16.3%**
reduction. The sampled `adapter.Value` site fell from about **146 to 45 MiB**.
Median execution was **400.81 ms** in both short runs, so this is an
allocation-pressure improvement rather than a demonstrated latency gain.
Profile receipts are in `.build/profile-callback-frames-20260928/` and
`.build/profile-host-value-20260928/`. Focused V8 tests, the relevant V8 race
tests, and the fresh-build ten-live-Page fast gate passed; the latter's warm
DOM/static/React completion medians were **438.24/37.19/100.08 ms**. Its
receipt is under `.build/fast-gate-host-value-20260928/`.

## 2026-09-28: reuse V8 host callback contexts

The DOM profile identified per-call `callbackContext` allocations in both the
packed and ordinary transient V8-to-Go host paths. Each path already retains a
bounded, nested-call-safe frame; the callback context now lives in that frame
and is cleared when the call returns. Non-transient callbacks retain their
existing ownership. Focused packed argument, result, error, reentrancy and
frame-release tests passed, including the V8 race run.

On the unchanged DOM profile, ten sequential executions allocated **761.94
MiB control** and **661.59 MiB candidate** of Go memory, measured from runtime
allocation counters at the same phase barriers (about **13.2% less**). Sampled
allocation profiles independently attributed roughly 69 MiB to the former
packed callback-context allocation. Profile directories are
`.build/profile-next-20260928/` and
`.build/profile-callback-frames-20260928/`. This is allocation pressure, not
retained RSS. The candidate's fresh-build ten-live-Page fast gate passed all
six semantic workloads and three static throughput waves; its DOM/static/React
warm completion medians were **443.79/37.89/113.45 ms**. Short gate samples
do not establish an end-to-end latency gain. The receipt is under
`.build/fast-gate-callback-frames-20260928/`.

## 2026-09-28: explicit trace capture and diagnostic dispatch

Diagnostic history is now empty by default. `Mimic.startTrace` starts a new
bounded capture, `Mimic.getTrace` reads it, and `Mimic.stopTrace` stops it while
keeping the captured events readable. Network completion records used by
Window/Worker Performance remain independent of diagnostic capture. CDP's live
subscribers receive only the event kinds they consume. DOM query bindings avoid
building diagnostic node snapshots and result-ID copies when neither capture
nor another all-event subscriber requests API events. The external all-event
subscriber contract remains available.

A temporary probe on ten live Pages added 5000 selector-result events per Page,
each carrying 1024 node IDs, then sampled the live Go heap after GC. With
capture explicitly enabled, the additional retained heap was **73,559,336
bytes** and each Page kept 5000 bounded events. With the default capture-off
state, the same probe retained **7,752 bytes** and zero diagnostic events.
The probe was removed after measurement; it isolates trace retention rather
than measuring page execution or process RSS. Network response records remain
available for the browser-observable Performance timeline.

The ten-live-Page fast gate passed all six semantic workloads and its ten-Page
waves. Against the preceding bounded-history binary, one fresh control/candidate
comparison measured median ten-Page static wave throughput **48.79 → 56.24
Pages/s**, warm DOM completion **467.31 → 452.88 ms**, static **38.90 → 37.25
ms**, and React **103.18 → 98.52 ms**. Static live RSS was **747.93 → 748.59
MiB**, React **765.36 → 762.84 MiB**. These short, non-paired samples do not
establish a general speed or memory improvement. Control/candidate executable
SHA-256 values are
`87d14a8cc8b9b819ef12ca34aa333aa9d59f6b71555d80996223381bd6ffa3d5`
and `3daed08a7698873fa3ad12cfb18caf4301e10de6fd9ab27f2dd31b70a0312241`.

The additional query diagnostic dispatch change passed three alternating
fresh-process pairs of the unchanged Wikipedia E2E. Workload times were control
**10917/10654/10752 ms** and candidate **10906/10665/10608 ms**; the paired
differences are too small to claim an E2E speedup. The full receipts and binary
hashes are in `.build/wikipedia-trace-pairs-20260928.json`. The workload's
optional late `Mimic.getTrace` calls no longer recover earlier events unless
capture was started before the workload; the dedicated profile script now does
so explicitly.

## 2026-09-28: constant-time bounded DOM mutation journals

The canonical arena and connected-observation journals previously shifted 256
records on every write after filling. They now overwrite the oldest slot in a
ring and return records in the same revision order. Missing revisions still
report overflow and require consumer recomputation. Focused tests cover
multiple recent records after wrap for both journals, plus the selector cache
consumer.

A temporary ten-live-Page Browser benchmark repeatedly mutated connected DOM
attributes across the Pages. Three control samples were **334.7/329.8/333.5
ns per mutation**, versus **72.13/71.80/71.15 ns** with the ring. Both
allocated **11 B and one object per operation**. The median change is about
**4.6x faster in this saturated-journal workload**; it does not imply a 4.6x
page or browser speedup. The benchmark probe was removed after measurement.
The usual ten-Page fast gate passed; warm DOM completion was **452.88 → 441.97
ms** against the preceding candidate, while static and React results varied
within this small sample. The gate does not isolate the journal's contribution
or demonstrate an E2E improvement on ordinary pages. Candidate executable
SHA-256 is
`03d3b95fa4c025f830b127d63f64ebf73342e9d4db472477f925482fd742a1a9`;
its raw ten-Page receipt is under `.build/fast-gate-dom-journal-ring-20260928/`.

## 2026-09-28: bounded diagnostic query history

The diagnostic recorder now retains at most 8192 recent events and at most 128
node IDs per large selector result, with the original result count preserved.
Network completions remain in an independent stream for Window and Worker
Performance timeline consumers, so eviction of diagnostic events cannot discard
resource timing entries. This is a diagnostic retention change, not a DOM query
or selector-result change. The retained network completion stream and arbitrary
large non-selector diagnostic payloads are not yet byte-bounded.

A temporary focused probe, removed after measurement, kept ten independent
Context/Page pairs live and added 5000 query-result events per Page, each with
1024 IDs. After explicit Go GC at both sampling barriers, the additional live
Go heap was **431.54 MB control** and **73.54 MB candidate** (a 358.00 MB,
83.0% reduction). The explicit GC belongs only to this attribution probe.
Both test binaries ran the same temporary test; the control used a Go overlay
of the committed `internal/trace/trace.go`. This probe injects trace records
directly and is not an E2E query-speed or process-RSS result.

The ordinary fast gate's ten-live-Page static memory sample was **755.60 →
747.93 MiB RSS** and **808.22 → 799.03 MiB private memory**; React was
**768.91 → 765.36 MiB RSS** and **820.37 → 817.61 MiB private memory**.
These are one fresh-process sample per workload, not a controlled memory
improvement claim. Three measured ten-Page static throughput waves were
49.38/48.13/49.80 versus 46.93/49.81/48.79 Pages/s. Warm DOM/static/React
completion medians were 468.82/42.28/113.58 ms control and
467.31/38.90/103.18 ms candidate; these short runs do not establish a
latency gain. The candidate used only ten-Page waves; the existing control
fast gate also ran its standard 25-Page wave, which is excluded here.

Control/candidate executable SHA-256 values are
`9e66a3e327976b73fe42316c17a625462b5f4eb04078a6e51cb8c40916a4caf9`
and `87d14a8cc8b9b819ef12ca34aa333aa9d59f6b71555d80996223381bd6ffa3d5`.
The full build and workload receipts are under
`.build/fast-gate-trace-control-20260928/` and
`.build/fast-gate-trace-candidate-20260928/`.

## 2026-09-27: connected frame execution owners

The compatibility fix for author stack continuity now keeps connected V8 frame
realms on one Page owner. Independent Pages keep independent owners by default.
Frames retain separate contexts and global objects; owner disposal waits for the
last connected realm. Matching frame contexts restore the platform seed while
other exposure graphs use a bare context in that same owner.

The first ordinary Page for an unseen exposure graph now synchronously prepares
its bootstrap artifact through the existing private preparation workflow. This
adds cold-start work and transient preparation state; it is not claimed as a
performance improvement. Steady-state frame overhead and cold-start cost need a
matched performance checkpoint before making any latency or memory claims. This
change does not alter resource admission or add rendering/media retention.

## 2026-09-25: generated/manual Context profile PoC

The [profile PoC diagnostic](profile-context-poc.md) runs 300 unique-seed Contexts
on a small local 200-link HTML fixture, with bounded concurrency 8 and genuinely
simultaneous 100-live waves. Maximum barrier-sampled RSS was 533.91 / 4441.53 MiB;
post-close RSS after 300 jobs was 195.28 / 404.72 MiB. Every closed wave had zero
registered Contexts. These are not continuous peaks, a matched optimization
comparison, or proof of a retention plateau. The 100-live memory target is not
solved. No compact-storage rewrite is included. Receipts, executable hash,
host-load qualifications and remaining coherence/diversity limits are linked.

## 2026-09-23: ResourcePolicy on the external Wikipedia AB scraper

The default scenario of `C:/Users/moreveal/Desktop/ab-scripts/run.ps1` runs
50 isolated BrowserContexts concurrently, one Page each, against a captured
Wikipedia Main Page replay from 2026-09-20. Both engines receive the same
Playwright `route.fulfill` fixture; the scraper compares title, link and
heading counts, body text length, first heading and first href. This is not a
live-site or physical-wire-traffic benchmark. An optional `--resource-policy`
argument was added to the external `benchmark.mjs` solely to pass a JSON
policy to Mimic; omitting it preserves the default scenario. The tested policy
(`C:/Users/moreveal/Desktop/ab-scripts/resource-policy-scrape.json`) admits
the main document, blocks all other resource kinds and skips HTTP/CDP body
retention for the document. That intentional fidelity reduction is valid for
this specific server-rendered scrape, not a general Wikipedia policy.

Fresh binary `.build/mimic-resource-policy-ab.exe` had SHA-256
`7B08ADB26C03146C455D3F8256C81F6ECD8B20D9915F187D49C064B541D190C6`.
An independent one-Page probe (`resource-policy-probe.mjs` in `ab-scripts`)
observed **26 resource decisions: 1 document fulfilled, 25 non-document
requests blocked before the Playwright route handler**. Its route handler
fulfilled exactly one request, aborted none; Context statistics reported zero
network acquisitions and zero retained response-body bytes. That is CDP replay
admission evidence, not a measured count of bytes on a physical connection.
Three alternating fresh-process pairs (default→policy, policy→default,
default→policy) all passed exact Mimic/Chrome scraper-result equality 50/50.
Raw JSON receipts, in execution order, are in the external `ab-scripts/results`
directory: `run-2026-09-22T21-35-44-778Z.json`,
`run-2026-09-22T21-36-06-968Z.json`,
`run-2026-09-22T21-36-35-707Z.json`,
`run-2026-09-22T21-37-00-976Z.json`,
`run-2026-09-22T21-37-27-197Z.json`, and
`run-2026-09-22T21-37-49-535Z.json`.

| Pair | Mimic default | Mimic policy | Paired reduction | Chrome default (policy run) |
| --- | ---: | ---: | ---: | ---: |
| 1 | 3375.31 ms | 2113.08 ms | 37.40% | 11519.44 ms |
| 2 | 3960.50 ms | 2073.01 ms | 47.66% | 11322.68 ms |
| 3 | 3632.31 ms | 2265.27 ms | 37.64% | 11444.08 ms |

Medians across the three Mimic samples were **3632.31 → 2113.08 ms**
(41.83% reduction of medians); the median paired reduction was **37.64%**.
Throughput medians were **13.77 → 23.66 Pages/s**. Peak process-tree working
set medians were **3024.70 → 2450.02 MB** (19.0% lower), private memory
**3202.54 → 2620.77 MB** (18.2% lower), and CPU time **59843.75 →
33296.88 ms** (44.4% lower). The median Chrome batch in the policy runs was
**11444.08 ms**, so policy-enabled Mimic's median batch was about **5.4×**
shorter for this exact scrape. Chrome was deliberately not given an equivalent
resource-blocking policy. The replay harness does not measure physical network
traffic; do not treat these results as wire-byte savings.

## 2026-09-23: opt-in ResourcePolicy, preliminary checkpoint

The default/no-policy path passed the unchanged six-workload fast gate from a
fresh build (`.build/resource-policy-fast-gate-20260923`): warm medians were DOM
299.62 ms, static 35.69 ms, and React 90.28 ms; static concurrency waves and
memory checks passed. These numbers are a regression gate, not a matched
policy-on/off performance comparison. One initial full browser-suite run ended
with a V8 access violation; an immediate independent full browser-suite rerun
passed. A subsequent `go test ./... -count=1` passed all packages, including
the browser suite (449.797 s). Focused ResourcePolicy tests and race checks
passed.

After the streaming budget and retention work, the unchanged fast gate was
rerun from a fresh build (`.build/resource-policy-fast-gate-final-20260923`)
and passed. Warm medians were DOM 288.77 ms, static 31.03 ms, React 82.59 ms;
the static concurrency waves at 10 and 25 Pages passed. A full `go test ./...
-count=1` passed at that checkpoint (browser package 273.907 s). After the
last admission/retention fixes, the full suite passed again (browser package
314.303 s), as did focused race checks.

A local 256 KiB image-response benchmark (100 iterations per case) measured
full 262144 body bytes/op and 932717 allocated bytes/op; headers-only 0 body
bytes/op and 50822 allocated bytes/op; 4096-byte prefix 4096 body bytes/op and
62347 allocated bytes/op; network block 0 requests/op and 10718 allocated
bytes/op. Full, headers-only and prefix each made 1 request/op. This is a
controlled loader benchmark, not a browser workload or a direct measurement of
wire bytes, RSS, throughput, or teardown retention. Transport buffering can
exceed the intentional body-read boundary.

Subsequent work added streaming Context body-byte reservations, logical image
pixel-work and shared response-body retention limits, plus known logical
avoided-body-read accounting. A second local 256 KiB benchmark (200 iterations
per mode) measured policy off at 400810 ns/929082 allocated bytes per op,
reportOnly at 362435 ns/931594 bytes, full policy at 325060 ns/930527 bytes,
headers-only at 448920 ns/48998 bytes, prefix at 466324 ns/62398 bytes,
and block at 10274 ns/10666 bytes. Timings are noisy: headers/prefix close
connections early and were slower than full in this local fixture, despite
lower allocations. They do not establish a throughput benefit.

A warm, controlled BrowserContext page with 20 no-store SVG images, ten
iterations per mode, observed approximately 21 requests/page with policy off
or reportOnly and 1 request/page with active image blocking. Shared retained
HTTP/CDP body storage averaged 1676 bytes before teardown in off/reportOnly
and 436 bytes with active blocking; it was zero after Context teardown in all
cases. Measured times were 81.5 ms off, 50.2 ms reportOnly and 52.7 ms active
per Context, but fixed scenario order and host noise make these unsuitable as
causal throughput comparisons. Allocations were approximately 7.37, 5.60 and
4.79 MB/op respectively, likewise subject to cold-order bias. These are
process-allocation/owned-storage measurements, not RSS or physical traffic.

The policy remains incomplete for production: exact transport wire-byte
measurement and enforcement, matched browser-workload policy-on/off RSS and
throughput comparisons, and comprehensive decoded/runtime memory accounting
are not complete. Known avoided body reads are not claimed as wire savings.
The public validator rejects a nonzero unsupported wire budget rather than
silently ignoring it. Do not interpret this checkpoint as the full
ResourcePolicy completion gate.

The synthetic-fulfillment admission fix used for the external AB replay passed
focused policy race tests and a subsequent full `go test ./... -count=1`
(browser package 382.093 s). The default no-policy test expectations remained
unchanged.

## 2026-09-22: persistent bootstrap reuse and deferred preparation

The persistent V8 bootstrap cache was keyed by the complete environment,
including `Time.WallOrigin`, which changes on every process launch. Each launch
therefore missed the disk cache, rebuilt an approximately 8.8 MB artifact and
waited for its durable write before opening the CDP listener. The key now omits
only this Page-owned clock value. A regression test gives a second Browser a
wall origin two hours later, verifies reuse of the same disk artifact, and
checks that restored `Date` and `performance.timeOrigin` use the second clock.

The CLI now opens the disk store before listening and prepares the bootstrap in
the background after opening the listener. A cached artifact can be loaded by
the first Page; a cold store is built for later Pages and process launches.
Browser-owned `PrepareBootstrap` remains available to callers that explicitly
need a ready snapshot before serving.

On the Obscura `obstacle-course` static stage, five fresh-process measurements
with a verified existing disk artifact gave median HTTP readiness **111.0 ms**
and completion **381.0 ms**, versus the earlier unchanged pre-listen path at
about **641 ms** and **790 ms**. An empty-store series gave medians of **115.8
ms** readiness and **396.6 ms** completion; its first host-cold sample was
654.8/1005.9 ms and is retained rather than silently discarded. These are
same-machine diagnostic
measurements, not a frozen suite baseline. A persistent-process nine-Page probe
kept later static Pages at approximately **14–15 ms** and DOM-build Pages at
**166–170 ms**. The frozen fast gate passed all six workload checks, static
concurrency waves at 10 and 25 Pages, and memory checks. Its warm medians were
DOM **276.8 ms**, static **28.3 ms**, React **84.2 ms**. Full browser, CDP and
textmetrics tests passed.

The other Obscura comparison gaps remain. In a 5000-row DOM probe, Mimic spends
roughly 61–79 ms creating detached elements and 50–56 ms setting text and
attributes; connected append is only about 11 ms. These synchronous DOM calls
return canonical nodes and expose immediate mutations, so their work cannot be
postponed as a group. In a 5000-node geometry probe, the first correct rect read
still takes roughly 300–383 ms, while repeated and irrelevant-attribute reads
take about 1–2 ms. Obscura's render-enabled first read was about 124–128 ms;
its no-render mode returned a fabricated width of 100 instead of the fixture's
123. Deferring Mimic's first layout read would change the synchronous result.
The system font catalog also remains eagerly warmed at Browser construction;
the earlier cold-start CPU profile attributed about 30 ms to that scan. Moving
it behind the listener would transfer that work to the first synchronous text
measurement if the background scan has not finished. No DOM, geometry or font
semantic path was changed in this batch.

## 2026-09-20: final Blitz production result

The [final production checkpoint](blitz-production-final-2026-09-20.md)
supersedes the checkpoint below. Blitz is unconditional for admitted Documents;
there is no engine environment switch. Five alternating unchanged Wikipedia
pairs measured **9648 → 6144 ms cold** and **5879 → 3977 ms warm**, paired
median reductions of **3489 ms (36.2%)** and **1854 ms (31.7%)**. Full browser,
race, controlled-chain, memory and zero-Wikipedia-document-fallback evidence is
archived with the report. Production code remains on `migration/blitz-producer`;
this main commit archives reports and receipts only.

## 2026-09-20: Blitz production migration checkpoint

[Detailed evidence](blitz-production-checkpoint-2026-09-20.md): full unchanged
Wikipedia, five alternating fresh-process cold/warm pairs, native batched
publication cold **9387 → 6010 ms (−36.0%)**, warm **6202 → 4430 ms (−28.6%)**.
The report preserves the failed unbatched matrix, stage displacement, memory,
binary hashes, controlled chains and the compatibility caveat. Native source
is on `migration/blitz-producer` (core `824cf0c`, browser integration `0e8bc43`);
later compatibility changes require a new measured binary. Old producer
performance work is stopped; legacy remains oracle and migration fallback.


## Playwright Wikipedia latency, current

Latest user scope: **stop diagnosis and plan real fixes**, warm first,
aspirational 2/2 s. The [implementation plan](wikipedia-production-plan.md)
prioritizes cheaper initial style/geometry computation, shared observation
results, then bounded dependency-correct reuse and secondary DOM read savings.
No production fix is applied. The final destructive combination reached five-pair
medians of 2251/2147 ms cold/warm against its paired destructive control
2717/2664 ms. It additionally bypasses geometry/scroll/occlusion and memoizes
Playwright queries; these results are not production performance or a guarantee
of what correct fixes can achieve. Patches and receipts are retained separately.

Earlier checkpoint:
Three paired runs of combined author-script suppression and stylesheet-cascade
suppression gave 4821/4383 ms cold/warm against 10138/6688 ms. Adding the previous
split-derived-state freeze in a separate matched comparison gave 4608/4172 ms
against 4790/4376 ms. All assertions passed, but these ablations deliberately
break browser semantics; they are not shippable optimizations. Cold approaches
warm, yet 2/2 remains unachieved. Bulk line shaping was flat warm; guarded
Taffy-first showed no material one-pair screen gain. Production changes were
removed and reproducible patches/receipts retained in `tools/performance/pocs`.
These results do not establish a persistent dependency-aware layout tree as the
single missing cause; first-build and repeated structural reads remain expensive.

The canonical handoff is [wikipedia-e2e-current.md](wikipedia-e2e-current.md).
It consolidates the September 15–19 investigation, including the ~10/6 s
cold/warm production target, correlated Runtime/gov8/host profiling, rejected
derived-state architecture, 13k-node actionability experiments, commit `58ca5f6`,
and the latest matched E2E result. Local actionability improved substantially,
but the complete Wikipedia workflow did not; future work must use the unchanged
Wikipedia E2E as an early go/no-go gate rather than treating microbenchmark wins
as completion.

The September 19 causal follow-up established fresh five-pair medians of 10004 ms
cold and 6547 ms warm and added a stage-attributed diagnostic twin with early
realm-preserving stop. A subsequent attribution audit found that nested owner
style/geometry execution lost the outer correlation ID and was absent from the
original deep trace. Lightweight host accounting then measured 1441 ms inside
two foreign owner observations in a 1530 ms warm callback. Five new paired E2E
trials disabling author stylesheet cascade improved medians from 11094/7061 ms
to 8196/5188 ms (-26.1%/-26.5%). This destructive experiment also changes layout,
so it does not isolate selector-matching CPU. Output-checked matching replay,
with zero verification mismatches, improved a separate five-pair control from
10411/7077 ms to 9302/6221 ms (-10.7%/-12.1%). Declaration-output replay yielded
10463/7038 ms to 9768/6388 ms (-6.6%/-9.2%), but carries larger tape/materialization
overhead; these are interventions, not exclusive CPU-time bounds. Warm runs
create new Pages, so first-use owner projection/layout construction remains
relevant without requiring repeated mutation invalidation as the explanation.
All experimental runtime edits were removed and saved as local reproducible
patches. No production optimization is authorized or retained; scope is diagnosis
and removable PoCs only. See the canonical handoff for raw tables and limitations.

The final stage-complete diagnostic attributes 4073 ms of Mimic excess versus
installed Chrome 153 to four stages: JavaScript first-visible (+1529 ms),
click/navigation (+849 ms), ECMAScript first-visible (+844 ms) and back (+851
ms). Repeated role resolution performs over 110k host crossings per operation,
but a callback read snapshot improved paired warm E2E only 2.7%. Click profiling
found redundant full hit/rect validation after Playwright had already established
the target; two unsafe trust-hint PoCs reduced click-navigation 924 -> 450 ms and
paired warm E2E 6940 -> 6577 ms (-5.2%). Exact replay of all 58 cross-realm owner
projections improved one cold/warm pair 10637/6749 -> 8747/5697 ms but moved
unprimed geometry into scrolling. Adaptive demand projection and Page-lifetime
text shaping were screened and did not materially improve E2E. The evidence now
supports a distributed cause: repeated first-use JavaScript style/cascade/layout/
AX/hit-test construction across new documents, plus native-vs-JS navigation and
bootstrap cost—not CDP transport, owner waiting, BFCache, or one cache miss.

The whole-chain scheduler audit sharpened that conclusion. Cold versus warm
diagnostic wall differed by 3716 ms; mutually exclusive scheduler intervals
explained 3321 ms, while foreground Runtime callbacks explained only 183 ms.
Nine cold IntersectionObserver-like DOM tasks spent 2778 ms repeatedly sampling
style and nine tables. Parse-only author scripts removed 36.1% cold but only
9.4% warm, proving author execution schedules the cold tail rather than paying
it in top-level evaluation. Image-epoch suppression, first-sample-only IO and
additive word shaping were low-single-digit or negative E2E results.

A final split-cache upper bound kept canonical DOM reads fresh while freezing
only post-load derived style/layout maps. It improved paired medians
9743/6418 -> 7946/5900 ms (-18.4%/-8.1%). Combining that destructive freeze
with exact matching replay reached 9746/6479 -> 7275/5258 ms
(-25.4%/-18.8%). All workload assertions passed, while focused mutation/font/
geometry tests correctly failed under the freeze. The result proves coarse
invalidation is a major cold amplifier but not the sole warm cause; even this
combined non-production upper bound misses the joint 20% gate. Production
runtime code was restored unchanged.

Pinned Chrome 152 back-navigation measurements also rejected BFCache as the
missing mechanism: the realm marker was absent, `pageshow.persisted` was not
true, and the navigation still reported `back_forward`. Mimic's fresh-realm
behavior is therefore aligned, although its `reload` navigation type is a
separate semantic defect. Completely deleting Mimic's back stage is only a 15%
warm upper bound and cannot independently satisfy the gate.

## WebAPI realm memory, 2026-09-14

The [realm-memory investigation](webapi-realm-memory-20260914.md) attributes the
large first-CDP step to materialization of the deferred Page realm, including its
V8 isolate and Chrome 152 WebAPI graph. Ten-Page diagnostics put marginal private
memory near 47 MiB for static and 51 MiB for React; explicit V8 collection reclaims
roughly 9--16 MiB/Page but costs about 6--9 ms/Page. Teardown releases all isolates,
so the observed issue is active-realm cost rather than an isolate leak.

An incompatible upper bound suggests about 8--9 MiB/realm is available in lazy
WebAPI publication. A descriptor-compatible JavaScript Proxy prototype recovered
only about 0.5 MiB/Page on static and 2.5 MiB/Page on React and cannot safely cover
existing handwritten prototypes without changing identity. It was rejected and no
production code remains. A future bounded proof may use gov8 native lazy data
properties and accessors; it must first prove snapshot ownership, exact reflection
and teardown behavior. The observations are diagnostic rather than a reportable
matched-revision comparison, as documented in the linked report.

## DOM, Fetch, and V8 lifetime package, 2026-09-14

The [measured campaign](optimization-campaign-20260914.md) is integrated. It removes
quadratic DOM child copying, redundant document/adoption crossings, temporary V8
roots, Fetch byte-array conversion, independent response-body copies and per-Page
bootstrap byte duplication. Rejected hybrid, snapshot, nursery, CSS and spill
prototypes were removed.

The final Windows Mimic/Chrome checkpoint passed 12/12 gates and 360/360 measured
single-page attempts. At 50 static Pages Mimic used 1.53 GiB RSS versus Chrome
3.98 GiB and delivered 3.32x throughput. Warm DOM execution is 96.58 ms versus
Chrome 29.63 ms, so Chrome parity remains open. A matched Linux control attributes
a 2.17x DOM execution improvement to this package. The independent Linux 100-Page
test now reclaims allocator high-water after teardown: recovered Mimic PSS is
368/374/566 MiB versus Chrome 647/751/696 MiB for static/CPU/React. Active Mimic
memory remains higher at 25.91/37.16/31.27 MiB per live Page versus Chrome
11.15/21.74/15.41 MiB, while throughput is 5.04x/3.87x/6.28x higher. Long-running
ownership tests show stable V8 roots and collected heap, and release the bounded body
store on Page close. See the campaign report and [published checkpoint](../../benchmark/runs/09-optimized-20260914/public-summary.md)
for measurement boundaries and raw evidence.

## Live site command stalls, 2026-09-14

The [CDP site investigation](../compatibility/cdp-site-stalls-20260914.md)
removes private-reflection recursion during frame imports and an owner-thread
deadlock during foreign microtask checkpoints. Lowe's, Macy's and Flyscoot
progress after those fixes; two observer compatibility fixes also restore
Flyscoot's application startup. These are correctness repairs supported by
local regressions, headful Chrome captures and live journeys.

Runnable tasks still delay same-Page commands: diagnostic departures took
9.23 s on Lowe's and 26.69 s on Macy's Sale, while browser and independent-Page
health probes remained responsive during observation. The report preserves
failed short-timeout recovery, eventual longer-timeout departure, source and
binary hashes, and external challenge/denial results. Live content and host
load were uncontrolled; no throughput, memory-retention or benchmark speedup
claim is made. Further work remains on long-task/navigation responsiveness.

## Snapshot hydration and task-boundary capture, 2026-09-11

The [snapshot investigation](snapshot-hydration-20260911.md) fixes inert lazy-load
observations, stylesheet lifetime/ordering, duplicated polyfill shadow export,
unsupported AVIF decoding and serial asset capture. It also separates width
resolution from recursive height work and makes navigation/capture cancellation
operate at explicit Page task boundaries. Live offline-checked captures include
YouTube thumbnails and Shorts, Twitch channel cards, and the Amazon storefront.
Individual totals were 24.2 s, 16.7 s and 14.9 s respectively, with uncontrolled
online content and host load; these are not faster-than-Chrome claims.

Relevant package suites passed. The first fast gate lost its process in a
25-Page wave for an unresolved reason; 500 diagnostic sessions and a complete
fresh-build repeat passed afterward. The linked report retains both outcomes,
the exact build hash, throughput/memory observations and remaining limitations.

## Frame bridge and bootstrap code reuse, 2026-09-10

The [implementation and measurements](bridge-bootstrap-20260910.md) reduce
the same iframe probe from the previous approximately 222 ms phase breakdown
to 90.5 ms (41.0 ms setup, 49.4 ms mixed operations). Remote operations and
encoding are batched on their owner; identity/shape descriptions now run in
JavaScript, with Go retention only on first export. Bootstrap reuses bounded,
immutable function code caches across independent isolates. Warm bootstrap
compile time is about 2.9 ms; its realm-local execution still takes about 37 ms.

The full suite, focused race checks and fresh-build fast gate pass. Host
crossings fall from roughly 2500 to 1500 per probe and persistent-handle growth
from 17101 to 6902. The two memory waves show 9–13 MiB higher private memory
after recovery than the previous gate; the bounded process code cache is an
intentional retention tradeoff, and the measurements do not isolate its share.
This is a reduction of bridge and compilation overhead, not the shared-isolate
rewrite or Chrome parity. Remaining actor/envelope costs and bootstrap object
construction are documented in the linked report. Frozen harnesses are unchanged.

## Remaining bridge cost and ownership alternatives, 2026-09-10

The [ownership breakdown](frame-bridge-ownership-20260910.md) measures about
69 ms iframe setup and 153 ms bridge execution for the existing 100-iteration
probe, using QPC medians after warm-up. About 4,609 actor dispatches, 2,500
host callbacks and 17,101 additional persistent handles are observed per loop.
A two-context/single-isolate Go+V8 experiment runs the same checksum kernel
in 0.147 ms, while preserving distinct constructors and child prototype identity.
It excludes browser DOM/security/lifecycle machinery and therefore does not
establish full iframe parity. With unchanged bootstrap, eliminating the loop
overhead would leave roughly 69–70 ms overall; Chrome-like whole-probe latency
also requires bootstrap work. An owner-batching candidate yielded only ~15%
and was withdrawn; its patch and measurements remain private. No production
ownership rewrite is retained from this experiment.

## Cross-realm reference description, 2026-09-10

The [task-profile investigation](../compatibility/task-profile-2026-09-10.md)
finds repeated V8 actor crossings in remote prototype, call and property
operations. One sampled 10.94-second task attributes about 10.18 seconds to
Mimic surface wrappers, including time waiting for remote isolates. A shared
100-iteration probe returns checksum 6050 in Chrome and Mimic; warm Mimic
executions fall from 492–535 ms to 192–197 ms when reference descriptions
execute in a single owner-side host invocation. System Chrome .83 takes
4.5–5.1 ms including CDP overhead. These are small diagnostic samples, not a
frozen performance checkpoint. There is substantial remaining bridge cost.

The live before/after profiles use different server programs and sampling
intervals, so their timing difference is not a controlled speedup measurement.
The post-change protected run still receives a repeated challenge; no complete
control-flow equivalence or successful passage is claimed. Private profiles,
source instrumentation and capture limitations are linked from the investigation.
Subsequent sequential controls without CPU sampling or host counters observe
a first flow interval of 15.454 s before and 6.962 s after. The same message
listener is installed in both. Different server programs still prevent a
control-flow equivalence claim; both controls receive a repeated challenge.
The fast gate passed on a freshly built, hash-verified binary, including all
six correctness workloads, concurrent waves and teardown/memory checks.
The complete browser suite and focused race checks also passed; see the
investigation for timings, commands and the initial suite deadline.

## Current evaluation criteria

The frozen Chrome 152 measurements are the reference. Absolute millisecond milestones are retired and are not stopping criteria. Every workload must report Mimic/Chrome ratios for creation, navigation and execution, together with attributed remaining overhead. Startup and Page creation should beat Chrome; navigation, DOM and CPU work should approach or beat its measured costs. Fixed memory, marginal Page memory and density should provide a substantial advantage, not merely parity.

Continue a measured path until its gap is small, profiling demonstrates an intentional architectural tradeoff or unavoidable backend boundary, or diminishing returns make another measured bottleneck materially more important. An expensive current implementation is not by itself proof of an unavoidable cost. Correctness remains mandatory. The fast gate is the iteration tool; the full frozen matrix is a milestone gate after a substantial group of changes.

**CORRECTION: runs `01-page-concurrency` and `02-dom-host` are invalid for optimization attribution.** The invocation used `--skip-build` without `--mimic`, so the frozen harness selected the pre-existing `.build/mimic-benchmark.exe` rather than the newly built `.build/mimic.exe`. The former embeds revision 2d6ae126 with modified=true. Tables and aggregate statements below for those two runs describe that stale binary, NOT the optimization commits. Source HEAD alone was insufficient provenance. Preserve all raw observations. Subsequent fast and milestone gates use explicit freshly built executables with per-launch SHA-256 verification, replacing the earlier per-class full-rerun plan. Diagnostic Go test profiles were built from their intended source and remain usable.

Frozen harness SHA-256: `ce1fce42fa9b9e03f105900601db4d6d7fa9b0d9cda51d5096322357277673e7`. Original `benchmark/results` is unchanged.

## 01: Page concurrency

Page-owned command locks replace the server-wide lock. Context bootstrap runs outside the registry lock; shared localStorage and permission delivery have their own synchronization. A network-barrier regression proves an independent Page can evaluate while navigation is blocked.

Full harness: `benchmark/runs/01-page-concurrency`; machine memory pressure stopped density growth at N=1. No N=50 throughput or marginal RAM conclusion is possible. Chrome cold DOM contains one `net::ERR_ABORTED`, retained without retry or exclusion.

Mimic warm medians (ms), mechanically extracted from the unchanged comparator:

| Workload | Metric | Frozen | After | Change |
|---|---|---:|---:|---:|
| static | session_create_ms | 118.28 | 85.87 | -27.4% |
| static | execution_ms | 2.82 | 2.97 | 5.5% |
| static | completion_ms | 127.51 | 90.26 | -29.2% |
| dom | session_create_ms | 95.33 | 78.78 | -17.4% |
| dom | execution_ms | 1560.32 | 1271.19 | -18.5% |
| dom | completion_ms | 1661.46 | 1341.98 | -19.2% |
| react | session_create_ms | 121.60 | 69.43 | -42.9% |
| react | execution_ms | 166.03 | 89.06 | -46.4% |
| react | completion_ms | 302.94 | 168.72 | -44.3% |

These single-session deltas are observations, not causal claims for concurrency: memory pressure and background load differed. Correctness: all 12 browser/workload gates passed; Go suite and CDP race suite passed. Initial browser race run passed (396.302s); subsequent storage/permission changes require the final full rerun.

## 02: DOM host boundary

Measured before changes: 120,035 API trace crossings (301.6 ms inclusive host time), six queryAllWithin calls (149.8 ms), 27,000 attribute writes (91.9 ms), 18,006 parent projections (85.7 ms), 9,001 insertion calls (78.6 ms). The sampled full DOM phase was 1,252.9 ms; these nested inclusive costs must not be added to total CPU. Go CPU attributes 37% flat to cgocall and substantial scanning/GC; native stacks are not resolved by Go pprof. Full allocation sampling attributed 857.7 MiB to gov8 callTextFn (its fixed-capacity string buffer), not to live DOM text.

Changes: deduplicate API traces before crossing into Go; borrow arguments only for explicitly synchronous hosts; retain callbacks for asynchronous APIs; use exact-length reads for strings with ToString fallback for other types; batch plain record arrays through native JSONParse; perform canonical ancestry traversal in Go; pass only node IDs for non-resource insertion while preserving resource scheduling.

Diagnostic replay: 1,252.9 ms -> 512–543 ms, persistent handles 664,483 -> 10,282, crossings 231,528 (including wrapper instrumentation) -> 99,523. The diagnostic wrapper counter itself creates 9,005 roots; ordinary builds do not install it. This is diagnostic attribution, not a frozen-harness result. Empty collections and byte arrays preserve recursive marshaling semantics. Baseline JSON/fixture correctness is unchanged.

Memory attribution before DOM changes: ten live static Pages use 362.1 MiB V8 physical heap, 10.1 MiB Go heap, 468.4 MiB process RSS; V8 external memory is zero and reported malloced memory totals 2.5 MiB. Static RSS slope from five to ten Pages is 37.8 MiB/Page in this diagnostic sequence. After close and Go scavenging, RSS remains 360.2 MiB despite Go heap 7.4 MiB and all isolates disposed. This is evidence of native/allocator retention, not evidence that Go retains all Page objects. RSS residual cannot be labeled exact native ownership: mapped code, stacks and allocator pools contribute. React follows static in this diagnostic and benefits from its allocator history; use the immutable density benchmark for comparative marginal RAM.

Full unchanged harness after class 02: `benchmark/runs/02-dom-host`, comparator `benchmark/runs/02-comparison.json`; all 12 correctness gates passed. Warm DOM execution median is 1123.71 ms; completion static 101.49 ms, DOM 1196.34 ms, React 173.36 ms. Static N=25 throughput 7.71/s; N=50 stopped on sustained system paging. React N=50 throughput 3.70/s. OLS marginal RSS: static 62.34 MiB, React 56.43 MiB. These stale-binary observations do not establish optimization progress. Chrome cold static has one retained ERR_ABORTED. Diagnostic replay lacks the CGO linkage of the production executable; its timings must not substitute for these measurements.

## 03: Initial Page and immutable bootstrap data

Production-linked diagnostic replay disproves CGO linkage as the cause of the benchmark/replay discrepancy: DOM remains approximately 492-496 ms when linked with QuickJS/CGO. The frozen CDP harness remains authoritative. Native code accounts for 60.45% flat CPU in this replay; unresolved native stacks prevent finer attribution from Go alone.

Before this class, warm blank creation takes 64 ms and immediately following navigation takes 71 ms. Compilation of the generated bootstrap takes 26-31 ms and execution 31-36 ms. Each initial blank document gets a separate full isolate which first navigation replaces. The initial document now owns its DOM, URL, identity and scheduler immediately; engine creation occurs on first engine observation. Navigation before observation avoids constructing the unused isolate. Independent mutable realms are preserved and tested.

Immutable source and exposure JSON are shared in a bounded Go cache. Generated metadata is parsed as JSON rather than compiled as JavaScript object literals; generated closures capture names instead of entire metadata records. Diagnostic warm navigation is 47.62 ms, bootstrap compilation 7.78 ms, execution 35.93 ms; initial creation is below 1 ms. Static live N=10 RSS falls from 466.8 to 399.1 MiB. Diagnostic V8 collection reduces those figures to 225.3 and 169.4 MiB respectively, showing much of the allocation footprint is collectible; this forced collection is NOT enabled in normal runtime or benchmark.

A compilation-byte cache experiment was discarded: pinned gov8 CreateCodeCache retries a native buffer after the read/delete API has freed it; its CompileCached also dereferences a nil Origin. No unsafe cache path or dependency replacement is included.

Validation: full ordinary Go suite passed (browser 58.043s); independent initial blank regression passed. Full race and frozen benchmark results are recorded after completion. Repository publication audit currently flags a machine-local executable path in the unmodified new benchmark raw shutdown exception; raw evidence has not been rewritten to conceal that failure.

## 04: One CDP event-loop pump per Page

Before: 16 WebSocket debugger connections advance one Page clock by 2309.238 ms during 150.387 ms wall time (15.36x). Mutex/block profiles are retained in `.build/pump-{mutex,block}.pprof`. The session-owned timer creates both redundant work and incorrect observable time.

After: Page-owned pumps shared by all connections, canceled when the target or server closes. Same probe: 150.352 ms Page time / 150.210 ms wall time (1.00x). Asynchronous navigation also holds its session binding lock, preventing a rebind from routing an ongoing navigation into another session. Ordinary and race CDP suites pass, including independent Page navigation and clock amplification regression. The full bootstrap race suite also passed; frozen benchmark evidence follows in explicitly version-verified runs.

## Verified rerun 01

Source `f1a526fc134be4cc009638ee2d1206363132f68c`, executable SHA-256 `7f55ba9d2729dcf7dc439d942a8fb488b60a15c6866441485038826feb7bedb3`. Clean detached source checkout and Go module path verified before building; the exact executable was supplied with `--mimic`. Go 1.26.1 VCS discovery recognizes `.git` directories but not worktree `.git` files, so the source and output hash are verified separately. All 12 gates pass. Static N=50 throughput: 5.4072 -> 8.5934 sessions/s (+58.9%); CPU N=50: 2.9174 -> 7.9820 (+173.6%). Static N=50 RSS: 3091.7 -> 2810.2 MiB. See `benchmark/runs/01-verified-page-concurrency` and `01-verified-comparison.json`.

| Warm workload | Metric | Frozen ms | Verified ms |
|---|---|---:|---:|
| static | session_create_ms | 118.28 | 70.45 |
| static | execution_ms | 2.82 | 1.94 |
| static | completion_ms | 127.51 | 73.00 |
| dom | session_create_ms | 95.33 | 91.56 |
| dom | execution_ms | 1560.32 | 1703.89 |
| dom | completion_ms | 1661.46 | 1803.17 |
| react | session_create_ms | 121.60 | 70.88 |
| react | execution_ms | 166.03 | 96.41 |
| react | completion_ms | 302.94 | 178.77 |


## Fast iteration gate and class 05: DOM identity transport

Full matrix runs are milestone gates, not per-change iterations. The in-progress second verified full run was stopped before completion; its completed raw samples remain in `benchmark/runs/02-verified-dom-host`, without a full-run completion claim. Runs 03 and 04 were not launched. The frozen harness and original raw baseline remain unchanged.

`tools/performance/fast_gate.py` builds an executable into a fresh output directory and checks its SHA-256 immediately before every process launch. All six semantic workloads are mandatory; warm DOM/static/React use five measured iterations, static N=10/N=25 use three measured waves, with excluded warm-ups. Static and React memory are measured with ten live pages and after teardown/recovery. See `fast-gates/*.json` for all rows and executable provenance.

Before class 05, the attributed Go allocation profile contains nodeData record projection, recursive JSON conversion and per-property native calls; queryAllWithin takes 63.24 ms for six calls. Query results now transport canonical node IDs and reuse existing wrappers, loading a full record only for a new wrapper. Canonical attributes remain visible after external Go mutations. Immutable realm token reads are hoisted once; plain record conversion is batched while retaining rejection of unsupported Go values.

| Fast gate metric | Before class 05 | After class 05 |
|---|---:|---:|
| DOM execution median ms | 511.95 | 382.50 |
| DOM completion median ms | 564.92 | 436.57 |
| Static completion median ms | 54.45 | 54.19 |
| React completion median ms | 118.96 | 123.22 |
| Static marginal RSS MiB/page, N=10 | 39.16 | 39.40 |
| React marginal RSS MiB/page, N=10 | 42.67 | 43.08 |

Before executable SHA-256: `029caf9f8e83fde87e3531cd9b50c78d866ceb8603ead9545bbe290c682b0910`; after: `71bdc64069292b41d8f09503d5c0971fe5d9f0bb1adf98b6b042f0e231cc281c`. DOM execution improves 25.3%; no memory or React gain is claimed. Full ordinary correctness passed on retry. The first run timed out in TestPerformanceObserverReceivesFinalizedNavigationEntry; isolated replay passed, and the complete fresh retry passed. Both logs are retained locally.

The new V8 Inspector profile attributes about 120-128 ms of DOM execution to fragment removeChild processing; bootstrap applyTargetExposure takes about 21 ms of a 41 ms execution phase. These are the next measured architectural costs. Go allocation sampling after class 05 totals 314 MiB over three DOM replays plus diagnostics (previous comparable replay 624 MiB, with an additional three obsolete blank bootstraps). Do not attribute that entire difference to class 05. The V8 heap snapshot after collection has approximately 15.2 MiB of live nodes: arrays 4.34, strings 3.60 (bootstrap source 2.69), objects 3.29, closures 1.63 MiB. This is not resident process memory; the gap to physical/resident allocation remains material. Diagnostic replay timings include profiler overhead and are separate from gate timings.


## 06: Linear DocumentFragment membership transfer

V8 CPU sampling identified 120-128 ms in removeChild when moving 3,000 fragment children: repeated front removal shifted the remainder each time. Fragment insertion now drains membership once and clears synthetic parent links before inserting the ordered children. Existing append/insert-before, identity, ownerDocument, comment and cycle regressions pass for Goja and V8. Full ordinary correctness passes (browser 58.867 s).

The fast gate records DOM execution 382.50 -> 333.43 ms (-12.8%). This run had substantial variance (DOM completion 301.60 to 494.85 ms across five iterations), and static also slowed from 54.19 to 69.54 ms; therefore a larger causal speedup is not claimed. V8 sampling confirms the former removeChild hotspot disappears. All raw samples remain in `fast-gates/fast-gate-linear-fragment.json`; native samples are in `linear-fragment-native-phases.json`. SHA-256 is in each gate's build receipt and launch records.


## 07: Per-isolate nursery sizing

Per-space V8 diagnostics identify 16 MiB physical new-space allocation on static Pages with only 1.71 MiB used; React has the same 16 MiB footprint with 1.35 MiB used. The engine's nursery growth is multiplied by the independent Page isolates. A diagnostic collection costs approximately 7-8 ms/page and releases about 23 MiB/page, but no forced collection is inserted into benchmark measurement.

Isolate creation now caps the young generation at 4 MiB through V8 CreateParams. Old-generation limits retain V8 defaults; Page isolation and event-loop ownership are unchanged. All ordinary correctness tests pass (browser 77.609 s), and all six frozen semantic workloads pass in the fast gate.

Static marginal RSS N=10: 39.40 -> 28.84 MiB/page; React: 43.08 -> 35.67. Corresponding private bytes: 28.90 and 36.01 MiB/page. These are measured reductions; Chrome-relative memory advantage still requires the full milestone comparison. Residual RSS after ten closed pages is still 207.50 MiB static / 261.76 MiB React above process readiness, so teardown remains unresolved. DOM execution median is 281.16 ms, static completion 57.72 ms and React completion 113.38 ms. Host-machine variance precludes attributing all timing changes to nursery sizing. Evidence is in `fast-gates/fast-gate-page-nursery.json` and `nursery-{before,after}-spaces.json`.


## Correctness gate finding: zero-duration navigation completion

The earlier intermittent observer timeout reproduced in 20 isolated repetitions. Trace shows a fast local navigation with zero transport duration; the observer deduplication key used duration to distinguish initial and finalized entries, so a zero-duration completed load was indistinguishable from its initial entry. This is a lifecycle-state defect, not a reason to inflate measured time. The private observer notifier now receives explicit load-finalized state, and its navigation key includes that state. No public Web API is added. The test accepts a nonnegative duration (zero is legitimate at timer resolution), has a bounded wait, and a deterministic zero-duration finalization regression covers the failure. Twenty repetitions pass after the fix.


## 08: Release unused V8 pages at isolate teardown

Before disposal, after all adapter roots and contexts have been released, a V8 low-memory notification lets the engine reclaim empty heap pages. The measured residual RSS above readiness after ten closed Pages falls from 207.50 to 68.66 MiB (static) and 261.76 to 88.10 MiB (React). This residual includes shared process/allocator state and Go memory, not just live Page objects. Live marginal memory stays at 29.10 / 35.45 MiB per Page.

The tradeoff is explicit: median teardown within the N=10 memory wave rises from 6.42 to 20.46 ms static and 7.54 to 26.40 ms React; static N=25 median throughput falls from 80.39 to 62.37 sessions/s in these samples. Warm execution/completion excludes teardown by the unchanged harness definition, while wave throughput includes it. There is no collection added by the benchmark: this is the production isolate lifecycle policy and its cost is included where applicable. Fast gate DOM execution is 258.06 ms, static completion 58.00 ms, React completion 109.01 ms. See `fast-gates/fast-gate-release-isolate.json` for SHA-256 and all raw rows.

After repairing the zero-duration observer correctness defect, the full race-enabled correctness suite passes (browser 377.989 s, CDP 2.797 s). This validation precedes the separate classList host batching change.


## 09: Canonical DOMTokenList toggle in one host operation

Before: classList toggle repeatedly exported and parsed the same attribute via getAttribute/setAttribute; the profile attributed 51.39 ms to all attribute writes, 31.99 ms to reads, and 33-47 ms of V8 samples to domTokens. The private toggle host now performs one canonical read/modify/write under the DOM mutex. There is no stale JS value cache. Existing force behavior and whitespace/deduplication semantics are retained, with explicit Goja/V8 tests including external Go attribute mutation and Unicode whitespace.

Fast gate DOM execution improves 258.06 -> 197.37 ms (-23.5%); static completion is 59.02 ms and React 111.23 ms. All six workload result gates and the full ordinary correctness suite pass (browser 62.332 s). Native attribution now shows 18,000 setAttribute calls / 25.20 ms and 12,000 toggleToken calls / 22.07 ms; the former 21,000 repeated attribute reads disappear. The next measured cost is 9,001 node creation hosts / 50.16 ms. Raw timing and source/executable hashes are in `fast-gates/fast-gate-token-toggle.json`; the diagnostic profile is `token-toggle-native-phases.json`.


## 10: Return canonical identity for newly created nodes

Before this class, create/createText/createComment consume 50.16 ms across 9,001 calls in the attributed DOM replay. The JS caller already owns the initial text/type/empty attribute state, but each call serializes that state back through a full Go record. New text/comment nodes and ASCII HTML names now return canonical IDs and construct their known initial JS record locally. Non-ASCII names preserve Go's original normalization path; UTF-16 surrogate text takes the canonical record path to preserve bridge string conversion. Go owns each node immediately, including detached nodes. Goja/V8 Unicode, identity and insertion regressions pass, as does the full ordinary suite (browser 53.165 s).

Fast gate DOM execution: 197.37 -> 156.11 ms (-20.9%); static completion 58.05 ms; React completion 103.97 ms. A slow DOM sample remains in the raw series (completion 325.42 ms); no sample was removed. Compared with frozen Mimic DOM execution 1560.32 ms this is approximately 10x; compared with frozen Chrome execution 30.326 ms it remains about 5.15x slower and is not a stopping point. The final full milestone matrix is required to confirm results.

The final diagnostic replay records 69,503 crossings including 9,005 diagnostic wrapper hooks, versus 231,528 originally with the same hooks. Persistent handles after DOM are 10,284 including those diagnostic hooks (approximately 1,279 without them), versus over 655,000 normal retained roots originally. Go sampled allocation totals 216 MiB over three DOM replays plus bootstrap and profiling; native/FFI execution remains dominant at 55% flat Go CPU samples. These profiling numbers are attribution, not benchmark timings.

The N=50 parallel creation/navigation diagnostic also collected CPU/allocation/mutex/block profiles. The principal aggregate mutex waits are initial immutable Surface OnceValue publication (~1.98 s across all waiters) and gov8 isolate construction bookkeeping (~0.78 s), not a lock held across independent Page CDP evaluations. Aggregate waits must not be read as wall-clock delay. Per-Page phase records are in `final-parallel-phases.json`; local profiles are `.build/final-parallel-*.pprof`. No isolation boundary was changed.

## Milestone 03: frozen full matrix, classes 01–10

Source `d69cff971c9aba22f28889ded904caff392ed8f1`; executable SHA-256 `967df4b8677f545b38ff9f5c74e7c607e0c6b7deadbc6fbceff0fda557461d68`. The external milestone wrapper built the executable and verified both executable hashes before every launch. The immutable comparator accepted harness, fixture, Chrome binary and recorded environment equality. Raw evidence: `benchmark/runs/03-architecture-milestone`; deltas: `benchmark/runs/03-architecture-comparison.json`.

All twelve system/workload correctness gates pass. React N=100 was stopped during the excluded warmup for sustained system paging in both systems; it is not a successful density observation. All other recorded levels complete. This matrix predates the callback-registry teardown correction.

Warm medians (20 measured samples each); ratios use the original frozen Chrome 152, not this run’s Chrome:

| Workload | Mimic navigation ms / Chrome ratio | Mimic execution ms / Chrome ratio | Mimic completion ms / Chrome ratio |
|---|---:|---:|---:|
| static | 53.10 / 2.31× | 1.04 / 0.21× | 54.22 / 1.94× |
| cpu | 53.53 / 2.25× | 45.38 / 1.56× | 98.79 / 1.88× |
| dom | 54.08 / 2.41× | 153.50 / 5.06× | 207.32 / 3.88× |
| async | 53.98 / 2.07× | 48.37 / 1.61× | 102.03 / 1.82× |
| react | 106.17 / 4.31× | 87.83 / 3.56× | 194.87 / 3.97× |
| wasm | 64.49 / 2.71× | 9.28 / 1.81× | 73.88 / 2.58× |

DOM execution improves 1560.32 → 153.50 ms (10.17×) but remains 5.06× Chrome. Profiles attribute the remaining cost primarily to synchronous DOM host crossings/native calls; attribute mutation is the largest remaining host family. Navigation spends about 8 ms compiling and 35–49 ms installing per-isolate bootstrap; exposure/prototype normalization is its largest measured phase. These are implementation costs still eligible for optimization. Static execution is already cheaper than Chrome. CPU and Wasm need dedicated native workload profiles to separate engine work from embedder overhead; async includes timers/worker/event-loop work and requires phase attribution.

React’s full warm median is worse than the short gate: execution rises from roughly 40 ms to 85–99 ms after the first few iterations, while RSS grows. A later isolated diagnostic reproduces memory growth but not the sustained doubling in execution cost; therefore memory retention alone must not be asserted to explain the timing shift. The raw slow samples remain included.

Static N=50 throughput is 75.11 sessions/s versus frozen Chrome 25.93 (2.90×); CPU 32.52 versus 27.72 (1.17×); React 30.78 versus 14.50 (2.12×). Static N=100 falls to 10.28 versus Chrome 20.95 (0.49×): four measured waves have about 10 extra seconds after the workload completion window, indicating a teardown/connection-tail investigation rather than slower DOM execution. This remains unresolved.

Fitted marginal RSS: static 24.47 MiB/Page versus frozen Chrome 59.46 (0.41×), CPU 26.12 versus 73.87 (0.35×), React 37.38 versus 72.27 (0.52×; current fit only through N=50). Linear-fit intercepts are 82.30/145.12/90.66 MiB respectively; these are modeled intercepts, not directly measured process readiness. Warm Page creation medians span 2.12–14.04 ms, all below the corresponding frozen Chrome 37.86–43.60 ms. Calibrated CDP readiness is 231.84 ms versus frozen Chrome 279.37 (0.83×); startup remains an optimization candidate because that advantage is modest.


## 11: Release Go host callback registrations at isolate disposal

A 20-Page React replay in one Context reproduces monotonic retained memory. The post-GC heap contains closed DOM nodes, response bodies and semantic trace records. gov8 explicitly requires `ReleaseIsolateHostState` on the owner thread before `Isolate.Close`; Mimic omitted it. The global callback registry therefore rooted every callback closure and its closed Realm. A weak-reference regression fails before the fix and passes after it, directly proving callback capture release. Cleanup now follows final native collection and precedes native isolate disposal.

The same diagnostic replay reduces post-GC live Go heap 32.33 → 12.45 MiB and RSS 119.91 → 84.23 MiB. First/last post-close RSS changes from 76.30/119.74 to 75.25/84.74 MiB. Ordinary execution median is 41.76 → 35.85 ms in this diagnostic, but the full-matrix sustained React timing doubling was not reproduced and is not declared solved. The fast gate is essentially unchanged for single-workload timing: DOM 156.11 → 162.74 ms, static completion 58.05 → 59.11 ms, React completion 103.97 → 106.80 ms. Relative to frozen Chrome, execution is 5.37× DOM / 0.23× static / 1.69× React. N=25 static throughput is 62.71 → 74.97 sessions/s; N=10 is 53.46 → 42.50, so a blanket throughput gain is not claimed.

Immediate ten-Page RSS retention in the fast gate does not improve: static residual 64.37 → 65.68 MiB, React 90.21 → 93.21 MiB. The benchmark does not force Go GC, and freed Go objects need not return allocator pages immediately. This change repairs indefinite ownership retention, not all allocator high-water memory. Further teardown/allocator attribution remains necessary.

All ordinary correctness passes (browser 87.783 s), including repeated owner-thread disposal/cancellation tests and the capture regression. All six frozen workload semantic gates pass. Fast executable SHA-256: `bfbac8a2a0db5b13fd663109c529bec3f3f2cbc5998513b634c21fdbc4f15d2d`. Diagnostic executable hashes are `31a2e6b5e75e0e091530da8e0202ed64e2d96ef3dd50a322f9ca416b4fb2f757` before and `400926c52d61fad9c790195ff089501c3b10003ef765e07d0c01a9dd78ef7b5d` after; each was verified immediately before launch. Phase evidence is `react-long-{before,after}-phases.json`; CPU/heap/allocation/mutex/block/goroutine profiles remain in `.build/perf-react-long` and `.build/react-long-*.pprof`.


## 12: Separate immutable catalog data from retained executable source

Before this class, V8 retains the approximately 1 MiB escaped generated catalog literal as part of every bootstrap script. Detailed static profiling attributes bootstrap costs to generated binding installation (~11 ms), exposure JSON/global setup (~5 ms), prototype normalization (~6.2 ms), native marking (~5.3 ms) and prototype ownership reconciliation (~5.3 ms), plus ~9 ms compilation. The generated catalog is now an explicit, deterministic JSON artifact embedded once in Go and delivered through a private host during bootstrap. Window and Worker use the selected bundle's catalog; JavaScript parses independent per-realm objects. Mutable prototypes/objects and isolate boundaries remain unchanged.

Static diagnostic navigation median falls 65.24 → 58.65 ms. At navigation, V8 used heap falls 18,151,632 → 15,350,320 bytes and physical heap 23,228,416 → 20,193,280 bytes. This is live heap evidence, without a forced collection in the benchmark. Fast gate marginal RSS decreases 28.81 → 26.44 MiB/static Page and 35.75 → 33.02 MiB/React Page. Immediate residual RSS remains 64.18 / 89.41 MiB; allocator retention is not solved by separating source data.

Fast gate DOM execution 162.74 → 142.89 ms (4.71× frozen Chrome), static completion 59.11 → 56.78 ms (2.03×), React completion 106.80 → 100.86 ms (2.05×). Static N=25 throughput 74.97 → 69.54 sessions/s, N=10 42.50 → 52.15; no general throughput gain is asserted. The full ordinary correctness suite passes (browser 85.584 s), as does a Goja/V8 regression proving nested catalog objects do not cross Page boundaries. Offline generation/hashes pass; the retained upstream IDL, CDP and exposure inputs are unchanged. Executable SHA-256: `4956f8dbfb00f5856fafbf4348a5d12f946cdc367bfe1d531a7e0014657d0cac`. Raw gate is `fast-gates/fast-gate-catalog.json`.

A separate safe FunctionCodeCache experiment reduced bootstrap compile time roughly 9 → 3 ms but did not reduce installation cost (43–52 ms); end-to-end navigation only improved a few milliseconds and cold cache production added 5.7 ms. It was removed, prioritizing the larger retained-source and installation costs. This used the safe Function cache API, not the unsafe Script cache retry described earlier. No cache implementation remains in production.

The N=100 static close diagnostic (`.build/profile-close-100/raw.json`, executable SHA-256 `54ca9e42680689566543533535afcf20a1a2b8db9bdbb5216829c27ce9a27291`) did not reproduce the full milestone's ten-second tail: three waves completed in 1.48/1.25/1.26 s; maximum observed individual CDP close was 39.84 ms. The tail's root cause remains unproven and must be monitored in later milestones, not erased from the previous result.


## Class 13: bounded transient callback argument storage

Fresh profiles replace the preceding attribution. Before this change, the 20-Page DOM replay without diagnostic hooks costs 155.16 ms and allocates 48.45 MiB Go bytes per execution. The detailed replay costs 182.90 ms: instrumentation is material and its wall time is not a benchmark result. The allocation profile assigns 41.49% of total allocated bytes to adapter callback argument construction. Conversion is called 153,112 times per diagnostic DOM execution, taking 48.46 ms inclusive of its diagnostic timer. These costs must not be added to inclusive host timings.

Synchronous transient callbacks now reuse bounded, per-adapter scratch frames (up to eight frames, eight arguments each). Nested calls check out distinct frames; every frame is cleared after result marshalling. Larger calls and persistent callbacks keep their original allocation/lifetime contract. No DOM wrapper, mutable prototype or isolate is shared. Reentrant callbacks, Unicode return values, persistent callbacks, and cleared scratch roots have regression coverage. The ordinary suite passes, including browser tests in 69.774 s.

The matched 20-Page replay drops to 139.72 ms (-10.0%) and 29.82 MiB allocated Go bytes (-38.5%). The unchanged fast gate gives DOM 140.11 ms versus the preceding gate's 142.89 ms (-1.9%, **not** an order-of-magnitude latency improvement). Its value is the large reduction in allocation traffic without a density regression. Static completion 53.92 ms (1.93× frozen Chrome); React execution 41.34 ms (1.67×), completion 102.48 ms (2.09×); DOM execution remains 4.62× Chrome. Live marginal RSS is 26.33 MiB/static Page and 32.82 MiB/React Page versus 26.44/33.02 before. N=10/N=25 static median throughput is 54.06/71.24 sessions/s versus 52.15/69.54. All six semantic gates pass; every launch is hash verified. Gate executable SHA-256: `1fc3bcabe51492c62bc46c750f73fbec2d62a5b002ea544bc49263337263aed8`.

Fresh evidence is retained under `dom-current` and `fast-gates/fast-gate-transient-frames.json`. Local Go CPU/allocation/mutex/block/goroutine and native CPU profiles are `.build/dom-fresh-*.pprof`, `.build/perf-dom-fresh-detailed`, `.build/dom-frames-*.pprof`, `.build/perf-dom-attribution`, and `.build/perf-dom-go-only`.

### Current DOM attribution and limitations

* **Host boundary / conversion:** dominant. The current backend calibration performs 60,000 empty transient host calls in 19.82 ms, and the same calls exporting a number and two strings in 71.02 ms. This is an independent boundary calibration, not a frozen workload measurement or a subtraction-based prediction. Go CPU samples also place native calls and callback dispatch first. The SDK's string extraction checks type, computes UTF-8 length, writes UTF-8, and copies bytes into a Go string; generic export adds another type check. Windows DLL calls and Go callback transitions remain part of this backend boundary. Canonical Go DOM requires synchronous host mutation; moving DOM ownership would change an intentional architecture boundary. Neither all 140 ms nor every SDK call is proven unavoidable.
* **Wrapper identity / mutation bookkeeping:** the fresh native baseline samples average about 7.04 ms in `wrap`, 2.96 ms in `observe`, 5.06 ms in Proxy `get`, and 3.92 ms in insertion steps per replay. These are sampled self times, not inclusive cost or exact uninstrumented wall time. Canonical wrapper identity remains intact. The earlier insertion experiment was saved outside production and reverted before this new baseline; it is not an accepted optimization.
* **Selector/tree traversal:** six `queryAllWithin` calls total roughly 4.3–4.5 ms inclusive in the detailed current replay. `elementChildren` executes 6,002 times. This is a smaller cost than conversion/host dispatch, despite the traversal count.
* **Go mutation bodies:** detailed callback bodies total 78.25 ms, including 56.68 ms of conversion instrumentation; this is not 78 ms of tree mutation. Argument framing and return handling measure 11.84/14.20 ms with detailed timers. Subtracting these overlapping, perturbed medians does not yield a precise additive decomposition.
* **Tracing/diagnostics:** native `recordAPIAccess` self time is about 6.99 ms per fresh baseline replay; Proxy work overlaps the tracing path. Optional wrapper counting itself adds 9,005 host calls. The control/detailed replay difference (~28 ms before scratch reuse) demonstrates substantial profiling perturbation; normal gates disable it.
* **V8 GC:** native baseline sampling attributes about 7.29 ms/replay to GC. **Go GC:** execution phase counters record a median 0 ms stop-the-world pause; this does not imply zero concurrent marking or allocation-assist CPU. CPU samples include allocation and GC work; the allocation traffic reduction is independently measured.
* **Scheduler/tasks:** aggregate block time is mostly the calling goroutine waiting for its Page's owner to finish execution, not an extra delay to add to DOM time. Mutex contention is primarily Go runtime/GC, with no evidence of a DOM-wide Page serialization bottleneck in this replay. Native profiler idle/setup samples are excluded from hotspot interpretation.

A 100-Page sequential diagnostic (no forced GC between Pages) keeps six goroutines after each close and two after the final collection. Static closed RSS grows 60.61 → 78.51 MiB, React 67.31 → 81.49 MiB; Go live heap fluctuates rather than accumulating one Page per close. This is allocator/process retention, not zero retention. React execution nevertheless rises from a first-ten median 32.44 ms to last-ten 75.92 ms; that timing regression requires fresh edge profiles and must not be dismissed as solved by the earlier callback-root fix.


## Class 14: explicit latency QoS for Page owner threads

The 100-Page React regression is now attributed with fresh measurements. Native edge profiles show the same heap (~20.0 MiB used), external memory (zero), allocator usage (524,432 bytes), and similar work, but approximately doubled costs across unrelated JS and native functions. OS topology identifies logical CPUs 0–15 as performance cores and 16–27 as efficiency cores on this i7-14700KF. Sampling the current processor every 1,024 host calls shows 60/60 samples on performance cores for each first-ten group; after ~30 Pages, 60/60 samples are on efficiency cores. Execution changes 32–33 → 75–82 ms. These samples are inside execution, not just the core on which the final diagnostic happened.

An opt-in diagnostic setting the thread's execution-speed QoS to latency-sensitive restores 32–36 ms across all 100 Pages, with 600/600 performance-core samples. The production change applies the same Windows scheduling hint to Page owner threads. It neither pins CPUs nor raises scheduler priority, does not change process-wide policy, honors an existing explicit thread policy, and restores the original state before V8 unlocks the OS thread. Unsupported OS queries leave the default behavior intact. Idle Page threads remain blocked. This intentionally prioritizes latency over Windows' automatic background energy-saving classification; energy consumption was not measured and no energy-efficiency claim is made.

Production validation, without the diagnostic policy override: React first/last-ten execution 33.44/36.25 ms, overall median 33.67 ms over 100 Pages (1.36× frozen Chrome's 24.692 ms, noting this is a diagnostic replay). DOM first/last-ten 142.57/138.76 ms, overall 144.38 ms (4.76× Chrome); 5,883 performance-core and 17 efficiency-core samples. The large React long-run regression is eliminated without claiming the DOM backend boundary is eliminated.

The unchanged short gate passes all semantic workloads. Static marginal RSS 26.24 MiB, React 32.74 MiB; static N=10/N=25 throughput 53.67/70.64 sessions/s, essentially unchanged from 54.06/71.24. Short-gate DOM execution regresses 140.11 → 152.11 ms; retain this result, do not replace it with the longer diagnostic. Static completion 52.63 ms, React execution 39.84 ms, completion 104.49 ms. Source and raw launch hashes are in `fast-gates/fast-gate-page-qos.json`; executable SHA-256 `8b42fc83cb03488ad8de95244b7788faaab004cc09ff3f0bcd5a26a76740c73c`.

All ordinary tests pass (browser 87.283 s); engine race checks pass. A Windows regression verifies active QoS and exact restoration on the same OS thread. Long-run evidence and CPU topology are preserved in `dom-current`. A single new full frozen matrix follows this measured subsystem milestone. Startup remains deprioritized: internal readiness and the frozen external readiness probe measure different phases; no readiness workaround was added.


## Milestone 04: full frozen matrix after classes 11–14

The full run completed once, including every N=100 level for both systems, with all twelve semantic gates and every measured wave successful. Source is clean commit `082a22657dd7117affcb4fa3ab8b57a9c7b04359`; executable SHA-256 `3d5c896c4f710e93ab876a40f46b49c6ac3b6ac85246e192907e02dc9e708a46`. Every process launch verifies the recorded executable hash. The immutable comparator accepts the original harness, fixture and Chrome provenance. Raw evidence is `benchmark/runs/04-dom-long-run-milestone`; comparison is `benchmark/runs/04-dom-long-run-comparison.json`. The subsequent edits only extend diagnostic test serving/collection, not production behavior.

Warm medians, 20 samples each. Ratios use the **original frozen Chrome 152**, not this run's Chrome. Each cell is Mimic milliseconds / Mimic-to-Chrome ratio.

| Workload | Page creation | Navigation | Execution | Completion |
|---|---:|---:|---:|---:|
| static | 8.28 / 0.22× | 54.45 / 2.37× | 1.10 / 0.22× | 55.53 / 1.99× |
| cpu | 12.96 / 0.30× | 55.92 / 2.35× | 46.91 / 1.61× | 103.14 / 1.96× |
| dom | 12.04 / 0.32× | 52.86 / 2.35× | 148.62 / 4.90× | 201.91 / 3.77× |
| async | 13.34 / 0.34× | 56.21 / 2.15× | 52.13 / 1.73× | 104.47 / 1.87× |
| react | 13.50 / 0.35× | 64.45 / 2.62× | 44.33 / 1.80× | 107.05 / 2.18× |
| wasm | 8.43 / 0.22× | 58.22 / 2.45× | 9.25 / 1.80× | 67.63 / 2.36× |

Relative to milestone 03: DOM execution 153.50 → 148.62 ms (3.2%); React execution 87.83 → 44.33 ms (49.5%), completion 194.87 → 107.05 ms (45.1%). DOM remains expensive: its original frozen 1560.32 ms is reduced 10.5× overall, but current Chrome-relative 4.90× is not parity or an evidence-based claim of an absolute performance limit.

| Workload | N=50 throughput / frozen Chrome | N=100 throughput / frozen Chrome | Fitted marginal RSS / frozen Chrome |
|---|---:|---:|---:|
| static | 76.72/s / 2.96× | 77.01/s / 3.68× | 21.19 / 59.46 MiB (0.36×) |
| cpu | 45.07/s / 1.63× | 37.72/s / 2.14× | 35.99 / 73.87 MiB (0.49×) |
| react | 44.42/s / 3.06× | 47.54/s / 3.43× | 27.99 / 72.27 MiB (0.39×) |

These slopes fit all six valid levels through N=100. They differ from the short gate's direct ten-Page 26.24/32.74 MiB measurements; do not interchange estimators. Cold ready RSS across measured workloads has median 24.36 MiB versus frozen Chrome 380.18 MiB. Model intercepts are not the ready-process footprint. N=100 static waves last 1.26–1.37 s, so the previous ten-second connection tail did not recur; its original root cause remains unproven.

**CPU memory regression:** compared with milestone 03, CPU marginal RSS rises 26.12 → 35.99 MiB (+37.8%) while N=50 throughput rises 32.52 → 45.07/s (+38.6%). That comparison spans classes 11–14, not QoS alone; it does not establish QoS as the sole cause. The prescribed static/React memory baselines are preserved and improved. This CPU memory regression is recorded, not hidden behind those improvements.

### Final-build profiles and residual costs

Fresh final-production profiles, captured after the full matrix, are summarized in `dom-current/final/native-hotspots.json`. Exact build and launch hashes accompany them. Native profiles and Go allocation/mutex/block/goroutine files remain in `.build/profiles-final-native-complete`. The diagnostic runner `tools/performance/profile_gate.py` rebuilds and verifies the executable before each replay, checks the frozen fixture fingerprint, and separates native sampling from execution-only Go CPU sampling. Its first async attempt found a missing `/worker.js` route in the diagnostic server, not a product regression; the server now serves the frozen runner's exact worker bytes. The full benchmark's async gates had already passed.

* **DOM:** exactly 60,060 host crossings per uninstrumented-wrapper replay. Final native sampling averages 98.72 ms in anonymous native/host frames, 6.00 ms in API-access tracing, 5.78 ms in `wrap`, 4.79 ms in Proxy `get`, 2.96 ms in `observe`, and 5.16 ms in V8 GC. Inspector idle/setup is excluded from attribution. The earlier detailed conversion/host counters explain the native family; their inclusive timings are not added to these sampled self times. Synchronous canonical Go DOM and the present typed-value/UTF-8 SDK boundary explain the dominant overhead. Further large reduction requires changing that bridge's representation/call granularity, while preserving immediate canonical mutations; small selector optimizations cannot close this gap. It is **not** proven that every current bridge operation is unavoidable.
* **CPU:** native samples predominantly execute `__benchRun` (37.63 ms/replay) and V8 GC (8.25 ms). This workload is not dominated by DOM host traffic. The four-MiB young generation remains an intentional density/GC trade-off; the exact 1.61× Chrome gap is not wholly explained by GC alone.
* **React:** native/host samples ~15.81 ms, V8 GC ~1.47 ms, plus React reconciliation and wrapper/Proxy/insertion work. The earlier long-run doubling is independently reproduced and removed by the thread QoS experiment and production 100-Page validation. Remaining 1.80× full execution includes canonical DOM bridge and task delivery.
* **Async:** the isolated replay is mostly waiting/task delivery; the CPU sampler cannot charge idle samples as CPU or equate them to the timed window. Its diagnostic median 24.70 ms is well below full CDP 52.13 ms, so the full gap is not solely JS execution. Exact CDP/scheduler/worker contributions remain unresolved; no false additive attribution is asserted.
* **Wasm:** isolated execution is 2.76 ms with ~1.07 ms sampled in the workload loop, ~0.51 ms in export lookup, ~0.41 ms in JS→Wasm and ~0.10 ms in the Wasm body. Full CDP execution remains 9.25 ms. Thus the full 1.80× gap cannot be called an inherent slow Wasm backend; surrounding task/CDP completion costs remain to be isolated.
* **Navigation/static:** the installed per-isolate compatibility surface and prototype/exposure work remain the principal measured navigation overhead. Static workload execution itself is already cheaper than Chrome. Startup probing was left untouched.

Ten held Pages, independent diagnostic (not the benchmark's RSS slope):

| Workload | Process RSS MiB | Go live heap MiB | Sum V8 used heap MiB | Sum V8 physical heap MiB | V8 reported malloc MiB |
|---|---:|---:|---:|---:|---:|
| static | 269.17 | 13.83 | 146.77 | 194.11 | 2.50 |
| cpu | 351.64 | 15.20 | 165.97 | 255.88 | 3.59 |
| react | 335.80 | 25.38 | 189.71 | 233.68 | 5.00 |

V8 reported external memory is 0/480/0 bytes respectively. Used/physical heap and process RSS are overlapping measures, not additive components; mapped code, stacks, Go reservations and native allocator capacity are not fully attributed by V8 statistics. After close, ordinary 250-ms recovery RSS is 71.91/72.44/94.16 MiB. Explicit Go GC/scavenging (diagnostic only) lowers it to 63.50/67.04/71.03 MiB and ~8.4–8.5 MiB Go live heap.

A separate **forced V8 collection diagnostic**, never substituted for benchmark results, lowers held ten-Page RSS from 267.04 → 145.59 MiB static, 355.09 → 154.44 MiB CPU, and 342.07 → 202.09 MiB React. Median per-Page collection costs are 5.06/6.00/7.90 ms. This proves considerable capacity/garbage is reclaimable and helps attribute CPU's footprint; it does not establish which class caused the full-fit regression. No collection was inserted into the workload or normal navigation to manufacture a smaller official memory number. Choosing an automatic reclamation policy would require a new measured latency/throughput/density trade-off, rather than treating forced-GC memory as the current product result.

## DOM crossing census — 2026-09-08 (diagnostics only)

No optimization or workload/harness/baseline changes. A new `profile_gate.py --hosts` mode enables existing inclusive host timers without wrapper hooks, bootstrap JS instrumentation, or conversion timers. Ten fresh DOM Pages each execute the unchanged workload once. Subtract navigation counters from execution counters; sum of named host counts equals the independent callback sequence delta in every replay: **60,060**, across 20 host names. These are JS-to-Go host invocations, not a count of all bidirectional SDK/FFI operations. All ten complete with the expected DOM result.

Top ten ordered by aggregate callback time. Counts are exact per replay; mean summed time is the aggregate over ten replays divided by ten (not mean duration of one call).

| Host function | Calls/replay | Summed ms/replay (mean) | Summed ms, all 10 |
|---|---:|---:|---:|
| setAttribute | 18,000 | 22.756 | 227.565 |
| toggleToken | 12,000 | 20.797 | 207.967 |
| insertPlain | 9,001 | 11.510 | 115.101 |
| elementChildren | 6,002 | 8.898 | 88.981 |
| queryAllWithin | 6 | 4.455 | 44.547 |
| create | 3,001 | 3.192 | 31.916 |
| createText | 3,000 | 2.888 | 28.884 |
| createComment | 3,000 | 2.415 | 24.151 |
| parentNode | 3,003 | 2.370 | 23.703 |
| contains | 3,001 | 2.315 | 23.148 |

Top ten cover 60,014 calls (99.923%). All host timers total 82.788 ms/replay. Remaining names/counts: apiAccess 31; queryWithin, textContent, query, performanceNow, getAttribute, setInnerHTML each 2; nodeChildren, storageGet, storageSet each 1. By call count, apiAccess replaces queryAllWithin in the top ten.

**Timing boundary and limitations:** timers begin inside the Go callback and end on callback return; they include argument wrapping, conversions, host body, result handling and nested work. They exclude transition/dispatch before callback entry and after return to V8. These are inclusive instrumented elapsed times, not isolated transition overhead or an additive CPU decomposition. Existing Go time.Now/time.Since counters exhibit zero/quantized measurements for short operations; zero does not establish zero cost, and close rankings should not be overinterpreted. Instrumented execution median is 147.449 ms; the separate subsequent control is 156.836 ms. Their -6.0% difference is run variation, not an optimization or a negative instrumentation overhead estimate.

Evidence: `dom-current/crossings-20260908/` contains both raw phase series, build/launch receipts, complete per-replay/per-type counts and times in `summary.json`, and fast-gate results. Reproduce aggregation with `tools/performance/summarize_crossings.py`. Both diagnostic launches rebuilt and verified SHA-256 immediately before execution: `995452e25e294a660d8f410da4f4f8f6da8e2b162597d342fcca25041ed57916`. Frozen fingerprint verification passed. Fast gate passes all semantic gates, prescribed warm runs, N=10/25 waves and ten-Page memory collection. That supplementary gate overlapped correctness tests, so its latency/throughput/memory numbers must not be used to claim a performance delta; the two attribution replays preceded these checks and did not overlap them.
Correctness: go test ./internal/engine/v8 ./internal/browser passes (browser 63.030 s). Full matrix is not rerun: no production optimization milestone occurred.

## Typed attribute argument experiment — 2026-09-08 (not adopted)

This isolated experiment tests a limited scope before full implementation: export hints for only setAttribute (number/string/string) and toggleToken (number/string/string/number). Synchronous Go DOM ownership and the workload remain unchanged. The prototype is isolated at `.build/cross-opt-typed`, detached commit `0b4c43c`; no runtime/browser change is applied to the main checkout. A complete patch and regression tests are retained in `dom-current/typed-attributes-experiment/prototype.patch`.

The first variant used NumberValueRaw. Its DOM fast gate regressed 146.833 -> 149.923 ms (+2.1%). SDK inspection showed that existing NumberValue already uses a direct-return fast path. The revised variant preserves that path and skips only redundant type probes. StringValue itself validates strings; mismatched hints fall back to ordinary Export. No unchecked native casts or changed coercion rules were introduced.

Fresh detailed profiles (ten executions each) show setAttribute inclusive mean 25.586 -> 21.911 ms and toggleToken 25.363 -> 21.179 ms; combined -15.4%. Export instrumentation falls 48.145 -> 42.069 ms. These overlapping instrumented times are attribution, not additive savings.

Uninstrumented execution replays were then run sequentially in A/B/B/A order, 20 fresh Pages per block, rebuilding and verifying SHA-256 before every launch. A is the unchanged checkout and B the revised isolated prototype. No correctness tests or other benchmark jobs overlapped these measurements.

| Block | DOM execution median ms | Mean Go allocated MiB/execution | Host calls/execution |
|---|---:|---:|---:|
| A1 | 183.239 | 29.829 | 60,060 |
| B1 | 166.422 | 28.773 | 60,060 |
| B2 | 171.822 | 28.784 | 60,060 |
| A2 | 175.688 | 29.824 | 60,060 |

Pooled medians across 40 executions per variant: 181.307 -> 170.608 ms (-5.9%, 1.063x); allocation traffic improves about 3.5%. System/run variation remains visible, including between these replays and the earlier gate. This supports a modest local effect, not a multiple-fold speedup or a precise universal 5.9% gain.

**Fast-gate regressions are retained:** revised prototype DOM 146.833 -> 153.567 ms (+4.6%), static completion 55.641 -> 65.619 ms (+17.9%), React execution 48.737 -> 55.280 ms (+13.4%). Static N=10 throughput 53.47 -> 42.64/s (-20.2%), N=25 61.58 -> 58.33/s (-5.3%). These sequential gates do not isolate environmental drift from product effects, so the A/B/B/A diagnostic does not erase them or establish gate acceptance. Marginal RSS/static Page 26.347 -> 26.442 MiB; React 32.835 -> 33.026 MiB. After ordinary recovery, process RSS static 88.953 -> 90.590 MiB, React 111.895 -> 114.512 MiB. Retention is not improved by this experiment.

All three fast gates completed the prescribed warm runs, N=10/25 waves, memory collection and all six semantic gates. Both detailed replays and all 80 uninstrumented DOM replays pass workload checks. Prototype engine/browser tests pass (browser 67.846 s, V8 0.363 s), including generic-vs-typed export equivalence, Unicode/lone-surrogate/NUL values, wrong-type fallback, non-finite numbers, large argument lists, nested callbacks and scratch-frame clearing. Frozen fingerprints and executable SHA-256 receipts are preserved beside raw phase/gate data and `summary.json` in `dom-current/typed-attributes-experiment/`.

Decision: retain the prototype for review, do not merge or expand it. The measured opportunity in these type probes is modest, while a multiple-fold win would require removing substantially more work/crossings. No full matrix is warranted for an unadopted, limited experiment. No C++/Rust, DOM ownership migration, selector changes or insertion/collection optimizations were attempted.

## Class 15: synchronous packed primitive bridge

Fresh host profiles precede this class (`dom-current/packed-bridge/before-phases.json`). Instead of exporting each primitive through several SDK calls, thirteen synchronous DOM hosts now use one private 2-KiB argument/result ArrayBuffer per V8 adapter. Go owns and pins its pointer-free allocation until the SDK backing-store deleter releases the final native reference. Only the owning Page thread touches it. Arguments are fully decoded into separate bounded Go frames before host execution, preserving nested calls. Short strings use UTF-16 with an ASCII decoding path; long strings and mismatched arguments retain the original callback path. Null/boolean/numeric/undefined results return through private slots; other results retain ordinary marshaling. Mutation remains immediate in the canonical Go DOM. No new Web API, global runtime lock, C++ or Rust implementation was added.

The initial copied-block experiment reduced diagnostic DOM execution 177.813 -> 122.788 ms; removing the per-call SDK copy and primitive result roundtrips reduced it further to 98.817 ms before the final ASCII decoding refinement. These diagnostic medians are not gate numbers. Native buffer ownership uses the SDK's explicit caller-owned backing-store API; no borrowed engine pointer is dereferenced by Go. The final buffer remains live across Go/V8 collections and is released on isolate teardown.

The unchanged fast gate on the final class build gives DOM execution 147.749 -> 95.692 ms (-35.2%, 1.54x), DOM completion 201.422 -> 148.722 ms. React execution 46.619 -> 39.922 ms (-14.4%), completion 109.211 -> 102.199 ms. Static completion regresses 54.062 -> 55.357 ms (+2.4%); no startup improvement is claimed. Static N=10 throughput 54.80 -> 53.90/s (-1.6%), N=25 61.36 -> 61.97/s (+1.0%). Marginal RSS/static Page 26.105 -> 26.383 MiB; React 32.853 -> 33.312 MiB. Post-recovery process RSS static 87.887 -> 88.633 MiB, React 112.840 -> 114.703 MiB. Memory/retention is slightly worse, not hidden by the latency gain. The number of host calls remains 60,060 per DOM execution.

All six semantic gates pass, as do the ordinary repository tests (browser 83.691 s). Focused packed-bridge tests also pass after the final ASCII refinement: primitive/wrong-type/long-string equivalence, Unicode including lone surrogates and NUL, captured string intrinsics, reentrant arguments/results, exceptions, negative zero, Go and V8 collections, repeated close and frame clearing. Build and per-launch SHA-256 receipts, frozen fingerprints, both complete gate files and raw profiles are retained in `dom-current/packed-bridge/`. Full matrix follows the next substantial class rather than replacing the immutable baseline.

## Class 16: skip frame insertion traversal when no iframe can exist

Fresh class-15 host profiles still show 6,002 elementChildren projections during DOM insertion. A conservative document flag now records whether any iframe has been parsed or created, including innerHTML and namespace creation. It never resets on detach: detached canonical nodes can be reused. Non-resource insertion returns that flag through the existing host call, allowing JS to skip the iframe-only traversal when it cannot do work. Ordinary detached DocumentFragments do not run connection steps; ShadowRoots preserve them. Pages that contain iframe elements retain the original traversal, including closed shadow trees. This is not a mirror DOM or a deferred mutation scheme.

Fast gate: DOM execution 95.692 -> 79.340 ms (-17.1%), overall 147.749 -> 79.340 ms (1.86x) from the fresh pre-bridge gate; completion 148.722 -> 134.418 ms. React execution 39.922 -> 36.365 ms, completion 102.199 -> 98.294 ms. Static completion 55.357 -> 54.910 ms. Crossings fall 60,060 -> 54,055 per replay; the diagnostic median is 84.065 ms. Canonical node identity is unchanged. Static N=10/N=25 throughput 54.55/61.02 sessions/s. Marginal RSS static/React is 26.445/32.355 MiB per Page; post-recovery process RSS is 90.293/114.945 MiB. Static density and residual RSS have not improved.

All six gate workloads pass. Focused Goja/V8 tests cover fragment identity and moves, iframe creation from innerHTML followed by fragment insertion, connected and newly connected shadow iframe navigation/load, and all canonical frame-creation paths. Broader tests follow the final group. Raw host/native profiles, gate data and per-launch hash receipts are in `dom-current/insertion-steps/`. The new native profile is the input for the next class; no claim of a full-matrix result is made yet.

## Class 17: share observation handlers and avoid repeated trace-key allocation

The fresh post-insertion native profile attributes about 5.41 ms/replay to recordAPIAccess, 4.43 ms to Proxy get, 2.51 ms to observe and 6.34 ms to V8 GC (sampled self times, excluding 18.59 ms idle/setup). Each DOM wrapper previously allocated fresh get/set closures and repeatedly concatenated the same qualified property and support keys.

Observation handlers are now shared by interface label within one realm. A bounded 128-entry property-name cache per label avoids repeated qualified-name construction; a two-bit state per name preserves once-per-supported/unsupported reporting without appending a boolean to every trace key. Support is still checked against the current receiver/prototype on every access. Wrapper identity and the actual getter/setter receiver remain unchanged; nothing is shared across Pages.

Fast gate DOM execution 79.340 -> 70.283 ms (-11.4%), overall fresh baseline 147.749 -> 70.283 ms (2.10x). Completion is 126.466 ms versus original 201.422 ms (1.59x, not a 2x completion claim). React execution 36.365 -> 33.002 ms; overall 46.619 -> 33.002 ms (1.41x). Static completion 54.196 ms. Static N=10/N=25 throughput is 52.80/62.64 sessions/s versus the original 54.80/61.36; N=10 regresses 3.6%, N=25 improves 2.1%. Marginal RSS/static Page 26.195 MiB and React 31.371 MiB, versus original 26.105/32.853. Post-recovery process RSS 86.953/109.812 MiB, versus original 87.887/112.840. These results do not establish that all workloads or throughput doubled.

All six semantic gates pass. Focused Goja/V8 regressions verify one trace per supported/unsupported state per Page, mutations of own properties and prototypes, and correct getter receivers for different wrappers sharing a label. Full suite/race checks and a single final frozen matrix follow. Gate raw data and launch hashes are `dom-current/observation/gate.json`; native pre-profile is retained under `insertion-steps/`.

## Milestone 05: packed bridge, insertion and observation — final validation

Final production source is clean commit `0abfa05` (classes 15–17); executable SHA-256 `ea237da15757c8f1a11e8a8632cc5a9495040cd64d4373e923a746e1833988c6`. The original frozen Chrome 152 executable hash remains `ea36dd818a90176f1a70616f0363d9be527229389a6c073a0b1688b9e73f67e9`. Every benchmark process launch verifies the just-built executable hash; the frozen harness/fixture fingerprint remains unchanged. Raw full-matrix evidence is `benchmark/runs/05-packed-dom-milestone`, with the immutable comparison against milestone 04 in `benchmark/runs/05-packed-dom-comparison.json`.

**The multiple-fold DOM gain is confirmed:** warm execution 148.618 -> 73.406 ms, **2.02x** on the full frozen workload, 20 samples per version. The fresh short gate independently measured 147.749 -> 70.283 ms, 2.10x. Full DOM completion (including navigation) is 201.910 -> 131.728 ms, only 1.53x. Relative to the original frozen Chrome 152 execution median, Mimic DOM improves from 4.90x to 2.42x Chrome; it has not reached parity.

| Workload | Previous / final execution ms | Previous / final completion ms |
|---|---:|---:|
| static | 1.097 / 1.420 | 55.531 / 59.097 |
| cpu | 46.907 / 50.255 | 103.142 / 107.403 |
| dom | 148.618 / 73.406 | 201.910 / 131.728 |
| async | 52.133 / 49.393 | 104.466 / 109.779 |
| react | 44.332 / 38.089 | 107.051 / 109.506 |
| wasm | 9.253 / 9.359 | 67.627 / 71.678 |

These full-run regressions are not erased by the short gate: CPU execution +7.1%; static/CPU/async/React/Wasm completion +6.4/+4.1/+5.1/+2.3/+6.0%. Navigation medians increased by roughly 3–6 ms across workloads. The bridge adds private wrapper setup, but environmental drift and setup cost have not been separately attributed; no startup speedup or all-workload improvement is claimed. React execution improves 14.1% in the full run, not twofold.

All twelve semantic gates and all measured cold/warm workload rows are valid. Concurrency waves through N=50 are valid for all three workloads and both systems. **N=100 is not certified:** each level stopped during its excluded warm-up because the unchanged harness detected memory pressure; Chrome React additionally reports sustained paging. No measured N=100 waves exist in this run. Any numeric N=100 throughput in the generated summary belongs to the stopped level and must not be used as a result. No memory guard was bypassed or workload modified to make these levels pass. Current fitted marginal RSS uses five levels through N=50: static 22.047, CPU 36.415, React 27.897 MiB/Page. Previous fits used six levels through N=100; they are not identical estimators. The ten-Page short-gate measurements remain in the class-17 section.

**Static N=50 teardown regression:** aggregate throughput drops 76.72 -> 18.42 sessions/s. One measured wave has 10.642 s elapsed despite a 0.632 s execution/completion window; its longest teardown is 10,006 ms. The other measured waves take 0.709–0.768 s. This reproduces the kind of intermittent CDP close tail previously recorded before these changes, but does not prove an unchanged cause. All samples and the aggregate regression are retained. Fresh hash-verified close diagnostics on both source versions (three 50-Page waves each) do not reproduce the tail: final elapsed 0.834/0.750/0.661 s, original 0.908/0.723/0.627 s; maximum individual CDP close 25.26/26.18 ms. The tail remains unresolved, and no general static throughput improvement is claimed. Full N=50 CPU throughput is 45.07 -> 45.19/s; React is 44.42 -> 46.15/s.

The measurement phase completed and saved its finished raw data. The wrapper initially exited nonzero only because its default Python lacked matplotlib for report generation. The unchanged `benchmark/report.py` was then successfully run with the existing `.build/benchmark-venv/Scripts/python.exe`; no benchmark samples were rerun or replaced. The frozen comparator accepted the resulting artifacts.

Final host replay records exactly 54,055 calls per execution, versus 60,060 before: 6,005 fewer. Mean inclusive host timer sum is 25.754 ms; this now excludes JS-side argument packing, so it is not an additive or like-for-like estimate of total bridge CPU. Final native sampling attributes about 44.10 ms/replay to anonymous/native frames, 5.63 ms to wrap, 5.03 ms to GC, 3.12 ms to observe and 2.97 ms to Proxy get. Idle/setup (19.56 ms) is excluded. These sampled and inclusive measures overlap; the full workload wall times above establish the actual gain.

The final ordinary suite passes: 222 test/subtest passes, zero failures; browser 82.879 s. V8 engine race checks pass (1.478 s). Lifecycle, Unicode, fallback, reentrancy, DOM identity, iframe/shadow insertion and dynamic trace-state regressions are included. Final raw host/native profiles, build/launch receipts, crossing census, validation summary, and both close-tail diagnostics are under `docs/performance/dom-current/packed-final/`. No production code from the earlier typed-argument experiment was merged; no C++/Rust implementation or DOM ownership migration was introduced.


## 2026-09-09 — isolated JS-owned DOM kernel, not production migration

The architecture spike is isolated in `.build/jsdom-spike`, commit
`93c1c4a`; its portable patch, receipts and raw evidence are retained in
`docs/performance/dom-current/jsdom-spike/`. Production code is unchanged.

Fresh native profiling and the required production fast gate preceded the
experiment. All fast-gate correctness rows passed (DOM/static/React warm x5,
static N10/25 x3, ten-Page memory). Current control DOM execution is 117.093 ms,
so historical 73.406 ms is not used as the experiment denominator.

The kernel owns its tree in JS and retains identity within that tree. The frozen
workload source is read verbatim and evaluated with a lexical document argument;
no frozen source, expected output, baseline or harness file changes. Native HTML
parsing is retained. This is an explicitly incomplete diagnostic, not an alternate
implementation that passes the full frozen matrix. Child collections are
snapshots; general selectors, live collection branding, events, resource steps,
shadow DOM, complete WebIDL and API access observation are missing. Go does not
observe JS tree mutations. Initial import/compilation is measured separately.

The naive prototype was slower: 227.923 vs 112.633 ms. A fresh prototype CPU
profile identified quadratic fragment removal (143.862 ms sampled self time)
and repeated class token parsing (46.449 ms). Bulk fragment transfer and coherent
token caching bring the in-process comparison to 56.162 vs 112.829 ms. All exact
result fields and independent insertion/identity/error atomicity/attribute/token/
Unicode/comment/HTML/query assertions pass on both variants.

Final CDP diagnostic, 20 samples per variant after one warmup each, ABBA order
within each runtime (runtime blocks are sequential):

| Runtime / DOM | Execution ms | Setup ms | Navigation ms | Close ms |
|---|---:|---:|---:|---:|
| Mimic / current | 111.271 | 0.731 | 73.617 | 6.392 |
| Mimic / JS kernel | 56.298 | 2.993 | 75.292 | 13.007 |
| frozen Chrome 152 / native | 45.210 | 4.298 | 27.542 | 1.698 |
| frozen Chrome 152 / JS kernel | 26.635 | 4.874 | 27.257 | 1.486 |

This is a 1.98x kernel execution improvement inside Mimic, but still 1.25x slower
than Chrome native DOM. Setup and teardown regress. Identical kernel code is
2.11x slower inside Mimic than Chrome in this series; the cause is not isolated.
Investigate runtime/embedding/scheduling before assuming DOM migration alone can
create a strong Chrome lead. No production 2x speedup is claimed.

One diagnostic ten-Page wave per variant, fresh processes, serial creation and
execution: Mimic active RSS increase 402.82 -> 319.60 MiB; after close +250 ms,
116.38 -> 57.92 MiB above ready baseline. Serial throughput including setup and
teardown 4.79 -> 6.31 Pages/s. These are not concurrent-capacity or long-run leak
results. Full matrix is deliberately not claimed for this incomplete kernel.
All benchmark executables were rebuilt and SHA-256 verified before launch;
frozen Chrome digest and harness fingerprint were checked and recorded.


## 18: Preserve engine-owned ECMAScript intrinsic prototypes (2026-09-09)

Fresh paired micro-diagnostics isolated a Page bootstrap defect: generic WebIDL
exposure normalization rewrote built-in ECMAScript prototypes, added spurious
Symbol.toStringTag properties, and invalidated V8 species/prototype guards.
Pure-V8 string/token microcode takes about 8.4 ms after warmup; Page takes about
51 ms before this fix and 9.3 ms after. V8 optimization traces show active Maglev
and TurboFan compilation; a disabled JIT was not the cause. Merely skipping the
final tag loop was insufficient: both exposure prototype passes also modified
intrinsics. The fix excludes engine-supplied globals from these WebIDL passes.
A regression compares intrinsic descriptors against the pristine engine, and
checks subclass species behavior plus native/browser brands, on Goja and V8.
The full ordinary Go suite passes; exact validation is under `intrinsics/`.

The independent JS DOM kernel now takes 16.414 ms inside Mimic versus 16.480 ms
inside frozen Chrome 152, in the same diagnostic protocol (20 measured samples
each). Chrome native DOM takes 28.902 ms. Current production DOM in that series
is still 65.855 ms. Thus the partial kernel demonstrates an execution-only lead,
not a production/browser lead; its missing semantics remain as previously listed.

The production fast gate control -> fixed: DOM execution 65.405 -> 67.519 ms,
completion 125.200 -> 118.425; static completion 55.510 -> 53.622; React execution
35.144 -> 37.684, completion 96.133 -> 99.066 ms. N10 throughput medians
49.55 -> 53.61/s; N25 72.79 -> 62.53/s. All correctness rows valid. Short samples
are noisy: do not present the micro-kernel gain as an across-workload gain.
An earlier exploratory control and CDP run overlapped; they are not the clean
control used here. All executable launches were freshly rebuilt and hash checked.
Production DOM identity, synchronous Go DOM, event loops and API observation
remain intact. No native code, runtime flag or JS DOM prototype is merged.


Timing-boundary clarification: in that diagnostic Chrome native DOM reports
8.6 ms on its in-page timer, versus 14.7 ms for the JS kernel. Its CDP wall times
are 28.902 and 16.480 ms respectively. Thus the apparent kernel lead includes
scheduling/protocol/browser work around script execution; it does not prove a
faster DOM algorithm. Mimic's deterministic in-page timer reports zero here and
must not be compared. Navigation + setup + execution medians are 68.714 ms for
Mimic's kernel and 47.653 ms for Chrome native DOM: end-to-end parity is NOT
achieved. The reproducible diagnostic additions are saved as `diagnostic.patch`
(commit b631da0), separate from the production fix.

## 19: Intrinsics full matrix and bootstrap catalog filtering (2026-09-09)

The completed full matrix `benchmark/runs/06-intrinsics-milestone` tests
4d0f0a1, before catalog filtering. All 12 gates and 396 cold/warm rows are valid;
all 36 concurrency groups completed, including five measured waves at N100.
Fresh executable hashes and the unchanged harness fingerprint are recorded in
`build.json` and per-launch `launches.jsonl`. Reaching N100 depends on available
workstation memory and is not attributed solely to the intrinsic fix.

| Warm workload | Mimic execution / completion ms | Chrome execution / completion ms |
|---|---:|---:|
| static | 1.401 / 54.699 | 5.015 / 29.526 |
| CPU | 37.365 / 93.800 | 29.121 / 53.068 |
| DOM | 74.142 / 131.286 | 34.034 / 60.368 |
| async | 47.495 / 101.933 | 30.288 / 55.251 |
| React | 33.284 / 96.160 | 28.330 / 58.233 |
| wasm | 9.416 / 64.933 | 5.443 / 32.085 |

Against full05, Mimic CPU execution changes 50.255 -> 37.365 ms and React
38.089 -> 33.284 ms; DOM is effectively unchanged (73.406 -> 74.142 ms).
These are separate run cohorts with uncontrolled background applications.

At N100, aggregate throughput across all five waves, including teardown, is
29.48 vs 18.85 Pages/s for static (1.56x), 45.29 vs 14.71 for CPU (3.08x),
and 57.82 vs 21.22 for React (2.72x), Mimic vs Chrome. Static's median-wave
throughput suggests 3.6x but hides a 10008.74 ms teardown tail; use the aggregate
ratio. Median active RSS is 2183.6 / 7112.1 MiB (static), 3316.2 / 8774.0
(CPU), and 2715.9 / 8566.0 (React). Median recovered RSS after 250 ms is
194.8 / 1257.4, 209.0 / 1432.3, and 395.7 / 1422.0 MiB respectively.
These are process-tree measurements, not a long-run leak proof.

The close diagnostic now accepts multiple waves and uses one shared observer
instead of one timer thread per close. Three separate ten-wave, 100-Page static
probes did not reproduce the tail (maximum individual closes 56.224, 66.60,
and 39.703 ms). Both Python environments use Python 3.14.2 / websockets 13.1.
Unread WebSocket events are a hypothesis, not an established cause. No
production teardown fix is claimed. Compressed evidence is under `close-tail/`.

The next production change, 52a2aa2, filters generated fallback bindings before
bootstrap instead of constructing bindings which exposure normalization removes.
The immutable bundle/profile cache stores only source and JSON. Ancestors,
aliases, static members, constants, and members affecting descendant lookups
are retained. Missing prototype capture data is not treated as an empty capture.
Exact exposed descriptor snapshots match the original catalog; the full Go suite
passes (241 test/package pass events, zero failures), including DOM identity.

Clean fast-gate control -> catalog filter:

| Metric | Control | Filter |
|---|---:|---:|
| DOM execution ms | 67.519 | 66.678 |
| DOM completion ms | 118.425 | 109.859 |
| static completion ms | 53.622 | 45.931 |
| React execution ms | 37.684 | 34.524 |
| React completion ms | 99.066 | 84.443 |
| static N10 median Pages/s | 53.61 | 72.37 |
| static N25 median Pages/s | 62.53 | 74.41 |
| static marginal RSS MiB/Page | 26.345 | 24.836 |
| React marginal RSS MiB/Page | 31.736 | 29.082 |
| static recovered RSS MiB | 90.023 | 98.285 |
| React recovered RSS MiB | 114.914 | 114.691 |

The static recovered-memory sample regresses by 8.262 MiB. The fresh detailed
profile measures warm navigation at 42.28 ms, generated bootstrap ending at
9.15 ms, and exposure ending at 27.20 ms, versus 57.63 / 13.84 / 40.44 before.
Fast-gate raw data, build receipts, profile, and validation are under `catalog/`.
The full matrix for the catalog change is a separate run; full06 does not
certify that change. Single-Page Chrome parity remains unachieved.

## 20: Catalog full matrix and refreshed crossing counts (2026-09-09)

`benchmark/runs/07-catalog-milestone` now validates production 52a2aa2.
All 12 correctness gates, 396 cold/warm rows, and 36 concurrency groups pass,
including five measured waves at N100 for static, CPU, and React. All 204
launch receipts match the freshly built executable or pinned Chrome digest;
the frozen harness fingerprint is unchanged. `audit.json` records the checks.

| Warm workload | Mimic execution / completion ms | Chrome execution / completion ms |
|---|---:|---:|
| static | 1.265 / 44.177 | 3.618 / 20.651 |
| CPU | 34.713 / 77.093 | 28.233 / 45.326 |
| DOM | 68.929 / 111.149 | 30.747 / 48.129 |
| async | 49.214 / 91.776 | 27.544 / 44.444 |
| React | 36.307 / 85.881 | 22.412 / 41.219 |
| wasm | 9.065 / 51.443 | 5.165 / 22.480 |

Here completion is the frozen harness's navigation + workload interval;
it excludes session creation and teardown. Against full06, Mimic completion
falls 19.2% for static, 15.3% for DOM, and 10.7% for React. React execution
regresses 9.1%, and async execution regresses 3.6%. Chrome also improves
materially between cohorts, so these changes cannot all be attributed to code.
There is still no single-Page DOM latency win over Chrome.

| N100, aggregate all measured waves | Mimic Pages/s | Chrome Pages/s | Ratio | Active RSS MiB, Mimic / Chrome | Recovered RSS MiB, Mimic / Chrome |
|---|---:|---:|---:|---:|---:|
| static | 91.53 | 28.63 | 3.20x | 1942.4 / 7075.8 | 186.6 / 1254.4 |
| CPU | 52.11 | 24.56 | 2.12x | 3056.2 / 8737.8 | 207.0 / 1411.8 |
| React | 61.26 | 23.07 | 2.66x | 2440.0 / 8339.9 | 394.0 / 1375.9 |

Throughput includes session creation and teardown. Maximum observed measured
concurrency teardown is 257.95 ms for Mimic and 572.33 ms for Chrome. The
previous 10-second tail did not recur; no causal teardown fix was made.
Memory figures are medians across five waves; recovery is sampled after 250 ms.

Fresh detailed profile counts, subtracting navigation from execution and
excluding the first iteration and diagnostic wrapper callbacks, remain 54055
production host calls per DOM iteration. `catalog/crossings.json` contains all
counts and measured host-inclusive time for four warm iterations. Top ten:

| Host call | Calls per iteration | Mean measured host-inclusive ms per iteration |
|---|---:|---:|
| setAttribute | 18000 | 7.527 |
| toggleToken | 12000 | 8.661 |
| insertPlain | 9001 | 5.085 |
| parentNode | 3003 | 0.542 |
| contains | 3001 | 0.935 |
| create | 3001 | 1.173 |
| createComment | 3000 | 1.313 |
| createText | 3000 | 1.451 |
| apiAccess | 28 | below timer resolution |
| queryAllWithin | 6 | 4.019 |

Host-inclusive counters do not include the entire V8/Go dispatch latency and
have instrumentation overhead; do not sum them into a prediction of unprofiled
wall time. The next ownership experiment is scoped in
`dom-current/detached-ownership-next.md`; it is not implemented or merged.

## 21: Detached ownership bridge foundation (2026-09-09)

Detached worktree `.build/detached-dom-spike`, commit b7dc510, now implements
reserved node IDs, atomic import of a plain detached forest, and private host
functions for the bridge. It is not merged into production. Normal WebAPI
creation still uses the eager Go path: no speedup or full ownership migration
is claimed. The preceding full07 matrix remains the production evidence.

The DOM package tests pass, including invalid-forest atomicity, reservation
retry, and isolation from input slice/map mutations. A browser integration test
passes on V8 and Goja using the existing canonical WebAPI wrappers created
before import: identity survives materialization, insertion, parent/child/query
lookup, retained classList use, and subsequent Go attribute mutation. This is
an explicit test-only probe of the bridge, not a substituted workload.

Patch and validation are in `dom-current/detached-bridge/`. Next are the private
JS pending store and complete entry/exit, microtask, error and reentrant-host
synchronization; benchmark only when their cost is included in normal execution.

## 22: Pending DOM and attribute ownership experiments (2026-09-09)

Detached spike ab5d374 enables the ordinary WebAPI creation path to stage plain
detached nodes in JS. Unknown host operations and outer engine operations flush
the forest; callback-local calls and property access also synchronize before
Go resumes. The original wrappers remain canonical. The ordinary Go suite
passes (256 test/package pass events), including added Promise, timer, module,
throw, Unicode, reentrant host reads, and Go mutation checks. An initial Unicode
normalization bug and missing nested-call flush were caught and fixed.

An initial JSON implementation made 3001 imports because a missing parent was
returned as 0 rather than null. Correcting that reduced imports to two, but JSON
materialization still cost about 15.5 ms in a profiled warm iteration. A private
length-prefixed UTF-8 stream reduced measured Go import time to about 5 ms.
Neither serialization format changes a Web API. All frozen workloads and harness
files are unchanged; each benchmark executable was rebuilt and hash-verified.

Fresh production control -> ab5d374 fast gate:

| Metric | Control | Detached spike |
|---|---:|---:|
| DOM execution ms | 79.197 | 74.556 |
| DOM completion ms | 125.965 | 119.036 |
| static completion ms | 47.964 | 47.753 |
| React execution ms | 31.032 | 33.159 |
| React completion ms | 85.765 | 88.148 |
| static N10 median Pages/s | 54.84 | 53.95 |
| static N25 median Pages/s | 70.89 | 62.84 |
| static marginal RSS MiB/Page | 25.006 | 25.043 |
| React marginal RSS MiB/Page | 29.185 | 28.913 |
| static recovered RSS MiB | 74.016 | 73.055 |
| React recovered RSS MiB | 89.762 | 88.016 |

Completion excludes session creation/teardown. Their costs remain included in
throughput; session creation is noisy and also regressed in several spike rows.
The modest DOM delta does not justify a production ownership change. Evidence
and patch: `dom-current/detached-ownership/`. No full-matrix result is claimed
for this experimental branch; production remains the full07 implementation.

The next experimental commit, 4a1983b, also queues ordinary attributes on nodes
created through the private plain-node path. Resource elements stay eager.
Attribute batches validate before writing; cached reads survive only the exact
Go queryAll/queryAllWithin functions which have no mutation or trace callbacks.
Other host calls, API observations, and engine boundaries invalidate the cache.
Tests cover raw duplicate classes, writes after cached reads, canonical Go writes
between evaluations, and queued writes on throws. The full ordinary Go suite
passes (258 test/package pass events, zero failures).

Without the read cache, 12000 toggle calls became 12000 getAttribute calls plus
serialization; DOM execution regressed to 83.860 ms. With the cache, it is
78.366 ms and completion 123.122 ms; React execution/completion 34.757/87.969 ms.
N10/N25 median throughput is 54.48/73.67 Pages/s. These later short runs have
uncontrolled workstation variation; there is no demonstrated multiplicative
latency improvement. Four fresh warm profiles consistently count **9104** host
crossings, down from production's **54055**, including 3003 insertPlain,
3002 getAttribute, 3001 contains, six attribute batches and five node imports.
Counts exclude profiling callbacks. Almost 6x fewer crossings is not 6x faster.

A deliberately invalid diagnostic, 793513d, disables only DOM Proxy observation
to bound its overhead. Execution/completion becomes 69.032/113.874 ms, still far
from a multiplicative gain. Its observation regression test fails on both V8 and
Goja, as expected; it must not be presented or merged as a valid optimization.
Attribute evidence and both patches: `dom-current/pending-attributes/`.

These experiments remain separate. Their result changes the next architectural
question: avoid repeated JS/Go tree work and materialization at read-only DOM
queries, rather than treating crossing count as the objective. A persistent
JS-owned tree with JS queries and explicit synchronization for actual Go consumers
is still unimplemented. It must retain the existing bindings, tracing, resources,
Go visibility and per-Page scheduling before any Chrome lead can be claimed.

## 23. JS query ownership experiment: measured local gain, React regression

Experimental commit c409137 (based on 4a1983b) retains plain-node records within
an evaluation, implements the existing queryAllWithin selector subset in JS,
shares records with canonical wrappers, and transfers private decoded wire
storage directly into Go rather than copying it again. Go remains synchronized
at engine boundaries. This is not persistent JS ownership across evaluations.
Unicode selector cases fall back to Go. Mutable trace observers force cache
invalidation; filtering CDP delivery does not discard recorded trace events.

The ordinary Go suite passes: 262 test/package pass events, zero failures.
Additional checks cover selector equivalence and mutation through an aliased Go
attribute map during a trace callback. Frozen harness and workloads are unchanged;
build and per-launch SHA-256 verification records accompany every fast gate.

Fresh sequential production control -> c409137, warm medians:

| Metric | Control | Experiment |
|---|---:|---:|
| DOM execution ms | 88.374 | 66.800 |
| DOM completion ms | 141.700 | 112.366 |
| DOM session creation ms | 3.559 | 16.824 |
| static completion ms | 54.460 | 46.367 |
| React execution ms | 36.717 | 47.311 |
| React completion ms | 101.671 | 107.550 |
| React session creation ms | 3.191 | 13.929 |
| static N10 median Pages/s | 52.70 | 51.90 |
| static N25 median Pages/s | 59.85 | 61.10 |
| static marginal RSS MiB/Page | 24.823 | 24.696 |
| React marginal RSS MiB/Page | 29.763 | 30.247 |

DOM execution improves 1.32x in this pair, while React execution regresses 28.9%.
Completion excludes session creation and teardown. The noisy session creation
regression must not be hidden by the completion metric. Earlier same-source
measurements reached 58.561 ms DOM execution; the newer 66.800 ms result shows
why best short-run numbers cannot establish a general win. There is no new
Chrome measurement here; full07 Chrome DOM execution was 30.747 ms.

Intermediate profiling counted 3104 real host calls, compared with the prior
9104 and production's 54055. The later profile with wrapper diagnostics disabled
measures about 7.8 MiB of Go execution allocation and approximately 13.9 ms of
V8 GC self time per warm iteration. Remaining host crossings and total latency
are different objectives. Profile overhead is excluded from fast-gate timings.

A separate 8 MiB young-generation diagnostic did not demonstrate an improvement:
DOM execution 63.037 ms versus the preceding 4 MiB run's 58.561 ms, with static
marginal RSS rising from 24.938 to 28.660 MiB/Page and React from 29.761 to
33.016 MiB/Page. It is not promoted. Workstation variation limits causal timing
claims from these short sequential runs.

Evidence, patch, raw memory/recovery samples, profiles and validation are in
`dom-current/js-queries/`. This experiment remains separate from production;
its React regression rules out promoting the whole change. No full-matrix
improvement is claimed.

A small SDK feasibility test also passed: snapshot round-trip preserves JS
classes, WeakMap slots and canonical object identity; two restored isolates
remain independent and can use different late-bound JS host dispatch objects.
Source and output are saved alongside the evidence. This does not yet test
native Go callbacks, actual Page bootstrap, scheduling, resources or speed.
The next bounded proof is snapshotting a real bootstrap slice with per-Page
state rebound after restore, measuring creation plus navigation together so
moving initialization work between phases cannot count as an optimization.

## 24. Actual bootstrap snapshot replay: feasibility, not a Page speedup

A fresh production static profile (`snapshot-startup-profile`, native/hosts,
five iterations) measures warm surface execution at 39.8–41.9 ms and compilation
at 6.1–7.5 ms. This supports investigating bootstrap serialization independently
of the JS query experiment and its React regression.

Detached experimental commit 6df6ba5 records actual bootstrap host calls without
changing frozen inputs. The successful one-iteration capture records 429 calls:
410 capabilityState, eight viewport, two documentSecurity, two screen, and one
each token, intlEnvironment, permissionsPolicy, windowRelations, catalogJSON,
exposureJSON and ready. The first diagnostic capture failed because exposure
normalization deleted its global recorder; keeping the recorder in a lexical
binding fixed the diagnostic. This is not production instrumentation.

A standalone SDK test replays the actual complete captured surface with these
responses, checking host call names and argument JSON in sequence. V8 successfully
serializes the resulting context. Observed single-process diagnostics:

| Operation | Time / size |
|---|---:|
| Compile and execute captured bootstrap | 41.611 ms |
| Create snapshot after bootstrap | 97.067 ms |
| Snapshot bytes | 9,487,360 |
| Restore fresh isolate + context, three samples | 9.665 / 9.255 / 8.869 ms |

Restored contexts pass basic Document/Element availability, document identity
and native Array behavior checks. These are deliberately narrow feasibility
checks, not DOM or Page correctness gates. Recorded responses include a specific
Page token, frame IDs, environment, security, permissions and capability state.
No restored context is yet connected to a real Go host or browser event loop.
Therefore these timings do not establish any workload or end-to-end speedup.

Next implementation requirements are late-bound private Go dispatch; fresh
per-Page token and frame relations; correct environment/security/profile state;
one-time ready effects; scheduler and callback initialization; and measured
snapshot build amortization, RSS, throughput and teardown. Bootstrap work moved
to session creation must remain counted. Snapshot reuse may share immutable
bytes, but independent Pages must never share mutable JS state or a runtime lock.
The initial snapshot creation cost also rules out claiming a first-Page win.

Evidence and reproducible test source are under `dom-current/bootstrap-snapshot/`.
Decompress the two bootstrap-capture files into `.build/`, copy the test source
there without its `.txt` suffix, compile it with `go test -c`, verify/log its
SHA-256 immediately before launch, and run the test binary from the repository
root. The saved hash receipt identifies the successful diagnostic executable.
The frozen harness fingerprint is unchanged. No production code is promoted.

The next standalone proof connects the restored surface to an actual gov8 Go
callback, bound after restoration through a private lexical dispatch variable.
It then deletes the temporary global binding. Ordinary localStorage.getItem and
setItem calls reach separate Go maps in three successive isolates; all begin
empty, return their own written value, keep sessionStorage separate, and leave
the temporary binding unavailable. Each isolate makes six live calls including
API observations. This verifies native dispatch after restoration, not the
browser's real storage backend, scheduler, or concurrent Page behavior.

Discarding the recorded bootstrap replies before serialization reduces the
snapshot from 9,487,232 to 6,703,584 bytes. The final diagnostic test passes;
restoration samples are 9.965 / 10.192 / 9.535 ms, snapshot creation 96.341 ms.
There is still no actual Page latency or RSS result for snapshot restoration.
The live test source, output and executable hash receipt are saved alongside
the earlier replay proof.

The source audit narrows integration requirements:

| Captured state | Required treatment before exposing a restored Page |
|---|---|
| 410 capabilityState calls, all feature queries | Select a matching immutable feature/profile configuration; do not reuse across incompatible exposure shapes |
| token | Rebind the private constructor guard for the new Realm |
| intlEnvironment | Refresh the locale/time-zone object captured by Intl wrappers |
| permissionsPolicy | Rebuild the private clause/origin slots for the existing canonical policy object |
| documentSecurity | Match exposure and refresh values captured by security getters |
| windowRelations | Reconstruct top/parent references, including remote WindowProxy relationships |
| viewport/screen | Audit exposure normalization: ordinary accessors are dynamic, but normalization may capture values |
| ready | Enable API tracking on the actual new Realm after binding, exactly once |

The engine currently initializes its Promise factory and time source around a
fresh context. Restoring a surface cannot bypass those steps or move their cost
outside measurement. The next implementation must integrate at the engine/context
creation boundary and finish binding before user scripts can run; replacing the
context after Go handles or scheduler callbacks exist would invalidate them.

An experimental `SnapshotFactory` now restores the context at the V8 engine
creation boundary, preserving the 4 MiB nursery, per-runtime isolate thread,
explicit microtask policy and ordinary adapter Promise-factory initialization.
Its snapshots are pure JS with an explicitly empty external-reference table;
Go callbacks are installed only after restore. No browser selects this factory.

`SnapshotSurface` adds a private restore function to the existing bootstrap. It
rebinds the host and token, updates the captured Intl environment and policy
slots without replacing the canonical policy object, clears API observation
deduplication, and invokes the actual new host's ready method. Security/exposure
and frame topology must still match; automatic selection, validation and frame
rebinding are not implemented. The hook must be captured and deleted before
user code runs. This API remains an experiment rather than an enabled fast path.

The new standalone proof restores actual bootstrap bytes through Mimic's engine
adapter twice; native callbacks, Promise creation/resolution and Close pass.
Three additional direct SDK restores rebind en-US/UTC/denied geolocation,
ja-JP/Asia-Tokyo/allowed geolocation, and en-US/UTC/denied geolocation respectively.
Canonical featurePolicy identity is preserved, ready fires once per rebind, and
private restore globals are removed. Ordinary storage still reaches separate Go
maps. The initial test incorrectly used document.permissionsPolicy, which this
captured exposure does not expose; using the existing featurePolicy surface
corrected the test without adding a Web API.

Final standalone diagnostics: snapshot 6,715,304 bytes, creation 100.987 ms,
restore-only samples 9.433 / 8.457 / 8.429 ms. These numbers exclude rebinding
and are not Page or frozen workload results. Reproduction source, patch, output,
hash receipt and ordinary Go validation are saved in the same evidence folder.
Experimental commit ca557ca contains the implementation. The ordinary suite
completed with 241 test/package pass events and zero failures; an additional
targeted test rejects malformed snapshots and verifies subsequent ordinary
runtime creation still works. That targeted test was added after the full suite
started and has its own saved result. Production remains unchanged.

## Stopped checkpoint — 2026-09-09

Optimization is stopped. See `checkpoint-20260909/checkpoint.md` for the commit
ledger, validation scope, production crossing census and preserved artifacts.
No new benchmark or performance change was started after this checkpoint.

The final isolated snapshot experiment a19b84d now reaches actual Pages with
source/configuration checks and live host rebinding. Three defects were caught:
viewport values captured during exposure normalization required refreshing;
generated bindings retained their original host and needed late dispatch; and
a snapshotted placeholder WebAssembly object prevented V8 from installing its
native namespace. All six frozen semantic checks then passed. Source/exposure
mismatches fail closed; automatic fallback, arbitrary frame/security support
and production snapshot generation/selection remain incomplete.

The already-running final control fast gate completed before the stop checkpoint.
Control -> snapshot DOM execution is 70.148 -> 69.721 ms, completion 114.515 ->
104.874 ms. React execution regresses 36.868 -> 39.848 ms. Static N25 throughput
falls 87.16 -> 75.78 Pages/s; marginal RSS doubles, static 24.959 -> 50.862 and
React 29.022 -> 59.364 MiB/Page. Recovered RSS above ready baseline increases
from 69.980 -> 316.773 MiB static and 88.246 -> 342.473 MiB React. Session
creation also regresses, and prebuilding the snapshot excluded its creation
cost from these timings. This is not a production-worthy general improvement.

Final ordinary correctness and scoped race checks pass; exact commands and raw
logs are preserved. Production remains 52a2aa2, with full07 DOM execution
68.929 ms versus Chrome 30.747 ms. The 73.406 versus 30.326 ms comparison is the
verified historical milestone05/original-Chrome comparison, not the latest run.

## Focused wrapper diagnostic after the optimization checkpoint

Exactly one frozen DOM execution was profiled at production source 7886b13,
using isolated diagnostic instrumentation (6cf4506), local call counters, V8 CPU
sampling and 1024-byte allocation sampling including collected objects. No
optimization or second diagnostic run was performed. Correctness passed.

`wrapper-focused-20260909/report.md` contains the top ten wrapper operations,
call counts, sampled self/inclusive CPU, sampled V8 allocation attribution,
direct host crossings and explicit Go-attribution limitations. Key counts are
24,008 wrap calls, 63,026 Proxy gets, 96,028 elementSlot reads and 66,028 API
observation checks. Only 28 observation checks cross to Go; wrap performs zero
nodeData calls. Total workload host calls remain 54,055. All raw artifacts,
diagnostic patch and launch-hash receipts are preserved with the report.

## Repository hydration compatibility workload — 2026-09-09

The first blocking module semantic on the repository-page workload was missing
`import.meta.url`. An independent local module → fetch → DOM reproduction now
populates correctly after supplying the module resource URL through V8's host
initializer. Synchronous rejected module evaluations are also reported. The
live site still encounters unimplemented custom-element/DOM APIs; this is not
a claim of complete GitHub hydration or a site-specific optimization.

Fresh DOM/static/React profiles and before/final fast gates preserve build and
per-launch hashes. All six correctness workloads pass; frozen files are unchanged.
DOM completion 111.249 → 113.278 ms (+1.82%); static 43.556 → 44.273 ms (+1.65%);
React 86.146 → 81.731 ms (−5.13%). Static throughput N10 55.593 → 53.023 Pages/s
(−4.62%), N25 72.954 → 71.884 (−1.47%). Marginal RSS per Page: static
24.427 → 24.675 MiB, React 29.102 → 29.161 MiB. Retained RSS above ready baseline
after teardown +250 ms: static 69.367 → 72.480 MiB, React 89.164 → 89.172 MiB.
These single-gate deltas include regressions and environmental variance; no
performance benefit is attributed to this compatibility fix. No full-matrix
milestone is claimed for this one scoped change.

[Investigation, limitations and raw gates](../compatibility/repository-hydration-20260909/report.md).

## Generic hydration domains and shadow export — 2026-09-09

The repository workload now populates metadata/file rows and its static export
retains all 23 relative dates. Changes cover canonical DOM/CharacterData/templates,
mutation reactions/custom elements, mature selector parsing/matching and Streams,
Fetch bodies, form values, canonical CDP serialization and Shadow DOM export.
There is no site-specific production path. Independent reproductions and the
[domain coverage matrix](../compatibility/coverage.md) record substantial remaining
WPT gaps; API presence is not counted as semantic coverage.

Fresh profiles preceded the selector, DOM, reactions, forms and final serialization
change classes. An initial fetch profile failed its DOM timeout while exposing an
O(N²) live childNodes bridge; that failure is retained rather than called a pass.
The resulting canonical ChildCount/ChildAt access and measured selector adapter
changes removed unnecessary projections/crossings. A later DOM host profile fell
from 60,489 crossings / 129.47 ms execution to 54,491 / 79.49 ms, versus baseline
54,485 / 72.22 ms. Final integration changed between these profiles, so the last
step is not a clean isolated attribution. See
[profile receipts](../compatibility/selectors-domain-20260909/perf-attribution.json).

The isolated release fast gate and source baseline `93656f5` both passed all six
frozen correctness workloads. Warm medians (five samples, excluded warm-up):

| Metric | Baseline | Release | Delta |
|---|---:|---:|---:|
| DOM completion, ms | 110.046 | 125.897 | +14.40% |
| Static completion, ms | 57.615 | 52.905 | −8.17% |
| React completion, ms | 85.203 | 99.600 | +16.90% |
| Static N10 throughput, Pages/s | 56.791 | 55.596 | −2.10% |
| Static N25 throughput, Pages/s | 89.404 | 75.038 | −16.07% |
| Static marginal RSS/Page, MiB | 24.380 | 26.118 | +1.739 |
| React marginal RSS/Page, MiB | 28.771 | 30.854 | +2.083 |
| Static retained RSS after teardown +250 ms, MiB | 68.254 | 78.070 | +9.816 |
| React retained RSS after teardown +250 ms, MiB | 86.051 | 91.219 | +5.168 |

Compatibility improved at a measurable cost: remaining bootstrap/execution and
memory regressions are not hidden or described as performance wins. No new
global runtime lock was introduced. These are single paired gates with OS variance,
not confidence intervals. The earlier `gate-integrated` ran alongside Go tests
and is explicitly excluded from final performance conclusions.

Build/per-launch executable hashes and unchanged frozen harness hashes are in
[release fast-gate evidence](../compatibility/repository-hydration-20260909/performance-comparison.json).
Raw gates and profiles remain under `.build/hydration-complete/`. The full matrix
was interrupted before completion. All 12
Chrome/Mimic correctness gates and 360 measured cold/warm single-page samples
completed successfully. Completed concurrency waves also passed correctness;
Chrome CPU N100 stopped under the frozen sustained-paging rule. The final React
N100 series/startup calibration did not finish, so this is not a completed full
matrix. [Partial results and launch receipts](../compatibility/repository-hydration-20260909/full-matrix-summary.json)
are retained; the original baseline is immutable.

## 2026-09-09 — Weather and additional site hydration

Release fast gate at source `40581bd` passed all six unchanged correctness
workloads, all measured concurrency waves, and both teardown/memory waves.
The executable was freshly built and checked before each launch; the frozen
harness hash remained unchanged. No full benchmark matrix was run for this
compatibility batch.

| Metric | Release observation |
|---|---:|
| Warm DOM completion median | 158.752 ms |
| Warm static completion median | 53.954 ms |
| Warm React completion median | 109.916 ms |
| Static N10 throughput median | 56.078 Pages/s |
| Static N25 throughput median | 73.916 Pages/s |
| Static marginal RSS/Page, N10 | 26.769 MiB |
| React marginal RSS/Page, N10 | 31.954 MiB |
| Static RSS after teardown/recovery | 102.063 MiB |
| React RSS after teardown/recovery | 118.918 MiB |

This is a release checkpoint, not a paired before/after experiment. DOM completion
and retained RSS are higher than the preceding report's release checkpoint;
the new DOM/CSSOM/event bindings have a compatibility cost, and this batch does
not claim a performance win. Bootstrap and retained state remain measured areas
for subsequent optimization. No global runtime lock or workload shortcut was added.

Live weather profiling found two correctness failures with severe CPU/memory
amplification: more than 120,000 listeners accumulated through premature Window
load events; an unsupported attribute-removal operation generated over 12 million
calls in a sanitizer loop. The semantic repairs eliminate both observed loops.

[Compact gate evidence](../compatibility/site-hydration-20260909/performance-summary.json)
includes executable/harness hashes, concurrency results and memory observations.
Raw data remains in `.build/site-compat-20260909/fast-gate-release/`.

## 2026-09-09 — GitHub profile lookup and cold module loading

Native profiling of a stalled profile identified DOM traversal through repeated
JavaScript/Go crossings in `getElementById`, reached from custom-element
callbacks. A direct traversal of the canonical node arena removes those
crossings without introducing a duplicate ID index. Per-element reaction queues
also preserve the Chrome-observed order during nested attribute callbacks.
The profile then completed in 12.05 s instead of exceeding the 35-second
diagnostic timeout. A separate final-build snapshot run loaded in 14.81 s and
captured in 10.89 s; capture/resource collection remains a substantial cost.

Static module preloads now overlap and share responses with imports within the
realm, including non-cacheable responses. A deterministic local regression test
requires both requests to start before either response is released, verifies
one request per module and checks dependency-first evaluation.

BrowserScan navigation observations on fresh processes were 2248.64 ms before
and 1787.47 ms after this batch; subsequent after runs were 432.26 and 420.95 ms.
These live-network samples are not a controlled speedup estimate. Cold DNS/TLS
and network cache state still explain part of the first-run gap. Snapshot capture
was 513.82 ms before and 593.88 ms after on the first run; it did not improve in
this observation. No hidden warm-up navigation was introduced.

The fresh-build fast gate passed all six unchanged correctness workloads and
all concurrency and teardown waves. Tests and live captures did not overlap the
gate. This is a release checkpoint, not a paired benchmark comparison.

| Metric | Release observation |
|---|---:|
| Warm DOM completion median | 162.308 ms |
| Warm static completion median | 55.324 ms |
| Warm React completion median | 113.256 ms |
| Static N10 throughput median | 56.283 Pages/s |
| Static N25 throughput median | 75.945 Pages/s |
| Static marginal RSS/Page, N10 | 26.786 MiB |
| React marginal RSS/Page, N10 | 31.959 MiB |
| Static RSS after teardown/recovery | 103.445 MiB |
| React RSS after teardown/recovery | 120.777 MiB |

DOM/static/React latency and retained RSS are slightly higher than the preceding
checkpoint; this batch does not establish a general benchmark improvement.
The full matrix was not run. The frozen harness and original baseline remain
unchanged. [Compact evidence](../compatibility/github-loading-20260909/validation.json)
includes binary/harness hashes and measurements; raw gate data remains in
`.build/github-fix-gate/`.

## 2026-09-09 — SVG snapshots and detached image requests

The fresh-build SVG/image checkpoint passed all six frozen correctness workloads,
all concurrency waves and both memory/teardown waves. The harness and baseline
were unchanged; no full matrix was run. This is a release observation, not a
paired before/after comparison. Other interactive runtime processes were present
on the machine, so these samples do not establish small performance changes.

| Metric | Observation |
|---|---:|
| Warm DOM/static/React completion medians | 161.137 / 55.335 / 114.047 ms |
| Static N10/N25 throughput medians | 56.339 / 76.686 Pages/s |
| Static/React marginal RSS per Page, N10 | 26.607 / 32.080 MiB |
| Static/React RSS after teardown/recovery | 103.348 / 121.148 MiB |

Live tracing found a cached detached-image request waiting 14.6 seconds before
transport start behind unrelated work at the lowest scheduler priority. Image
starts now use ordinary resource tasks, retaining precedence for script starts.
The verified BrowserScan export subsequently loaded, reached its selected ready
state and captured in 3.61 s total. A prior fixed-delay capture took 36.36 s and
missed the background. These live observations are not controlled speedup ratios.
One live BrowserScan teardown stalled and required stopping the diagnostic
process; the successful local teardown gate does not resolve that limitation.

[Build and measurement receipts](../compatibility/svg-snapshot-20260909/performance.json)
and [visual verification](../compatibility/svg-snapshot-20260909/report.md) retain
the evidence and boundaries. Raw gate data is in `.build/svg-snapshot-gate/`.


## 2026-09-09 — Callback exception propagation and lazy style geometry

A native V8 CPU profile of an isolated live weather.com navigation attributed
27.6% of weighted samples inclusively to computed CSS declarations and 25.2% to
layoutRectFor. Reading one computed property eagerly calculated both fallback
dimensions, recursively resolving descendant styles even when the requested
property was unrelated or already declared. Fallback dimensions are now lazy;
no stylesheet cache or invalidation model was introduced.

The same batch restores useful errors from direct V8 function calls and preserves
thrown-value identity through reentrant host callbacks. The fresh-build fast gate
passed its correctness, concurrency, and memory waves. Compact measurements and
binary/harness hashes are in the linked receipt. These are unpaired measurements
with other interactive processes present, not a demonstrated end-to-end speedup.
The frozen harness and original baseline were unchanged; no full matrix was run.

Live weather.com still exceeded a full-load timeout, as did frozen Chrome 152.
A DOMContentLoaded-plus-three-seconds export took 14.93 seconds but retained
forecast placeholders. Live teardown also stalled. Neither the successful local
gate nor the CSS profile establishes that live hydration, load completion, or
teardown is resolved. See the [investigation and limitations](../compatibility/callback-style-20260909/report.md)
and [measurement receipt](../compatibility/callback-style-20260909/performance.json).

## 2026-09-09 — Incremental document parser diagnostic

`BenchmarkDocumentParser` compares ordinary batch parsing with one streamed
write plus close of the same 2,178-byte HTML fixture (24 sections with text and
tables). The stream case includes initial blank-Document creation and teardown.
On the local Windows amd64 i7-14700KF, one 300 ms benchmark sample measured:

| Path | Time/op | Throughput | Bytes/op | Allocations/op |
| --- | ---: | ---: | ---: | ---: |
| Batch | 55.7 µs | 39.09 MB/s | 103,035 | 1,096 |
| Stream | 273.5 µs | 7.96 MB/s | 171,276 | 4,938 |

This is a local diagnostic, not a reportable browser-workload comparison or a
performance improvement. The canonical DOM adapter currently reconstructs node
projections, including attributes, for tree-builder reads; it also crosses a
stream-local channel at tokenizer starvation and script boundaries. These are
known overheads to profile before optimizing. The implementation prioritizes
canonical node identity, reentrant insertion, and parser correctness. No frozen
workload, baseline, or performance harness was changed; no full matrix was run.

## 2026-09-09 — Cross-realm object identity lookup

A live navigation reached a 30-second DOM-task deadline while making steady
progress through hundreds of remote Window properties. The canonical handle
lookup scanned retained objects and performed one native equality check per
candidate. A bounded diagnostic measured 100 repeated object encodings:

| Retained objects | V8 linear scan | V8 identity index |
| --- | ---: | ---: |
| 10 | 29.75 ms | 30.79 ms |
| 100 | 76.28 ms | 31.60 ms |
| 500 | 292.01 ms | 27.33 ms |

The replacement uses a captured, realm-owned WeakMap to index canonical object
identity. The existing Go handle table still owns retained values; object
contents are not copied or cached. Goja measured about 1 ms in both paths.
These single local samples isolate lookup scaling, not end-to-end browser
speedup. Symbol lookup still scans retained handles. Raw diagnostic logs are
`.build/frame-handle-probe.log` and `.build/frame-handle-probe-after.log`.

The fresh-build fast gate passed all six mandatory workloads, eight static
concurrency waves (10/25 pages), and both memory waves. Its build receipt and
raw measurements are in `.build/worker-reference-indexed-fast-gate/`. Warm
completion medians were 159.53 ms (DOM), 57.99 ms (static), and 114.24 ms (React).
Other validation and a live navigation ran concurrently, so these are unpaired
health checks, not a browser speedup comparison. The frozen harness and baseline
were unchanged; no full matrix was run.
## WebKit CSS parsed-state checkpoint (2026-09-10)

The iterative fast gate passes all six frozen correctness workloads after moving
inline parsed declaration state onto its owning DOM node. Current-run median
completion times are DOM 176.24 ms, static 65.01 ms and React 125.51 ms. The three
measured 25-session static waves reach 65.69, 66.78 and 65.40 sessions/s.
These are checkpoint measurements, not a paired before/after speedup claim.
The gate verifies the freshly built executable hash on every launch and leaves
the frozen harness unchanged. Raw workload, concurrency and memory observations
and the build receipt are in `webkit-css-state-20260910/`. Full ordinary tests
and focused browser/DOM race tests also pass.


## 2026-09-10 — Integrated window bootstrap snapshots

The current implementation caches complete preinitialized window surfaces per
browser Context, retaining separate isolates and ordinary cross-frame bridges.
It starts from restored `2a3f362`; historical shared-parent experiment timings
are not its baseline. Cache admission is bounded to four entries / 32 MiB.

A bounded ten-Page memory probe passed 111 runtime-close assertions. The final
four-stage seed is 5,220,080 bytes (4.978 MiB), down from the initial 13,954,264
bytes. With eleven live isolates after GC, private memory was 340.816 MiB versus
268.664 MiB with snapshots disabled; Go heap was 71.071 versus 11.723 MiB.
After Page.Close the snapshot cache remains intentionally. After Context.Close
and GC, Go heap was 10.557 versus 10.570 MiB in control, with no remaining
consumer snapshot roots, handles or contexts. This establishes a material live
copy cost and recovery at full closure, not complete native allocator attribution.

The final creation-phase diagnostic attributes 8.425 ms of a 15.148 ms
instrumented mean to native isolate creation and Context deserialization; restore
publication/rebinding is 0.867 ms. Nonoverlap was verified, but uninstrumented
medians around 16.2 ms show process/code-layout variation. See the
[breakdown, method and limitations](bootstrap-snapshot-20260910.md#remaining-iframe-creation-cost)
and [implementation and memory data](bootstrap-snapshot-20260910.md).
The unchanged complete frame probe measures 130.35–139.82 ms with ordinary
bootstrap versus 97.98–105.24 ms with snapshots in final A/B/B/A runs (24.8%
lower two-run mean). Both final fast gates pass all six semantic workloads,
concurrency and memory waves. Warm static completion is 72.29 versus 25.38 ms;
React is 150.24 versus 106.68 ms. Cold cache admission remains real work.
The full test checkpoint passes 1,071 test/subtest events; the final source also
passes the snapshot race corpus, including exact exception-stack regression.
One earlier process exit did not reproduce in subsequent gates and dedicated
stress runs; its cause remains undetermined and is documented in the detailed
report rather than presented as fixed. Final build and raw measurement receipts
are linked there.
## 2026-09-11: dedicated-worker native completion starvation

A local V8 regression starts `WebAssembly.instantiate` in a Blob worker with
no timers or subsequent messages. Before the fix it failed to deliver a result
within 3 seconds. The worker runner woke every 2 ms, but an empty scheduler did
not perform a checkpoint, so completed native foreground work remained pending.
Window realms already scheduled a Control task while native work was pending;
dedicated workers now use the same mechanism. No JavaScript enters the isolate
from another goroutine and no unrelated browser task is required for completion.

The private 014358 capture corroborates the cause: worker task 14 ended at
21:44:06.2717582 UTC (sequence 2660), and its next task was an unrelated timer at
21:44:08.8094005 (2811), a 2537.6423 ms gap. The timer's checkpoint (2814) then ran
the WASM reaction, whose 69.8967 ms workload posted its result at 08.8792972
(2819). The reported instantiate-to-reaction interval was 2539.7027 ms. These
observations localize the seconds to idle native-task servicing, not compilation.

After the fix, three local regression runs measured 2.0401–2.0984 ms from worker
script start to result posting, excluding bootstrap. Total worker creation and
delivery was 116–139 ms in those runs; this is a small correctness fixture, not
the captured SIMD workload or a new Chrome benchmark. The worker/native-WASM,
microtask ordering and immediate-termination focused tests passed three times;
the new regression also passed with `-race`. The protected workload was not
replayed and no conclusion about its server decision follows from this fix.

### Performance clock quantization

Frozen Chrome152.0.7977.82 local worker probes (20,000 reads) observed a100us
non-isolated grid and a5us isolated grid; both were monotonic and repeated
values within a bucket. The implementation now uses stable per-bucket random
transition thresholds, clamps the absolute timestamp and origin separately,
and shares an immutable Page-owned seed with its realms/workers. It preserves
scheduler time ownership and adds no global browser lock. The algorithm was
checked against [Chrome152 TimeClamper](https://raw.githubusercontent.com/chromium/chromium/152.0.7977.82/third_party/blink/renderer/core/timing/time_clamper.cc)
and its Performance timestamp conversion. Window and worker tests cover both
security modes on V8 and Goja, monotonicity, repeatability and zero origin.

A fresh `.build/mimic-timing.exe` loopback run of the captured SIMD worker code
reported8.7ms instantiate-to-reaction and67.7ms workload execution, compared
with the earlier isolated live binary7.8893/69.8623ms and Chrome1.9/66.7ms.
These individual runs show no claim of compilation speedup; the fixed defect
is completion starvation when the worker is otherwise idle. The observed
minimum adjacent clock-read delta was0.4ms despite100us quantization: host-call
cost remains and is not a clock-resolution measurement. Only performance.now
was coarsened here; timeOrigin and other timing-entry producers were not migrated.
Private oracle and build-probe data: vm-stages-014358/chrome-clock-*.json and
mimic-fixed-worker-probe.json. Existing worker/capture/performance tests pass.

Correction after independent clock-source probes: the0.4–0.5ms adjacent-read
minimum above was incorrectly attributed to host-call cost. A native Go loop
without V8 returned999,993 identical adjacent timestamps out of1,000,000reads,
minimum positive512.5us, while QPC on the same host returned minimum0.1us.
Scheduler.nowLocked currently uses time.Since, backed by Windows interrupt time
in this Go runtime. The coarsener therefore receives a source coarser than its
100us/5us target. No precision-source fix is included yet. Evidence and probe
sources: private-captures/vm-stages-015815/report.md.

## 2026-09-11: QPC source for scheduler elapsed time

Windows scheduler task start, in-task clock reads, task completion and WaitAny
elapsed accounting now use QueryPerformanceCounter through internal/monotime.
The worker loop uses the same source when advancing idle virtual time. QPC is
anchored once and read without changing system timer resolution or introducing
browser-wide serialization; Page/realm virtual clocks and execution scaling
remain authoritative. Other platforms retain Go monotonic time.

A fresh .build/mimic-qpc.exe measured0.1000ms in the same5000-pair worker clock
probe, matching Chrome152's0.1000ms; the preceding coarsened interrupt-time
build measured0.4ms. The underlying QPC source test checks monotonic reads and
positive increments finer than100us. The captured WASM workload on that local
page measured8.8ms instantiate-to-reaction and66.3ms execution (single run,
not a compilation speedup claim). Private receipt: vm-stages-014358/
mimic-qpc-worker-probe.json. These local probes do not imply that a protected
site's server decision changes. Scheduler and browser timing/worker tests were
run alongside the clock-source regression.

Validation: internal/monotime and the full scheduler suite passed, including
-race. Focused browser clock-grid, timer/fetch, worker microtask/native-WASM
and capture regressions passed with -race (27.5s). A broader -race selection
also included performance profiles and was stopped before completion; no full
performance-matrix or full browser race-suite result is claimed.


## Window reflection and iterator-result bridge, 2026-09-11

The WindowProxy reflection fix preserves the complete Chrome key surface; the
pre-fix parent enumeration's tiny wrapper result is not a valid fast baseline.
Publication order now matches frozen secure/insecure/isolated Window captures.
See [compatibility scope and correctness](../compatibility/window-reflection-2026-09-11.md).

A bounded A/B/B/A experiment compares identical corrected builds with the fresh
native iterator-result specialization enabled/disabled (10 observations each).
The same 1234 keys, property types, identity, descriptors and getter counts agree
in every observation. Complete local operation median: **520.35 → 288.65 ms**
(44.5% reduction); remote-array materialization median: **340.20 → 140.75 ms**.
No iterator lookahead or array snapshot is used. The result's primitive fields
are invalidated before mutation or any direct/transitive reference escape.
Ordinary remote reflection metadata is encoded on one owner turn. Raw controls:
`compatibility/private-captures/window-sweep-controls-20260911/`.
These numbers exclude iframe creation and are not a protected-site timing claim.

The unchanged fast gate was run on fresh b94c63c and corrected builds. The first
corrected run lost its process in the 25-Page static wave; exact failure cause
is unavailable because the frozen runner removed its process log. Baseline and
corrected repeat completed all mandatory workloads and concurrency/memory gates.
The repeat captures process logs through a wrapper around Runtime.close, without
changing the frozen workloads or assertions. The intermittent failure remains
open, not reclassified as a confirmed preexisting defect.

| Warm median, ms | b94c63c baseline | Corrected repeat |
|---|---:|---:|
| DOM execution / completion | 449.70 / 472.95 | 461.19 / 497.38 |
| Static execution / completion | 2.99 / 22.84 | 2.95 / 24.04 |
| React execution / completion | 56.32 / 83.75 | 56.10 / 84.07 |

Single-wave process-tree private memory (MiB), 10 simultaneous Pages:

| Workload | Baseline active / after recovery | Corrected active / after recovery |
|---|---:|---:|
| Static | 431.48 / 165.07 | 444.89 / 171.89 |
| React | 484.71 / 173.96 | 492.73 / 173.52 |

Ready private memory is about77MiB. Recovery is the harness's250ms window, not
forced GC or proof of leak absence. This batch improves cross-frame iteration;
it does not claim a general workload speedup or lower memory usage. Receipts:
`.build/window-reflection-fast-gate{,-baseline,-repeat}/raw.json` and build.json.

## Interface-inheritance cleanup control, 2026-09-11

The clean `037f8ff` baseline gate **FAILED** in its first measured 10-Page static
wave after a valid warm-up: native `0xc0000005`, process exit 2, disconnected CDP.
The process log is retained; its truncated stack does not identify a serializer
or string-table cause. No retry replaced this failure. The corrected interface
inheritance build passed all six mandatory semantic workloads, warm scenarios,
10/25-Page waves and memory checks. This is not a complete passing A/B comparison
or evidence of snapshot repair; P0 remains open.

Final-build warm medians, execution / completion milliseconds: DOM 446.74 / 466.04,
static 2.61 / 23.18, React 56.85 / 84.09. Measured static wave throughput ranges:
10 Pages 49.07–73.17 sessions/s; 25 Pages 58.89–115.87 sessions/s. Active / recovered
private memory for the 10-Page memory waves: static 428.30 / 155.77 MiB, React
485.20 / 165.45 MiB. Recovery is the unchanged 250 ms window, not forced GC or a
proof of leak absence. No performance improvement is claimed from this pair.

The frozen runner and workloads are unchanged. A diagnostic wrapper copies each
process log and records its exit status before the runner removes temporary files.
Receipts: `compatibility/private-captures/cleanup-20260911/gate-{before,final}/`.

### Srcdoc navigation validation, 2026-09-11

The unchanged complete fast gate passed for the srcdoc navigation package,
including 10/25 concurrent Pages and static/React retained-memory waves after
teardown. A first-chance ProcDump collector monitored each owned process and
produced no exception dump. Debugger timing precludes a performance improvement
claim. This finite pass does not close P0: clean 037f8ff separately reproduced
a native StringTable failure with a full dump. See the
[srcdoc validation receipt](../compatibility/iframe-srcdoc-20260911/gate.json)
and [native investigation](../compatibility/snapshot-firstchance-2026-09-11.md).

### Captured owner bindings, 2026-09-11

Two complete monitored baseline/changed gate pairs passed all mandatory
workloads, 10/25-Page concurrency and retained-memory waves. The latest pair
has warm completion medians DOM 497.91→534.20 ms, static 26.65→26.86 ms,
React 89.35→84.39 ms. DOM execution itself is 468.17→463.99 ms.
Throughput medians are 73.71→66.41 sessions/s (10 Pages) and 71.98→71.14
(25 Pages); the initial 25-Page baseline was 89.15, demonstrating substantial
run variation. Post-recovery private memory is static 158.29→162.10 MiB and
React 169.94→170.50 MiB. The 10-Page decrease/static memory increment are
not dismissed or claimed neutral; their cause needs profiling. First-chance
debugger overhead and two pairs do not support an improvement claim.
No native exception was captured; P0 remains open.
[All receipts](../compatibility/owner-bindings-20260911/gates.json).
Final tested executable SHA256:
`2c951f5c4a8c393c4a0c26f44aa25c9599478d3f897495441cb63dc18b80b7e0`.
See [semantic coverage](../compatibility/cleanup-2026-09-11.md) and the
[separate native stability report](../compatibility/snapshot-cleanup-control-2026-09-11.md).

## Location ownership and reflection checkpoint — 2026-09-11

The clean 8780592 baseline gate failed at the first measured 10-Page static
wave after a valid warmup, with native access violation and exit 2. A new
389,882,223-byte first-chance dump and full stderr were retained. The changed
Location/reflection build completed the full gate, including every mandatory
workload, 10/25-Page waves and teardown-memory measurements. This is not a
passing pair and does not close native P0. No failed workload was excluded or
retried into a passing result.

Warm completion medians before/after (ms): DOM 491.32/489.90, static
26.89/26.94, React 94.65/86.41. Changed throughput medians: 66.08 sessions/s
at 10 Pages, 86.64 at 25. Changed post-recovery private memory: 166.94 MiB
static, 172.37 MiB React. The baseline interruption leaves its corresponding
concurrency and memory comparison incomplete; previous throughput/memory
concerns are not declared resolved. Debugger overhead remains part of both
attempted topologies.

[Receipts](../compatibility/location-owner-reflection-20260911/gates.json) and
[semantic validation](../compatibility/location-owner-reflection-2026-09-11.md).
Tested executable SHA256:
`cc97570c06cf010f6525cd41c946a935f603bfab1d71061105eb1403724ee88d`.


## Distinct snapshot read-only layouts - 2026-09-11

The P0 ownership correction retains snapshots and concurrent Pages while
preventing custom objects from extending the isolate group's shared read-only
layout. A clean 71d54f0 complete gate failed natively in the first measured
10-Page static wave. Three predeclared changed gates completed every mandatory
workload, 10/25-Page concurrency and teardown-memory wave, with no native dump.
Both sides used the same first-chance debugger collector and frozen harness.

| Metric | Baseline (incomplete gate) | After 1 | After 2 | After 3 |
|---|---:|---:|---:|---:|
| DOM warm execution / completion ms | 447.20 / 476.13 | 450.92 / 487.70 | 444.13 / 475.45 | 440.52 / 465.51 |
| Static warm execution / completion ms | 2.89 / 26.17 | 2.94 / 31.01 | 2.80 / 27.83 | 2.78 / 24.74 |
| React warm execution / completion ms | 53.83 / 81.73 | 53.58 / 84.96 | 54.18 / 83.86 | 47.96 / 79.02 |
| Throughput, 10 Pages, sessions/s | incomplete | 66.46 | 76.41 | 73.20 |
| Throughput, 25 Pages, sessions/s | not reached | 72.75 | 87.58 | 86.32 |
| Static private memory after recovery, MiB | not reached | 159.97 | 159.31 | 156.77 |
| React private memory after recovery, MiB | not reached | 172.91 | 166.72 | 164.44 |

A failed wave's zero throughput is not a performance measurement. This is not
three successful pairs and does not establish performance neutrality or close
all historical native signatures. The Goja-only retained-srcdoc race test also
timed out on the changed tree; the V8 flag is not executed in that subtest.
The independent timing concern remains open without deadline changes.
[Native scope and validation](../compatibility/snapshot-readonly-lineage-2026-09-11.md);
[full gate receipts](../compatibility/snapshot-readonly-lineage-20260911/gates.json).


## Native function finalization - 2026-09-11

Both complete monitored gates passed: clean ed4261b and the source-finalization
package. All mandatory workloads, 10/25-Page waves and teardown-memory workloads
ran; neither attempt produced a native dump. This adds bounded P0 evidence and
does not causally close the separately retained historical signatures.

Warm execution/completion medians before / after (ms): DOM 438.72/469.39 /
439.59/484.31, static 2.98/25.61 / 2.73/24.32, React 55.05/83.80 / 54.58/86.16.
Throughput is 74.48 / 69.42 sessions/s at 10 Pages and 84.48 / 69.22 at 25.
Post-recovery private memory is static 159.92 / 164.92 MiB and React 168.36 /
175.36 MiB. These decreases/increases remain a performance concern, not a
neutrality claim. One monitored pair does not identify the cause or separate
implementation overhead from the earlier measured run variation. Profiling is
required before undertaking an optimization or attributing the complete delta.
No workload was excluded or retried into a pass.

[Complete receipts](../compatibility/native-function-finalization-20260911/gates.json)
and [semantic scope and validation](../compatibility/native-function-finalization-2026-09-11.md).


## Callable metadata - 2026-09-11

Clean 706fc15 and the metadata-normalization build both completed the full
monitored fast gate, including mandatory correctness, 10/25-Page concurrency
and teardown memory. No native dump or workload exclusion occurred.

Warm execution/completion before / after (ms): DOM 443.43/476.26 /
450.52/506.93, static 2.96/31.10 / 2.74/33.02, React 48.95/83.16 / 50.90/81.79.
Throughput is 73.01 / 64.62 sessions/s at 10 Pages and 69.98 / 62.49 at 25.
Static private memory after recovery is 150.80 / 159.03 MiB; React 170.87 /
169.02 MiB. Throughput/latency remain a performance concern, with profiling
pending; no neutrality or whole-delta attribution is claimed. The full browser
suite passed in 515.399 s versus 435.027 s in the preceding package, which is
retained as an uncontrolled suite-timing observation. Targeted browser race
passed; the separate prior Goja deadline issue is not closed by that filter.

[Complete receipts](../compatibility/callable-metadata-20260911/gates.json) and
[semantic coverage and limits](../compatibility/callable-metadata-2026-09-11.md).


## Goja observer profiling checkpoint (2026-09-11)

[Detailed results and limitations](../compatibility/goja-observer-2026-09-11.md)
record the completed native/density profile pair and a measured redundant
Reflect.has path in Goja global observation. Deduplicate support queries before
calling Reflect.has; preserve live membership, accessor receiver and exception
identity. Corrected baseline regression fails and changed engine race passes.
Both retained-srcdoc race/CPU-profile runs still exceed the original deadline
(25.59/23.53 s); no timeout resolution or complete workload speedup is claimed.
Full browser passes. V8 fixed differential/control and corpora are unchanged.
The earlier concurrency throughput concern and unreduced snapshot signatures
remain open; diagnostic forced Go reclamation is not a production workaround.


## Document getter ownership (2026-09-11)

[Package evidence](../compatibility/document-getter-ownership-2026-09-11.md):
full browser and targeted race pass; paired complete gates pass with no native
dumps. DOM execution/completion 441.26/472.10 to 444.81/479.53 ms; static
2.86/26.86 to 3.32/28.65; React 53.97/88.92 to 54.83/83.45. Throughput at
10/25 Pages 71.12/62.36 to 74.02/72.72 sessions/s. Static/React recovered private
memory 157.96/165.97 to 162.19/170.81 MiB. These mixed single-pair measurements
do not establish neutrality or complete owner-reference reclamation. Focused
semantic differences fall 9 to 0 without changing the fixed general corpus.


## Concurrent snapshot serialization boundary (2026-09-11)

[Separate P0 investigation](../compatibility/snapshot-serialization-2026-09-11.md)
reproduces CreateBlob writes into an OS-protected read-only page using only four
snapshot builders. Three same-binary default/stock-RO-heap pairs yield three
native failures and three complete passes. The existing ed4261b configuration
covers this new reproducer without serializing Pages or disabling snapshots.
The new subprocess regression and full engine/race suites pass. This does not
close the original SizeFromMap signature or turn finite fast gates into proof
of global native stability. No new production performance change is introduced.


## Attr/Node binding checkpoint (2026-09-11)

[Package and full receipts](../compatibility/attr-node-2026-09-11.md): all mandatory
paired fast-gate workloads pass without native dumps, after correcting an import
failure by selecting the existing benchmark Python environment. Throughput at
10/25 Pages is 74.33/83.15 to 70.98/63.14 sessions/s. DOM completion is nearly
unchanged (482.05/484.04 ms), React completion increases 84.11 to 93.03 ms.
This remains an open performance concern; a single mixed pair does not establish
causality or neutrality. A separate unobserved-iframe diagnostic retained nine
realms after eight removals with zero exported Window references; lifetime cleanup
is the next measured ownership task. The current production change fixes Attr
inheritance/Node borrowing; it does not yet change realm reclamation.

Static/React private memory after recovery is 156.75/166.61 to 157.24/170.47 MiB.


## Unobserved detached-frame reclamation (2026-09-11)

[Full evidence](../compatibility/detached-frame-lifetime-2026-09-11.md): after 32
unobserved iframe removals, live realms improve 33 to one and private memory
1051.06 to 287.50 MiB. Page-close plus 250 ms private memory improves 533.83 to
250.56 MiB without forced collection. Both complete paired fast gates pass with
no native dumps; their throughput/latency results are mixed and do not close the
broader concurrency concern. Expanded race still hits the pre-existing Goja
retained-srcdoc setup deadline; full race success is not claimed. Strong bridge
caches and DOM arena reclamation remain separate boundaries.

## Goja bootstrap cache and exposure transport (2026-09-11)

[Complete checkpoint](../compatibility/bootstrap-transport-2026-09-11.md): five-Page cold/warm Goja waves improve 527.12/443.03 to 485.05/333.42 ms; warm recovery private memory improves 551.71 to 483.23 MiB. Diagnostic post-GC live heap increases 13.72 to 21.57 MiB because compiled code is retained. Both full V8 gates pass with mixed latency/throughput results. Full browser race still fails six Goja deadlines; no data race is reported. Performance neutrality and snapshot P0 closure are not claimed.

## Node names, host records and DOMException package (2026-09-11)

The complete fast gate passes for 660713a against bda67db. Median 10/25 Page throughput changes from 69.42/71.48 to 71.01/69.67 Pages/s. Static/React recovery private memory changes from 159.73/170.54 to 170.93/163.82 MiB. Completion latency increases in this sequential pair; performance neutrality is not established. See [package validation](../compatibility/domexception-state-2026-09-11.md) for latency, test status and evidence. No native dump occurred in this gate; historical snapshot P0 remains open.

## Platform binding bootstrap allocation (2026-09-11)

[Package and complete receipts](../compatibility/platform-bindings-2026-09-11.md)
compare fresh 6bfa245 with the integrated descriptor/native-source optimization
and three semantic fixes. Cold five-Page Goja startup improves about 3–4% in two
pairs; the latter warm comparison is effectively unchanged, 389.010→388.170 ms.
Normal recovery private memory is variable across the two pairs; diagnostic
post-GC live heap is nearly unchanged. Full browser tests and targeted browser
race pass, including all six previously failing Goja deadline scenarios.

Both full fast-gate attempts and the pair after releasing our idle servers are
preserved as incomplete failures. The latter pair passes six mandatory workloads,
10-Page waves and all 25 warmup operations, then hits the unchanged host RAM guard.
Measured 25-Page and dedicated memory waves do not execute. Median 10-Page
throughput is 74.19→73.16 Pages/s; DOM/static/React completion is
517.87/27.74/90.03→518.84/25.97/94.67 ms. No V8 throughput improvement, full gate
pass or overall P0 closure is claimed. No native dump or unexpected process exit
was observed in these attempts.

## Optimization backlog after semantic checkpoint (2026-09-11)

Per the current scope decision, performance/concurrency investigation is separate
from semantic cleanup. The preceding measurements and
[platform-binding performance receipt](../compatibility/platform-bindings-20260911/performance.json)
remain unchanged. Both recovered gates reached the 25-Page warmup but stopped at
the host-memory guard; measured 25-Page and dedicated teardown-memory waves did
not execute. They remain failed/incomplete gates, not passes.

Next work requires sufficient RAM to complete the unchanged gate, then matched
latency, throughput, concurrency and retained-memory measurements after teardown.
Profile Goja bootstrap allocation and retained bridge/DOM arena state before a
substantial optimization. Do not extrapolate the small measured Goja cold-start
change to V8 throughput or overall memory recovery. The semantic checkpoint adds
no new performance claim and does not rerun gates merely to chase a green result.

The unexplained broader native snapshot signatures now have a separate
[stability-debt disposition](../compatibility/snapshot-stability-debt-2026-09-11.md)
with immediate P0 reopen on any new native reproduction.

## Cold child snapshot eligibility (2026-09-11)

[Cold child bootstrap capture](../compatibility/bootstrap-cold-child-2026-09-11.md)
removes eager native WindowProxy creation and incidental getter reads during
constructor discovery. A bounded offline program replay changes cached snapshot
failure records from seven to zero. This is a capture correctness observation,
not a latency, throughput, concurrency or retained-memory measurement. No gate
was run for this package, as requested by the user. The existing optimization
backlog and incomplete performance results remain unchanged.

## Runtime/CDP latency and canonical DOM projections (2026-09-12)

[Measured investigation](runtime-cdp-latency-2026-09-12.md) removes repeated
whole-tree class-collection scans and full-node transport during identity-only
reads, and narrows structural selector candidates before the existing matcher.
A controlled local CDP pair improves selector 24.98 to 0.89 ms, live collection
iteration 2217.38 to 0.48 ms, and traversal 211.37 to 14.46 ms. The localhost
client connection fix separately improves 4056.46 to 58.46 ms.

Four complete frozen fast gates pass, including 10/25-Page and recovery-memory
waves. Final paired DOM execution improves 191.24 to 160.25 ms; React execution
is unchanged, but completion increases 121.48 to 150.03 ms. Throughput varies
substantially across pairs; overall neutrality is not established. Full Go tests,
focused browser/CDP race, full DOM race and eight Python tests pass. Two final
live attempts fail before snapshot while the external document is unavailable;
these are not E2E passes. See the report for remaining style initialization,
network/navigation limitations and identical baseline repository-audit failures.

## CDP automation compatibility (2026-09-12)

The [CDP compatibility checkpoint](../compatibility/cdp-automation-2026-09-12.md)
includes a complete unchanged fast gate on a fresh intermediate build. All
mandatory workloads, 10/25-Page concurrency waves and the two memory-recovery
waves passed. Median DOM/static/React completion was 218.62/35.73/123.92 ms;
median measured 10/25-Page throughput was 73.19/85.84 Pages/s. Static/React
recovery private memory was 146.80/169.79 MiB.

The [machine-readable receipt](../compatibility/cdp-automation-20260912/performance.json)
retains the executed binary hash, build state, frozen harness fingerprint,
launch checks, concurrency/memory measurements and raw-data hash. Other tests
and owned oracle processes were active on this host. This is a single
intermediate checkpoint, not a controlled baseline pair or a performance
improvement claim. Later timer/blank-frame changes are covered by the final
semantic and race checks. Prior performance measurements and stability debt
remain historical evidence and are not reclassified by this gate.

## Style runtime and high Page concurrency (2026-09-12)

The [style runtime checkpoint](style-runtime-2026-09-12.md) profiles and removes
repeated CSS rule work, large host projections and document/base lookups, and
adds Chrome-verified HTTP freshness heuristics. The live script median improves
6.23 → 2.69 s; Chrome remains faster at 1.70 s. The long queued evaluation drops
2535 → 395 ms. Two full frozen fast gates pass.

The unchanged local workload runner validates 3620 Page executions up to 100
concurrent Pages. At 50 static Pages, updated Mimic reaches 83.4 Pages/s versus
Chrome's 25.1, using 1.7 versus 4.0 GiB active RSS. This concurrency advantage
predates the patch. The patch's own parallel throughput change is not established;
100-Page medians decline in both pairs with wide wave-to-wave variance. See the
checkpoint for recovery memory, test limitations and executed binary receipts.

## Performance API semantic subsystem (2026-09-12)

The [Chrome 152.0.7977.82 compatibility package](../performance-api/README.md)
introduces authoritative document/worker timelines, observers, transport and
lifecycle timing, task/input observations and a synthetic memory projection.
The controlled differential improves from zero to 20 complete matches across
23 groups; three explicit streaming/opaque/agent-cluster boundaries remain.

Four unchanged fast gates complete with 184 VALID executions and eight verified
fresh-binary launches each. Initial-base/intermediate/final/repeated-base React
completion medians are 87.09/87.15/104.50/97.83 ms. Final 10/25-Page throughput is
68.20/68.19 Pages/s versus repeated-base 75.15/90.16, a 9.2%/24.4% decline. Host
variation is visible, but the throughput regression remains unresolved;
performance neutrality is not established. The full
[receipt](../performance-api/performance.json) preserves all runs rather than
selecting the approximately neutral intermediate pair.

Final 10-Page static/React private memory is 494.61/536.34 MiB active and
151.29/162.00 MiB after teardown plus 250 ms, versus repeated-base
488.66/538.45 and 155.07/165.18 MiB. Long-run retention and allocation-pressure
neutrality were not established. Concurrency regression profiling remains open.
Focused Performance race checks pass. Ordinary full tests passed earlier, but
the final repeat fails the snapshot navigation responseEnd relation after body
completion; this remains an open semantic defect. The full race
attempt times out at ten minutes in the browser package's Goja child-navigation
test without a preceding data-race report. See the compatibility report for
all skips, initial failed attempts and unchanged baseline audit failures.


## Environment profile contract and test setup (2026-09-12)

The versioned [Mimic profile contract](../environment-profiles.md) shares one
Context/Page environment with standard CDP. Public getters deep-copy mutable
values; internal read-only projections avoid copying complete graphics and
capability catalogs on each host call. Proxy pools remain Context-owned.

The unchanged final fast gate completes **184 VALID executions and eight fresh
binary launches**, including 10/25-Page waves and teardown-memory observations.
The [retained receipt](environment-profiles-20260912.json) records build/hash,
latency, throughput and memory. Warm static/DOM/React completion medians are
41.57/209.84/103.62 ms. Median measured static throughput is 49.18/59.96 Pages/s
at 10/25 Pages. Static/React 10-Page private memory is 537.16/561.71 MiB active
and 159.73/175.23 MiB after teardown plus recovery. This is a correctness and
resource checkpoint, not evidence of performance neutrality: no paired clean
base comparison was made, and these throughput values do not resolve the prior
Performance API regression.

An earlier gate lost its runtime connection during a 25-Page wave while a full
Go suite was active. Its process log was not retained by the frozen runner; the
cause is unestablished. A separate unchanged-workload diagnostic completed
100/100 executions across four 25-Page waves with the process alive, and the
final complete gate above passed. This does not prove the earlier failure fixed.

The full Go suite passed with the browser package taking 608.839 seconds; a
subsequent instrumented browser run passed in 693.682 seconds while other
validation was active. The default ten-minute package timeout was insufficient
in an earlier full run without a preceding assertion failure. Focused profile,
state, network, browser and CDP race checks pass, as do CLI validation and proxy
authentication/remote-DNS/no-direct-fallback checks.

Test-only commit `4923dd7` removes the unused ordinary-oracle seed navigation,
closes Context resources in the common test helpers, and limits independent
oracle cases to two concurrent executions. Both engines, snapshot-restoration
assertions and frozen expectations remain intact. Eight affected groups passed
together; representative parallel cases passed under the race detector. The
instrumented pre-change run identified the computed CSS catalog at 77.27 seconds
(57.47 seconds in two Goja cases). A final isolated before/after speed claim is
not yet established. Use `python tools/testing/timings.py <go-test-json-log>` to
inspect group wall time, or `--leaves` for individual cases. Parallel parent Go
Elapsed values omit children; the report measures run-to-completion instead.

The [controlled Chrome profile probe](../compatibility/environment-profile-contract-20260912.json)
retains native version, binary and script hashes. Stable screen/available-area,
outer-window, DPR, UA and language observations agree after metrics overrides.
Existing Workers retain their startup UA/languages in both runtimes. Initial
viewport geometry depends on the owned Chrome window's startup UI, and native
event delivery varied with hidden-window scheduling; exact event timing is not
certified by this capture. Focused regression tests cover Page-task media changes
and duplicate-change suppression separately.

## Automation waits, dialog activation and font fallback (2026-09-12)

The automation failure was not evidence that synchronous module fetching needed
an architectural rewrite. A bounded native profile attributed 9.67 seconds to
202 text-shaping calls; six module responses accounted for only about 0.2 seconds
of network time. Repeated missing-glyph coverage searches reopened and decoded
the installed font catalog. The Page-local cache now retains only proven misses
(at most 8,192 resource/cluster keys, clusters at most 128 UTF-8 bytes), not the
decoded rejected fonts. Intrinsic and control sizes are shared only within one
style read, and hidden boxes return before sizing descendants. Closed dialogs
participate in the same computed-display state used by geometry.

`BenchmarkMissingGlyphFallback` measured a repeated unsupported codepoint at
123,200 ns/op, 57,558 B/op and 774 allocations with the coverage cache, versus
1,677,422,900 ns/op, 2,896,188,920 B/op and 9,636,256 allocations when that cache
is cleared before each operation. These are cumulative allocation bytes, not
retained memory, and a narrowly targeted microbenchmark, not a whole-page speedup.

The fresh final [gate receipt](automation-activation-20260912.json) records
**184 VALID executions and eight verified binary launches**, with the frozen
harness unchanged. Warm completion medians are DOM 197.10 ms, static 36.72 ms,
and React 99.60 ms. Static 10/25-Page wave medians are 52.61/70.31 Pages/s.
Static/React 10-Page private memory is about 532/559 MiB active and 158/171 MiB
after teardown and recovery. This is a correctness/resource checkpoint, not a
paired clean-base performance comparison. The built executable hash is retained
with the dirty-source receipt; subsequent edits only added these results.

Related semantic fixes use the existing browser state: CSS font variables resolve
before shaping (including invalid-variable inheritance and zero sizes), module
metadata resolves URLs without fetching, and button commands activate dialogs
through their existing modal state. Fabricated 1x1 intersection and 16x16 CDP
fallback boxes were removed. Pointer input hit-tests the current boxes rather
than trusting a previously queried node. Stacking-context order keeps a panel's
children above its background without letting them escape its z-index boundary;
positioned auto-z groups are distinguished from actual stacking contexts.
Chrome 152.0.7977.83 probes establish
command-event flags and the inspector-only unsafe-eval/Trusted Types exception;
ordinary author tasks still enforce the document policy.

`compatibility/pyppeteer_activation_smoke.py` is an independent local end-to-end
test for XPath, strict-CSP waits, a hidden duplicate button, real pointer clicks
inside a stacking context, dialog opening, occlusion and unrelated task exceptions.
It passes against both the final Mimic binary and Chrome. Four additional native
pointer probes cover child targeting, higher overlays, escaping auto-z groups and
confinement inside lower-z contexts. Focused browser, V8,
CDP and text-metrics regressions pass. A full-suite attempt completed the browser
package in 398.72 seconds but exposed missing optional-constructor guards; those
were fixed and every failed group was rerun successfully. That attempt's QUIC
tests failed to bind UDP sockets because Windows reported exhausted socket
resources. System DNS also failed, including GitHub resolution; an unrelated
MSI process held approximately 15,500 UDP endpoints. No system service was changed.
After the environment recovered, the entire network package passed on rerun.
The complete final suite was not rerun as one invocation; the previously failing
groups and the affected regression groups passed individually.

The final gate binary also completed the live user-script flow: DOMContentLoaded
in 3.63 seconds and the actual login dialog open at 6.86 seconds from script start,
with both `open` and `:modal` checked. The script selects a visible element handle
instead of re-querying the first hidden duplicate with `page.click(selector)`.
These live timings are observations, not a frozen website benchmark.

Import maps, general bidirectional shaping, complete layout/top-layer behavior
and popover commands remain unsupported boundaries; this patch does not claim
complete CSS or browser API coverage. Network navigation failures are currently
traced but can still surface as an automation navigation timeout after the early
`Page.navigate` acknowledgement.

## Context locale and persistent modal follow-up (2026-09-12)

Custom IANA timezone and Intl locale now use context-owned state on V8. Date
local operations retain native Date values; an eight-entry transition cache
avoids repeated Go crossings within a zone interval. Default formatters are
cached per realm and invalidated on bootstrap restoration. Intl, locale string
methods and Temporal's default zone/formatting share the profile without changing
process-global ICU or the host timezone. Workers and frames use the same source.
750 observations across six zones match Chrome 152.0.7977.83, including gaps,
repeats, historical offsets, Date limits, native Temporal and explicit options.
Concurrent contexts, navigation, iframe/worker inheritance and snapshots have
focused tests. Non-native-Intl backends still reject custom locale profiles.

The disappearing login dialog was a page reload, not a close operation. The
site's startup watchdog observed `clientComplete: true` but
`stylesheetSettled: false`: parser-discovered stylesheets lacked load events.
Each owner now receives a scheduler-owned load/error after its retained response
is available, including duplicate links sharing one fetch. Teardown releases
stylesheet bodies and event bookkeeping. No website-specific workaround or
watchdog suppression was added.

Dialog beforetoggle/toggle coalescing, cancelation and focus restoration now match
the focused Chrome lifecycle trace. Preview exports canonical modal membership
and restores the native top layer, including reopening order. CSS percentage
tokenization accepts adjacent tokens such as `50%auto`; rejecting this valid
serialization had dropped the centering inset from CSSOM and preview. The viewer
test checks centering, ten live updates, close/reopen, and multiple-modal order.
The live US script reached DOMContentLoaded in 3.48 s and opened login at 6.50 s;
site-visible languages were `en-US, en`, timezone `America/New_York`, offset 240
minutes. A later preview still showed the centered English login dialog. These
are live observations, not a frozen website benchmark.

Fresh fast-gate builds and a freshly rebuilt `c3610c3` comparison are recorded in
`profile-modal-20260912.json`; every run completed 184/184 valid executions.
Last repeat versus fresh baseline completion medians (ms): DOM 223.14 vs 209.10,
static 37.86 vs 36.08, React 100.44 vs 99.17. The preceding new-build run measured
201.78 / 39.57 / 116.99 ms, so timing variance is material. Do not claim a speedup
or a zero-regression result: the last repeat is about +6.7% / +4.9% / +1.3%.
Further profiling would be needed to attribute these differences reliably.
Marginal RSS at ten sessions was 45.77 / 48.01 MiB (static / React), versus
45.22 / 47.56 MiB for the fresh baseline. Post-teardown reservations also vary;
the receipts retain recovery memory and concurrency results rather than imply
that a lower active-memory number proves complete teardown.

The complete browser/CDP/webapi/profile package run passed (browser 416.63 s),
with subsequent focused locale, Temporal, dialog, stylesheet, CSS and viewer
checks passing on the final sources. General layout, resource/device/graphics
customization and non-native Intl remain explicit limitations.


## Live Lightpanda/Mimic/Chrome comparison (2026-09-12)

An independent [live comparison](live-browser-comparison-20260912.md) retains 80 final trials over 16 URLs, a frozen Mimic executable, Lightpanda nightly 9268 in Ubuntu WSL, and native Windows Chrome 152 headless. Three-run median content observations for Wikipedia/React are 1.38/3.65 seconds in Mimic, 0.51/0.51 in default Lightpanda, 0.86/0.78 with Lightpanda resources and CORS enabled, and 0.55/0.85 in Chrome. The clocks include CDP/extraction overhead and exclude process/Page creation; these small samples do not establish a universal speedup.

Mimic transitioned from a Cloudflare challenge 403 to the explicit ScrapingCourse success page (HTTP 200 at 10.39 seconds; content observed at 13.74). The other tested configurations remained challenged within 35 seconds. LowEndTalk admitted Mimic directly while challenging the others; the old IroShop test route cleared to an application 404 and is not a content success. Browser identities, platform and resource policies differ, so the cause of admission differences is not established. GitHub/Spigot CDP responsiveness, delayed Modrinth observations and an Amiibo title mismatch remain practical limitations. All four configurations completed the local TodoMVC input action.

The exploratory clone-based probe caused repeated image requests and was discarded from the final timing comparison. The final corpus was rerun with one frozen read-only traversal probe. Raw evidence, binary/probe hashes, corrected exploratory findings, CSV and methodology are linked from the report. Production runtime code and frozen performance harnesses were not changed by this comparison; CPU, concurrency throughput and retained-memory claims were not measured here.

## Navigation and automation stall fixes (2026-09-12–13)

The [detailed investigation](stall-investigation-20260912.md) records the
implementation, profiles, pinned Chrome observations, unprofiled paired runs,
build/source hashes and remaining limitations. The original desktop login
script and preceding Lightpanda comparison are preserved.

Confirmed causes included network waits holding Page command ownership, module
fetching inside V8 callbacks, repeated ancestor/style/geometry work, eager font
unit resolution, redundant text shaping/glyph serialization, repeated completed
image loads, and delayed wake-up of ready Page work. Fixes preserve one event
loop per Page and reject stale navigation continuations. Transient geometry
memoization and bounded realm/document caches avoid a persistent stale DOM model.
Final review also added child-DCL microtask interruption and image-cache bounds.

Five alternating clean-context pairs on the measured optimization checkpoint:

| Observation, median | Before | Optimized |
| --- | ---: | ---: |
| ChatGPT visible login button | 3.583 s | 1.460 s |
| Real click to confirmed modal | 1.339 s | 0.778 s |
| Complete login-dialog path | 4.946 s | 2.234 s |
| GitHub first content / DCL | 31.050 / 28.574 s | 0.981 / 17.953 s |
| SpigotMC DCL | 0/5 successful; execution deadline ~40 s | 5/5 successful; 11.074 s |
| Modrinth first content / DCL | 28.689 / 28.662 s | 1.102 / 5.741 s |

The local full 200-element geometry read dropped from 30.444 to 8.052 seconds
in a separate single cold stress pair. The bounded 30-repeat control reduced
read/mutation/click medians from 637.75/808.89/495.09 to 176.72/219.21/156.91 ms.
Seven held-resource scenarios each completed 30 sequential CDP commands before
resource release; maximum observed p95 was 1.443 ms. Five retained-context pairs
without preview reached the ChatGPT modal in 1.888→1.258 seconds; actual preview
and complete input-event diagnostics are separately recorded.

These are scoped improvements, not universal speed or compatibility claims.
GitHub still has pre-existing bare-module/import-map failures. Both builds retain
a ChatGPT shell error although the tested login modal opens. Wikipedia's optimized
main campaign had 4/5 successful trials and one main-document network timeout
while CDP stayed responsive. The specific Cloudflare laboratory challenge still
cleared in both builds; an Iroshop route clearing to HTTP 404 remains a failure.
At that checkpoint Spigot still had multi-second synchronous tasks; subsequent
profiles identified repeated shaping, retained invocation handles and redundant
geometry observations. Those paths received the additional fixes below.

Intermediate frozen fast gates exposed static completion ~38–41→60 ms and lower
concurrent throughput. The cause was an eagerly acknowledged navigation allowing
readiness polling to instantiate an otherwise unused empty-document V8 runtime.
The completed implementation acknowledges document commit outside Page/session
locks, preserving explicitly concurrent reads and old-realm identity. It also
closes the cancellation handoff race at commit.

The last measured fast-gate checkpoint (`9aed84f1…`, `.build/stalls-fast-release-final/`),
before the final shadow-stylesheet and isolated-owner corrections,
versus a fresh unchanged baseline recorded static/DOM/React completion medians
of **37.691→38.905 / 218.412→199.611 / 99.440→89.367 ms**. Static throughput
at 25 Pages was **69.977→69.346 sessions/s**; recovered RSS was
**423.60→421.71 MiB**. The intermediate 17.46% throughput loss did not persist.
These small fast-gate samples are controls, not stable tail-latency estimates.

Further fixes use transient V8 invocations, aggregate text metrics with bounded
HarfBuzz scratch, the existing 1 MiB text-cache budget without premature count
eviction, shared canonical ancestor reads and a definite-height projection that
skips unnecessary descendant flow. Revision-based observation retention is
restricted to the top main realm until the next microtask, with tested DOM,
CSSOM, shadow, form/focus and font invalidation. Foreign/child/isolated projections
keep synchronous reuse.

`Page.stopLoading` now cancels current document requests, including stylesheet,
module dependency, fetch and XHR. Pinned Chrome 152 and deterministic regressions
confirm that worker requests survive and future fetch/import remain usable;
stopped parsers do not restart or fabricate DCL/load. All oracle artifacts,
checkpoint identities and final verification results are in the detailed report.

The broad correctness run recorded 871 passing top-level tests and two shadow
stylesheet failures; all other 19 tested packages passed, including CDP.
Both failures were corrected and their related focused suite passed. A final
isolated-world correction routes Playwright style/geometry observations through
the canonical main owner. Its focused tests passed, and the reproduced GitHub
navigation/snapshot timeout became two successful runs of approximately
11.9 and 11.4 seconds on binary `23774c4a…`.

The user explicitly requested committing without another full run. No final-source
full-suite, frozen-matrix or five-pair live result is claimed after those last
corrections. The committed build is `.build/mimic-optimized.exe`; its exact commit
and binary hash are recorded in `.build/stalls-committed-release.json`.

### 2026-09-13: isolated hit testing and auth-menu geometry

A live blast.hk input click exhausted Playwright's 5-second deadline. Protocol
timestamps showed approximately 3.6 seconds in hit-target setup before mouse
dispatch. Isolated `elementsFromPoint` now asks the document owner for the whole
hit list and wraps node IDs locally, instead of requesting owner geometry for
every candidate separately. On fresh `.build/mimic-auth.exe` runs, the auth input
click completed in approximately 2.1–2.6 seconds; filling and clearing it also
succeeded. These are diagnostic live samples, not a controlled benchmark.

Nested row-flex intrinsic widths, auto-width border-box edges and single-length
`calc()` resolution were corrected. Preview now retains the target's 1272×653
viewport in both 1677px and 900px viewer windows. This removes viewer-induced
reflow, but does not make the limited geometry model pixel-identical to native
Chrome (the live navigation still differs in control/icon dimensions).

Focused CSS, input, isolated-world and preview regressions pass. The CDP package
was tested in full; no new full-browser or frozen performance-matrix run is claimed.

Follow-up: removed the CDP content-quad hit hint. Reading an element's quad must
not force subsequent mouse events onto that element or bypass an overlay.
The regression test failed before the change (the anchor received the click
instead of its child) and passes with coordinate-based hit testing. Three live
blast.hk cycles opened the menu, focused/filled/cleared the input, closed with
Escape and reopened successfully. Initial clicks took 3.7–3.8 seconds; the AJAX
form was visible at 4.7–4.9 seconds. These supersede the hinted-click timings above.

A subsequent first-click capture exposed the intermittent lost click: pipelined
CDP mouse release could execute before mouse press because transport workers
raced for a mutex. Input commands now reserve FIFO order at dispatch, per
session; legacy message envelopes preserve inner dispatch order too. Control
commands and independent sessions are not queued behind input. A 32-click burst
regression failed with reordered down/up on both page and flattened sessions
before the fix, and passes repeatedly for page, flattened and legacy sessions.

### 2026-09-13: retain unchanged geometry across checkpoints

A fresh GitHub attempt reproduced a 10.31-second click. CDP timing attributes
0.7–0.9 seconds of Page wait to each action command; native/Go profiles trace
the recurring task through IntersectionObserver, box sizing and CSS matching.
Canonical geometry was being discarded at every microtask checkpoint even
when its DOM/resource/CSSOM/environment/state epoch had not changed.

Top-main-realm observations now survive checkpoints until the epoch changes;
failed observations discard provisional results. Child, isolated and foreign
dependencies retain the conservative synchronous path. Shadow membership
already invalidates the canonical epoch, so an unchanged shadow tree also uses
the IntersectionObserver no-change check.

Three paired local trials (90 validated click rounds per binary) reduce the
median-of-trial-medians click latency from 109.97 to 27.29 ms, geometry reads
from 29.79 to 2.00 ms, and mutation/read sequences from 142.89 to 112.72 ms.
In separate instrumented GitHub observations, warm hit tests fall from 3653.27
to 32.20 ms and pointer movements from 780.05 to 7.86 ms; Page wait disappears
in the measured candidate samples. The first candidate hit test still costs
639.89 ms. The attempted GitHub menu click hit a different control and must not
be reported as a successful interaction. The repeated diagnostic checks preserve
the same observed hit element.

Six validated TodoMVC toggles remain about 48 ms versus pinned Chrome's 12.71 ms;
three final-build blast.hk menu cycles also pass, with first open improving
from 2.72 to 1.39 seconds but later opens still taking 2.10–3.11 seconds versus
Chrome's 32–45 ms. Mutation-driven CSS/geometry rebuilding remains expensive;
there is no blanket faster-than-Chrome claim. All timing provenance, the
remaining cold-geometry/coordinate limitations, raw evidence locations and
final validation are in [the investigation](click-investigation-20260913.md).

### 2026-09-13: mutation-aware style reuse and verified form interactions

The follow-up removes repeated selector matching, cascade construction,
inheritance/length work and primitive Go↔V8 transfers after unrelated mutations.
Static results require exact canonical inputs; structural/state selectors are
rechecked. Geometry positions/flow are not retained across mutation. Parsed text
metrics use a bounded cache with authoritative font-collection invalidation.

Against the previous tested executable, nine verified blast.hk menu opens fall
from median 1879.85 to 380.78 ms. Six repeat opens through visible form readiness
fall from 2144.84 to 436.67 ms; repeat field clicks from 510.81 to 217.50 ms.
Every cycle clicked the actual field, typed fixed synthetic text, cleared it,
and closed with Escape without submitting a form. First-form readiness is
median 904.60 ms and includes network/initialization. Chrome remains faster
(reopen through form readiness about 15.62 ms in its two warm samples).

Three paired local trials reduce mutation/read latency from 110.11 to 31.34 ms;
already-warm local reads/clicks and TodoMVC show little change. Three ChatGPT
click-to-modal trials improve from median 944.44 to 833.62 ms. Fresh fast gates
pass correctness, N=10/N=25 waves and memory checks, with approximately unchanged
throughput and 0.3–0.6 MiB higher marginal RSS/page in these samples. Live Blast
peak RSS median increases approximately 9 MiB. No universal Chrome-beating or
stable p95 claim is made.

The final-source full Go test suite passed every package, including browser
and CDP. Focused regressions and the pinned Chrome selector oracle also pass.

Binary hashes, native/Go attribution, exact checks, samples and limitations are
in [the follow-up report](click-followup-20260913.md).

### 2026-09-13: prevent live-preview starvation

The asynchronous preview publisher restarted its 50 ms delay on every Page
command. Continuously active pages could therefore remain at Connecting with
no first snapshot. Keep the coalescing delay fixed instead, then serialize the
latest canonical state under the Page lock. Disabled/unsubscribed paths and
the one-item network mailbox are unchanged. The new continuous-command
regression fails before the fix and passes 20 repetitions after it; preview
tests pass three repetitions and the full CDP package passes. An owned live
viewer in Chrome 152 displays and updates a 10 ms timer-driven Page without
viewer errors; the existing 25-update DOM mirror smoke test also passes.

### 2026-09-13: implement scrolling for off-viewport automation

Steam's failed link action was missing functionality, not a slow click:
scrollIntoView returned without moving the viewport. The canonical scrolling
implementation now updates geometry, clipping, input, intersections and preview.
Unchanged layout is retained when offsets change. A fresh Playwright trial on
the Steam link passes, as does a real local below-viewport button click. These
are diagnostic checks, not reportable latency/throughput comparisons. Chrome
oracle coverage, focused validation and explicit remaining boundaries are in
[the scrolling notes](../scrolling.md). The user requested stopping the repeated
full test run and publishing; no final full-suite or fast-gate pass is claimed.

### 2026-09-13: account-page cross-realm diagnostic

After the parser-defer correction, account-page traces contained consecutive
synchronous timer callbacks lasting 5.334 s and 5.214 s, delaying a ready iframe
response. A separate CPU profile identified substantial crossFrameData,
reflection and value-encoding work. A warmed loopback diagnostic measured
1000 foreign-function calls at 115.4 ms and 1000 foreign-property reads at
61.8 ms; an iframe-local loop with one bridge call took 0.1 ms. Frozen Chrome's
matching cases were below its timer resolution. Results are single diagnostic
samples, not a frozen benchmark matrix or a performance improvement claim.

No bridge optimization was made in this investigation. Google accepted and
rejected different sessions with long connection-check delays, so the measured
runtime overhead is not a proven rejection criterion. Captures, methodology,
CPU-profile caveats and correctness requirements for future work are in
[the rejection investigation](../compatibility/google-signin-rejection-20260913.md).

### 2026-09-13: batch cross-realm bridge transactions

Imported proxy operations now perform argument materialization, reflection and
result encoding as one owner operation. V8's private string-call boundary uses
one handle scope without persistent scratch roots. Origin checks, canonical
references, getters/Proxy traps, incumbent-document tracking and Page task
ordering remain on the existing ownership paths. The design and remaining
boundaries are in [the transaction notes](frame-transactions-20260913.md).

The identical `frame_bridge_probe.py` ran sequentially against the preserved
pre-change binary, the fast gate's freshly built binary and frozen Chrome
152.0.7977.82. Each row below is the median of five warmed samples of 1000
operations; every sample passed its result check. These focused diagnostics
do not replace the frozen benchmark baseline.

| Operation | Before, ms | After, ms |
| --- | ---: | ---: |
| Foreign function call | 117.3 | 48.3 |
| Property read | 66.1 | 42.7 |
| Property write | 102.6 | 67.6 |
| Property presence (`in`) | 50.0 | 42.2 |
| Prototype read | 68.0 | 51.0 |
| Own descriptor | 102.2 | 49.3 |
| Own keys (two keys) | 87.1 | 50.0 |

Local arithmetic and the iframe-local loop controls remained below or around
0.1–0.2 ms. Chrome medians were below timer resolution except ownKeys at 0.1 ms;
the remaining actor/bridge overhead is not claimed to match native Chrome.
Receipts are `.build/frame-transaction-{before,after,chrome}-probe/result.json`.
Before SHA-256 begins `31e4c263bf1d`; after begins `e15939de75fe`; each receipt
retains the complete binary hash, common probe hash and individual samples.

`go test ./... -count=1 -timeout=15m` passed (browser 484.386 s, CDP 32.381 s,
V8 1.104 s). Final argument-array hardening was separately checked with the
transaction, string-call, frame-reference and reflection tests after that full
run started: browser 4.833 s, V8 0.207 s. New tests verify exception/return/
receiver identity, symbols and special values, private codec isolation, pending
jobs, interruption/recovery and stable persistent-root counts for scratch calls.

The unchanged fast gate passed all six semantic workloads, N=10/N=25 static
waves and both memory cases. Median warm completion: DOM 195.475 ms, static
35.713 ms, React 83.590 ms; median static throughput 52.87 and 67.35 sessions/s
at N=10 and N=25. These are this run's absolute results, not a paired general
workload speedup claim. Ten static pages used 544.86 MiB private memory while
active and 149.96 MiB after teardown/recovery; ten React pages used 566.07 and
170.30 MiB respectively. Ready-process private memory was approximately
80.9 MiB. Warm shared artifacts and allocator retention remain; no leak-absence
or return-to-cold-footprint claim is made.

Gate receipt: `.build/frame-transaction-fast-gate-20260913/{build,raw}.json`.
Logs: `.build/frame-transaction-full-tests.log`,
`.build/frame-transaction-final-focused.log`,
`.build/frame-transaction-fast-gate.log`. Frozen harness/workload fingerprints
and each launched executable hash were verified by the gate. The benchmark
virtual environment was used; the default Python lacked websockets.sync.

Live follow-up: the fresh diagnostic executable completed the identifier step
with account-not-found, zero JavaScript exceptions and zero network failures.
Two synchronous timer callbacks still lasted 5.013 and 5.246 seconds. This
optimization therefore does not establish that those stalls or intermittent
server rejection are resolved. Capture:
`compatibility/private-captures/google-signin-20260913-frame-transaction-mimic/`.

Further profiling localizes the residual work to nested cross-realm traffic:
the child realm recorded 155,100 frame transactions taking 9.497 seconds
inclusive, with 155,188 access checks taking 0.529 seconds. The parent's
11.685 seconds in 2,774 transactions includes child execution; these durations
must not be summed. The Go profile also shows substantial native-call and
thread-wakeup costs. A separate parent V8 profile attributes 10.421 seconds
to a native call beneath the imported function proxy, which cannot by itself
attribute the child work. These instrumented live runs are diagnostic, not
paired benchmarks or evidence of a specific application timeout rule.
Receipts: `.build/frame-transaction-child-residual/` and
`.build/frame-transaction-native-residual/`. Temporary diagnostic test code
was removed. Removing the remaining mass-call overhead needs further work
on realm execution ownership; sharing an isolate was not introduced into this
scoped, validated bridge change.

### 2026-09-13: fresh public-beta benchmark checkpoint

The unchanged full matrix ran against a fresh build of clean revision
`2d3b21469d73936eae280098de5534aa43ebc353` and frozen Chrome 152.0.7977.82.
The wrapper verified both executable hashes before every launch and preserved
the original harness fingerprint. No production code or frozen workload changed
for this checkpoint. The original baseline and earlier milestones remain intact.

| Measurement | Mimic | Chrome |
| --- | ---: | ---: |
| Common-probe CDP readiness, median ms (10 fresh processes) | 219.09 | 270.36 |
| Process-tree RSS at readiness, median MiB | 28.78 | 378.52 |
| Private bytes at readiness, median MiB | 80.80 | 177.59 |
| Warm static completion, median ms (20 iterations) | 33.68 | 21.32 |
| Static N=100 throughput, successful sessions/s | 76.64 | 27.78 |
| Static N=100 active RSS, median MiB | 3988.31 | 7039.66 |
| React N=100 throughput, successful sessions/s | 44.47 | 20.07 |
| React N=100 active RSS, median MiB | 5175.78 | 8503.54 |

All six correctness gates passed in both systems. All 240 warm iterations passed,
but Chrome was faster on warm completion in every fixture. Of 120 cold attempts,
119 passed: one Chrome WebAssembly navigation returned `net::ERR_ABORTED`. Its
cold comparison is withheld from the public summary; the failed observation is
retained. Mimic CPU at N=100 stopped after one of 100 pages failed initialization
with `gov8: JSONParse failed (status -5)` in the first measured wave. N=50 is its
highest completed CPU level, not N=100. Static and React completed every level
through N=100 in both systems. The cause of the initialization error was not
investigated or fixed in this documentation/measurement task.

The 92% readiness RSS reduction and 43% active RSS reduction at static N=100
describe different checkpoints. Readiness includes the initial page and is not
per-page memory. Higher concurrency throughput does not imply lower CPU cost:
static N=100 used 135.97 vs. 120.53 ms CPU per successful session; React N=100
used 335.62 vs. 203.69 ms. At 250 ms after teardown, static N=100 RSS remained
1519.87 vs. 1248.70 MiB and React N=100 remained 2009.55 vs. 1396.86 MiB.
Allocator retention and teardown recovery remain important limitations.

The historical comparator accepted matching harness, fixtures, Chrome binary,
machine, power configuration and iteration policy against milestone 07. Its
output is retained; these runs were days apart on an interactive workstation,
not a paired isolated optimization experiment. The new numbers replace the
historical README cards, without claiming that every difference is a code effect.

[Public-facing summary](../../benchmark/runs/08-public-beta-20260913/public-summary.md),
[full report](../../benchmark/runs/08-public-beta-20260913/report.md),
[public numeric results](../../benchmark/runs/08-public-beta-20260913/public-results.json), and
[historical comparison](../../benchmark/runs/08-public-beta-20260913/comparison-to-07.json).
Build/launch receipts and the artifact manifest are in the same run directory.
`public-results.json` is byte-identical to the numeric export in the new public
product repository; detailed traces and internal research are not exported.

The public quick-start profile was exercised with Puppeteer 25.10.0 on this
fresh binary: page content, language, timezone, viewport and dark theme matched.
Both documented profile JSON examples validated; context creation, dynamic
viewport update, readback and disposal passed over browser CDP. Existing focused
native HTTP/HTTPS/SOCKS5 proxy tests, remote DNS/authentication, and no-direct-
fallback checks passed. No full Go suite was rerun for these documentation-only
changes. The English report generator now describes the actual checkpoint and
does not incorrectly label later builds as an unoptimized original baseline.

#### Browser-target compatibility follow-up

The public Puppeteer example initially failed at `browser.target()` because
browser-target discovery/attachment was incomplete. The runtime was fixed rather
than requiring a different client pattern. Frozen headful and headless Chrome
observations, targeted identity/lifecycle regressions, and the complete CDP suite
passed (33.205 s). The original `browser.target().createCDPSession()` quick start
then passed on the newly built fast-gate executable.

The follow-up fast gate completed all six correctness fixtures, N=10/N=25 waves,
and both memory scenarios. Warm completion medians were DOM 198.60 ms, static
34.63 ms, and React 84.41 ms. These smaller-run values do not replace the full
checkpoint or establish a performance change. The measured full checkpoint
predates this fix and retains its own executable identity and two recorded
failures. The N=100 CPU initialization failure remains unresolved.

Follow-up binary SHA-256:
`166d2b58ea2e39835fce5b084a818e618da7239eab7f3d22908f974926981b0d`.
Receipts: `.build/public-beta-browser-target-fast-gate/{build,raw}.json` and
`.build/public-beta-quickstart-check/result.json`. The frozen harness hash was
verified; no timing/fixture changes or new full-matrix claims were introduced.

### Native Taffy vertical-slice checkpoint (2026-09-14)

The project-specific native C ABI was measured before making its Flex/Grid boxes
authoritative. Both executables were fresh Windows amd64 processes on the same
host: the baseline was built from `HEAD`, while the candidate included the
snapshot adapter and Rust/Taffy static library. Each cold sample invalidated the
observable DOM/style epoch; the immediately following warm sample read the same
geometry and therefore exercised epoch reuse. Layout time and process RSS were
recorded separately. Raw local receipts are
`.build/layout-head-cold.json` and `.build/layout-taffy-cold.json`.

| Workload | Median cold, HEAD → Taffy | Median warm, HEAD → Taffy | Live RSS delta, HEAD → Taffy |
| --- | ---: | ---: | ---: |
| DOM, 600 nested flex items | 117.7 → 58.2 ms | 12.85 → 12.45 ms | 144.3 → 154.3 MiB |
| Static, 200 grid/flex cards | 18.6 → 31.0 ms | 5.80 → 6.05 ms | 56.2 → 61.7 MiB |
| Google home | 19.9 → 25.5 ms | 12.8 → 7.65 ms | 237.7 → 147.4 MiB |

DOM cold layout improved substantially. Static cold layout regressed by roughly
67%, but that baseline did not calculate the same Grid geometry (its checksum
was 3,121,576 versus 925,079 with explicit Taffy tracks); end-to-end workload
wall time increased about 19%. Google cold layout increased about 28%, while its
warm reads and live RSS delta improved. The candidate therefore does not show a
major general regression, but snapshot construction remains measurable on small
static Grid pages. The implementation keeps a per-epoch flat-box cache and does
not add a persistent tree yet; this cold cost is the trigger to profile the
numeric adapter before considering more complex cross-epoch state.

### Playwright Wikipedia journey follow-up (2026-09-16)

A black-box Playwright journey used the unchanged Chromium-oriented script in
`tools/runtimecheck/playwright_wikipedia.js`: open Wikipedia, fill and submit
search, inspect article DOM, locate and click the exact accessible ECMAScript
link, verify the destination, then navigate back. The control Chrome 152 run
completed in 2,711 ms. The initial Mimic measurements were 14.5–15.3 s; the
best fresh final-candidate process completed in 12,118 ms; the post-regression
gate run completed in 12,987 ms. This is a real improvement but remains
4.47–4.79x the control and does not meet the requested 7 s target.

The measured command profile attributes 4,552 ms inclusive to 49
`Runtime.callFunctionOn` commands (3,870 ms direct work), 1,313 ms inclusive to
three mouse dispatches, 851 ms to the initial `Page.navigate`, 696 ms to ten
`Runtime.evaluate` commands, and 476 ms to two scroll-into-view commands. These
overlap: mouse dispatch included 599 ms waiting for Page turns and 509 ms queued
behind ordered input. The remaining largest JS CPU path is accessibility and
visibility work in the isolated world, including repeated canonical-owner style
observations and layout. A bounded foreign-style read-ahead experiment made the
one-shot query slower even though hot repeats fell below 200 ms, so it was not
retained.

Retained changes remove repeated collection/state projections, preserve style
and geometry observations across matching mutation epochs, avoid unnecessary
resource invalidation, yield the event-loop pump to queued CDP commands, reuse
scroll ranges, and preserve a verified scroll target through the following
pointer sequence. The original footer Docs click reproducer now resolves the
link as its own hit target and completes navigation in 2,986 ms on a fresh
process. No website-specific behavior or benchmark-specific shortcut was added.
# Wikipedia isolated-world document style projection cache (2026-09-16)

Document-wide computed-style projections requested from isolated worlds are now
owned and keyed by the canonical document, rather than by the first element that
happened to request them. Property-set keys are order-independent. This avoids
rebuilding the same full-document projection when Playwright creates another
isolated world or asks for `display` and `visibility` in the opposite order.

The unchanged `tools/runtimecheck/playwright_wikipedia.js` remained PASS and
improved from the current approximately 14.1 s baseline to 13.086 s in the final
development run. The next measured large cost is repeated IntersectionObserver
sampling during active DOM/resource mutation (about 2.3 s inclusive in the
captured JavaScript profile); it requires rendering-scheduler coalescing rather
than another geometry leaf optimization.

### Wikipedia full differential and protocol geometry checkpoint (2026-09-16)

Work continued from clean revision `84cfe72` with the frozen
`tools/runtimecheck/playwright_wikipedia.js`. The previous 13.086 s Mimic run
and 2.711 s frozen Chrome 152 run left a 10.375 s absolute differential. A
fresh paired stage run later in the session measured 13.045 s for Mimic and
2.961 s for the locally installed Chrome control. The latter included 788 ms
of Chrome launch and page creation, while Mimic connected to an existing
process in 24 ms, so the stage differences below are more useful than the raw
totals:

| Stage | Mimic | Chrome | Recoverable gap |
| --- | ---: | ---: | ---: |
| Initial main-page navigation | 1,914 ms | 762 ms | 1,152 ms |
| Fill and verify search input | 645 ms | 12 ms | 633 ms |
| Search navigation to JavaScript | 4,200 ms | 715 ms | 3,485 ms |
| Article text/DOM inspection | 1,267 ms | 15 ms | 1,252 ms |
| Resolve and scroll the ECMAScript link | 1,305 ms | 90 ms | 1,215 ms |
| Click navigation to ECMAScript | 2,483 ms | 338 ms | 2,145 ms |
| Back navigation | 1,191 ms | 236 ms | 955 ms |

The stage rows account for essentially the complete post-connection gap. They
also show why optimizing only the two navigation windows cannot close the full
differential.

Fresh wall attribution found synchronous Playwright geometry, visibility and
input work on the critical path. `DOM.getContentQuads` previously spent about
878 ms rebuilding box geometry through an isolated-world wrapper. Protocol box
model reads now execute once in the canonical document owner realm and return
the complete border, padding, content and margin model. Protocol scrolling uses
the same owner-realm boundary instead of invoking the isolated wrapper path.
Focused DOM quad, control-font geometry and input hit-target tests passed. The
unchanged Wikipedia workload remained PASS.

A fresh task-scoped V8 profile of the long navigation tasks identified repeated
CSS declaration serialization as another exclusive path. Shorthand lookup used
to scan the complete shorthand catalog for every emitted declaration and then
linearly search declarations for every component. A reverse component index and
per-serialization declaration map remove those repeated scans. Focused CSSOM,
stylesheet, computed-style and shorthand tests passed. The first version of
this change completed the unchanged journey in 11.389 s PASS; individual live
Wikipedia runs varied materially with network and page callbacks, so this is a
best observed checkpoint rather than a stable median.

After removing all temporary instrumentation and unproven experiments, the
exact checkpoint candidate rebuilt from the commit contents completed the same
unchanged workload in 11.456 s PASS.

The current retained changes do not close the Chrome gap. Fresh command timing
still contained an 855 ms mouse command chain, a 585–668 ms visibility call,
and a 453 ms scroll command. Mutually exclusive scheduler attribution found
several long DOM tasks (823, 522, 357, 355, 354, 297 and 245 ms). Their common
work repeatedly rebuilt table and ancestor geometry after connected DOM
mutations. A retained table-layout experiment reached 11.479 s PASS but did not
beat the 11.389 s best checkpoint reliably, so it was removed rather than
committed as an unproven cache.

Several structural experiments were also rejected after one focused test and
one unchanged E2E run: a shared V8 isolate (15.643 s), per-element full-style
snapshots (21.750 s), compact document-style projection (12.949 s), direct
CSSOM-AST-to-matcher projection (12.254 s), JSON transport for large selector
results (12.508 s), and always-on trace compaction (11.927 s without a clear
gain over the best checkpoint). Earlier document-wide property batching stayed
near 13 s, while per-element style projection was about 20.6 s. None is retained.

The remaining architectural cost is not simply the absence of a shared tree.
Main-world observers and isolated Playwright reads already enter one canonical
owner observation, but its initial JS style/geometry construction is expensive
and coarse epochs discard useful pieces. Visibility, hit testing, `innerText`,
role resolution and input dispatch therefore build or traverse overlapping
derived state across each newly created document. Dependency-aware invalidation
and separate intrinsic/placement lifetimes are necessary for reuse, but the
split-freeze upper bound later showed they are insufficient alone: first-build
cost must also fall. Another Playwright-specific property or selector shortcut
will not close that combined gap.

The follow-up causal pass also rejected three lower-level explanations. Shared
geometry bindings and removal of element observation proxies were only low
single-digit E2E effects. Replacing selector IDs plus lazy `nodeData` reads with
one bulk array of complete node records regressed both cold and warm runs, so
Go/V8 wrapper transport is not the hidden four-second cost. A callback-scoped
demand style projection accelerated first-visible but displaced its work into
later actionability/scroll stages and left warm E2E flat. These experiments
were removed; only the diagnostic scenario and the attribution record remain.

An exact Page-lifetime intrinsic-width cache was also rejected. Its key covered
retained style identity, font epoch, direct text and recursively validated child
identity/width. Correctness tests passed, but recursive validation regressed
cold median (10300 -> 10681 ms) and left warm effectively flat (6988 -> 6957
ms). Intrinsic/placement lifetime separation therefore needs O(1) canonical
subtree dependency epochs; recomputing the dependency key in JavaScript is not
a viable substitute.

This is an intermediate checkpoint. No full suite, CI, reportable benchmark
matrix or push was performed. Temporary CPU/wall profiling instrumentation and
the modified diagnostic workload were removed before the checkpoint commit.

## Controlled Wikipedia observation-chain comparison — 2026-09-20

See [the detailed report](observation-chain-2026-09-20.md) for the new paired
Chrome 152/Mimic local-DOM matrix and its connection to a fresh complete
Wikipedia trace. All 48 controlled cases passed. Retained geometry is effective
for unchanged direct reads; expensive first construction, over-broad mutation
reconstruction, repeated property-specific document projections, and differing
frame cadence are distinct contributors. The four small Chromium-inspired PoCs
combined improved quiet Wikipedia only 10071/6062 -> 9632/5756 ms cold/warm;
they were removed and do not meet the user's <=4 s goal. Original incoming source
changes were preserved exactly; no production acceptance, full benchmark gate,
race or memory result is claimed for these diagnostic experiments.

## Connected observation revision — 2026-09-20

Style and geometry projection epochs now follow a per-document connected-tree
revision instead of every write to the shared node arena. Detached element and
fragment construction, inert document parsing, and template-content mutation
continue to advance the authoritative arena revision but do not invalidate the
active document's retained style state. Connected insertion, removal,
attributes, text, form/focus state, and conservative structural mutations do.
Selector reuse has a separate bounded connected-attribute journal; structural
or state changes leave an intentional gap and force a complete match.

A three-pair same-binary screen compared the connected revision against an
environment-controlled fallback to the old arena revision. Median complete E2E
changed from **9254/5791 ms to 9052/5819 ms cold/warm**: cold -202 ms (-2.2%),
warm +28 ms (+0.5%, effectively flat). Every unchanged workload assertion
passed. This is retained as a cold-amplification reduction and prerequisite for
later dependency-aware reuse, not as a solution to the remaining Chrome gap.
Raw stages and hashes are in
`tools/performance/pocs/results/connected-revision-paired-results.json`.

## 2026-09-20 -- combined Wikipedia production checkpoint

The connected observation revision, bounded selector mutation journal, ready
navigation priority and observation-local Taffy-root memo were measured together
against the pre-investigation control binary in three alternating cold/warm
pairs. All unchanged Wikipedia assertions passed. Control medians were
10075/6720 ms; the combined build measured **9500/5813 ms cold/warm**, a
**5.7% cold** and **13.5% warm** reduction. Every warm pair improved by
0.69--0.94 s. The result is material but remains above the <=4 s goal, and the
3.69 s cold/warm differential remains unresolved.

An exact-verification postorder Taffy PoC removed a measured 520 ms legacy
prepass and matched all consumed heights and root origins. Its first complete
cold candidate run nevertheless regressed by 805 ms because local measurement
recomputed displaced work. It was kept isolated and is not production code.

The combined production tree passes the full DOM and scheduler suites, focused
browser style/geometry/navigation tests, DOM and scheduler race tests, and the
fresh-build `tools/performance/fast_gate.py` semantic, warm, throughput and
teardown gate. Raw combined Wikipedia data is
`.build/combined-current-paired-results.json`; the fast gate is
`.build/fast-gate-wikipedia-combined`.

The first full browser-suite run exposed two synthetic-shadow invalidation
regressions: shadow descendants are detached in the native arena, so attribute
and inline-style writes did not advance the new connected revision. Those
writes now explicitly advance the realm style epoch. Both failing frozen-Chrome
tests pass for Goja and V8, and a fresh final-source fast gate passes at
`.build/fast-gate-wikipedia-combined-shadowfix`. The complete 677-second suite
was not rerun after this focused correction within the time-box.

The exact combined source was then traced once cold and warm. Diagnostic wall
was 10997/6970 ms. Scheduler intervals explain 3462 ms (86%) of the 4027 ms
gap: DOM +2465, timer +799, navigation +162 and network +31 ms. Eight cold
IntersectionObserver/table-geometry tasks total 2440 ms versus three warm tasks
at 379 ms. Together with timer work this accounts for 2860 ms (71%) of the
cold gap. This establishes the remaining cold target as cross-frame retained
layout with dependency invalidation, not script execution time or CDP latency.

Mutation-complete instrumentation of the eight heavy samples (1885.3 ms)
further bounded that fix: 1045.9 ms is first-build work with no reusable prior
state; 107.7 ms follows global html/body/head structural/state batches; 215.0 ms
follows a remote later-sibling style plus layout-inert title mutation and is
directly recoverable; 516.7 ms of class/state invalidation requires selector
and pseudo-state dependency proof. The credible retained-layout upper bound is
731.7 ms (38.8%). Both first-build optimization and dependency-aware retention
are required to reduce cold materially; a coarse persistent cache cannot do it
correctly.

## 2026-09-21 -- browser-owned immutable bootstrap

Bootstrap snapshots are now owned by `Browser`, keyed by the complete observable
environment and shared across sequential `BrowserContext` lifetimes. Browser
shutdown closes all contexts, joins builders and releases the bounded 32 MiB
artifact cache. Context-local cookies, storage, networking, permissions, DOM and
realm state remain independent.

The official 100-run Lightpanda Campfire workload was measured with standalone
compiled executables. Against commit `35667ce`, commit `5dfea5a` changed Mimic
from **37236 ms / 372 ms per page / 456.56 MiB peak** to
**14631 ms / 146 ms per page / 429.92 MiB peak**. Chrome in the candidate run
measured 22959 ms and 992.59 MiB; Lightpanda measured 2165 ms and 26.62 MiB.
The timing improvement is 60.7% and peak-memory improvement is 5.8%. This makes
the official fresh-Context workload faster than Chrome but does not satisfy the
separate 30% lazy-surface memory target.

V8 `FunctionCodeClear` was also tested and rejected: ten-Page private memory was
effectively unchanged (static 505.2 -> 505.0 MiB, React 566.1 -> 564.3 MiB).
The dominant cost is therefore the retained constructor/prototype/closure graph,
not eagerly retained compiled machine code. Achieving the remaining memory goal
requires virtualized observable descriptors with value materialization, rather
than merely changing snapshot code retention.

## 2026-09-21 -- persistent bootstrap and deferred image backing

The browser now prepares its default bootstrap artifact before opening the CDP
listener and persists the immutable V8 blob in the user cache. The disk key
combines the complete Context profile/object-graph key, cache schema and both
the linked and runtime V8 identities. Files carry a length and SHA-256 checksum,
are published by same-directory atomic rename, and corrupt entries are removed
before an ordinary rebuild. Mimic-owned artifacts unused for 30 days are
removed and the directory retains at most eight current profiles.

Image resources now retain one immutable compressed backing plus cached
metadata and validation results. Image loading still performs full stream
validation to preserve `load`/`error` behavior, but it no longer retains or
copies an RGBA bitmap. Layout and `<img>` observations use the metadata barrier;
Canvas and `createImageBitmap` synchronously materialize and share the bitmap
through the decode barrier on first use.

Fresh-build fast gate `235f843` passed all six semantic workloads. Median warm
completion was 393.12 ms DOM, 42.41 ms static and 120.61 ms React. Later static
waves reached 52.81-54.84 sessions/s. Ten-Page marginal private memory was
41.98 MiB/Page static and 44.33 MiB/Page React; recovered process private memory
was 203.33 and 207.30 MiB respectively. These workloads contain no meaningful
image payload, so they validate non-regression but do not quantify the deferred
bitmap saving; that saving is approximately the avoided decoded raster area
(`width * height * 4`) per loaded-but-unobserved image, less compressed bytes.
# Lazy WebAPI implementation domains (2026-09-21)

The Window bootstrap now publishes the generated WebIDL shape eagerly but keeps
WebAudio, WebGL, and WebGPU implementation source in process-wide immutable Go storage.
Stable realm-local behavior cells synchronously materialize a domain on first
construction/call (or WebGL context request) without replacing constructors,
prototypes, methods, accessors, aliases, or descriptors. Dedicated regression
tests retain reflected references before materialization and compare identity,
native source, descriptors, `instanceof`, and prototype ownership afterward.

On the external replay benchmark with ten isolated BrowserContexts/Pages alive
concurrently, three repeated compiled-binary runs measured median peak private
memory of **1132.77 MiB**, versus the preceding recorded **1419.29 MiB**
baseline (**-20.2%**). Median batch wall time was **878.22 ms**. This is a real
checkpoint, not the final memory gate: it does not yet meet the planned -30%
threshold, and remaining domains must be migrated behind the same single
registry rather than adding independent lazy mechanisms.

## 2026-09-21 -- bounded V8 realm pooling

Restored Pages now use independent V8 contexts in a Browser-owned pool, with a
default bound of eight realms per isolate. Realm teardown releases only that
Page's context and roots; isolate teardown occurs after the final realm. The
isolate-wide string-code-generation callback is rebound across nested realm
entries so Trusted Types and `eval` policy remain Page-local. Pool capacity can
be overridden with `MIMIC_REALMS_PER_ISOLATE` for diagnostics.

The compiled replay binary `d482e32` (SHA-256
`65C72ED39867C4E6E7B47425EBDFEDE007E75300096E8B0B8C74A3C2B2A90C2C`)
measured median **1239.81 MiB private / 1005.24 ms** across three default-pool
runs at 10 concurrent Pages. The same binary with pooling disabled by capacity
one measured **1426.84 MiB / 957.21 ms**, a **13.1% private-memory reduction**
at a one-sample **5.0% wall-time cost**. Capacity 16 reached 1209.93 MiB in an
earlier run but saved little beyond capacity eight and increases serialization.

DOM nodes now allocate attribute maps only after the first attribute while host
style projections continue to expose empty attribute objects. This removes a
per-node allocation without changing DOM state or JavaScript observations.
V8 `--optimize-for-size` and tighter heap constraints were measured and rejected:
both increased memory and/or CPU on the same workload. Pooling therefore remains
an incremental layer, not the claimed 50% solution; most remaining marginal
cost is per-realm native/context state and navigation allocation pressure.

The generated shape publisher was subsequently changed to discard its parsed
IDL graph after publication and Browser-selected catalogs now contain only the
fields needed to construct observable shape. Rich generator metadata (source,
types, arguments, origins and Blink annotations) remains process-wide and no
longer crosses into every realm. A compiled 10-Page replay measured 1232.19 MiB
private and 894.31 ms. This is compatible and reduces bootstrap allocation, but
its RAM delta against the pooled median is small; V8 diagnostics bound the live
JavaScript heap at roughly 20-24 MiB for the entire pooled isolate. Replacing
these facades with native FunctionTemplates therefore cannot account for the
remaining roughly 1.2 GiB and is not pursued as a high-risk identity rewrite.

## 2026-09-21 -- demand-driven isolated-world backing

Named isolated worlds now publish their logical execution-context identity and
lifecycle immediately, but do not allocate a V8 context or install the WebAPI
surface until an operation first observes that world. Executable initialization
scripts remain synchronous and force materialization before document scripts.
Whitespace/comment-only initialization scripts create the named world without
materializing it because they have no JavaScript-visible effects. The first
evaluation restores the same bootstrap snapshot and binds the same realm-local
host state as the previous eager path.

This removes a complete unused realm from automation sessions whose utility
world contains only a `sourceURL` comment. It is a general realm-lifecycle fix,
not a Campfire resource or URL shortcut. Three 50-run official Lightpanda
Campfire measurements of compiled binary SHA-256
`2D1681EB47447D05D25B095FDDDDCB788F34D5F38B5DFA216586D32F1BCE9399` measured
Mimic totals of **2734 / 2753 / 2841 ms**, peak RAM of
**260.75 / 262.29 / 262.88 MiB**, and CPU of
**5437.5 / 5437.5 / 6484.38 ms**. Against the supplied 4079 ms / 520.83 MiB /
9593.75 ms baseline, the median is **32.5% less wall time (1.48x throughput),
49.6% less peak RAM, and 43.3% less CPU**. All three runs completed 50/50, and
the full repository test suite passed.

## 2026-09-21 -- context-owned V8 callback lifetime

Pooled isolates previously retained every direct native-function registration
until the entire isolate was destroyed. Closing a Page disposed its V8 context
and all JavaScript functions, but the gov8 host registry still held their Go
closures and native dispatch contexts. Sequential short-lived Pages therefore
accumulated unreachable realm, DOM and binding state in the intentionally hot
isolate.

Direct native functions are now registered to their creation context and their
host registrations are released only after successful native context disposal.
Isolate-owned template and shared callbacks keep their existing lifetime. This
is teardown correctness rather than collection policy: no callback reachable
from a live context is changed, and the hot isolate and bootstrap snapshot stay
available for the next Page. A regression test creates and closes 32 contexts
and verifies that the isolate callback registry returns to its baseline after
every close.

Three compiled 100-run Campfire measurements produced **243.57 / 222.17 /
230.76 MiB** peak RAM and **5661 / 5778 / 5916 ms** official totals. Against
the immediately preceding compiled control (**335.16 MiB / 5925 ms**), median
peak RAM falls **31.1%** while median wall time improves **2.5%**. The rejected
alternative of forcing V8 collection on every closed realm reached 294.59 MiB
but regressed wall time to 6575 ms; it was fully removed.

## 2026-09-21 -- fresh public v0.1.5 benchmark checkpoint

The unchanged repository benchmark was rerun from the current v0.1.5 source and
published under `benchmark/runs/12-release-20260921`. All 12 correctness gates,
360/360 measured single-Page attempts and every concurrency series passed. At
50 concurrent static Pages, Mimic measured **87.01 sessions/s and 1360.69 MiB
active process-tree RSS**, versus Chrome 152 at **21.37 sessions/s and 4117.22
MiB**: **4.07x throughput with 3.03x less active RSS** on this controlled local
fixture. At CDP readiness, Mimic used **147.71 MiB RSS** versus Chrome at
**374.99 MiB**, while its median readiness latency was slower (**734.94 ms vs
264.47 ms**) because startup includes eager preparation of the persistent
bootstrap artifact.

The README now uses one reproducible benchmark story generated by
`tools/performance/benchmark_story.py`. Its decorative Mimic adaptation artwork
is a checked-in input; all displayed measurements come from the checkpoint's
integrity-checked `raw.json` and `summary.json`. The full report remains the
authoritative view, including mutation-heavy and cold workloads where Chrome is
faster.

## 2026-09-22 -- release idle bootstrap isolates after Page closure

The memory attribution probe found that the bootstrap runtime pool retained an
isolate after its final realm closed, until Browser.Close. The pool now removes
and disposes that owner on the last realm close. The immutable bootstrap
snapshot stays cached and restores subsequent Pages; live sibling realms remain
valid until they close.

On the local CDP static fixture, six successive waves of 25 concurrent Pages
all passed. Process-tree RSS two seconds after closing each wave was **174,
223, 265, 279, 284, 306 MiB**, compared with the preceding binary's **475,
205, 888, 1204, 1353, 1390 MiB** on the same diagnostic. The final wave thus
retained about **1.08 GiB less**. Three React waves of 25 Pages and one React
wave of 100 Pages also passed. In the 100-Page wave, RSS fell from **4.82 GiB
active to 548 MiB** two seconds after closure. Teardown median for static
25-Page waves was **40-86 ms** versus **17-80 ms** before; disposing idle
isolates can make close slower, especially once the old pool was fully warm.
These figures are diagnostic snapshots, not a replacement for the frozen public
benchmark. Native snapshot consumer copies and active-Page memory were not
changed.

## 2026-09-25 -- generated-profile bootstrap sharing and memory

Distinct generated Contexts had selected different bootstrap keys despite the
same exposed JS graph. Managed Pages now prepare the exact security/exposure
graph once, then restore independent realms with Page-owned host callbacks.
In three alternating fresh-process pairs with 100 simultaneously live distinct
profiles, median maximum sampled RSS fell from **4004 to 3135 MiB**
(**-869 MiB, -21.7%**); all 100 candidate realms restored one shared artifact.
With eight live Pages processing 300 jobs, the two-pair midpoint fell from
**537 to 398 MiB**. A 1000-job, concurrency-eight soak sampled **403 MiB**
maximum live RSS and 149–189 MiB after each hundred closed, with no retained
Contexts. Profile environment ownership transfer also removed a sampled
~1.5 MiB/100-Context retained Go clone. Direct resolved-profile validation and
typed hashing lowered profile-attributed sampled allocations from ~156 to
~34 MiB across the first 100 Pages. Removing the per-Page bootstrap-source copy
and canonicalizing empty profile collections lowered a later matched-fixture
sample of total Go allocations from 408 to 334 MiB. Native V8 remains the
dominant RSS cost.
These are local barrier samples, not continuously measured peaks or a Chrome
comparison. See [method, raw receipts and limitations](profile-context-final-20260925.md).

## 2026-09-25 -- compact DOM Node layout

A separate DOM change groups three `Node` booleans after pointer-sized fields,
reducing the struct from 296 to 288 bytes and its Go allocator class. Five
DOM-heavy documents with 10,000 repeated element pairs each used median live
Go heap **73.82 → 68.99 MB** across three alternating pairs (-6.54%). Parser
bytes per batch/stream parse fell 114,089→107,721 / 183,003→176,507;
allocation counts and semantics stayed the same. The small-DOM 100-Page fixture
did not show a meaningful process-RSS change. Five control and three candidate
fast gates passed, but candidate warm DOM completion was 309.0 versus 295.9 ms
median, a possible 4.4% workload cost that the parser benchmark did not show.
See [method and limits](dom-node-layout-20260925.md).

An earlier 16-record block allocator saved 3.16% DOM-heavy live Go heap and
reduced allocation counts, but one of five candidate fast gates stalled all 25
jobs in a static wave; all five controls passed. Its
[negative result and removable patch](dom-node-blocks-20260925.md) are retained.

## 2026-09-25 -- generated GPU/font recipe density check

After adding four paired GPU/font recipes, a fresh in-process Windows V8 probe
created 100 distinct generated Contexts and navigated one Page per Context
concurrently to a local 200-link fixture. All 100 Pages returned the expected
link count. At the live barrier, process RSS was **2968 MiB** (private bytes
**3169 MiB**, Go heap **81 MiB**). Two seconds after closing every Context,
without forced GC, RSS was **381 MiB** and three goroutines remained. The
earlier final profile build's single 100-Page sanity sample was **2978 MiB**;
these nearby single samples suggest no large density regression, but they are
not a matched A/B measurement. The temporary measurement probe was removed
after the run.

Expanding that catalog to 47 paired recipes (40 distinct WebGL renderers) and
repeating the same single-sample 100-live-Context fixture gave **2960 MiB**
live RSS (private bytes **3163 MiB**, Go heap **95 MiB**) and **384 MiB** RSS
two seconds after closing all Contexts. The 100 sampled profiles selected 37
different renderers. Compared with the four-recipe sample, the larger catalog
did not materially change process RSS; both results are diagnostic snapshots,
not a controlled A/B peak-memory comparison. The temporary probe was removed.

## 2026-09-26 -- Flickr Turnstile borrowed DOM getter

The live Flickr workload previously reached a Turnstile token about 40–44 s
after navigation started. A per-realm transaction count attributed roughly
60,000 reads of a foreign function's `call` property and 60,000 calls of the
borrowed `Node.parentNode` getter to the main Page. These crossed the realm
bridge even when the receiver was a local canonical DOM wrapper. The main
realm spent about 22 s in the corresponding frame transactions in the
instrumented baseline; this is inclusive timing, not an additive CPU total.

Commit `e0449ff` recognizes the exact native getter after binding finalization
and invokes it on a local DOM receiver in that receiver's realm. It also uses
the internal ShadowRoot slot in the native style admission walk, avoiding
prototype-chain traversal through foreign proxies. Cross-origin checks and
foreign receivers retain the ordinary bridge path. Focused cross-realm,
snapshot and shadow-style tests passed; the full local suite was not run.

Two subsequent uninstrumented live runs obtained both Turnstile POSTs and a
730-character token at about 20.7 s after navigation began. The main document
response took about 0.7 s, response-to-DOMContentLoaded about 8 s, iframe
response-to-first-POST about 1.4 s, and first-POST-response-to-second-POST
about 8.5 s. This is a large improvement, but still slower than the preserved
successful Chrome reload HAR, where document-to-second-POST was about 4.8 s.
The HAR was a warm Chrome reload, so its absolute time is not a matched A/B
baseline.

The remaining instrumented workload attributed about 6.2 s / 63,699 frame
transactions to the main realm, dominated by live reads of the foreign
`Function.prototype.call` property. A Turnstile iframe realm spent about
5.0 s / 32,166 transactions plus 1.0 s / 10,047 foreign global reads. The
first-to-second-POST gap remained about 8–9 s. These live mutable reads cannot
be memoized without an invalidation model that preserves cross-realm writes;
the residual latency is not a network-response delay. Temporary diagnostics
were removed from production source after measurement.


## 2026-09-27 -- diagnostic trace retention

A focused attribution probe issued 10,000 queries against a fixed 1,024-node
result. Live Go heap grew from 22.54 to 103.71 MiB; approximately 83 MiB was
attributed to retained query-result trace payloads. Clearing the trace returned
live heap to 20.88 MiB. Persistent V8 handles stayed at 1,272. This establishes
unbounded diagnostic retention without attributing all memory in a live process
or excluding other leaks. This was an attribution probe, not an E2E benchmark.

`trace.Recorder` retains full result ID lists and `Mimic.getTrace` copies and
serializes the complete history. Network retained-body budgets do not cover
these payloads. A fix is deferred: Window and Worker performance timelines also
consume network completions through `Recorder.EventsSince`, so bounding diagnostic
history must preserve semantic delivery independently. No event-dropping policy,
forced production GC or memory fix is included. Private profiles and live-site traces are excluded from the public changes.

## Bootstrap preparation admission, 2026-09-28

Profile preparation captured its seed in one temporary Page, then initialized a
second temporary Page solely to select the cache entry and start serialization.
It now admits the captured entry directly. A focused runtime-creation counter
reproduced two ordinary runtimes before the change and verifies one afterward,
with an admitted artifact in both cases. Browser ownership, cache budgets,
independent Page owners and first ordinary Page cold semantics remain intact.
Focused cache admission and restored-frame callback/job tests pass on Windows
and Linux. This removes measured duplicate initialization; no latency percentage,
memory improvement or Wikipedia E2E result is claimed.
