# Troubleshooting Optimize

## Baseline fails

The original workload did not pass against fresh Mimic. Check its saved stdout/stderr and assertions before attempting optimization. A connection failure often means the workload uses a different endpoint or another process occupies the selected listener. Optimize does not silently attach to that process.

## Local replay is unstable

The CLI identifies the state and repetition that failed. Inspect the trial directory. Possible causes include state outside the browser, time/randomness, ordering-sensitive requests, or an uncovered request. Do not treat such a run as a passing candidate. Refresh stale managed evidence with `--record` when a new live baseline is appropriate.

## Workload contacted another listener

Optimize normally chooses a private free port. Read `MIMIC_CDP_URL` or map your existing endpoint variable with `--endpoint-env NAME`. It is safe to leave normal Mimic running. A hardcoded `localhost:9222` connects to that other listener, not to the trial; a successful exit cannot validate the trial. If you explicitly select `--listen 127.0.0.1:9222`, stop the listener owning that address first. See [endpoint setup](workloads.md).

## Request outside the capture

This request identity was not covered by the recorded environment. The same URL may have appeared with different headers, request body, Browser Context or occurrence. No live request is made during optimization. This is UNSUPPORTED, even if the client subsequently reports PASS. Exact URL changes can be harmless in live mode but still unsupported for that capture. Only explicitly nonsemantic query keys should be normalized.

## Runtime or artifact incompatibility

Profiles bind to the build that validated them. Re-optimize with the current binary. Corrupt, incomplete or oversized containers are invalid evidence; do not edit their internal metadata to bypass validation.

## Timeout, crash or Mimic failure

Each trial keeps the primary workload status separately from replay validity. Logs and metadata remain in the printed trial directory. Timeout and cancellation terminate the owned workload/browser process trees; external state owned by another service is outside that ownership.

## No advantage over manual configuration

This is a valid result. A well-chosen document-only policy may already minimize acquisition. Check the matched table rather than assuming a generated profile is automatically better.

## Manual activates an uncovered fallback

The reference may legitimately request a branch absent from the original
recording. Managed training can record the reference once before search and add
only new immutable resources while preserving the original environment. If the
existing state changed or a new response is mutable, the capture cannot be
extended safely. Explicit capture inputs remain offline. A table labeled
Manual+repair includes separately validated coverage exclusions and is not a
clean measurement of your original manual rules.
