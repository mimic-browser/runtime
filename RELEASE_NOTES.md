# Mimic v0.2.0

Changes since v0.1.9:

## Workload optimization

- Run `mimic optimize --name shop -- <command>` to learn which network acquisitions an ordinary CDP workload can avoid. The external command and its own assertions define success; no Mimic-specific SDK or assertion API is required.
- Optimize owns an isolated endpoint supplied through `MIMIC_CDP_URL`, records the browser network environment to disk, validates local replay and searches resource combinations without live-network fallback during trials.
- Save named or exported workload profiles and apply them with `mimic --profile shop`. Multi-state training checks the same plan against every supplied workload state.
- Generated resource decisions share the existing ResourcePolicy enforcement. Search can avoid individual requests, acquire headers without unused bodies, and suppress validated external classic-script execution.
- Live profile admission is scoped to recorded document routes and request inputs, rather than byte-identical HTML. Unknown requests use normal Mimic; changed script sources execute normally. Profiles remain empirical: keep assertions in live workloads and retrain when relevant states change. Profiles are tied to their validating executable.
- Captures preserve intentionally policy-limited response bodies. Replay requiring missing bytes is unsupported, never a fabricated successful response or a live fetch.

## Runtime and diagnostics

- Preserve parser insertion tails across nested document.write suspension and allow demanded modules after denied speculative preloads.
- Add resource acquisition and script execution measurements, bounded search, process cleanup, replay diagnostics and color-aware terminal reporting.

## Evidence and limitations

The unchanged guest GitLab content/menu workload passed three independent live runs with a trained profile, acquiring 10.79% fewer encoded HTTP body bytes than a strong manual policy. Request count, CPU and latency did not improve. This is one workload, not a general performance guarantee. See the [full method and results](https://github.com/mimic-browser/runtime/blob/v0.2.0/docs/performance/workload-optimization-adaptive-live.md).

Mimic remains a public beta for Windows and Linux amd64. Optimize does not prove future site correctness, provide universal browser replay or roll back skipped effects. Capture artifacts may contain credentials and personal data. See [Optimize](https://mimic.boo/docs/optimize/) and [safety limits](https://mimic.boo/docs/optimize/safety/).
