# Request-scoped Optimize: independent live GitLab validation

2026-10-06, Windows/V8, headless environment, Playwright Core 1.58.2.
This is a guest, read-only scenario on https://gitlab.com/gitlab-org/gitlab.
No account, upload, purchase or other write action was involved.

## Contract and method

The existing `tools/workload/heavy_dynamic_client.js gitlab` was unchanged.
It asserts the GitLab heading, activates the Code button through the DOM,
waits for its author-controlled expanded state, checks Repository content,
closes the menu and verifies the collapsed state. This establishes this content
and hydrated-menu scenario, not file uploads, checkout, rendered pointer
interaction or arbitrary GitLab functionality.

A freshly built executable recorded one live default baseline. Its workload
passed (1,480,238 encoded body bytes, 66 acquired responses), but repeated avatar
responses made unconfigured replay unsupported. Explicit background exclusions
were tested locally; baseline replay then passed 3/3. The strong checked-in
`manual/gitlab-navigation.json` was used without weakening it. No live fallback
was allowed during candidate search. Eighteen candidates took 240.1 seconds;
training and final validation took 297.1 seconds. The capture, individual trial
logs and profile remain private under `.build/adaptive-gitlab-20261006`.

## Local matched finalists

| Metric | Strong Manual | Installed Auto |
| --- | ---: | ---: |
| Encoded HTTP body bytes | 1,109,441 | 979,837 |
| Acquired responses | 51 | 48 |
| Workload time, median | 3.58 s | 3.60 s |
| Browser CPU, median | 4.95 s | 5.36 s |
| Classic executions | 40 | 37 |
| Workload and covered replay | 3/3 PASS | 3/3 PASS |

The selected plan avoids three shared JS chunks and acquires only headers for
repository-tree and README API responses. The scenario does not consume these
bodies. Required resources removed by rejected candidates caused the unchanged
Code-button wait to fail. Full Default matched replay remains unavailable and
must not be presented as a passing matched baseline.

## Independent live reuse: no retraining between runs

Three sequential pairs were run against new live responses with fresh browser
state. Pair order was Manual/Auto, Auto/Manual, Manual/Auto. The saved profile,
workload and executable were identical for all trials. Metrics below are medians;
three runs are a small sample, not a latency performance claim.

| Metric | Strong Manual | Installed Auto |
| --- | ---: | ---: |
| Encoded HTTP body bytes | 1,109,594 | 989,843 |
| Acquired responses | 51 | 52 |
| Workload time | 4.94 s | 5.46 s |
| Browser CPU | 5.83 s | 6.09 s |
| Sampled peak RSS | 340.11 MiB | 339.22 MiB |
| Classic executions | 40 | 37 |
| Workload assertions | 3/3 PASS | 3/3 PASS |

Auto saves **10.79% body acquisition beyond strong Manual** in these independent
live pairs. It does not improve request count, CPU or elapsed time. One Auto
run took 9.37 seconds; no speedup is claimed. The three saved Manual documents
have distinct decoded hashes at equal lengths (65,854 bytes), confirming that
this live evaluation did not operate against byte-identical documents.

All Auto live workloads passed, with zero runtime violations and no active
transport requests at measurement. Their diagnostic recording status was
`UNSUPPORTED_CAPTURE`: the two deliberate header-only responses were incomplete
as replay evidence. No reusable Auto live capture was produced. This limitation
is distinct from workload failure and is not silently counted as successful
capture/replay. Auto HTML hashes are therefore unavailable; the saved Manual
captures supply the document-change evidence. The measured live acquisition and
37 executions show that specialization remained effective rather than falling
back wholesale. Unknown live inputs are still permitted normally; exact request
counts need not match the captured environment.

## Conclusion and limits

The earlier exact-document admission obstacle is removed for this scenario:
a newly trained profile retains useful savings on subsequent guest live runs.
This is evidence for one asserted interaction on one route, not proof of future
business correctness. Same-URL requests can become necessary later. Live
assertions and further held-out states remain necessary. The 297-second training
cost, capture storage and absence of a runtime-speed win must be considered
separately from body savings. No public deployment or release was performed.

See [sanitized measurements and hashes](workload-optimization-adaptive-live-results.json).
