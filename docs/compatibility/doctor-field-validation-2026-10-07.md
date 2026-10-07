# Compatibility Doctor contributor field validation · 7 October 2026

This checkpoint exercises the uncommitted contributor tool on varied public
pages and uses its retained evidence to fix a runtime defect. It does not
establish universal browser compatibility or a completed tool release. No runtime
product changelog was changed.

## Screening scope

The fresh `.build/mimic-doctor-site-audit.exe` was built from the working tree.
Each initial scenario used a dedicated Mimic process, bounded observation times,
recorded CDP events and sealed evidence. Two independent screening processes
could run concurrently. Initial screening was not a Chrome agreement verdict.

| Page | Declared observations and result |
| --- | --- |
| Books to Scrape | Catalog heading and 20 product entries passed |
| Quotes to Scrape `/js/` | Ten JavaScript-rendered quotes passed |
| Hacker News | Thirty story entries passed |
| TodoMVC React | Rendered app; real click, text input and Enter created the named task |
| TodoMVC Vue | Rendered app; real click, text input and Enter created the named task |
| MDN Array reference | Exact heading passed; no uncaught exception; Beacon fallback warnings require a separate runtime follow-up |
| htmx.org | Static content loaded; XPathEvaluator construction broke htmx and its extensions; fixed below |
| Python documentation | The selected `main` element was absent in both Mimic and frozen headless Chrome; this was an incorrect scenario expectation |
| example.com | The expected historical `h1` was absent in both browsers; retained HTML showed the changed page; this was an incorrect scenario expectation |

Wrong scenario expectations were retained rather than relabelled as runtime
defects. MDN's warnings concern `Navigator.sendBeacon`, currently an unimplemented
generated operation; Chrome emitted no matching warnings. The page still reached
the requested content. Fetch fallback and an unfinished telemetry request remain
visible; this checkpoint does not add Beacon support or claim telemetry agreement.

## XPath defect and correction

Frozen Chrome 152 headless diagnostics had no htmx initialization exception.
The normal Doctor run used the same frozen binary, directly launched headful,
with a fresh profile, fixed nonzero CDP port and unmodified
`navigator.webdriver === false`. It retained the three Mimic-only exceptions:
illegal XPathEvaluator construction and two consequent extension failures.

Doctor's `inspect` and sealed script bodies identified the construction site.
Paired HTTP archive replay with diagnostic source/native-trace collection
reproduced the failures. Subsequent live Mimic runs reused the sealed Chrome
reference, and intercepted replay reused the original response archive.

The underlying correction is shared browser semantics:

- A constructible, branded XPathEvaluator and private compiled XPathExpression
  state retain the existing interface prototypes and canonical DOM wrappers.
- Document.createExpression, evaluator creation/evaluation and expression
  evaluation share one bounded query compiler.
- Nested attribute predicates, Boolean `or`/`and`, `name()` and string-prefix
  conditions are compiled generically, not selected by a site or attribute name.
- Compilation validates the expression before evaluating a possibly empty root.
  Constructor/receiver/argument errors and callable lengths are covered by the
  retained probe.

The reduced local probe uses unrelated attribute names and measures constructor
branding, compiled iterators, canonical snapshot nodes, direct Document
evaluation, invalid syntax, illegal receivers and function metadata. Its exact
output agreed between frozen headful Chrome, frozen headless Chrome and the
fixed Mimic. Public minimized observations and provenance are retained in
`internal/browser/testdata/xpath_compiled_chrome152.json`; the readable regression
probe runs on both Goja and V8.

This remains partial XPath support. Namespace resolution, other axes, scalar
results, result-object reuse and mutation-invalidated iterators are outside this
change.

All three htmx exceptions disappeared in the fresh fixed binary, both live and
on archived HTTP responses. Remaining report findings concern network sequences
and replaying an unfinished sponsor iframe response; they are not silently
promoted to a complete agreement verdict.

## Doctor finding and correction

The tool previously retained native-window state only at startup. A final hidden
document could therefore be flagged without showing whether the native window
had been minimized, hidden or remained visible. The collector now independently
retains final CDP bounds and owned native-window state, including failed query
gaps. These passive reads do not restore a window or change tab activation.

The field run showed that a document can report hidden even with a visible,
non-minimized native window and normal CDP bounds. The presentation gap remains
explicit. Runtime XPath conclusions rely only on the measured mode-invariant
semantic probe; no visibility or geometry expectation is derived from that run.

Normal htmx collection also retained a child attachment/startup race and missing
request-start events for its sponsor iframe. Archive replay correctly refused
the unfinished response instead of accessing the live origin. These documented
scope limits remain release qualification constraints, not fabricated successes.

## Validation and private evidence

Focused XPath regressions, including the retained probe on Goja/V8, passed.
All 62 Doctor tests passed after adding final-window observation regressions. The
full local runtime suite was not run.

The agent-facing `inspect`, filtered `events`, native-trace pagination and
JSON-pointer `evidence` commands successfully read sealed field evidence. Final
verification used the fresh `.build/mimic-doctor-site-audit-final.exe`; the htmx
exception stream was empty and the local XPath output exactly matched its saved
Chrome reference. Nonzero report exits still reflect the retained presentation,
network or original discovery-check gaps, not a collector crash.

All original and subsequent evidence remains private and sealed under:

- `.build/doctor-sites-20261007/`: nine Mimic screenings and labelled headless diagnostics.
- `.build/doctor-htmx-live-20261007/`, `doctor-htmx-replay-before-20261007/`,
  `doctor-htmx-after-20261007/`, `doctor-htmx-replay-after-20261007/`.
- `.build/doctor-xpath-before-20261007/`, `doctor-xpath-after-final-20261007/`
  and `doctor-xpath-pointer-20261007/`.
- `.build/doctor-htmx-final-20261007/` and `doctor-xpath-final-20261007/`:
  confirmation against the final freshly built executable, reusing the references.
- `.build/doctor-mdn-live-20261007/`: the separate Beacon warning evidence.

Evidence files can contain cookies and live response bodies. Only the minimized
local semantic observation was promoted into project test data. This is enough
to plan a scoped contributor-tool release with the remaining boundaries stated;
CI qualification and publication remain separate work.

## Follow-up fixes requested during merge preparation

The retained sponsor iframe events established a collector defect: its navigation
started on the parent session and completed on the OOPIF session. Doctor now
retains both owners, joins only unambiguous related requests, and collects the
body from the completing session. Draining waits for pending network work even
when no body-read task is currently active. A labelled child-startup diagnostic
enables recording before resuming new targets. Replay likewise installs each
child interceptor before execution, preserving the fail-closed HTTP boundary.

A new retained child-startup htmx capture contains 64 complete requests, including
the sponsor iframe and its early scripts. No missing-start or unfinished-response
gap remains. That capture predates the user's instruction to stop headful runs;
all subsequent Chrome experiments were headless. The tool now supports explicit
`--headless` runs and replay, recording mode/profile identity and authority in
reports without changing navigator identity or suppressing historical evidence.

The Beacon follow-up is implemented through the shared Fetch loader. Body
extraction supports null, strings, byte ranges, Blob, URLSearchParams and FormData;
URL/receiver/argument errors and stream rejection were measured with a labelled
frozen headless Chrome probe. A per-document 64 KiB outstanding upload budget is
shared with Window Fetch keepalive. Accepted work survives navigation and Page
close, while Context cancellation joins operations before transport teardown.
Focused tests cover Goja/V8, borrowed Navigator methods, credentials, CORS
preflight, CSP rejection, quota release and isolated Page budgets. A fresh MDN
run reusing the sealed reference emitted no Beacon fallback warnings or uncaught
exceptions; all six observed POST requests finished. Presentation-sensitive
expectations were not promoted from headless evidence.

The only missing htmx response representation in paired replay was a CSS
background SVG. Resource discovery now reads the canonical applied declarations,
loads their background URLs through the existing document network/lifecycle
boundary, and ignores unmatched selectors and descendants of `display:none`.
No drawing backend was introduced. A local headless Chrome/Mimic probe retained
the same four requests, zero exceptions and zero collection gaps. Regression
tests also exercise CSS mutation and avoid refetching unchanged backgrounds.

The final htmx headless/archive pair contains **64 requests in each runtime**,
with identical URL/method/status/body-hash/error multisets, no uncaught exceptions
and **zero collection gaps**. Request arrival order and environment/header
differences remain explicitly reported; they were not weakened into equality.
The historical hidden-window gap remains attached to its original capture.

Follow-up evidence is private under `.build/doctor-htmx-child-fix-20261007/`,
`doctor-htmx-child-replay-20261007/`, `doctor-htmx-all-fixes-20261007/`,
`doctor-beacon-probe-20261007/`, `doctor-mdn-beacon-fix-20261007/` and
`doctor-css-images-20261007/`. Windows CI additionally exposed automatic Go
toolchain download during binary metadata inspection. Metadata now uses the
installed toolchain without downloading; an unavailable observation remains an
explicit gap and does not prevent the owned launch or cleanup. The focused Doctor
suite now contains 69 passing tests. Runtime checks remain focused; the full local
suite is not run.
