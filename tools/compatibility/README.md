# Private native captures

The [Compatibility Doctor](../../docs/compatibility-doctor.md) is contributor
tooling for evidence-backed runtime fixes. It launches an ordinary
headful frozen Chrome and a dedicated Mimic process, records an explicit scenario,
and creates an HTML/JSON report with evidence gaps. Its local demo reproduces a
documented DOMException difference. Saved Chrome captures can be reused without
launching Chrome again. It does not claim full execution tracing or zero false negatives.
Separate commands provide intercepted HTTP archive replay and bounded action
reduction, with exact failure-pair checks and preserved evidence for every attempt.
Agents use `--json`, `inspect`, paginated `events`, and JSON-pointer `evidence`
reads; HTML is optional. `replay --diagnostic` also retains Chrome script sources
and Mimic's bounded native runtime trace.
Use `run --headless` or `replay --headless` for explicitly labelled diagnostics
without visible browser windows. `run --pause-children` captures early child
traffic by waiting for recording setup; replay enables child interception before
resuming execution. Both modes retain their instrumentation and authority scope.

The [Blazor Server Interactive probe](blazor_server_probe/README.md) runs a
local .NET app through frozen Chrome 152 and Mimic. Its Playwright journey
covers SignalR circuit startup, server DOM patches, forms, JS interop, and
navigation, and reports observed differences without joining the full Go test
suite.

## Shared differential corpus

`differential.py` executes `compatibility/corpus/index.json` in both runtimes.
It creates a loopback HTTP fixture server, disposable Chrome contexts and new
Mimic targets. **Use a dedicated Mimic process**: Mimic's cookies outlive targets,
so the runner clears that process's cookies between cases. It never launches
either browser. Supply an existing CDP endpoint for each:

```text
python tools/compatibility/differential.py --chrome http://127.0.0.1:9343 --mimic http://127.0.0.1:9349 --output compatibility/private-captures/corpus-new
```

The default reference is exactly 152.0.7977.82. An explicit alternate, for example
`--reference-version 152.0.7977.83`, is accepted and marked `frozenReference:false`.
Google Chrome branding, locale, font installation and graphics backend can differ
from the frozen Chromium profile; these differences are preserved in the report.
Use `--only partition temporal` for selected cases. Exit 1 means differences,
incomplete probes or harness errors; it does not mean the tool failed to run.

Each corpus file is a JS expression returning a plain observation or a Promise.
Cases may contain sequential navigation steps sharing storage. Compare causal
ordering, monotonicity and relations, not exact wall-clock durations. Expected
exceptions must be caught inside the probe. An uncaught top-level probe exception
is recorded as incomplete coverage even when both sides throw. Capture independent
subprobes separately so a missing method does not hide unrelated observations.

The normalizer tags undefined, NaN, infinities, negative zero, bigint and array
holes before CDP serialization. `$jsValue` is reserved. Functions, symbols and
cyclic identity must be described explicitly by probes. It does not flatten
exceptions, sort observation arrays, round numeric values or suppress missing keys.
`value-normalization` checks this contract in both runtimes.

Outputs:

* `report.json`: versions, corpus/fixture hashes, normalized observations and
  JSON-pointer differences; `incomplete` and `harnessError` distinguish bad probes.
* `received-http.json`: server-observed method, path, ordered headers, body,
  HTTP version, connection identity and timestamps, independently of CDP.
* `cdp-events.json`: raw diagnostic events. Mimic can nest target events inside
  `Target.receivedMessageFromTarget`; decode its `params.message` before analysis.

The serial network case additionally compares redirect/method/body sequences and
connection reuse with first-use ordinals. Raw headers stay in the wire artifact.
This measures **loopback HTTP/1.1**, not TLS, HTTP/2 or QUIC serialization. Do not
generalize its transport findings to HTTPS or claim CDP projections are wire proof.
Cookie observations retain domain/path, Secure, HttpOnly, SameSite, session and
partition key. Clock-dependent expiry timestamps and unimplemented CDP metadata
such as source port are not part of that normalized comparison.

Run helper tests with:

```text
python -m unittest discover -s tools/compatibility -p test_differential.py
```

See `docs/compatibility/differential-2026-09-10.md` for measured results and limits.

## Native page capture

`capture_native.py` records a new disposable browser context in an already running
Chrome CDP instance. It does not launch the browser, inspect existing tabs or
storage, or close the browser. Only the new context and its recursively attached
iframe/worker targets are recorded; the context is disposed at the end.

Install Python's `websockets` package, start the intended frozen Chrome separately,
then provide its verified local debugging port and an explicit destination URL:

```text
python tools/compatibility/capture_native.py compatibility/private-captures/run-name 9343 https://example.test/
```

Use a new output directory for every attempt. The targeted gitignore rule keeps
`compatibility/private-captures/` private: response bodies, POST data, storage and
cookies may contain credentials. Never promote raw captures into tracked fixtures.

The recorder saves request/response events and headers, available POST data,
decoded response bodies, parsed script sources including eval and worker scripts,
console/runtime events, final DOM snapshots, screenshot, owned-context storage and
cookies. Durable network messages retain bodies across process swaps. The manifest
indexes sources and bodies and hashes finalized files. Inspect error responses in
individual artifacts as well as transport/time-out errors in the manifest.

Browser-wide Tracing is omitted to avoid recording unrelated tabs. Raw TLS bytes,
deterministic random seeds and replayable server decisions are not captured.
Tokens expire, and the output is not a guarantee of deterministic offline replay.
No fresh-process or profile hash is claimed when attaching to an existing browser.

`summarize_trace.py` reads either Mimic trace JSON or native CDP events JSONL.
It includes console errors/warnings as well as runtime exceptions and network
failures; filtering only Mimic's `kind=error` misses caught exceptions logged by
the page. Use `--output` to keep its UTF-8 JSON alongside the private input.
The summary records redirects and native document responses but does not infer
challenge passage from the absence of exceptions or from an HTTP status alone.
Caught exceptions that are never logged still require a separate inspector run.

For the private iroshop capture made on 2026-09-09, successful **Cloudflare passage**
means the initial HTTP 403 `cf-mitigated: challenge` was followed by the application
document without that header and a `cf_clearance` cookie. The requested
`/mimic-e2e` route then returned a genuine Next.js 404; this is not an application
HTTP-200 success. The retained successful attempt has zero response-body,
script-source or POST-data retrieval errors and explicitly omits global Tracing.
