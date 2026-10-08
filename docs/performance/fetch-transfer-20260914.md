# Fetch binary transfer spike — 2026-09-14

Decision: promote binary response transfer. This slice cuts both delivery CPU and
resident memory for medium/large responses. It does not implement network streaming.

## Measured result

Linux (Ubuntu/WSL2), Go 1.26.4, V8 152, source base
`f07bd2d643eebbadc28d582ef93f01c832a50106`. Control/candidate/candidate/control,
three iterations of each size in each process: six observations per variant/size.
Server, Page and test driver run in the same Linux process; no other builds or
tests ran during the timing window. All bytes are checked.

| Response | Page operation control | Candidate | Speedup | Go allocation control → candidate |
|---|---:|---:|---:|---:|
| 1 KiB | 1.916 ms | 1.930 ms | 0.99× | 272,232 → 201,348 B |
| 64 KiB | 16.877 ms | 2.243 ms | 7.52× | 5,276,852 → 459,000 B |
| 1 MiB | 246.427 ms | 5.131 ms | 48.02× | 80,336,236 → 3,629,364 B |
| 4 MiB | 1,003.765 ms | 13.488 ms | 74.42× | 320,897,524 → 14,627,512 B |

Values are medians. The operation is `Page.Evaluate` including real loopback Fetch,
body reader consumption and validation of every byte, not a bare native conversion.
The 1 KiB timing difference is below 1% and provides no evidence of a speed gain.
The frozen performance harness, fixtures and original baselines were not modified.

After 12 Fetch calls in one live Page and explicitly requested V8/Go collections:

- Private resident memory: control about 609.9 MiB; candidate about 151.9 MiB.
- V8 used heap: 168.9 → 17.0 MiB. V8 external memory returns to about 0.252 MiB.
- Go live heap: about 30.4 → 34.5 MiB; candidate has one final 4 MiB buffer still
  reachable through the live owner. Total process memory is about 458 MiB lower.
- Both variants grow from 1,379 to 1,703 persistent roots; this slice excludes the
  independently developed handle-lifetime fixes. Eliminating the old numeric-array
  representation prevents those roots from retaining the full response bodies.
- After `Page.Close` plus explicit Go GC/scavenging: private resident about
  124.2 → 117.7 MiB. Context still retains the closed Page; Loader history is not
  cleared by Page.Close, so this is not proof of complete browser teardown.

These forced collections are attribution interventions outside measured operation
intervals. They are not presented as natural benchmark recovery or peak memory.

For a 256 KiB response sent in four chunks delayed by 20 ms each, operation time
is 147.09 → 84.77 ms. Candidate headers arrive at 82.55 ms and first data at
82.60 ms: it still waits for the complete network body. Two samples per variant.

## Change and ownership

`fetchResponse` returns an explicit `engine.BinaryBuffer` rather than an integer
slice and duplicate UTF-8 string. V8 copies it directly into engine-owned
ArrayBuffer storage; Goja/QuickJS retain corresponding native buffer semantics.
The source Go slice is not pinned, retained by native memory, or shared with JS.
The temporary V8 backing-store reference closes synchronously and KeepAlive guards
the source across the native copy. Ordinary Go byte-slice marshaling is unchanged.

Fetch creates a Uint8Array view over the fresh buffer and transfers that view to
the byte stream, removing the previous extra slice. Response.clone still goes
through the existing byte-stream tee; mutations cannot alias another branch,
another response, the loader history or the HTTP cache. Window and Worker use the
same fetchResponse and JS adapter. Upload body conversion is unchanged.

Promote these production files:

- `internal/browser/fetch.go`
- `internal/engine/binary.go`
- `internal/engine/goja/runtime.go`
- `internal/engine/quickjs/runtime.go`
- `internal/engine/v8/binary.go`
- `internal/engine/v8/runtime.go` (binary marshaling hunks only; handle work is separate)
- `internal/webapi/fetch_compatibility.js`

Promote focused tests `internal/engine/binary_test.go` and
`internal/browser/fetch_binary_test.go`. The opt-in diagnostic
`internal/browser/fetch_transfer_probe_test.go` can be retained as a non-frozen
measurement tool or moved to the final diagnostics location by the campaign owner.
Do not promote `internal/network/stream_source_spike_test.go`: it is the isolated
transport/decode/cancellation proof, not an integrated Loader API.

Validation completed: Windows focused engine binary/host-record tests, shared
WebAPI tests, browser Fetch/WorkerFetch/ResourceTiming suite; Linux compiled
candidate Window/Worker binary, clone and abort tests. The test-only real HTTP
source proof passed with identity and incremental gzip bodies. QuickJS's three-line
conversion adapter has not yet been independently tested in its optional package.

## Reproduction and receipts

Artifact names below refer to
[the measurement archive](data/optimization-20260914/fetch-transfer/).
JSON is stored with an additional `.gz` suffix; decompression recovers the exact
original bytes. The archive manifest records both source and stored hashes.

- `linux-control-build.json`, `linux-candidate-build.json`: commands, source hashes,
  versions/commit and executable fingerprints.
- Control browser test SHA-256:
  `293ac4607c30098a649e9fd527f9d03a42f0e04fd3abc20a25097a95ee5ad20b`.
- Candidate browser test SHA-256:
  `20ec619975ef46fa458c8c660553609941d31e924af34dfdd898b8cd4b1ba4f4`.
- `linux-paired/receipts.json`: SHA verification and exact command before each launch.
  `linux-paired/summary.json`, `memory-summary.json`, per-launch
  `fetch-transfer.json`: all timing and memory evidence.
- `linux-delayed/`: analogous delayed-body evidence.
- Corresponding Windows builds/receipts exist; Windows was used for correctness,
  not for the reported timing comparison.

The temporary driver and implementation spikes are discarded after the decision.
The retained regression tests exercise actual Window/Worker Fetch, binary
independence, clone and abort behavior. The fixture dimensions and measurement
boundaries above describe the diagnostic; the unchanged full workload harness
is used for the final integrated checkpoint.

## Independent next slices

Shared BodyStore: the current cache and 128-entry request history keep separate
full response copies. History retention is count-bounded, not byte-bounded, and
survives Page.Close while Context still owns the Page. Preserve mutable Go response
and After-interceptor contracts; share one immutable stored representation across
cache/history/CDP, bound resident bytes, and release each owner's storage on eviction
or teardown. Measure copy count, live/recovery memory, disk spill cost and CDP reads.

Integrated headers-first: introduce one Loader headers/body lifecycle and a
buffering Load adapter. Resolve final headers only after redirect/CORS decisions;
keep owner cancellation alive until EOF. Use pull-driven incremental decompression
with stable delivered buffers, separate EOF/failure completion, and update cache,
timing and trace from that same lifecycle. Legacy mutable After interceptors require
buffering before headers are exposed; they must not be skipped. The isolated source
proof establishes that transport/gzip can deliver before EOF with bounded pulls and
real cancellation, but does not establish browser event ordering or CDP semantics.

The transport-only correctness proof exercised identity and gzip responses. The
server sent headers and then withheld all body bytes behind a channel: opening
the source returned before that channel was released, with zero body reads and
no initialized decoder. Releasing it delivered the 11-byte `first block` before
EOF. Cancellation unblocked a pending next read and reached the server within
the test's one-second deadline. A separate 256 KiB deterministic binary source
performed exactly one read per pull, returned fresh 16 KiB chunks, and preserved
the first chunk after the next pull. These are correctness bounds and counts,
not measured first-data or cancellation latency distributions. No browser Fetch
stream implementation or speed claim is inferred from this proof.
