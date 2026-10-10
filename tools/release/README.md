# Mimic releases

Release archives are packaged from a clean, committed `mimic-browser/runtime` revision.
Windows and Linux must use the same binary source revision. The Package release
CI workflow uploads verified archives and receipts. Download both artifacts into
`.build/releases/VERSION/` before publishing. The receipts identify both the
binary source revision and the packaging commit.

## Release version lifecycle

Choose one intended version from the last **published release**, not from failed
candidate tags. A CI failure does not constitute a release and must not consume
another patch version. Iterate on candidate commits without creating intermediate
semantic version tags.

Finish the code and required CI checks on its exact source revision first. Only
after those checks are green, create and push the single intended release tag.
Run the tag build, require it to pass, and package its verified Windows and Linux
binaries unchanged. Publish only after package verification succeeds. A failed
tag check blocks publication; it is not a reason to create the next release
version automatically.

The tag must exist before the release binaries are built so Go embeds the correct
version. This requirement does not justify tagging unfinished candidate code.
Never move a published release tag or relabel an existing binary with a different
version. Release documentation and the website must identify the one version
actually published.

## Prepare

Requirements: Go 1.26.4+, a platform C compiler, Python 3.12+, Node.js 22+, and
the Linux fonts documented in [getting started](../../docs/getting-started.md).
Rust/Cargo is a build-time requirement for the native Blitz producer.

```powershell
git tag v0.1.6
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

Create the release tag on the intended clean binary source commit **before
building**. Go derives the semantic module version from that existing tag;
creating a GitHub Release afterward cannot change an already-built executable.
The `--version` passed to `prepare.py` selects and validates that tag and names
the artifacts; it does not create another runtime version source. The packager
rejects binaries whose embedded module version, VCS revision, or clean status
does not match the release. It repeats the check on the extracted executable.

Outputs are written to `.build/releases/VERSION/`. Reusing an existing
version/platform output directory is rejected so stale artifacts cannot be
mistaken for a fresh build.

Push the commit and release tag together. The Windows and Linux build workflow
runs on release tags and fetches full history, including tags, before compiling.
Use its successful **tag build**, not an earlier main-branch artifact. The hosted
Package release workflow checks out the requested tag and requires a successful
build workflow run ID and its full source SHA for both platform jobs. It reuses the validated
executables unchanged instead of rebuilding native libraries and Mimic. The packager
downloads that run's executable artifact, verifies its embedded VCS revision,
then checks the extracted archive and public examples. The release commit may
add notes or packaging changes; it must not alter the binary. For a local run:

```powershell
python tools/release/prepare.py --version v0.1.9 --ci-run TAG_BUILD_RUN_ID --binary-source-revision TAG_COMMIT_SHA
```

If documentation or packaging changes follow the tagged binary build, pass the
exact committed `packaging_revision` to the Package release workflow. It keeps
`binary_source_revision` bound to the successful tag build and records both
revisions in the receipts; it does not rebuild or relabel the executable.

## Publish

Push the exact release commit and its tag to `mimic-browser/runtime`, then run:

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

To replace an existing release deliberately, rebuild both platform packages
from the corrected release tag and run:

```powershell
python tools/release/publish.py --version VERSION --replace-existing
```

The publisher verifies both receipts, the local and remote
tag, and the exact asset set before replacing anything. It preserves the previous
release's assets under `previous-release/`, replaces every archive, the manifest,
and checksums, then downloads and verifies all published bytes. It keeps the
existing release rather than deleting it. Update the website's release notes and
cached asset metadata after replacement as well.

## Publish the website

Updating the separate `mimic-browser/website` repository is a required part of
every release. After publishing the GitHub Release:

1. Run `npm run release:sync -- --version VERSION` in the website repository,
   replacing `VERSION` with the exact published tag. The command verifies the
   release manifest and checksums before atomically updating
   `public/release-cache.json`.
2. The shared release snapshot supplies the current version, download links,
   documentation links and release body. Do not edit those projections separately
   or rewrite the release notes for the website.
3. Run `npm run build` and `npm run test:links` in `mimic-overview`.
4. Commit and push `main`, then verify that the GitHub Pages deployment succeeds
   and that the live changelog shows the new release.

A release is not complete until both the GitHub Release and the public website
are published and verified.

## SDK compatibility evidence

`.github/workflows/qualify-sdk.yml` listens for a published runtime release. It
verifies the public manifest, checksum list, Linux archive and archived executable
before requesting `runtime-released` qualification in `mimic-browser/sdk`. This
only refreshes compatibility evidence; SDK versions, default pins and package
publication remain independent explicit decisions.

Configure the runtime repository's `SDK_QUALIFICATION_TOKEN` with a GitHub App
installation token or fine-grained token that has Contents write permission only
on `mimic-browser/sdk`, as required by GitHub's repository dispatch API. The SDK
listener must exist on its default branch. Without this secret the workflow
retains a verified `not-configured` receipt and emits a warning; automation is
not active until repository setup is complete. A manual dispatch with an exact
published tag can retry qualification without replacing any release.

The helper is read-only unless `--execute` is explicitly supplied in official
runtime CI.

## Replacing the public benchmark

Replacing the public benchmark means updating every current presentation together: runtime README, benchmark entry point, performance summary, website home and comparison pages, charts/images, downloads and methodology links. Present only the latest measured checkpoint; remove previous checkpoint links and stale speed/CPU claims from current surfaces. A memory-only update publishes memory results only. Preserve frozen historical evidence and exact reference provenance in the current methodology; do not present archived results as current claims.
