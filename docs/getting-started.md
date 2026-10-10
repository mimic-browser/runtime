# Build, automation & verification

[Back to Mimic](../README.md) · [Platforms and browser targets](../README.md#platforms-and-browser-targets)

Commands below cover Windows amd64 and Linux amd64 with the Chrome 152 target.
The browser profile is independent of the operating system running Mimic.

Mimic models Chrome-visible JavaScript and browser state in Go without running
or embedding Chromium. It has an explicit browser scheduler, resource loader,
separate document/frame/Worker realms, and a subset of CDP.

Mimic provides [approximate screenshots](approximate-screenshots.md) from an
independent renderer. It does not guarantee Chrome-like pixels or full browser
compatibility.
Generated API names describe known shape; they do not imply implemented semantics.
No external-site result or performance claim is a product guarantee.

The [reproducible Windows benchmark](../benchmark/README.md) compares the V8 backend
with exact Chrome 152.0.7977.82 using local correctness-gated workloads, process-tree
CPU/memory accounting and cold, warm and concurrent sessions. See the
[latest measured checkpoint](../benchmark/runs/14-memory-20261010/public-summary.md)
for current workload results and limitations.

## Build and run

Supported hosts are **Windows amd64** and **Linux amd64 with glibc 2.39+**
(Ubuntu 24.04+). Both need Go **1.26.4+**, Rust with Cargo, Python 3,
CGO enabled, and a C compiler
(`gcc` on Linux; MinGW-w64 `gcc` on Windows). The bundled QuickJS fallback
uses CGO even when V8 is selected. Linux ARM64, Alpine/musl, and macOS are
not supported by the packaged engine.

On Ubuntu, install the build and optional capture prerequisites:

```sh
sudo apt-get update
sudo apt-get install build-essential ca-certificates cargo python3-venv fonts-dejavu-core fonts-liberation
```

Install Go 1.26.4+ or allow its automatic toolchain download. From the checkout:

```sh
python tools/build_native_layout.py
go build -o .build/mimic ./cmd/mimic
./.build/mimic -listen 127.0.0.1:9222 -chrome 152
# Explicit fallback, using the same binary:
./.build/mimic -listen 127.0.0.1:9222 -chrome 152 -engine quickjs
```

Windows (PowerShell):
Dependencies are pinned in `go.mod`/`go.sum`; the local `third_party/tls-client`
replacement is required. A first build needs those module dependencies available.
The AVIF decoder requires Go 1.26.4; standard Go toolchain auto-selection can
download that pinned toolchain when the installed patch version is older.

```powershell
python tools/build_native_layout.py
go build -o .build/mimic.exe ./cmd/mimic
./.build/mimic.exe -listen 127.0.0.1:9222 -chrome 152
# Explicit fallback, using the same binary:
./.build/mimic.exe -listen 127.0.0.1:9222 -chrome 152 -engine quickjs
```

V8 is the default (`github.com/maclof/gov8 v0.1.1`); its bundled native engine
reports **15.2.124.1-rusty**. QuickJS (`quickjs-go v0.7.7`) is retained as an
explicit fallback; goja is retained for development and regression tests.
Neither fallback promises V8-equivalent ECMAScript behavior. gov8 extracts its
packaged DLL or shared library into its user cache; no separate V8/Chrome download is needed.
The cache must permit executable mappings (`noexec` mounts prevent V8 loading).
On Linux, a built executable needs glibc, libm and libgcc_s; it needs no compiler,
Rust installation, GPU, display server or Chromium at runtime. Rust is only a
build-time dependency for the statically linked Taffy layout archive.
No `.env` file or secret is required. Stop the foreground server with Ctrl+C; Linux SIGTERM also performs orderly shutdown.
The AVIF resource decoder is pinned to `gav1d v0.2.5` and needs Go 1.26.4;
standard Go toolchain selection downloads that patch version when necessary.
It decodes image bytes in Go without a GPU or external codec DLL.

CDP listens on loopback by default. It has no authentication and is intended for
trusted local clients. Navigation has no arbitrary deadline by default
(`-navigation-timeout 0`); set e.g. `-navigation-timeout 30s` explicitly if needed.
This setting does not terminate background application callbacks. Individual CDP
evaluations and snapshot serialization retain independent 30-second bounds.
Mimic is not a security sandbox for hostile code.

## CDP

Read `http://127.0.0.1:9222/json/version` or `/json/list`, then connect a WebSocket
client to the returned `webSocketDebuggerUrl`. Send, for example:

```json
{"id":1,"method":"Runtime.evaluate","params":{"expression":"1 + 2"}}
```

`Page.navigate` accepts an HTTP(S) URL. Its reply acknowledges navigation start;
wait for lifecycle events before treating the page as loaded. See the
[CDP matrix](cdp-compatibility.md) for the actual supported contract.
An optional static snapshot exporter is available as
`python compatibility/save_snapshot.py --endpoint http://127.0.0.1:9222 --output snapshots/example`
Use `--wait-selector '#ready'` when the application exposes a readiness marker:
the browser's `load` event can precede application hydration. The exporter also
accepts `--timeout` in milliseconds for that wait.
with Pyppeteer 2.0.0 installed. It may load referenced resources and does not
capture canvas pixels, live form state or embedded frames.

For navigation and capture in one command, use
`python tools/mimic_snapshot.py URL OUTPUT --settle-ms 2500`. Its `--timeout`
is a no-progress threshold rather than a hard navigation deadline: network,
lifecycle and active browser-turn progress all keep the wait alive. Readiness
uses Chrome's `networkidle2` shape by default; change the accepted number of
active transports with `--idle-connections`. If progress stops, the tool stops
the outstanding load and preserves the committed DOM with diagnostics in
`snapshot.json`. `--max-wait` supplies a separate absolute safety cap; when a
reported JavaScript/browser turn exceeds it, the tool identifies and interrupts
that turn before taking a consistent snapshot instead of waiting forever.
Interactive terminals get a live progress bar with HTTP status, lifecycle state,
active/request counts, transferred bytes, quiet time and the latest resource.
Redirected output emits the same progress every `--log-interval` seconds. The
bar shows elapsed time against the wait budget, not a predicted completion
percentage. The final summary and `snapshot.json` include per-stage timings.
Portable asset capture uses a bounded concurrent fetch pool and reports its own internal
clone/fetch/rewrite breakdown. Use `--no-progress` to disable animation while
retaining ordinary logs.

No Poetry is needed. Start the freshly built server above, then in another
terminal from the **same checkout**.

Linux:

```sh
python3 -m venv .venv
./.venv/bin/python -m pip install pyppeteer==2.0.0
./.venv/bin/python tools/mimic_snapshot.py "https://example.com/" snapshots/example --settle-ms 2500
```

Windows:

```powershell
python -m venv .venv
./.venv/Scripts/python.exe -m pip install pyppeteer==2.0.0
./.venv/Scripts/python.exe tools/mimic_snapshot.py "https://youtube.com/" snapshots/youtube --settle-ms 2500
```

The legacy `tools/mimic/_snapshot.py` entry point forwards to this tool. Pass a
plain URL (not Markdown link syntax) and a new output directory; existing captures
are never overwritten. Connecting to an old server binary does not enable the
new runtime fixes, even when the Python script is current.

Readiness is a heuristic based on main-document lifecycle, network quiet and
running-task diagnostics, not proof that an application is fully hydrated.
The default absolute budget is 120 seconds (`--max-wait 0` disables it).
When it expires, capture reserves a task boundary and interrupts current work;
`navigation.partial` and `interruptRequested` distinguish this from normal capture.
Asset downloads reuse the runtime's resource loader, with up to 32 concurrent
fetches, 512 resources and an eight-second fetch budget; missing assets are listed
as warnings instead of silently adding serial waits. Scripts and embedded frames
are not exported. Media decoding/playback remains unsupported, and geometry,
including intersection observations, uses the runtime's approximate box model;
it does not implement full layout, scrolling or paint visibility.
Independent resource transfers run concurrently; author JavaScript callbacks
and their microtask checkpoints remain ordered on each Page. Adding Python
threads cannot shorten a busy browser callback. The
[snapshot investigation](performance/snapshot-hydration-20260911.md) records
the measured runtime bottlenecks and the limits of the live-site checks.

## Host-dependent behavior

The default Chrome 152 environment remains the measured Windows browser profile,
including navigator identity. Running on Linux does not turn Windows reference
captures into Linux Chrome observations. Historical benchmark results are still
Windows measurements; the frozen Windows Job Object harness is not a Linux benchmark.

Font discovery uses Windows font directories or `/usr/share/fonts`,
`/usr/local/share/fonts`, `$XDG_DATA_HOME/fonts` (default `~/.local/share/fonts`),
and `~/.fonts` on Linux. `MIMIC_FONT_DIR` overrides discovery with a path list
(`;` on Windows, `:` on Linux). Fonts are not bundled: matching Windows Chrome
text metrics requires the same legally available font files. Different fonts
produce different metrics; Mimic does not fabricate reference glyph metrics.
Without an explicit `MIMIC_FONT_DIR`, missing generic reference families fall
back to installed Liberation, DejaVu or Noto fonts on Linux. These are actual
font resources: text measurement and control geometry remain consistent with
the chosen glyphs. An explicit font directory remains strict; missing resources
report unsupported behavior. Install at least one of these font sets for text,
control geometry and coordinate-based input.

Native speech synthesis uses Windows SAPI. Linux retains the explicit
`synthesis-unavailable` error boundary. This does not prevent browser execution.
Windows thread power hints are isolated from Linux scheduling; Linux retains
the caller's scheduling policy. Per-thread CPU diagnostics use native OS counters.

## Rebuilding the native engine

Ordinary `go build` uses verified, compressed native engines already in the
checkout. To change the C++ shim, rebuild and repackage the matching platform:

```sh
# Linux amd64; requires Rust/cargo, Python 3.11+, Go, binutils, and glibc headers.
python3 third_party/gov8/scripts/setup_linux.py
```

The script verifies pinned V8 inputs and Temporal locks, obtains the matching
Clang toolchain, builds the same shim semantics, and updates the Linux asset
and checksum metadata. Do not substitute system libc++ headers or a different
V8 build: their ABI can differ. Rebuilding on a newer glibc may raise the runtime
requirement; inspect the resulting library before distributing it.

See [gov8 build provenance](../third_party/gov8/README.mimic.md) for Windows
rebuild commands, versions, packaging and focused validation.
`GOV8_SHIM_LIBRARY` selects a trusted custom native library;
`GOV8_SHIM_DLL` remains a legacy alias. Normal runs need neither variable.

## Architecture and target

`CDP -> browser commands -> canonical state / scheduler / resource loader -> engine`.
`chrome/152` is selected through a version-neutral compatibility registry.
Handwritten Web API semantics consume the generated Blink WebIDL/CDP surface.
Transport/session observations remain authoritative for network protocol,
connection reuse and Resource Timing.

The target is **Chrome 152.0.7977.82**, Chromium
`d04cdb24d67b081f6cf80200ffc5233f44b61109` (r1669021). Exact Blink, V8/CDP and
WPT identifiers are in `chrome/152/target.json`. The target V8 source revision
and the packaged gov8 binary are separately identified; their equivalence has
not been established by this cleanup.
The authoritative behavioral oracle is normal **headful** Windows x64 Chrome with
a fresh controlled profile. Headless observations are explicitly mode-scoped and
non-authoritative until a probe is proven invariant. See
[the oracle policy](oracle-policy.md) and [the headless audit](oracle-headless-audit.md).

Read [architecture and invariants](architecture.md),
[compatibility limits](compatibility.md), and
[generated-data provenance](generated-data.md) before changing state ownership.

## Verification

[Recorded Windows/Linux checks and limitations](platform-validation.md).

```sh
go test ./...
go test -race ./...
python3 tools/generate_compat.py --check
python3 tools/check_repository.py
```

The full suite includes **exact Windows font measurements**. To run those
unchanged expectations on Linux, provide the same reference font files through
`MIMIC_FONT_DIR`. For example (use a local Linux directory for the test fonts):

```sh
MIMIC_FONT_DIR=/path/to/reference-fonts go test ./...
```

In WSL, the source font files are available under `/mnt/c/Windows/Fonts` on
the reference host. Copy them locally for a full run: repeatedly scanning a
Windows mount can exceed the suite’s default timeout.

This is a test-resource requirement, not a Windows runtime dependency. The
fonts are not redistributed. With ordinary Linux fonts, exact Windows geometry
assertions will differ; do not rewrite their frozen expectations to hide it.
CI compiles all Linux tests and runs portable components, native ownership/race
checks and the executable smoke test. Full oracle tests run on Windows; full
Linux oracle validation requires separately supplied reference fonts.

Verify a freshly built executable, with only local fixtures:

```sh
go run ./tools/runtimecheck -binary .build/mimic
go run ./tools/runtimecheck -binary .build/mimic -engine quickjs
# Windows: use -binary .build/mimic.exe
```

This starts the binary outside the checkout with a fresh engine cache and checks
CDP navigation, JavaScript/Promises, real text metrics, coordinate mouse input,
target teardown and orderly shutdown (including Linux SIGTERM).

Use `python` instead of `python3` on Windows if needed. Run the tests on each
host; cross-compilation alone does not validate the native library or callbacks.

Tests use local fixtures; no external E2E investigation is part of this baseline.
The race detector makes generated-surface bootstrap substantially slower; retain
Go's default test timeout. See [the stabilization audit](stabilization.md)
for exact successful and failed runs, including the original hangs.
The nested transport module has its own focused tests documented there.

The [Navigator capability matrix](navigator-capabilities.md) links exact
Chrome captures, before/after differentials and tested semantics. Shape parity
and intentionally unavailable service/hardware backends are reported separately.

`--check` is offline: it verifies retained artifact hashes, pins and deterministic
IDL/CDP projections. Full upstream regeneration is a separate explicit operation.
Historical observations and small before/after captures are retained under
`docs/` and `compatibility/captures/`; optional site-oriented tools live in
`compatibility/research/` and are never loaded by production code.

Known gaps include pixel-accurate rendering, complete CSS layout, Canvas/WebGL pixels, media,
full DOM/Web API algorithms, full CDP object handles, cookie/CORS completeness,
Streams backpressure/BYOB and complete fallback microtask semantics. Do not infer
full Chrome compatibility from a passing focused suite.
