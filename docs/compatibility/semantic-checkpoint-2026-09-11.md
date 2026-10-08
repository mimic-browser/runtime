# Semantic cleanup checkpoint, 2026-09-11

This checkpoint supersedes the **current-work status**, not the historical
measurements, in [platform bindings](platform-bindings-2026-09-11.md).
Baseline is clean `d889379e6dc4a341e22de1f20a1c3c78a407377e` on
`codex/compat-cleanup-20260911`. The integrated implementation is clean
`b5b62e63f1fbb278d8931e7bbc797303e42be32c`. No foreign work was staged.

The reference remains frozen Chrome **152.0.7977.82**, Windows x64, headful,
reused controlled profile. Only loopback fixtures were opened. Exact CDP metadata,
source/probe hashes, before/after binary hashes and test binary hashes are in
[the receipt directory](semantic-checkpoint-20260911/identity.json).
The clean implementation binary SHA256 is
`6d7f60bf18964a2e2b11190cae629801117cfd955d98e8580decf7d0ec8166ea`.
Raw observations and merged stdout/stderr are retained in ignored
`compatibility/private-captures/semantic-checkpoint-20260911/`.

## Implemented package

| ID | Root cause | Repro | Chrome / old Mimic | Priority | Status | Commit | Limits |
| --- | --- | --- | --- | --- | --- | --- | --- |
| HISTORY-BINDING | Missing private receiver validation and WebIDL conversion before owner dispatch | history_storage_binding_oracle.js | Invalid receiver throws without conversions / accepted; title conversion required / omitted | P1 | Fixed measured operations | eb4bd51 | Foreign object structured cloning is separate |
| STORAGE-BINDING | Methods used the caller realm and an unvalidated area lookup | same oracle | Invalid receiver/arity/Symbol throw; borrowed methods use owner's storage / inconsistent conversion and area | P1 | Fixed methods and length ownership | eb4bd51 | Storage named-property exotic is not implemented here |
| FORM-NAME | Missing name attribute reflection | form_name_oracle.js | Assignment updates attribute / expando only | P2 | Fixed through existing string reflection machinery | 3d19e07 | Shared reflection helper conversion/brand coverage remains incomplete |

The attempted WindowProperties layer was withdrawn in `b5b62e6`: placing a
JS Proxy in the native Window prototype chain made an undeclared global
identifier return undefined instead of throwing ReferenceError. The full suite
caught this despite a passing named-access oracle. The retained local oracle
and [withdrawal receipt](semantic-checkpoint-20260911/withdrawn-window-candidate.json)
are evidence for a future native interceptor implementation, not an accepted
compatibility gain. The native global proxy and its prototype behavior are
preserved in the final implementation.

## Comparison

These are separate, overlapping corpora; their counts must not be added.
All normalized observation paths were compared, including common and extra array
indices. Fuzzer first-difference signatures are used only for observational
grouping, not as proof of root causes.

| Fixed corpus | Matches before / after | Differing records before / after |
| --- | --- | --- |
| Original 236 probes | 210 / 210 | 88 / 88 |
| Original 28 representatives | 24 / 24 | 10 / 10 |
| Brand matrix | 0 / 0 | 495 / 493 |
| State relations | 0 / 0 | 2 / 2 |
| General 15 cases | 12 / 12 | 40 / 40 |

New discovery is separate: History/Storage **62 to 0** differing records;
form name reflection **4 to 0**. Both focused Chrome controls have zero drift.
The general Chrome-to-Chrome control has 15 complete valid cases and zero
differences. The fixed replay has no Chrome drift or new differing paths.
The remaining fixed observations comprise four observational groups, not four
proven bugs. Window/Document named access still differs; the withdrawn candidate is excluded
from the final before/after. No fresh broad discovery/sweep was run.

## Validation

- Final full browser and WebAPI/V8/Goja/CDP/network/scheduler/Chrome packages:
  PASS, command 402.310 s, 1370 test/subtest pass events; 18 skip events include
  opt-in scenarios and packages with no tests. Unchanged package cache hits are
  explicitly listed in the receipt; browser ran freshly (389.140 s test output).
- Targeted browser race: PASS, 56.459 s command, 90 pass events, zero skips.
  This is not a full browser race. Core-package race: PASS, 10.376 s command,
  206 pass events, two skips; some unchanged packages use Go's test cache.
- Fresh snapshot stress: browser PASS, three repetitions, 126 pass events,
  48.231 s command; V8 PASS, three repetitions, 42 pass events, 1.953 s command.
  Neither matrix skips a selected test; no native crash or race warning appears.
- Python: 23 tests pass, six opt-in tests skipped. Detailed skip names, commands,
  hashes and failures of the withdrawn candidate are preserved in the receipt.
  No new performance gate was run.

Candidate validation failures include reduced-catalog installation and missing
global ReferenceError semantics; their complete failures and logs are retained.
The final full suite reruns after withdrawing that candidate. A first diagnostic
used unsupported cross-realm setPrototypeOf and failed before completion; the
final oracle tests local prototype detachment and owner dispatch separately.
No frozen expectation was
weakened. A repeat differential initially reused an existing output directory
and failed before capture; fresh final output directories were used. These
harness failures are retained, not counted as semantic improvements.

## Stopping boundary and remaining work

This closes the reviewed implementation package. It is not a claim that all
reasonable browser capabilities or all semantic defects are finished. The next
items exceed a small safe patch or need new controlled evidence:

- **Implementation requiring native integration:** WindowProperties named access
  must preserve unresolved-identifier semantics and snapshot restoration.
  **Incomplete implementation:** Document named access, including legacy
  override-builtins, own-key ordering, shadowing, inert documents and lifetime.
  A getter-per-name patch or a full DOM scan on every Document access would not
  be a satisfactory implementation. The remaining representative identifies
  both missing projections. The withdrawn Proxy shortcut is not a solution.
- **Incomplete implementation:** Document.hasFocus needs actual Page/focus and
  lifecycle state. Its 24 divergent probes do not justify a constant boolean.
- **Candidate, not diagnosed root cause:** remaining surface.window projection;
  cross-realm History object cloning diagnostic. Primitive borrowed History
  dispatch passing does not establish foreign object serialization.
- **Architecture limits:** retained native closure/global-proxy retargeting;
  saved-eval native call/apply/bind access gate; strong bridge caches and old DOM
  arena retention. None is claimed fixed by the new child-frame oracle.
- **Environment-dependent or unmeasured:** codec/device/GPU capabilities and
  TLS/H2/H3 behavior; localhost HTTP/1.1 does not establish them.

The remaining stability boundary is documented separately in
[stability debt](snapshot-stability-debt-2026-09-11.md). Performance/concurrency
is in the [optimization backlog](../performance/report.md); unchanged prior
baselines remain authoritative, including incomplete gates. No target-site
outcome or maximal compatibility claim is inferred from passing probes.


## Final Unicode-preservation follow-up

Final diff review caught an accidental encoding change to two existing Intl
fallback formatter literals. `bd0408a` restores those two lines byte-for-byte to
`d889379`; it is not an additional compatibility gain. The production diff now
contains only the intended History/Storage bindings and form name reflection.

A clean executable from `1a6652c` was then checked again. The full browser and
WebAPI run passes (290.332 s command, 1169 pass events, 14 skips). Fresh browser
snapshot stress passes three repetitions (126 pass events, zero skips, 52.150 s)
and fresh V8 snapshot stress passes three repetitions (42 pass events, zero
skips, 1.966 s). The earlier targeted browser/core race results remain recorded;
race was not rerun for this literal restoration.

The 236 fixed probes and 28 representatives were freshly replayed against Chrome
and the final executable, retaining the original d889379 before observations.
All Chrome and after observations are unchanged from the preceding checkpoint.
Fresh brand, relations and 15-case general captures are likewise unchanged at
every compared observation, and the fresh 15-case Chrome control has zero
differences. No invalid/incomplete cases were observed. Exact build/test identities,
commands, logs and comparisons are in
[the final receipt](semantic-checkpoint-20260911/post-unicode-validation.json).
Documentation-only commits after this run do not change the tested implementation.
