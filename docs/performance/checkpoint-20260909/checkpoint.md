# Benchmark checkpoint — 2026-09-09

The control fast gate completed; no full matrix was run. Results below identify
their measured source revisions and distinguish production code from experimental
variants.

Production implementation remains 52a2aa2. Latest snapshot code is committed
separately as a19b84d, based on ca557ca; it is not promoted. The rejected 8 MiB
nursery experiment is preserved separately as 2d4264e. Neither experimental
revision is part of the measured production implementation.

## Commits

Complete main and detached experimental history for this checkpoint is in
[commits.txt](commits.txt). Adopted code changes: 6844dcd (packed DOM bridge),
6277094 (insertion traversal), 0abfa05 (observation handlers), 4d0f0a1 (native
intrinsic preservation), 52a2aa2 (catalog filtering). Main evidence commits and
all isolated experiments are listed separately so experimental wins cannot be
mistaken for production results.

## Benchmarks and limitations

The **73.406 ms Mimic versus 30.326 ms frozen Chrome 152** is verified
for milestone 05 against the original frozen baseline. It is historical, not
the latest full-matrix result. Latest full07 is **68.929 versus 30.747 ms**.
Neither establishes a DOM latency lead over Chrome.

| Scope | Before | After | Delta |
|---|---:|---:|---:|
| Milestone 04 -> 05 DOM execution | 148.618 ms | 73.406 ms | 2.02x faster |
| Milestone 04 -> 05 DOM completion | 201.910 ms | 131.728 ms | 1.53x faster |
| Production DOM host calls | 60,060 | 54,055 | -10.0% |
| Final local control -> snapshot DOM execution | 70.148 ms | 69.721 ms | -0.6% |
| Final local control -> snapshot DOM completion | 114.515 ms | 104.874 ms | -8.4% |
| Final local control -> snapshot static completion | 43.132 ms | 38.384 ms | -11.0% |
| Final local control -> snapshot React execution | 36.868 ms | 39.848 ms | +8.1% slower |
| Final local control -> snapshot React completion | 88.878 ms | 85.441 ms | -3.9% |
| Static N10 throughput | 57.23 Pages/s | 55.55 Pages/s | -3.0% |
| Static N25 throughput | 87.16 Pages/s | 75.78 Pages/s | -13.1% |
| Static marginal RSS | 24.959 MiB/Page | 50.862 MiB/Page | +104% |
| React marginal RSS | 29.022 MiB/Page | 59.364 MiB/Page | +105% |
| Static recovered RSS above ready baseline | 69.980 MiB | 316.773 MiB | +246.793 MiB |
| React recovered RSS above ready baseline | 88.246 MiB | 342.473 MiB | +254.227 MiB |

Completion excludes session creation and teardown. Snapshot DOM session creation
is 19.650 versus 1.941 ms; React 23.300 versus 11.980 ms. Its snapshot was built
before the benchmark, so its creation cost is not in these timings. These
regressions and short-run workstation variability rule out promoting this
experiment or claiming a general speedup. Latest full07 includes valid N100
waves and superior Mimic throughput/memory versus Chrome, but that result does
not establish single-Page latency superiority. The intermittent ten-second
teardown tail remains causally unresolved even though it did not recur in full07.

## Five remaining measured DOM host-work groups

Latest production census, four warm iterations, host-inclusive milliseconds per
iteration. This ranks measured host bodies, not all V8/native/JS overhead; these
numbers overlap other profile measures and must not be added to predict wall time.

| Group | ms/iteration |
|---|---:|
| Token/class mutation (`toggleToken`) | 8.661 |
| Attribute writes (`setAttribute`) | 7.527 |
| Canonical insertion (`insertPlain`) | 5.085 |
| Subtree selectors (`queryAllWithin`) | 4.019 |
| Element/text/comment creation, combined | 3.937 |

Additional native attribution at milestone 05: anonymous/native frames 44.10 ms,
wrap 5.63 ms, GC 5.03 ms, observe 3.12 ms, Proxy get 2.97 ms. These are sampled
self costs from that historical cohort, not fresh snapshot measurements. Fresh
production startup profiling measures surface execution 39.8–41.9 ms plus
6.1–7.5 ms compilation. Snapshot startup and memory costs are not yet fully
attributed; do not infer that reducing host counts alone removes these costs.

## Remaining production DOM crossings

| Category | Calls/iteration |
|---|---:|
| `setAttribute` | 18,000 |
| `toggleToken` | 12,000 |
| `insertPlain` | 9,001 |
| `parentNode` | 3,003 |
| `contains` | 3,001 |
| `create` | 3,001 |
| `createComment` | 3,000 |
| `createText` | 3,000 |
| `apiAccess` | 28 |
| `queryAllWithin` | 6 |
| `getAttribute`, `performanceNow`, `query`, `queryWithin`, `setInnerHTML`, `textContent` | 2 each |
| `nodeChildren`, `storageGet`, `storageSet` | 1 each |
| **Total** | **54,055** |

Source: `../dom-current/catalog/crossings.json`. Bootstrap and diagnostic hooks
are excluded. Separate incomplete prototypes measured 9,104 and 3,104 calls;
they were not merged, and those counts do not describe production or a new
snapshot census. No new profile was run after this checkpoint.

## Validation and preservation

Fresh checkpoint validation on the final experimental source:

- `go test -json ./...`: 242 test/package pass events, zero failures; opt-in
  snapshot proofs are skipped by this default invocation.
- `go test -race -json ./internal/engine/v8 ./internal/cdp`: 33 pass events,
  zero failures.
- Opt-in `go test -race -json ./internal/browser -run
  '^TestSnapshot(ActualPage|ReactProof)$' -count=1`: three pass events, zero failures.
- Snapshot fast gate completed all six frozen semantic checks, prescribed warm
  samples, N10/N25 waves and ten-Page memory collection. Full frozen matrix and
  full browser race suite with snapshot enabled were **not** run.

Raw validation logs, successful and failed gate artifacts, source patch, snapshot
bytes, builder log, build/launch receipts and a SHA-256 manifest are preserved in
this directory. The original frozen baseline, full05/full06/full07 and their
reports remain unchanged in `benchmark/` and `docs/performance/`. Session binary
artifacts also remain in `.build/`. The snapshot environment and blob/source/
contract hashes are recorded in `manifest.json`; all successful gate executable
launch hashes were checked against their build receipts and current binaries.

## Single smallest next experiment — not started

Measure one restored Page's `StartupData`/isolate-holder lifetime through Close
and collection, compared with one ordinary Page. The observed +247–254 MiB
recovered RSS after ten Pages makes this a more immediate question than another
speed change. Determine whether snapshot storage is released at the correct
lifetime boundary before implementing any correction or expanding snapshots.
