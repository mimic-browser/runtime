# Document/Node descriptor and Location semantics

Baseline f6135d7, following [Document getter brands](document-getter-brands-2026-09-11.md).
Related fixes are validated together; this is not a new broad discovery budget.

| ID | Root cause | Repro | Chrome | Mimic before | Priority | Status | Fix | Limits |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| DOC-PLACEMENT | Document specializations were published as extra prototype properties | document_descriptor_oracle.js | Inherited Node accessors and own unforgeable Location | Extra firstChild/textContent/location on Document.prototype | P1 | Corrected | Keep Node specializations on Node; install Location on each Document | Other interface placement differences remain |
| NODE-DOC | Borrowed Node accessors omit Document semantics | Same oracle | Correct first child; null textContent with setter conversion before no-op | Wrong child/null behavior and conversion/receiver checks | P1 | Corrected | Private node/document state, captured original behavior for other nodes | Does not claim all Node operations complete |
| DOC-LOCATION | Own getter captured one Location and had no PutForwards setter/lifecycle check | Same oracle plus iframe removal | Receiver-owned Location, null without active context, forwarding setter | Borrowed receiver ignored; inert own descriptor absent; stale Location after detach | P1 | Corrected | Shared instance descriptor, private owner dispatch, active-state projection | Native global proxy and saved-eval limits unchanged |

The final focused oracle improves 26 differing leaves to zero, with zero focused
Chrome-to-Chrome differences. It checks own/prototype placement, same-realm getter
identity, borrowing across realms and inert documents, setter receiver/conversion
ordering, Document/DocumentType null textContent, private first-child state under
public childNodes shadowing, and Location after iframe removal. The earlier
17-leaf discovery and later lifecycle/conversion additions are separate probe
revisions; they are not summed as fixes. Source hashes and before/after observations
are retained in [compact evidence](document-descriptors-20260911/).

The unchanged broad getter probe now has zero differences against its previously
controlled Chrome capture (down from three). Original fixed corpus stays 205/236,
representatives 19/28, no new leaves, regressions or Chrome drift. Original brand
corpus improves 583 to 578 records, five removed with no added/changed remaining
records. General corpus stays 40; relations stay two. These sets overlap and are
not added. Fresh full Chrome-to-Chrome corpus, fuzzer discovery, sweep and paired
performance gates were not repeated in this unchanged environment. Their previous measurements
remain historical; the fixed and affected corpora were replayed on this build.

An initial new regression fixture used unimplemented DOMImplementation.createDocumentType
and could not complete in Mimic. It was corrected to reuse the existing inert
document's doctype; the complete Chrome observation is exactly unchanged. The
initial failure is retained as a fixture prerequisite error. No expectation was
weakened and no unrelated API stub was added. Final focused ordinary/restored
oracles pass, including the preceding getter ownership and receiver regressions.

Full browser suite PASS (486.392 s output / 493.418 s completion). CDP and
WebAPI pass; unchanged engine V8/Goja results were cached. Targeted browser race
PASS (11.264 s); filtered WebAPI race had no matching tests. Full browser race is
not claimed. Existing opt-in skips remain in the test receipt.
No new performance or full lifetime-reclamation claim is made. P0 remains tracked
in the [separate serialization investigation](snapshot-serialization-2026-09-11.md);
its unreduced historical SizeFromMap signature and Goja race timeout remain open.
All work uses loopback fixtures and frozen Chrome 152.0.7977.82. No target site ran.
