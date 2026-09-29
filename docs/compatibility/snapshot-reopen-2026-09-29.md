# Snapshot stability P0 reopened, 2026-09-29

The [2026-09-11 stability disposition](snapshot-stability-debt-2026-09-11.md)
requires one fresh native crash to reopen P0. The Windows CI job for release
packaging revision `57550e80c6d315ea70d422dde397191602f94e3e` crashed
while V8 serialized a bootstrap snapshot. This is a new observed signature;
its root cause is not yet known.

## Preserved evidence

- Workflow run: [36583063089, attempt 2, Windows shard 2](https://github.com/moreveal/mimic/actions/runs/36583063089/job/109464199831).
- The job log is retained locally at
  `.build/ci/native-crash-36583063089/job.log`, SHA256
  `c163ff1cec7345d978e2128b44a4557e7fa36fa13b3f66f4297b979a4b4b8ad7`.
  CI did not upload a native dump or module map.
- The test process reported `Unknown external reference 00007FFFB613CD90`,
  Windows exception `0x80000003`, and exited with code 1. Its Go stack enters
  `gov8.(*SnapshotCreator).CreateBlob` from `buildBootstrapSnapshot` line 250.
- The failing browser batch used `go test -json -parallel 4 -timeout 10m`
  through `tools/ci/windows_tests.py`. Several browser tests ran concurrently;
  the log does not identify which Page or seed owned the failed builder.
- The release binary artifacts were built at clean source revision
  `6224fecf2444c6b9ba58043f97782e2ec007d350` in source CI run
  `36577485575`. Windows `mimic.exe` SHA256 is
  `8daa3ea5bd13a9417a7295c78ea4a7170453c0faca2d624ba3f156099c183cec`.
  The crash came from a CI test executable rebuilt at packaging revision,
  whose exact SHA256 was not retained by CI. The source differences between
  these revisions are release tooling and notes, not the V8 implementation.
- Bundled Windows shim archive
  `third_party/gov8/internal/prebuilt/windows_amd64/gov8_shim.dll.gz` has
  SHA256 `5d4bebe39baf6104476989983445686bc78630e8bdd189a935895c7d6da466d5`.
  The loaded DLL identity and host memory conditions were not captured.

`v0.1.9` was returned to draft status and the public overview site was reverted
to `v0.1.8`. A succeeding retry would not establish that this native failure is
fixed. Next investigation must identify the unknown callback address and
snapshot builder topology, then compare unchanged focused load on the prior
and changed builds without disabling snapshots or globally serializing Pages.

## Address-keyed cleanup race and repair

Local Windows snapshot diagnostics place the property-observation factory
callback first in the external-reference table. Its address ended in `cd90`,
matching the unknown address's lower 16 bits. This strongly suggests the
snapshot creator lost a live table; without the CI process's module map it is
not a definitive symbol identification.

Review found two address-keyed cleanup windows. `SnapshotCreator`'s native
teardown already drops its table before returning, but Go teardown repeated
the drop after the isolate could be recycled. Ordinary isolate disposal also
freed the isolate before a separate Go call dropped its table. A concurrent
new isolate could reuse the address in either window and lose its own table.
The repair removes the redundant creator cleanup and performs ordinary
isolate table cleanup in the same native call as disposal. It does not
serialize independent Pages or disable snapshots.

The repaired Windows and Linux shims were rebuilt from the project's pinned
patched V8 archives (`07896581257a5880946602eba27dfb2677b114e093de2cc98c116c6416755b02`
and `b05abd203317307e55de5b18c006b04c760302182a2ccd1de780430c2c752615`).
Focused bootstrap V8 tests passed on both platforms. Three repetitions of the
concurrent browser snapshot lifecycle and barrier builder subset passed on
both platforms. Broader CI and release verification remain required before
closing this P0 or republishing `v0.1.9`.
