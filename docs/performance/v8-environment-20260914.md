# Pure CPU kernel across V8 environments, 2026-09-14

The CPU gap to Chrome is already present in Mimic's minimal V8 runtime. Loading
the full Page environment adds only about 2.1% to this synchronous kernel and
3.3% to the version with Promise checkpoints. This result does not support a
WebAPI or Page rewrite as a remedy for the pure CPU workload.

## Bounded comparison

The diagnostic extracts the Array.from/object, JSON parse/stringify, Map and
RegExp loop directly from frozen `benchmark/fixtures/workload.js`. Its thirty
rounds return the original sum, 59614380. The asynchronous form retains all
thirty `await Promise.resolve()` statements; the synchronous form removes only
that statement. Crypto.digest and the surrounding browser workload are outside
this CPU slice. The frozen file is unchanged, and exact extracted source bytes
and checksums agree across every engine result.

Mimic uses one shared operation implementation: run on the isolate owner,
Eval, explicit microtask checkpoint, Await, Export, then release both temporary
result roots on that owner. The three boundaries are minimal engine.Runtime,
the runtime of a real fully bootstrapped Page, and the same Page operation
inside its actual scheduler task. The last two retain the normal Page globals,
wrappers, observers and tracing. This isolates the environment cost and the
scheduler cost; it is not a measurement of the complete public CDP or Page API.

Four independent processes per boundary/form each execute one first kernel and
twelve subsequent kernels, with forward/reverse boundary order and no concurrent
heavy build or test workloads. Chrome runs the exact expression through Runtime.evaluate
with awaitPromise/returnByValue. Its wall time includes CDP transport, unlike
Mimic's direct owner operation; those values indicate the gap rather than an
exact pure-JavaScript execution ratio. Diagnostic profiling is disabled during
timings. Explicit memory collection occurs only after the measured operations.

## Results

Medians of four process medians, milliseconds:

| Boundary | Synchronous loop | Loop with thirty awaits |
| --- | ---: | ---: |
| Minimal Mimic V8 runtime | 24.517 | 28.746 |
| Fully bootstrapped Page runtime | 25.033 | 29.684 |
| Same operation inside a Page task | 24.835 | 30.353 |
| Frozen Chrome 152, CDP round trip | 17.470 | 18.702 |

The first kernel operation is approximately 30–35 ms for all Mimic boundaries,
and 43.24/53.49 ms for Chrome sync/async in these trials. This is separate from
environment setup. Mimic Page setup takes approximately 291–297 ms versus
31–32 ms for the minimal runtime; process-to-first-checksum medians are
388–397 ms versus 125–129 ms respectively. Chrome browser startup is not measured
by this probe and must not be compared with those process-to-first-result values.

The measured kernels make zero Go host callbacks in every Mimic boundary.
Persistent-handle counts remain at their pre-loop level, two in minimal V8 and
1275 in the Page, both immediately after execution and after explicit collection.
The Page's existing baseline roots are not claimed to be leaks. Collected V8
used heap is approximately 0.55 MiB in the minimal environment and 14.88 MiB in
the Page. The comparison does not establish RSS/page savings or throughput from
removing browser capabilities; those capabilities are required by real workloads.

Mimic reports V8 `15.2.124.1-rusty`; Chrome 152.0.7977.82 reports V8
`15.2.124.21`. Both run in the same Linux environment, but their V8 patch version,
embedding configuration and native build differ. Mimic keeps the pinned
`--no-extensible-ro-snapshot` invariant and four-MiB maximum young generation.
This experiment does not identify which GC, JIT or build difference causes the
remaining gap. It permits a separate bounded nursery experiment before more
expensive native rebuilds.

## Evidence and reproduction

[data/v8-environment-20260914](data/v8-environment-20260914/) contains complete
compressed JSON rows, summary and receipts. Each raw row embeds the exact
executed CPU expression, its hash and the checksum; Mimic rows include native
heap spaces, total allocations, owner-thread CPU counters, Go heap, result-root
counts and setup/first-result timing. Temporary probe sources/binaries are
discarded after the campaign rather than added as a maintained benchmark mode.

To repeat, use the embedded sync/async expression and identical owner-scoped
Eval → checkpoint → Await → Export → Release operations. Build a full ordinary
Page before using its runtime, retain its observation/tracing policy, and
measure a separate variant wrapped in its scheduler. Use one process per
boundary/form/trial and validate all thirteen checksums. Collect memory after
the timed twelve-operation sequence, then close the owner.

The test binary is built from f07bd2d plus only the additive diagnostic overlay,
without the bootstrap/CSS prototypes or subsequent production optimizations.
Its SHA-256 is
`dc6f3d7c7e50d70ea722c13a5526665932db7cb53686a5dd32d9545fe8c053e7`.
The frozen fixture SHA-256 is
`484019063e33e131756f67ab6f3076ad2c18e3959072fbae42f540455448c214`.

One final Chrome process completed but its temporary-profile deletion raced
background shutdown before its result was persisted. The runner was changed to
close Chrome normally; only the missing final trial was repeated. The original
28 rows were preserved, the final four rows completed the series, and the
receipt records both runner hashes and the retry reason. No timed kernel or
correctness predicate was altered.
