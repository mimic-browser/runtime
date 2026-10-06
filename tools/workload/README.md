# Workload optimization tools

The current product workflow is native: `mimic optimize --name shop -- <command>`.
See [the Optimize guide](../../docs/optimize/index.md). No Python installation is
required by the feature. `feature_benchmark.py` drives the native command for
matched research evaluation; its clients retain their own dependencies.

`challenge_benchmark.py` runs the real React documentation, Vue/VitePress,
RealWorld/Angular and Books workloads sequentially against explicit private
captures. It uses only the native optimizer, strong manual comparison policies
and five rotated finalist measurements. Supply `--binary`, `--output` and
`--captures-json`; the latter maps case names to `capture` and, where needed,
`supplementalCapture` paths. The command refuses to substitute live acquisition
for missing recorded data. Each ordinary client owns its assertions. These
research clients use Playwright; Optimize itself has no Playwright dependency.

`live_validate.py` records one separate live validation without search or
automatic retries. It can validate default Mimic, `--manual-policy FILE` or the
normal `--profile FILE.mprofile` activation path. Its exit status reflects the
external workload oracle; consult the separate recording/replay status in the
receipt before using the capture as optimization evidence. Preserve outputs in
fresh private directories. See the [dynamic evaluation report](../../docs/performance/workload-optimization-feature.md).

The Python optimizer and notes below preserve the original PoC for historical
reproduction. They are not a second public contract or the shipping launcher.

---

# Experimental workload optimization

This is an offline experiment, not a production specialization or deoptimization
system. It runs an ordinary external CDP client unchanged. Its assertions and
exit status are the primary correctness contract. It searches for lower encoded
HTTP body acquisition, not for lower retained memory or a smaller CDP transcript.
The public ResourcePolicy API is unchanged.

## Run

Build Mimic normally (`go run ./tools/buildnative`, then
`go build -o .build/mimic.exe ./cmd/mimic`). Install Python dependencies from
`tools/workload/requirements.txt`. The workload retains its own dependencies.

```powershell
python tools/workload/optimize.py `
  --binary .build/mimic.exe --output .build/my-experiment `
  --manual-policy my-competent-manual-policy.json `
  --endpoint-env EXISTING_CDP_ENDPOINT_VARIABLE `
  -- node existing-workload.js
```

The client must already accept an endpoint through an environment variable or
use a fixed endpoint. Defaults are `MIMIC_ENDPOINT` and `PW_MIMIC_ENDPOINT`.
`--websocket-env EXISTING_VARIABLE` supplies the actual browser WebSocket URL;
`--endpoint http://127.0.0.1:9222` supports an unchanged hardcoded-port client.
The client connects to the owned browser; a command which launches its own
unrelated browser does not exercise Mimic. No Playwright API is required.

The command records one successful baseline, validates three fresh offline
replays, validates the supplied manual policy, searches, and measures rotated
Default/Manual/Auto trials (five repetitions by default). All candidate and
matched trials use the capture, with no transport fallback to live networking.
`--timeout`, `--max-trials`, `--replays`, and `--repetitions` bound the experiment.
Use a fresh output directory for each invocation.

```powershell
python tools/workload/optimize.py `
  --binary .build/mimic.exe --output .build/reuse `
  --capture .build/my-experiment/environment.mcap `
  --profile .build/my-experiment/optimized.mplan `
  --manual-policy my-competent-manual-policy.json `
  -- node existing-workload.js
```

`--capture` skips live recording. `--profile` skips search and revalidates the
existing plan. Plans require the exact capture and executable identities. They
cannot be activated on live networking. A rebuilt executable requires a new
search; changing metadata to bypass identity checks is not supported.

If the ordinary workload already emits JSON, `--result-file existing.json` or
`--result-env EXISTING_OUTPUT_VARIABLE` additionally compares the entire JSON
value with the first successful baseline. This is optional. It adds no page
assertion API and does not infer the client's semantic intent.

## Outputs and failure classes

* `environment.mcap`: versioned binary ZIP container, original response headers
  and encoded bodies, per-Context request identities and occurrence metadata.
* `optimized.mplan`: binary ZIP container, ResourcePolicy plus separate external
  classic-script execution admission identities, capture/executable hashes.
* `report.json`: baseline stability, chosen plan, all candidate outcomes, matched
  measurements, optimizer cost and measurement definitions.
* Each trial directory: separate client/browser stdout and stderr, `run.json`,
  candidate policy where applicable, and browser stacks for failed workloads.

`workloadStatus` preserves the primary external oracle independently of replay
coverage. Nonzero exit, crash, timeout, browser failure, optional JSON mismatch,
and corrupt replay are distinct outcomes. `UNSUPPORTED_REPLAY` means a missing
or ambiguous environment input, unsupported protocol, or unfinished transport;
it is **not proof of a semantic workload failure**. Such candidates cannot be
validated offline and are rejected without silently visiting the website.
Recording errors likewise do not claim the workload's assertions failed.

URL changes are normal on the live path. If a known query parameter is purely
a timestamp/cache buster, declare `--volatile-query-key parameter` during
recording. Only transport matching and script identity normalize its value;
the original request URL, response and browser observations are preserved.
Semantic query keys remain exact. This declaration is an assumption made by
the caller, not an inferred safety guarantee. New origins, paths, headers,
semantic states or branches can require new evidence; a capture miss is reported
as coverage, not as a broken site or incorrect result.

## Boundaries

Recording streams each acquired body chunk immediately into one temporary disk
spool. Finalization atomically writes the one binary artifact from disk. Replay
indexes metadata and opens individual stored body members lazily; it never
loads the whole capture into RAM. Normal loaders still decompress, run CORS,
follow redirects, update cookies and populate normal caches/body retention.
Already acquired data is not discarded to manufacture a traffic improvement.

Search first tries non-document resource kinds, recursively splits failing
groups, then tries individual URLs grouped by existing kind/owner/mechanism
provenance and ranked by encoded capture cost. Passing eliminations accumulate;
equivalent candidates are memoized. Manual and default remain eligible
finalists. The score is encoded body bytes, then acquired HTTP responses;
single-run timing noise cannot select a winner. This is bounded elimination,
not an exhaustive global optimum or a full dependency graph. Nonmonotonic
combinations and unseen request branches can be missed.

External classic scripts can separately be acquired normally while their author
execution is suppressed. Source bytes plus normalized URL identify the action.
Inline scripts, modules, workers, init scripts and CDP evaluations are not
suppressed. Normal acquisition, scheduling and load lifecycle remain in place;
existing parser-blocking-script load-event limitations are inherited. A separate
all-external-classic ablation shows whether runtime work can be removed, even
when that does not improve the traffic objective.

Supported experiments use fresh Contexts and HTTP(S), including ordinary
redirects, cookies and compressed bodies. Distinct requests have independent
occurrence cursors. Repeated identical requests with different recorded results,
concurrent identical requests, WebSockets, HTTP upgrades, incomplete bodies and
unreplayable transport errors are explicitly unsupported. Recording a WebSocket
does not block the ordinary live socket, but makes that capture unsupported;
offline replay cannot dial it. The existing 32 MiB response boundary applies.
Time/randomness, request scheduling, persistent storage/checkpoints, socket
messages, arbitrary client networking and full physical wire timing are not
emulated. Baseline replay instability stops the experiment.

The wrapper prevents Mimic transport requests from escaping replay. It is not
an OS network sandbox for the external process or external proxy integrations.
Run clients whose target-page networking uses Mimic's ordinary HTTP path.

## Measurement and validation

`encodedBodyBytes` counts encoded bodies actually read at transport, not physical
wire traffic: headers, TLS/framing, buffered unread bytes and CDP fulfillment
are excluded. Candidates block entire resources rather than partially reading
bodies. `requests` includes transport attempts; `responseAcquisitions` excludes
pre-header cancellations. `processedBodyBytes` counts loader deliveries, including
cache and synthetic responses. Script acquisition includes worker scripts;
execution counts cover classic author scripts only.

Finalists disable detailed search inventory. Browser CPU covers the owned process
tree during the external command; time includes client startup/assertions but
excludes browser startup. RSS is sampled every 20 ms, not exact heap memory.
Retained RSS after the command is not proof of leaked live objects. Body-store
retention is separately measured without disabling normal retention. Windows
Job Objects own browser/client descendants; POSIX uses process groups, with
less complete accounting of children which exit between samples.

Run focused boundaries:

```powershell
go test ./internal/workload -count=1
go test ./internal/browser -run '^TestWorkload' -count=1
go test ./internal/dom -run TestStream -count=1
$env:MIMIC_WORKLOAD_TEST_BINARY=(Resolve-Path .build/mimic.exe).Path
python -m unittest discover -s tools/workload -p test_optimizer.py -v
```

`benchmark.py --binary ... --output ...` runs real Books to Scrape extraction and
the existing unmodified async/React benchmark pages with strong manual policies.
It verifies source-server request counts did not increase during replay.
`--wikipedia-script path/to/playwright_wikipedia_local.js` adds the unchanged
existing saved Wikipedia workload when its private fixtures are available.
The fixture-server suite currently reuses Windows benchmark infrastructure.
See the measured [experiment report](../../docs/performance/workload-optimization-poc.md)
for results and negative findings.

## Native multi-state and heavy evaluation

The public workflow is `mimic optimize`, not the historical Python launcher.
`react_states_client.js` with `react-states.json` and `manual/react-article.json`
exercises three ordinary asserted CDP states. Pass `--states-file` and the same
client command; state environment inputs belong only to the external process.

`scout_client.js` / `scout_corpus.py` inventory pages, not correctness.
`heavy_dynamic_client.js` fixes content/author-handler contracts before search.
`manual/gitlab-navigation.json` is the strengthened baseline; the generic heavy
policy alone is not a competent final GitLab comparison. The separately scoped
`shadcn_command_client.js` still fails default theme behavior and is not a win.
See the [new report](../../docs/performance/workload-optimization-multistate-heavy.md)
for local gains, live fallback, rejected profiles and ineligible workloads.
Captures and security-bearing logs remain private.
