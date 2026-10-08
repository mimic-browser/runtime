# CDP site stalls, 2026-09-14

Four browser compatibility fixes remove a recursive frame-import failure, a
cross-realm checkpoint deadlock, and two MutationObserver defects. Lowe's,
Macy's and Flyscoot now expose application content and updating preview HTML.
Long tasks still delay commands on the busy Page, particularly Macy's Sale.
Home Depot's expected denial and Royal Albert Hall's challenge remain.

## Scope and provenance

- Worktree: `E:\GitHub\mimic-cdp-sites-20260914`.
- Branch: `codex/cdp-site-stalls-20260914`.
- Base: `f07bd2d643eebbadc28d582ef93f01c832a50106`, the latest committed
  checkout state when the investigation began. That commit changes tests,
  documentation and test orchestration, rather than production runtime code.
  The failures reproduced from its clean source; original uncommitted work was
  excluded.
- Final executable: `.build/mimic-final4.exe`, freshly built from `2c78fbb`
  with all four fixes and the journey tool committed.
- Windows amd64, Go 1.26.4, V8 backend, preview enabled. This was a live
  compatibility investigation, not a controlled performance comparison.
- [Retained measurements](cdp-site-stalls-20260914.json) record binary hashes,
  source provenance, response status, Page state, preview counts, command
  failures, departures and recovery for the baseline and subsequent runs.

## Final live journey

Each visit observes the same Page for approximately 30 seconds, subscribes to
its preview websocket, and checks `Browser.getVersion` and an independent
Page's `Runtime.evaluate(6*7)` on the same CDP connection. It then requests an
ordinary navigation to a local HTTP page and verifies its `Recovery` title.
The final run allows 45 seconds for departure to distinguish long tasks from
a deadlock. Earlier runs used an 8-second departure limit and retain failures.
Follow-up destinations are links discovered on the visited page, navigated
through CDP after the recovery check. No purchase, account or booking flow was
exercised.

| Page | Main HTTP | Last state / DOM nodes | Preview updates | Ordinary departure | Observed result |
|---|---|---|---:|---:|---|
| Home Depot | 403 | complete / 7 | 2 | 0.092 s | Expected access denial |
| Lowe’s home | 200 | interactive / 1141 | 58 | 0.043 s | Shop content; still interactive at the last sample |
| Lowe’s Deals | 200 | complete / 1758 | 43 | 3.955 s | Shop/category content |
| Macy’s home | 200 | complete / 7650 | 42 | 0.030 s | Storefront content |
| Macy’s Sale | 200 | complete / 14197 | 17 | 26.392 s | Sale content; slow same-Page departure |
| Royal Albert Hall | 200 | complete / 34 | 26 | 0.163 s | Anti-bot challenge |
| Flyscoot home | 302 → 200 | complete / 1051 | 94 | 0.027 s | Flight-search controls and destination offers |
| Flyscoot advisory | 200 | complete / 492 | 41 | 0.031 s | Advisory and site navigation |

All eight departures in this run reached the local recovery page without
`stopLoading`. Browser/independent-Page health checks had no failures during
observation. Final executable SHA-256:
`b44cab5e87f50f08ea02b1ead32f318dce8166fa56f5e5181e02468016b106d9`.

`complete` means the document lifecycle reached that state. It does not prove
that all widgets, resources or workflows work. Preview counts measure received
HTML updates, not screenshot/rendering parity. A challenge or denial page is
not counted as the requested site's application loading successfully.

Home Depot returned HTTP 403 / `Access Denied`; the same response was reported
in a separate ordinary-browser session. This does not establish a Mimic-specific
semantic defect or identify the restriction mechanism.
Royal Albert Hall returned HTTP 200 with `Pardon Our Interruption` and
Incapsula resources. Its content is a challenge, despite the successful HTTP
status. Commands and departure work there; the challenge's precise decision
reason was not established.

Flyscoot redirected `/` to `/en`. With the first two fixes it escaped the
checkpoint deadlock but showed only cookie/privacy text. Fixing the legacy
observer constructor exposed a second startup error, missing `observe` on the
instrumented observer. After the enumerable-method fix, its home contained
flight-search controls, destination offers and an advisory link. The advisory
page loaded too. `/privacy` was also visited successfully in the earlier run.

## Fixes and evidence

### 1. Private frame imports invoked public reflection hooks — `dc1715b`

Imported arrays/functions require a local proxy target. Its construction used
mutable public `Object.getOwnPropertyDescriptor`, `Reflect.ownKeys`,
`Object.defineProperty` and `Function.prototype.bind`. Website instrumentation
could therefore observe private scaffolding and recursively cross back into
the foreign realm. The Lowe's diagnostic showed thousands of repeated frame
transactions, while one network callback occupied the Page for over a minute.
Same-Page evaluation and preview stopped progressing; browser commands and the
independent Page still answered.

The bridge now uses its captured intrinsic functions for that private work.
Normal author-visible reflection and foreign accessors retain their behavior.
The focused fixture instruments reflection in the parent using a child-native
function and imports a foreign array/function. The baseline incorrectly called
the public hook three times. Frozen headful Chrome 152 and the fixed runtime
both return `answer: 43, privateReads: 0`.

Evidence: [fixture](../../internal/browser/testdata/frame_import_reflection_oracle.js),
[Chrome capture](../../internal/browser/testdata/frame_import_reflection_chrome152.json),
[regression](../../internal/browser/frame_instrumentation_test.go).
Local diagnostics: `.build/lowes-diagnostic/goroutines.txt` and `cpu.pprof`,
`.build/lowes-deep`, `.build/reflection-before.log`.

### 2. Foreign microtask checkpoints could deadlock an owner — `8b550e2`

On Flyscoot, dynamic inline-script cleanup ran on a child V8 owner thread and
drained a pending parent microtask. That parent job synchronously accessed the
child. The child was waiting for the parent checkpoint while the parent was
waiting for a command on the occupied child thread. Even cancellation could
not finish the cycle.

When cleanup drains a foreign checkpoint, it now keeps its own owner receptive
through `RunNested`. Owner reentry also applies after the JS host callback has
returned, since Go cleanup still occupies that actor thread. The fix preserves
the Page's checkpoint queue and does not drive unrelated timers or other Pages.

The [regression](../../internal/browser/checkpoint_owner_reentry_test.go)
constructs this ownership cycle without network access: child-owner cleanup
drains a queued parent job that reads the child. It timed out before the fix
and completes afterward. The native owner-thread case runs on V8; goja skips
that engine-specific test. Local evidence:
`.build/flyscoot-diagnostic/goroutines.txt`, `.build/checkpoint-before.log`.

### 3. The legacy observer alias retained a stub — `8c0c6cf`

`WebKitMutationObserver` still referenced the generated unsupported constructor
after `MutationObserver` was replaced with its semantic implementation. Angular
startup on Flyscoot encountered `Illegal constructor`. Both Window properties
now initially reference the same canonical constructor and observer state.

The fixture verifies identity, branding and delivery order from observers
constructed through both names. Chrome and fixed goja/V8 agree; the baseline
throws. Evidence: [fixture](../../internal/browser/testdata/mutation_observer_alias_oracle.js),
[Chrome capture](../../internal/browser/testdata/mutation_observer_alias_chrome152.json),
[regression](../../internal/browser/mutation_observer_alias_test.go),
`.build/observer-alias-before.log`.

### 4. Observer operations were hidden from enumeration — `29da882`

The implementation's JS class methods were non-enumerable. Chrome exposes
`observe`, `disconnect` and `takeRecords` as enumerable prototype operations.
The site's constructor instrumentation discovers methods by enumerating an
instance, so its replacement constructor lost those operations. Flyscoot then
failed with `(intermediate value).observe is not a function` in its polyfills.

The three operation descriptors now expose the Chrome enumerable flag. A
focused fixture enumerates and forwards the methods to their original branded
observer and verifies real mutation delivery. It fails on both engines before
the fix and agrees with the headful Chrome capture afterward.

Evidence: [fixture](../../internal/browser/testdata/mutation_observer_enumeration_oracle.js),
[Chrome capture](../../internal/browser/testdata/mutation_observer_enumeration_chrome152.json),
[regression](../../internal/browser/mutation_observer_enumeration_test.go).
The locally retained `polyfills.53c324ed5cc73449.js` in
`.build/scoot-enumeration` confirms the constructor-instrumentation path.
Its Angular startup exceptions disappear after the fix and the main
application content appears.

## Remaining failures and limits

- **Busy-Page command latency remains.** With the first three fixes, a
  diagnostic ordinary departure from Lowe's took 9.23 seconds; a stack sample
  was inside a runnable native microtask checkpoint. Macy's Sale took 26.69
  seconds, with samples executing nested frame operations and DOM child reads.
  These visits eventually recovered without stopping the Page. The final run
  still shows a long Macy's Sale departure. The Page command lock waits for
  the running task/checkpoint; resolving that navigation boundary needs further
  compatibility work. The patches do not guarantee immediate `goto` during
  arbitrary long-running scripts.
- **Keep the failed short-timeout attempt.** In `.build/final-journey`, Macy's
  Sale exceeded the 8-second departure limit. `stopLoading` replied, but the
  subsequent navigation/evaluation also exceeded their limits. The process
  remained alive. This is a failed recovery within the observation window,
  not proof of a permanent deadlock. Longer independent diagnostics and the
  final run establish eventual departure on subsequent visits.
- **Application/telemetry errors remain.** The retained results include
  Dynatrace `Illegal constructor`; a Flyscoot trace identifies the unsupported
  `ReportingObserver.constructor` boundary. Insider's push SDK accesses an
  undefined promise (`then`). Other visits report null `style`/React `type`,
  undefined `success`, missing `jQuery`, or unavailable control-font metrics. Their full
  semantic causes were not all isolated. They must not be counted as fixed
  merely because the main content and CDP progress.
- **Trace collection can itself exceed a client's message limit.** An early
  diagnostic collected cumulative whole-page traces: saved results grew to
  approximately 125 MB and 205 MB, then a subsequent trace request lost the
  websocket. This is consistent with the client's 128 MiB message limit;
  there was no demonstrated process crash. Later journeys disable whole-trace
  retrieval by default and clear retained trace events between visits. A
  bounded/paginated trace API remains a separate improvement.
- **No process-wide CDP outage was established.** Browser and independent-Page
  health probes kept answering during the observed Page stalls. Their samples
  cover the observation phase, not every instant of the departure timeout.
  All retained final process checks found the diagnostic process alive before
  harness cleanup. Cleanup kills only the harness-owned process tree; this
  does not establish graceful teardown or memory-retention bounds.

A fresh Chrome 152 **headless** live control returned denial content for Home
Depot (403), Lowe's (200 with `Access Denied` text), Macy's (403) and Flyscoot
(403); Royal Albert Hall's main document returned 403 and loaded a challenge
iframe with 200. Those session/mode-dependent results are supplemental network
observations, not behavioral oracles. The three retained semantic captures use
fresh **headful** Chrome `152.0.7977.82` with full capture metadata.

## Validation and reproduction

- Full affected package suites passed with the first three fixes:
  `go test ./internal/browser ./internal/cdp ./internal/engine/v8 -count=1 -timeout=8m`.
  Browser: 458.943 s; CDP: 31.842 s; V8: 1.163 s. Host load was uncontrolled.
- After the final descriptor fix, focused mutation/observer, frame-import,
  checkpoint-owner and native-frame tests passed in 15.442 s. The four new
  regressions were also checked for failure before their corresponding fix.
- Python compilation, oracle-capture validation and `git diff --check` passed.
  The retained CLI also emits UTF-8 diagnostics so site text can be printed
  on Windows without depending on the active legacy code page.
  Logs remain under `.build/final-tests.log`, `.build/final4-focused-tests.log`
  and the per-fix before/after logs. Frozen harnesses and expectations were
  not weakened.

From this worktree, a fresh repeat can be run with a new output directory:

```powershell
go build -o .build/mimic-repeat.exe ./cmd/mimic
python tools/compatibility/site_journey.py --binary .build/mimic-repeat.exe --binary-revision (git rev-parse HEAD) --out .build/site-repeat --preview --follow --budget 30 --probe-timeout 8 --departure-timeout 45 --keep-waiting
```

Use `--departure-timeout 8` to retain the stricter responsiveness check, or
`--sites macys --url 'https://www.macys.com/shop/sale?id=3536'` for a targeted
visit. Raw output includes transient headers, cookies and site HTML, so keep
it under `.build`. The committed JSON excludes those payloads and preserves
the observed outcomes. The original checkout was neither merged nor modified
by this investigation.
