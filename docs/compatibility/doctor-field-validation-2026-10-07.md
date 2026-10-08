# Compatibility Doctor evidence and replay boundaries

Compatibility Doctor is a contributor tool for collecting, inspecting and
comparing browser observations. A screening result describes declared
observations; it is not a universal browser-compatibility verdict. An expectation
that fails in both Mimic and the reference browser is a scenario defect until
further evidence establishes otherwise.

## Collection and ownership

Requests can start on a parent session and complete on an out-of-process frame
session. The collector retains both owners, joins only unambiguous related
requests and reads the response body from the completing session. Draining waits
for pending network work even when no body-read task is currently active.
A labelled child-startup diagnostic enables recording before a new target resumes.

Window observations include final CDP bounds and owned native-window state, with
failed reads recorded as gaps. These passive reads do not activate, restore or
resize windows. A visible native window does not by itself establish document
visibility, so presentation observations retain their capture context.

Explicit headless collection and replay retain mode and profile identity in the
report. They do not change `navigator.webdriver` or erase historical evidence.
Only mode-invariant measured semantics may be compared across launch modes;
presentation-sensitive expectations require the matching reference conditions.
See the [oracle policy](../oracle-policy.md) before collecting Chrome evidence.

## Replay and interpretation

Archived replay installs child interceptors before execution and fails closed for
missing or unfinished responses. It does not fill an evidence gap from the live
origin. Preserve original captures and their authority; additional diagnostics do
not retroactively replace them.

URL, method, status, response-body hash and error observations can agree while
request ordering, environment headers or presentation observations still differ.
The report retains those differences separately. A nonzero report result can
represent a collection gap or an observed disagreement rather than a collector
failure. Binary metadata inspection uses the installed Go toolchain and records
an unavailable observation as a gap instead of downloading another toolchain.

The `inspect`, filtered `events`, paginated native trace and JSON-pointer
`evidence` commands expose retained observations for reduction and diagnosis.
Promote minimized semantic probes into public test data; keep session cookies,
response bodies and other private capture material outside public documentation.

## Related runtime semantics

The [compiled XPath reference](../../internal/browser/testdata/xpath_compiled_chrome152.json)
retains frozen Chrome 152 observations and provenance. `TestXPathCompiledExpressionChrome152`
checks constructor branding, shared expression compilation, canonical result
nodes, invalid syntax, receiver checks and function metadata. XPath support is
partial; a successful reduced probe does not certify unrelated axes, namespace
resolution, scalar results or iterator invalidation.

Beacon uses the shared Fetch loader and a per-document outstanding upload budget
shared with Fetch keepalive. Accepted work can outlive a Page, while Context
teardown joins transport operations. `TestBeaconQueueQuotaNavigationAndContextTeardown`,
`TestBeaconCORSUsesSharedPreflightAndPolicy` and `TestBeaconBlockedPolicyReleasesQuota`
cover lifetime, policy and quota boundaries.

CSS background resource discovery reads canonical applied declarations through
the existing network and document lifecycle. It does not introduce a rendering
backend. `TestCSSBackgroundResourcesUseAppliedDeclarations` and
`TestCSSBackgroundAdmissionIncludesNewAdoptedSheets` cover discovery and mutation
without refetching unchanged resources.

## Focused validation

The Doctor regressions are in `tools/compatibility/test_compat_doctor.py`,
`test_doctor_agent.py` and `test_doctor_workflows.py`. They cover collector,
inspection and replay behavior independently of a public site's availability.
Run the affected tests and reduced runtime probes when changing these boundaries.
The [Doctor guide](../compatibility-doctor.md) defines the supported workflow;
individual capture comparisons do not imply complete tool-release qualification
or automatic repair of browser semantics.
