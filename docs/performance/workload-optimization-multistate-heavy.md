# Multi-state Optimize and acquisition-heavy evaluation

2026-10-06, Windows/V8. This is a local, unpublished implementation checkpoint.
The evidence supports precise acquisition specialization, but does **not** yet
support a broad claim of reliable savings on changing heavy live sites.

## Implementation delivered

Optimize defaults to its own `127.0.0.1:0` listener. An existing normal Mimic on
9222 can remain running. The external command receives `MIMIC_CDP_URL`; ordinary
Mimic still defaults to 9222. Explicit fixed endpoints are available, with an
occupied-port diagnostic. A command that never connects to the owned trial is
UNSUPPORTED even if it exits successfully. Optimize never attaches to the other
listener. Focused integration coverage occupies 9222 throughout optimization and
checks that it remains available afterwards.

`--states-file` supplies 1–32 named environment-input states to the same external
command. Each has independent captured evidence, fresh browser state, baseline
replays and result comparison. Inputs belong to the client process, not browser
configuration. Candidates must pass every state; installed finalists must not
regress acquisition in any state. Repeated explicit `--capture` arguments map to
state order. Named artifacts/cache reuse and binary profiles remain managed.

Terminal output uses the existing Mimic brand, restrained progress/status colors,
NO_COLOR/plain redirected output, actionable documentation URLs and a per-state
acquisition table. Inspection describes resource matches rather than exposing
blank internal selectors. No language SDK or custom assertion API was added.

## React: three independently asserted states

The unchanged generic CDP contract in `tools/workload/react_states_client.js`
reads the selected article, verifies its heading and section content, exercises
author theme handlers in both directions, and follows a real section anchor.
States are Quick Start, Adding Interactivity and Managing State. DOM activation
is used; this is not a claim about rendered pointer actionability.

| State | Manual encoded bodies | Installed Auto | Saving | Responses Manual/Auto | Correctness Manual/Auto |
| --- | ---: | ---: | ---: | ---: | --- |
| Quick Start | 556,903 | 526,843 | 5.4% | 20 / 18 | 5/5 / 5/5 |
| Adding Interactivity | 558,985 | 528,925 | 5.4% | 20 / 18 | 5/5 / 5/5 |
| Managing State | 557,776 | 527,716 | 5.4% | 20 / 18 | 5/5 / 5/5 |

Strong Manual already removes visual assets, speculation, analytics, unused route
prefetch, legacy polyfills and the unrelated editor service. That service's
absence activates two first-party fallback chunks. Auto removes chunks 834 and
921 together: 30,060 encoded bytes and two classic executions per state. Both
are separate fully covered experiments, not accepted replay misses.

The initial full run preserved three default captures plus three manual branch
captures, validated 9/9 baseline replays and searched 18 candidates for 300.1 s;
total training/validation was 610.4 s. Live training acquired 5,681,885 encoded
bytes. At 30,060 saved per workload execution, acquisition preparation breaks
even after approximately 190 runs; this excludes CPU/time/disk training costs.
The final current executable reused that environment entirely offline: 4 search
candidates, 63.4 s search, 267.8 s training/validation and another 15/15 installed
Auto passes. These are distinct runs, not a substituted shorter training claim.

Current per-state median Manual/Auto time (ms), CPU (ms), sampled peak RSS (bytes):

- Quick Start: time 3020.9/3097.7; CPU 3625.0/3671.9; RSS 237305856/233725952.
- Adding Interactivity: time 4887.8/4907.1; CPU 5890.6/5578.1; RSS 263454720/258625536.
- Managing State: time 5229.3/5117.7; CPU 6125.0/6015.6; RSS 267325440/263405568.

No consistent latency or CPU benefit is claimed. Default live workloads passed,
but volatile telemetry/ambiguous visual responses leave Default local replay
uncovered; its matched metrics are unavailable. Mixture medians must not hide
individual input regressions. The earlier one-state live React acquisition win
is retained in the previous report; it is not a new three-state live validation.
No held-out fourth state or future-build generalization is claimed.

## GitLab: a meaningful local win, a failed live generalization gate

The public GitLab project workload asserts the project heading and repository
content, and opens/closes the hydrated Code navigation using its author handler.
It does not claim repository-tree or README extraction. Assertions were fixed
before search. Strong Manual removes visuals, speculation, telemetry, GraphQL
not required by these assertions and obviously named tracker, Sentry and project
widget bundles. An earlier 32% result against a weaker Manual is **not** the
headline: the baseline was strengthened and the comparison rerun.

| Metric | Default matched | Strong Manual | Installed Auto |
| --- | --- | ---: | ---: |
| Encoded HTTP bodies | unavailable | 1,110,148 | 1,013,774 |
| Acquired responses | unavailable | 51 | 50 |
| Classic executions | unavailable | 40 | 39 |
| Median workload time | unavailable | 3,288.0 ms | 3,315.3 ms |
| Browser CPU | unavailable | 4,656.3 ms | 4,765.6 ms |
| Sampled peak RSS | unavailable | 338,837,504 | 338,358,272 |
| Correctness | unsupported replay | 5/5 | 5/5 |

Auto saves 96,374 encoded bytes, **8.68% beyond competent Manual**. It removes a
non-obvious shared activity/analytics chunk (84,739 bytes), and acquires only
headers for the repository-tree and README API responses (6,870 and 4,765 body
bytes). Required navigation still passes. Header-only acquisition is distinct
from fabricating empty successful API data: attempting body consumption fails.
Search cost: 12 trials, 180.1 s; training/validation: 236.6 s. No timing/CPU win
is claimed. A different additional shared chunk remains unremoved under this
budget; this is not proof of a global minimum.

A separate live Manual/installed-Auto pair both passed, but Auto's document guard
rejected the new HTML and used general Mimic: Manual acquired 1,109,641 bytes,
Auto 1,479,165 bytes. **The live profile did not preserve its acquisition win.**
The default, Manual and Auto document bodies have the same 65,853 decoded bytes
but distinct hashes. Offline structural comparison found changed script nonces,
metadata, sidebar/breadcrumb attributes, image addresses, two inline script
blocks and numerical text. Masking nonce/CSRF values alone still leaves different
hashes. This cannot safely be fixed by treating all differences as irrelevant.
General fallback is honest; it is not rollback of previously skipped effects.

## Supabase: optimizer expressiveness and admission remain limiting

The default live database documentation workload passes content assertions and
the author Connecting-to-your-database accordion round trip. Its default capture
contains ambiguous visual requests. A separate strong-Manual capture is covered
and preserves 1,273,820 acquired encoded bytes. Changing speculation changes the
cookie/header chain, so these environments cannot be blindly unioned.

Local search finds an unnecessary first-party chunk while retaining the asserted
interaction. But final **installed-profile** validation is UNSUPPORTED: an unseen
Sentry POST body falls outside the generated request guard, takes the general
path and is absent from the capture. The workload itself exits successfully;
that does not turn uncovered replay into PASS. No profile is installed and no
validated saving is claimed. Search used 15 trials and 240.0 s. Historical
failed-run TrainingMs is unavailable (stored as zero), not zero training cost.

This is category (3), an enforcement/admission limitation, not evidence that all
remaining bytes are necessary. Raw search host exclusions and request-body-guarded
profiles can behave differently on volatile telemetry. A manual reference is a
benchmark, not automatically an unconditional user constraint. Silently promoting
all reference rules into permanent constraints or weakening POST identity would
change the correctness/safety contract. Final guarded validation catches the gap.

## Corpus selection and negative evidence

Six additional public pages were inventoried once with preserved binary captures:
Supabase, Next.js, Discourse, GitLab, shadcn/ui and React Native. Inventory is not
a correctness workload. Nonvisual inventory bodies were approximately 1.16 MB,
0.87 MB, 2.07 MB, 1.16 MB, 2.75 MB and 0.85 MB respectively; these are exploratory,
not matched measurements. Discourse's majority was speculation, not a strong
post-Manual workload.

The asserted default scenarios for Next.js, React Native, Discourse and shadcn/ui
failed required hydration/theme/search behavior. They are ineligible for judged
specialization; neither assertions nor browser behavior was patched for a win.
An initial shadcn scenario used a nonexistent main container; a separately scoped
DOM command/theme scenario still failed its required theme interaction and was
not optimized. We do not infer exact browser semantic causes from these failures.
Books, Vue and RealWorld ties from the prior checkpoint remain negative controls.

## Remaining acquisition: required, removable, model-limited

- React content/application resources survive bounded search and support the
  exercised handlers; two fallback chunks are removable across three states.
  Survival is experimental evidence, not an exhaustive necessity proof.
- GitLab hydrated core/shared vendor code remains; the shared analytics chunk and
  two unused response bodies are removable with current execution actions.
  Future HTML admission, and additional chunks left under budget, are limits.
- Supabase's required article/accordion code remains, and search identifies a
  removable chunk; exact volatile-request admission prevents a validated reusable
  final plan. This is a model limitation.
- Vue's static sponsor import and RealWorld's comments resolver remain coupled
  to required initialization despite unrelated business outputs. Current whole
  module/request decisions cannot eliminate that coupling. No fabricated results,
  arbitrary function pruning or instruction-level taint was added.

The practical next engineering gate is robust evidence/admission for changing
heavy sites and consistent guarded treatment of manual constraints/generated
volatile exclusions, with held-out states. It is not more CLI polish or a claim
that resource elimination has reached true minima. Arbitrary normalization of
HTML, cookies or request bodies would invalidate the result.

## Reproduction and evidence

Use the checked-in clients, `react-states.json`, `manual/react-article.json` and
`manual/gitlab-navigation.json`. Native Optimize owns fresh instances and uses
local trials. Captures, logs and security-bearing HTML remain private under
`.build`; public numeric evidence contains no cookies or token values.

The [sanitized numeric artifact](workload-optimization-multistate-heavy-results.json)
records exact executable hashes, per-state medians, trial counts and evidence
paths. Initial training, current-build replay and GitLab use their corresponding
preserved executable; profiles are not rebound to a different binary. Measurements
use five repetitions, normal final instrumentation, encoded transport body reads,
process-tree CPU and sampled RSS. Local wall time is not a live latency forecast.

Recommendation: ship the workflow as explicitly empirical specialization with
honest unsupported/fallback diagnostics. Multi-state React and acquisition-heavy
GitLab justify continued investment. Reliable heavy-site live savings and
held-out generalization are still open release/marketing gates. No public
publication or deployment occurred.

## Pre-commit validation, 2026-10-06

A fresh Windows executable built successfully. Focused `internal/workload`,
`internal/optimize`, `internal/cliui` and `internal/network` tests passed.
The native optimizer integration tests were rerun with
`MIMIC_OPTIMIZE_TEST_BINARY` pointing to that executable, including coexistence
with an occupied 9222 listener, multi-state orchestration, external oracle
failures/timeouts and process cleanup. Browser workload/profile/script/body
admission and geometry regression tests, demanded-module-after-denied-preload,
and DOM streaming tests passed. The historical Python suite passed its two
unit tests; seven opt-in integration cases were skipped. No full suite or new
live-site benchmark was run. Linux and broad compatibility remain CI gates.
These checks do not strengthen the live-generalization claims above.
