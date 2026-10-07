# Windows / Linux validation вЂ” September 13, 2026

## Supported build targets

- Windows amd64: Go 1.26.4+, CGO and MinGW-w64 GCC for application builds.
- Linux amd64: Go 1.26.4+, CGO and GCC; the packaged engine requires glibc 2.39+
  and libgcc_s. Validation used Ubuntu 24.04 under WSL2.
- Both binaries include their own compressed V8 shim. Deployment does not
  require Rust, a C++ compiler, Chromium, a display server or a GPU.
- Linux text and control geometry need installed font resources. Liberation,
  DejaVu and Noto generic fallbacks use real glyph data; Windows reference
  measurements require the same reference font files.
- The host does not change the frozen Chrome 152 environment identity.
  Native speech synthesis remains a Windows SAPI provider; Linux reports
  `synthesis-unavailable`. ARM64 and musl are not packaged.

## Native portability boundary

The source uses one browser runtime and one V8 adapter. OS-specific files own
native loading, thread identity, scheduling hints, CPU diagnostics and cache
file locking. Windows keeps its existing native DLL and calling convention;
Linux loads a shared library with the System V ABI. Independent Page execution
still has independent owning threads and event loops.

Five legacy floating-point exports need Linux wrappers because the Windows ABI
uses positional XMM arguments. Wider pointer-word exports use a native bridge
beyond purego's 15-argument limit. Regression coverage includes numbers, signed
zero, NaN, callback results, Date construction, cached scripts and snapshots.
The shared extraction code verifies size/SHA-256 and serializes only cache
publication/repair; a corrupt cache is repaired safely with concurrent callers.

Native versions: V8 `15.2.124.1-rusty`, Rust v8 crate `152.2.0`, Temporal C API
`0.2.6`, shim ABI `44`. Platform-specific size/hash metadata is checked in under
`third_party/gov8/internal/prebuilt`. The Linux setup script was run repeatedly
against the final native source and produced the same packaged-library digest.

## Checks

| Check | Windows | Linux |
| --- | --- | --- |
| Build `cmd/mimic` | Passed | Passed |
| Nested gov8 tests, native ABI and cache repair | Passed | Passed |
| V8 tests with the Go race detector | Passed | Passed |
| Portable components used by the CI matrix | Covered by full suite | Passed |
| Full root suite | Passed (browser package 439.617 s) | Passed (browser package 409.734 s; local reference fonts) |
| `runtimecheck`, default V8 | Passed | Passed |
| `runtimecheck`, QuickJS | Passed | Passed |
| `runtimecheck`, goja | Passed | Passed |

`tools/runtimecheck` launches the executable outside the checkout with a fresh
native cache and local HTTP fixtures. It verifies CDP discovery/navigation,
DOM updates, fractional arithmetic through a Promise, real Canvas text metrics,
control geometry, coordinate-based mouse input, target disposal and shutdown.
Linux shutdown is driven by SIGTERM; Windows uses Browser.close. These checks
are functional validation, not latency/throughput benchmark results.

Commands:

```sh
go build -o .build/mimic ./cmd/mimic
go run ./tools/runtimecheck -binary .build/mimic
go run ./tools/runtimecheck -binary .build/mimic -engine quickjs
go run ./tools/runtimecheck -binary .build/mimic -engine goja
go test -race ./internal/engine/v8
(cd third_party/gov8 && go test ./...)
MIMIC_FONT_DIR=/path/to/reference-fonts go test ./...
```

Use `.build/mimic.exe` for the Windows executable. Full Linux oracle validation
uses the reference machine's font files copied into the local Linux filesystem.
An earlier run scanning those files through the Windows mount reached the
10-minute suite timeout and a short observation deadline; expectations and
timeouts were not relaxed. Running the full Windows-geometry oracle with only
ordinary Linux fonts also fails its exact metric assertions by design. CI does
not redistribute proprietary fonts: it runs the full suite on Windows, compiles
all Linux tests, and exercises the portable components, race checks and all
V8/QuickJS executable scenarios on Linux.

## Hosted Windows CI follow-up

Initial GitHub-hosted Windows runs built the executable but exceeded Go's default
10-minute aggregate browser-package test budget. The timeout stacks showed
different recently started tests (0вЂ“2 seconds), including stylesheet and graphics
oracles, rather than a single test blocked for ten minutes. The workflow now
builds the executable and Browser/CDP test binaries once per Windows run. Three
workers download those test binaries and split every discovered Browser and CDP
root into complete, disjoint shards. Package-qualified names prevent collisions;
every selected root retains all subtests. Browser batches contain at most 25
roots and CDP batches at most 10. The suspended-navigation gate still runs alone
in a fresh process. Test executables run from their package directories, through
`go tool test2json`; the original assertion deadlines and ten-minute process
budget are unchanged.

The remaining Windows packages, native-binding checks, race check and executable
scenarios run once in the build job. Linux capture/codec checks run in the existing
Linux job. Both macOS capture architectures are checked when their implementation,
dependencies or workflow change. Branch CI runs on main pushes and pull requests;
release tags no longer repeat the same commit's whole CI. Concurrency cancels
superseded runs for the same branch or pull request.

All workers retain coverage plans and partial observed timings even on failure.
The checked-in initial timing history comes from successful run `37564450067`,
source `07e4b8ec36c9293ec75485fbad06d23c30503ef7`. Newly discovered roots receive
the heaviest known weight until measured. Unit checks verify complete partitioning,
package-name collisions, process isolation and the executable working directory.
Other Windows packages run one at a time so CLI/CDP readiness deadlines do not
compete with unrelated package initialization on the hosted runner.

One run also exposed a synchronization race in
`TestSnapshotInterruptsNavigationContinuation`: `scriptStart` is emitted before
compilation, so 20 ms of wall time does not prove the first DOM mutation happened.
The test now waits for the fixture's console event after that mutation, just as
the child-parser continuation test already does. It still interrupts the infinite
script and requires the snapshot to contain the mutated DOM. No runtime behavior
or frozen oracle was changed for this test fix.

The same barrier replaces a 10 ms elapsed-time assumption in
`busySnapshotPage`: snapshot/close tests now observe the first timer's DOM
mutation before interrupting it, and still assert that the next timer has not
run. The 3-second readiness and command deadlines are retained.

## Existing publication-audit issues

These are distinct from the platform build and were not hidden by editing
frozen expectations:

- `python tools/generate_compat.py --check` reports that the artifact manifest
  does not cover `chrome/152/generated/window-secure-member-order.md`.
- `python tools/check_repository.py` also inspects existing local downloads,
  benchmark/capture paths and retained binary assets. It reports many existing
  findings and treats both packaged native `.gz` libraries as unexpected binary
  files. Publication should use an intentional source tree and validate native
  assets against their retained hashes, preserving historical provenance.

The frozen Windows performance harness, benchmarks and Chrome captures were
not changed or relabeled as Linux measurements.
