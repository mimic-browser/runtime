# Manual capture 211032 and offline replay scope correction

This is a read-only analysis of the retained capture
`manual-20260911-211032`, compared with `manual-20260911-202858` and the
existing offline `after-descriptors` trace. No site, VM, browser test, oracle,
race, stress or performance run was started for this analysis.

It follows [the cold child snapshot fix](bootstrap-cold-child-2026-09-11.md),
committed as `531e1e7`. It **supersedes the claim that the offline driver reached
the end of the saved exchange**. That driver covered one cycle and the beginning
of a reload, then stopped because of its response-selection limitation.

## What the new capture shows

These are trace records and distinct diagnostic names, not counts of semantic
root causes or normalized Chrome differential leaves. The captures are separate
executions with different server programs and inputs.

| Observation | Manual 202858 | Existing offline after-fix | Manual 211032 |
| --- | ---: | ---: | ---: |
| bootstrapSnapshotUnavailable | 18 | 0 | 0 |
| imageDecode | 5 | 3 | 5 |
| Scheduler error records | 0 | 0 | 0 |
| CDP Runtime.exceptionThrown | 0 | Not attached | 0 |
| Unsupported records | 326 | 163 | 484 |
| Distinct unsupported names | 161 | 161 | 161 |
| Semantic-missing records | 186 | 86 | 169 |
| Distinct semantic-missing names | 18 | 17 | 18 |
| Network responses, including local/cache projections | 39 | 20 | 39 |
| Top-document response statuses | 403, 403, 403 | 403, 403 | 403, 403, 403 |

The unsupported-name sets are exactly equal across all three traces. The
increase of 158 records between manual captures consists of the same 158
WindowProxy names being recorded three times instead of twice, in three child
realms instead of two. It is not evidence of 158 new defects. Access tracing
deduplicates observations, so these counts are not JavaScript operation counts.

The semantic-missing name sets are equal between manual captures. The difference
of 17 records is Window.structuredClone (25 to 8), not a newly established fix.
The offline set lacks only CDP.Runtime.runIfWaitingForDebugger because the driver
does not attach CDP. Existing capability boundaries still require independent
behavioral evidence; their presence in a trace does not identify the failed
server decision.

The five image diagnostics remain three unsupported ICO decodes and two PNG
zlib errors. The ICO bytes match the preceding capture exactly. Both new saved
PNGs have valid chunk CRCs but their concatenated IDAT data fails independent
Python zlib decompression (headers `012d` and `01cd`). The earlier saved PNGs
also fail that check. These are not a newly demonstrated transport-corruption
regression or a proved reason for the application outcome. No new Chrome image
decode measurement was made.

Both manual captures contain 34 responses with status 200, three with status
403 and two with status 401; both have two DNS failures and the same CDP event
counts. The top document still reloads with GET and receives the challenge
response. Both contain two child messages whose captured shapes include a
`code` field. The values of `event` and `code` are not recorded, so this does not
establish a specific fresh failure code. Absence of uncaught exception events
does not exclude exceptions caught by application code.

## Why the offline driver stopped earlier

The private driver scans its fixture list from the beginning on every request.
Already-used entries are excluded for non-GET requests, but not for GETs. It
therefore chooses the first matching GET response again even when later
responses for that URL contain a different document.

The saved fixture list contains three different top-document bodies at indices
0, 15 and 30. The recorded offline selection is:

1. Fixture 0 for the first document request and its Critical-CH restart.
2. Fixture 0 again after the first reload; fixtures 15 and 30 are never selected.
3. The first cycle's orchestration script again, producing the first cycle's
   POST route.
4. The two stored POST responses on that route, indices 3 and 14, have already
   been consumed. The driver cancels execution instead of returning a response.

Only 11 distinct fixture indices were selected from the 39 saved response
objects. This is not a coverage percentage: that list also includes blob/cache
responses, which do not necessarily reach the replacement transport.

The earlier seven-to-zero snapshot comparison is still an observation of the
same limited driver and saved inputs, and the new manual capture independently
has zero such diagnostics. It never proved that the offline execution completed
all recorded cycles, passed the challenge, or reproduced the new live result.
The offline trace itself contains a code-bearing child message and a reload.

In addition, response selection did not validate newly generated request bodies
or headers. The fixture schema did not contain the request method. Stored
responses were returned regardless of whether the server would accept the new
submission. Transport timing, H3 metadata and fresh server evaluation were not
reproduced. Reaching a saved continuation therefore cannot establish acceptance.

Further full-cycle replay needs occurrence-aware request/response matching that
accounts for Critical-CH restarts and cache/local responses, reports unused
fixtures, and distinguishes a capture boundary from an exhausted response
queue. Merely replaying the first GET forever or extending the time budget will
not supply that coverage. The historical driver and its results remain intact.

## Identity and remaining evidence gap

At inspection, main was clean at `531e1e7`. The supplied directory has no build
passport. The current process listening on local port 9222 was created at
17:10:41 UTC, before the captured events, and runs a temporary `go run` binary
built with Go 1.26.4. Its SHA256 is
`8c1a938b5a3485c136df4149540abe697bb727b515b07a590086ee85ba2303d4`.
It contains no VCS metadata. This is supporting process evidence, not an
independent proof of the captured source revision; a different hash from the
published build does not by itself establish stale source.

The exact browser observation responsible for the server decision remains
unidentified. This capture confirms the snapshot diagnostic is gone and the
application outcome has not changed. It does not reveal a new confirmed runtime
root cause to patch. The missing evidence is the value path from a browser
observation through the program to its submitted result and decision, with a
comparable native reference. Native stability debt and optimization backlog are
unchanged; no performance or broad crash-family claim follows from this analysis.

Source hashes, counts, response-selection receipts and process identity are in
the ignored `.build/manual-211032-analysis/summary.json`. Raw responses, request
bodies, cookies and tokens are not added to Git.
