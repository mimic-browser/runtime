# Mimic

Mimic is a lightweight browser execution runtime focused on reproducing
observable Chrome behavior without embedding Chromium. Frozen Chrome 152 is
the current behavioral reference.

## Core Principles

- Preserve observable Chrome behavior. When behavior is uncertain, measure it
  against frozen Chrome 152 rather than guessing from specifications or other
  browser implementations.

- Preserve each successful frozen Chrome 152 capture and its provenance before
  changing instrumentation or testing another hypothesis. Reuse the saved trace
  and offline replay for iterative analysis; do not repeatedly launch live
  Chrome 152 for the same case. Run it again only when the saved evidence cannot
  answer a specific remaining question, and retain the new capture.

- Before capturing Chrome, read `docs/oracle-policy.md`, including its normal
  browser launch and recording rules. Launch the headful binary directly with
  a fresh dedicated profile and a fixed nonzero CDP port, then attach the
  recorder. Do not use an automation framework's default launch as a normal
  browser control. Verify and record the original `navigator.webdriver` value;
  do not hide launch artifacts with injected property or prototype overrides.
  Preserve exact launch arguments and distinguish passive controls from
  instrumented, intercepted, headless, and network-modified experiments.

- Prefer behavioral compatibility over superficial API coverage. An API that
  exists but behaves incorrectly is not considered implemented.

- Preserve canonical object identity, realm boundaries, lifecycle ordering,
  task/microtask semantics, and one event loop per Page.

- Keep Page state isolated. Independent Pages must be able to execute
  concurrently and must not depend on a global runtime lock or shared mutable
  browser state unless the behavior explicitly requires it.

- Maintain a single authoritative state model. CDP, JavaScript bindings, DOM,
  navigation, networking, and other projections should observe the same
  underlying state rather than maintain independently synchronized copies.

- Mimic does not render images and must not require a GPU or a native graphics
  backend. Model the observations scripts can make, including consistent
  metrics, readbacks, local changes, resets and copies. Do not substitute
  independent random buffers or a fingerprint derived from the entire command
  sequence; repeated and overlapping observations must agree. See
  `docs/architecture.md` for the graphics observation boundary.

- Do not implement behavior specifically for individual websites, tests, or
  benchmarks. Fix the underlying browser semantic or architectural cause.

- Do not add Web APIs merely to increase API counts. Implement capabilities
  when required by real workloads, compatibility evidence, or architectural
  dependencies.

- Prefer the smallest implementation that reproduces the required observable
  behavior. Do not introduce layout, media, device, or other browser
  subsystems unless their observable behavior is actually required.

## Correctness

- Treat compatibility regressions as correctness regressions, even when the
  affected behavior appears obscure.

- When fixing a compatibility issue, add or extend a focused regression test
  whenever practical.

- Do not run the full local test suite. Before changing behavior, run only the
  focused tests needed to establish the baseline; after the change, rerun only
  the focused tests needed to verify it. Let GitHub CI detect broad regressions.

- Fix the shared semantic cause across related APIs and value types. If a
  failure appears through one entry point, check the underlying boundary and
  its other callers before adding a local conversion or workaround.

- Distinguish unsupported behavior from incorrectly implemented behavior.
  Prefer an explicit unsupported boundary over plausible but incorrect browser
  behavior.

- Do not weaken existing tests, frozen harnesses, or reference expectations to
  make an implementation pass.

- Preserve useful failures and diagnostics. Avoid silently swallowing errors
  solely to make workloads continue.

## Architecture

- Prefer structural fixes over accumulating special cases.

- Avoid duplicated state, unnecessary Go↔V8 crossings, hidden global
  synchronization, and ownership that makes Page teardown incomplete.

- Keep boundaries between runtime, DOM, navigation, networking, CDP, and
  platform APIs explicit. Do not bypass ownership boundaries for convenience
  without a demonstrated architectural reason.

- Be conservative with new abstractions. Introduce them when they remove real
  duplication or enforce an invariant, not in anticipation of hypothetical
  future requirements.

## Performance

- Optimize measured architectural overhead rather than benchmark-specific
  behavior. Profile before undertaking substantial performance work.

- Before changing performance-sensitive runtime/DOM/style/geometry/CDP code for
  the Playwright Wikipedia workload, read
  `docs/performance/wikipedia-e2e-current.md`. It is the canonical record of
  disproved hypotheses, destructive experiments, and the current E2E gate.

- Use `tools/performance/fast_gate.py` for iterative performance validation
  when appropriate. Reserve the full benchmark matrix for substantial
  optimization batches, regression investigation, and reportable checkpoints.

- Evaluate latency, throughput, memory usage, allocation pressure, concurrency,
  and retained state after teardown together.

- Performance changes must preserve correctness and observable semantics. Do
  not add APIs, weaken compatibility, or introduce workload-specific shortcuts
  solely to improve benchmark results.

- Do not modify frozen performance harnesses, workloads, or original baseline
  data. For reportable comparisons, ensure the executed binary corresponds to
  the intended fresh build.

- Keep significant validated results, remaining bottlenecks and known limitations
  in `docs/performance/report.md`. It is a current technical summary, not a daily
  activity log. Retain intermediate trials and investigations privately.

## Changes

- Choose one release version from the last published release. Fix candidate CI
  failures with commits, not additional release versions or intermediate semantic
  version tags. Complete the required CI checks on the final source revision
  before creating its release tag. Then validate that tag's CI build and package
  its verified binaries before publication. Do not move published release tags
  or relabel binaries built for another version. See `tools/release/README.md`.

- Public repositories, websites, documentation and release notes present the
  project and its current user-facing behavior. Never publish private issue
  tracker identifiers, links, exported tasks, internal task relationships or
  project-management metadata.

- Do not publish daily work diaries, personal browsing activity, agent progress
  reports, prompt histories or task-completion checklists. Explain usage,
  supported behavior, material limitations and significant shipped changes.
  Contributor documentation may explain technical mechanisms and reproducible
  validation without narrating private work sessions.

- Preserve detailed local investigations and their original evidence in ignored
  local storage or private CI artifacts. Keep public benchmark methodology,
  frozen behavioral evidence and compatibility limitations accurate; retaining
  evidence does not require publishing internal work logs. Review documentation,
  filenames, copied release text and generated outputs before publication.

- When adding, removing, or changing CDP handlers, update
  `internal/cdp/protocol_support.json` in the same change with the actual support
  scope, limitations, and focused test evidence. This is the authoritative
  semantic support registry; generated wire schemas do not imply implementation.
  Regenerate its projections with `python tools/generate_cdp.py`, then run
  `python tools/generate_cdp.py --check` and the focused CDP coverage tests.
  See `docs/cdp-protocol-generation.md`. Do not edit generated support tables
  independently or omit the registry update because a handler test passes.

- Write all public documentation and other public-facing project materials in
  English. Keep technical identifiers, quoted evidence, and external titles in
  their original form when changing them would lose meaning.

- Expose one current public contract per mechanism. Do not add API contract
  versions, parallel legacy handlers, or compatibility modes for older Mimic
  releases. Clients inspect the Mimic release version and adapt. This applies
  to CDP extensions, profiles, resource policies, and other public mechanisms.
  Artifact provenance and installed Chrome bundle identities are data, not
  selectors for an older API implementation.

- Environment profile JSON is only a CDP import/export format, not a CLI
  configuration file or an arbitrary createContext override document. Generated
  and imported profiles must pass the same coherence checks.

- Keep changes scoped to the problem being solved. Avoid unrelated refactors
  unless they are necessary for correctness or materially simplify the fix.

- Keep human-maintained JavaScript and inline JavaScript readable and
  formatter-compatible. Do not minify authored source code or collapse it into
  dense one-line expressions; run the repository formatter after editing it.
  Use `npx --yes prettier@3.6.2 --write <files>` for JavaScript and HTML.
  Do not reformat vendored, generated, frozen-reference, capture, or oracle
  artifacts unless the task explicitly requires regenerating those artifacts.

- Inspect and understand the existing implementation before replacing it.
  Preserve intentional invariants unless evidence shows they are wrong.

- Keep substantial changes reviewable and logically separated.

- Document non-obvious architectural decisions and important limitations close
  to the code or in the relevant project documentation.

- Keep experimental rewrites, including Rust spikes, separate from the
  production implementation until they have been evaluated against equivalent
  correctness and workload requirements.
