# Snapshot stability debt, 2026-09-11

This is a current disposition update. It does not rewrite or invalidate the
[historical lifecycle audit](snapshot-lifecycle-audit-2026-09-11.md), its dumps,
or the separately reproduced [numeric allocator failure](snapshot-numeric-allocator-2026-09-11.md).

At implementation revision `b5b62e6`, all executed known snapshot regressions and
opt-in lifecycle stress scenarios pass the bounded matrix below. The unexplained
`SizeFromMap` / broader native crash family remains **stability technical debt**.
Its cause is not established and absence of future crashes is not proved.

## Evidence and scope

The final browser matrix passes three repetitions with **126** test/subtest
pass events and **zero skips**, exit code 0, command duration 48.231 s.
The final V8 matrix passes three repetitions with **42** pass events and **zero
skips**, exit code 0, command duration 1.953 s. Both use explicitly built test
executables, rather than cached test results. The ordinary full browser suite
and targeted browser/core race checks also pass.

The browser topology includes four independent contexts with 32 Page lifecycles
per context, concurrent barrier builders, test-only sequential-builder controls,
ordinary concurrent Pages, cold admission, close/restore and cache lifetime.
The V8 matrix includes concurrent numeric graphs (four builders, eight rounds),
serialization and distinct read-only layouts. The same native V8 test executable
SHA256 is `801a05e948706c8fd5a796abc0c3cc9433fd0f7f5534b2a30ec236cd41cbed2a`.
Exact commands, binary hashes, exit codes, test names/skips and combined
stdout/stderr hashes are retained in the
[validation receipt](semantic-checkpoint-20260911/validation.json) and
[test identity](semantic-checkpoint-20260911/test-binaries.json).
`MIMIC_SNAPSHOT_LIFECYCLE_STRESS=1` enables the final opt-in scenarios; snapshots
remain enabled. No production lock, retry or snapshot disabling was introduced.
Test-only sequential-builder cases are controls, not a production remedy.

The earlier current-HEAD `d889379` browser lifecycle/barrier subset also completed
three repetitions. Its preserved log is separately hashed; it must not be read
as including every V8 numeric scenario covered by the final matrix.

Three historical allocator dumps remain locally available and their SHA256s
were rechecked in [retained evidence](semantic-checkpoint-20260911/retained-evidence.json).
The numeric report retains `ReadOnlySpace::EnsureSpaceForAllocation` with
`feeefeeefeeefeee` and `ReadOnlySpace::AllocateRaw` with `baadf00dbaadf00d` through
`SnapshotCreatorImpl::CreateBlob`. Those reproduced signatures support the
existing `--no-extensible-ro-snapshot` fix. The older `SizeFromMap+4` (RVA
`0x11cb34`) and `StringForwardingTable::GetRawHash` signatures are **not** thereby
proved to have the same cause. Historical raw captures/dumps remain ignored and
have not been added to Git.

The earlier 25-Page fast gates stopped at the host RAM guard before measured
25-Page waves and dedicated teardown-memory waves. They are incomplete
performance workloads, not successful stability stress runs or new native
crashes. This disposition does not convert them to passes.

## Immediate reopen rule

**One fresh native crash is sufficient to reopen P0 immediately.** This includes
a repeated historical signature or a different native failure during snapshot
construction, restore, ordinary runtime use or teardown. An unexpected process
exit or dropped CDP connection requires immediate triage; a harness disconnect
alone must not be assigned a native root cause without process evidence.

On recurrence preserve the exact executable/native library identities, source
revision and dirty state, stderr, exit status, dump/native stack, runtime flags,
Page/context/builder topology and host memory conditions. Compare unchanged load
on the prior and changed builds. Do not retry away the first failure, globally
serialize Pages, or disable snapshots to manufacture a passing result.

Until such evidence recurs, no unbounded absence-of-crash experiment is required.
The independent performance/concurrency work remains an
[optimization backlog](../performance/report.md), without claims beyond its
preserved measurements.


## Final source-preservation recheck

After `bd0408a` restored two original Intl fallback literals, a clean build at
`1a6652c` passed the full browser suite and another three repetitions of both
snapshot matrices: browser 126 pass events, V8 42 pass events, zero selected
skips and exit code zero. The [final receipt](semantic-checkpoint-20260911/post-unicode-validation.json)
records separate binary identities and logs. The stability-debt disposition and
immediate reopen rule remain unchanged; no broader native cause is inferred.
