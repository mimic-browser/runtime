# Mimic releases

Release archives are packaged from a clean, committed `mimic-browser/runtime` revision.
Windows and Linux must use the same binary source revision. The Package release
CI workflow uploads verified archives and receipts. Download both artifacts into
`.build/releases/VERSION/` before publishing. The receipts identify both the
binary source revision and the packaging commit.

## Prepare

Requirements: Go 1.26.4+, a platform C compiler, Python 3.12+, Node.js 22+, and
the Linux fonts documented in [getting started](../../docs/getting-started.md).
Rust/Cargo is a build-time requirement for the native Blitz producer.

```powershell
python tools/release/prepare.py --version v0.1.6
```

Run the equivalent command under Ubuntu 24.04/WSL2 for Linux. The tool builds a
content-addressed, locked native producer first and then builds the public
`./cmd/mimic` command as a stripped standalone binary. It assembles the
allowlisted package, collects third-party notices, and verifies the extracted
package with V8, QuickJS, goja, Playwright, Puppeteer, and the concurrency
example.

The release entry point is deliberately the single command above. Do not package
`tools/runmimic`: it is a source-checkout bootstrap that may invoke Cargo. Do not
invoke `go build` directly for a clean release checkout without first running
`go run ./tools/buildnative`; the release tool performs both operations in the
required order. The resulting `mimic`/`mimic.exe` contains the statically linked
native producer and needs neither Go, Rust nor Cargo on an end-user machine.

For a local unpackaged release-equivalent build:

```powershell
go run ./tools/buildnative
go build -trimpath -ldflags="-s -w" -o .build/mimic.exe ./cmd/mimic
```

On Linux, use `.build/mimic` as the output path. This binary is the same command
the release packager builds; `go run ./tools/runmimic` is only the convenient
clean-checkout development path.

## Version shown by the binary

The startup banner derives its version from Go build information through
`runtime/debug.ReadBuildInfo`. Do not add a second version constant, edit source
files for a release, or inject a display version with `-ldflags -X`.

- A semantic module version is displayed unchanged, for example `v0.1.5`.
- A repository build displays `dev+<short-revision>` from the embedded
  `vcs.revision` and adds `-dirty` when Go records modified sources.
- A Go pseudo-version such as `v0.0.0-20260920165425-17f508b820af` describes a
  development build and is normalized to `dev+17f508b8`; it must never be shown
  as the Mimic release version.

The `--version` passed to `prepare.py` names and validates release artifacts; it
does not create another runtime version source. Before publishing, build from
the intended clean commit and verify the banner provenance against that commit.

Outputs are written to `.build/releases/VERSION/`. Reusing an existing
version/platform output directory is rejected so stale artifacts cannot be
mistaken for a fresh build.

The hosted Package release workflow requires a successful build workflow run
ID and its full source SHA for both platform jobs. It reuses the validated
executables unchanged instead of rebuilding native libraries and Mimic. The packager
downloads that run's executable artifact, verifies its embedded VCS revision,
then checks the extracted archive and public examples. The release commit may
add notes or packaging changes; it must not alter the binary. For a local run:

```powershell
python tools/release/prepare.py --version v0.1.9 --ci-run 36577485575 --binary-source-revision 6224fecf2444c6b9ba58043f97782e2ec007d350
```

## Publish

Push the exact release commit to `mimic-browser/runtime`, then run:

```powershell
python tools/release/publish.py --version v0.1.6
```

Publication requires verified Windows and Linux receipts for the current
packaging commit and, when reusing binaries, one successful CI run for their
shared source revision.
It creates a draft release, uploads the archives, manifest, and checksums,
downloads every asset to verify its hash, and only then publishes the release.
The receipts and archive hashes are authoritative; the publisher does not wait
for CI. Use only artifacts from the successful run named in both receipts.

## Publish the website

Updating the separate `moreveal/mimic-overview` website is a required part of
every release. After publishing the GitHub Release:

1. Update every displayed version and version-specific download link in the
   website repository.
2. Copy the release's `RELEASE_NOTES.md` to the website repository unchanged.
   The website changelog and the GitHub Release body must have exactly the same
   source text; do not maintain a shortened or rewritten website variant.
3. Run `npm run build` and `npm run test:links` in `mimic-overview`.
4. Commit and push `main`, then verify that the GitHub Pages deployment succeeds
   and that the live changelog shows the new release.

A release is not complete until both the GitHub Release and the public website
are published and verified.
