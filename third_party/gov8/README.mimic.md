# Mimic's gov8 extension

This is a minimal source distribution of `github.com/maclof/gov8` v0.1.1
(https://github.com/maclof/gov8/tree/v0.1.1). The upstream LICENSE and
THIRD_PARTY_NOTICES.md are retained, including Temporal dependency licenses.
Upstream examples, conformance fixtures, and unrelated tests are omitted.

## Changes

- Expose V8 ObjectTemplate::MarkAsUndetectable through the checked Go API
  and a new native export. This supplies real HTMLDDA operator semantics.
- Preserve accessor getter/setter values in property-definer callbacks. Their
  handles occupy existing otherwise-unused kind-specific frame slots; the
  callback frame remains 160 bytes and native ABI remains 44.
- Use observation-only native property callbacks and strict native receiver
  dispatch. Callback references are shared by snapshot creation and restoration;
  callback data belongs to the realm rather than a Go registry handle.
- Include the loaded native artifact digest in snapshot identity. Package the
  matching patched Windows and Linux libraries with verified extraction metadata.
- Build V8 with pointer compression and a separate isolate group for each
  execution isolate and snapshot builder. Pages retain independent heaps and
  concurrent execution; there is no process-wide compressed-heap size limit.
  Compression imposes V8's 4 GiB address range on each individual JS heap.
- Compile trusted embedder bootstrap through a separate checked native entry
  point. Classification precedes parsing and survives code caches and snapshots;
  author scripts remain ordinary scripts regardless of their resource names.
  Preserve actual engine intrinsics by Script provenance rather than source-text
  matching when installing public platform callables.

- Expose permanent context microtask shutdown while retaining native objects.
  Queue observations use pinned flat bindings, avoiding the virtual-layout
  difference introduced by V8's cppgc microtask-queue build mode. Other contexts
  retain their shared queue; shutdown is applied only at idle execution boundaries.

The adapter and browser implementation live in Mimic rather than this binding.
The new export fails explicitly if an old GOV8_SHIM_DLL override is selected.
Normal builds require no override and use the embedded verified binary.

## Rebuild

Normal Go builds use the packaged libraries and require no native build.
Maintainers must build V8 from the hash-pinned `v8` crate 152.2.0 with the
patch in `patches/v8-native-observation.patch`. Stock upstream archives do not
implement the required callback semantics. The setup scripts require a patched
source tree and its matching static archive; they never silently use stock V8.

Download `https://static.crates.io/crates/v8/v8-152.2.0.crate`, verify SHA-256
`a10fe1a92da5c32c7c7f838ce36c0ccfcfd5edf0865b58bdde820aa64cea9888`, then
extract it into a dedicated build directory. From this fork:

```sh
python scripts/verify_native_source.py /path/to/v8-152.2.0 --apply
```

This checks every modified file before and after applying the retained patch.
Use Rust 1.98, the crate's locked dependencies and its pinned compiler/dependency
fetchers. Set `V8_FROM_SOURCE=1`, a dedicated `CARGO_TARGET_DIR`, and
`GN_ARGS='symbol_level=0 v8_enable_pointer_compression_shared_cage=false'`, then
build the crate with
`cargo build --release --locked --features v8_enable_pointer_compression --manifest-path /path/to/v8-152.2.0/Cargo.toml`.
The shim's `internal/shim/v8-gn.h` must match the resulting GN
`v8_header_features` and `cppgc_header_features` defines, including the crate's
internal-field counts. Upstream V8 defaults are not the crate's configuration.
On Windows install MSVC x64 tools and configure `GYP_MSVS_OVERRIDE_PATH` to the
Visual Studio installation, `GYP_MSVS_VERSION=2022` and
`DEPOT_TOOLS_WIN_TOOLCHAIN=0`. Preserve the source, exact command and resulting
archive digest as build provenance. The static archive is under
`$CARGO_TARGET_DIR/release/gn_out/obj/` (`rusty_v8.lib` on Windows,
`librusty_v8.a` on Linux).

Windows shim packaging from this fork:

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/setup_windows.ps1 `
  -NativeSource C:\build\v8-152.2.0 `
  -NativeArchive C:\build\target\release\gn_out\obj\rusty_v8.lib
go run ./internal/cmd/package-shim
```

Update `Size` and `SHA256` in `internal/prebuilt/prebuilt_windows_amd64.go`
from the packaging output. Validate without a shim override using focused
native identity, property observation, Date receiver and snapshot tests.

Packaged Windows DLL: 46,458,880 bytes, SHA-256
`a4f771213c2feacd047ec8559d9c3b3311fbdabdd16b8d1efef6ec2b268d7d8a`.
Patched Windows V8 archive SHA-256:
`07896581257a5880946602eba27dfb2677b114e093de2cc98c116c6416755b02`.
Native engine: V8 15.2.124.1-rusty, crate 152.2.0, temporal_capi 0.2.6.

Platform exception initialization removes ordinary Error's hidden original
message state from freshly created DOMException objects. V8 still owns lazy
stack capture, formatting and `Error.prepareStackTrace`; the formatter observes
the exception's current interface name and message. Ordinary Error objects keep
their original state. The realm-owned native initializer has an external
reference shared by snapshot producers and consumers, with no Go callback or
author-visible transport global retained after bootstrap.

## Linux amd64

The same shim and Go binding also run on Linux using the System V ABI.
`internal/native` isolates Windows calls from Linux `dlopen`/`dlsym` and purego
callbacks. Pointer-word exports with more than 15 arguments use a native bridge;
thread affinity remains checked against the owning kernel thread. No global
Page execution lock is introduced.

The packaged Linux library requires glibc 2.39+ and libgcc_s (Ubuntu 24.04+).
It embeds V8, the matching Chromium libc++ and Temporal; no system C++ ABI or
GPU/display service is needed. Its size and digest live in
`internal/prebuilt/prebuilt_linux_amd64.go`. Runtime extraction and integrity
verification are shared with Windows.

The pointer-compressed Linux archive SHA-256 is
`b05abd203317307e55de5b18c006b04c760302182a2ccd1de780430c2c752615`.
The packaged library is 58,996,888 bytes, SHA-256
`1d2969c26bb1787e379088d7585049185377a7538f0405bbffb1076c80e40221`.
Under WSL, use `--build-dir` on the Linux filesystem to avoid extracting the
pinned source tree through the Windows filesystem mount.

From the Mimic root, after building the patched V8 archive as above:

```sh
python3 third_party/gov8/scripts/setup_linux.py \
  --native-source /path/to/v8-152.2.0 \
  --native-archive /path/to/target/release/gn_out/obj/librusty_v8.a
go test ./internal/engine/v8 -run 'Test(PropertyObservation|NumberExport|BootstrapSnapshotIdentity)' -count=1
```

The shim rebuild requires Python 3.11+, Rust/cargo, Go 1.26.4+, binutils and glibc
headers. It verifies the source patch, uses the hash-pinned crate's Chromium
Clang updater and libc++ headers, and builds Temporal with `cargo build --locked`.
The generated extraction metadata records the resulting library's size and
SHA-256. Linux source builds inherit the builder's glibc floor.
For a minimal Linux V8 archive build, set
`GN_ARGS='symbol_level=0 use_glib=false use_sysroot=false v8_enable_pointer_compression_shared_cage=false'`
and enable the `v8_enable_pointer_compression` Cargo feature. Generating the crate's
Rust FFI bindings additionally requires libclang 21.1 or newer; that step is
separate from compiling the V8 archive consumed by the Go shim.

Both engines report V8 `15.2.124.1-rusty` and shim ABI 44.
`GOV8_SHIM_LIBRARY` is the cross-platform developer override; `GOV8_SHIM_DLL`
remains supported. Neither is needed for normal builds or deployments.
