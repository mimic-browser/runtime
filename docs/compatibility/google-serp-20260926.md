# Google Search response opt-in investigation — 2026-09-26

## Confirmed shared network defect

Google's intermediate Search response sent `Accept-CH` across multiple header
field lines. The first line requested `Sec-CH-Prefers-Color-Scheme`; later
lines requested high-entropy UA hints. The loader used `http.Header.Get`,
which returned only the first line. Consequently, the next same-origin
navigation omitted the accepted UA hints.

The loader now combines all `Accept-CH` field lines before updating the
canonical context-owned session. There is no Google-specific dispatch or
profile override. Focused regressions cover ordinary next-request opt-in and
`Critical-CH` restart with a leading unsupported hint and separate UA hint
lines. The existing redirect and iframe permission checks also pass.

## Live result and limits

A freshly built Mimic sent the accepted architecture, bitness, form factors,
full version, full version list, model, platform version and WoW64 fields on
the second Search navigation. It still received a redirect to `/sorry/index`
and HTTP 429, with no JavaScript exception notifications. Fixing this defect
does **not** establish Google admission or explain the remaining rejection.

Retained ordinary Chrome 154.0.8037.57 and incognito HARs both contain
successful Search HTML. The ordinary capture is a reload in an existing
session. The incognito capture starts at a Search request already containing
`sei`; it does not contain the preceding intermediate document. These are
useful successful network references, but they do not yet establish a matched
comparison of the intermediate program's execution. Frozen Chrome 152 was
not launched again in this investigation.

Private HARs, captures and extracted response bodies remain untracked under
`.build/google-serp-success-20260926/`. Their SHA-256 hashes are:

- Ordinary Chrome HAR: `ea5c4a1410014c947295352fe25168b819bec28b7b3ea421eb3de66f42ae9327`.
- Incognito Chrome HAR: `a6d07d8df9d53e5cd0270c1f06ada78be4fa3cbff2e47d3720f7084db8a06cea`.

No cookies, account identifiers, session tokens or captured application
bodies are included in public project files. Temporary header diagnostics
were removed. The full local test suite was not run.

## Follow-up: complete incognito CAPTCHA chain

An additional retained HAR includes the previously missing intermediate
document. Its SHA-256 is
`0287cc6905e36cf82a22048c92e4c7a78dacd68fe589057366a2077cdbc6c4aa`.
Chrome 154 first received 92,415 bytes of intermediate Search HTML, then
requested Search with `sei`, received HTTP 302, and reached `/sorry/index`
with HTTP 429. Later, the HAR records a return to Search with `google_abuse`,
another redirect, and successful result HTML. The initial intermediate-program
execution therefore did not itself establish admission in this Chrome session.
The successful result cannot be used as evidence that Mimic failed a branch
which this Chrome execution passed.

The saved intermediate document was replayed unchanged in fresh contexts in
Mimic and frozen Chrome 152. Every outgoing request was fulfilled locally;
neither replay contacted Google. Both executions created an `SG_SS` cookie
with a 1,332-character value, Lax SameSite and no Secure attribute, and issued
the next Search navigation with the original query-key set plus `sei`.
Neither emitted a page exception. Mimic reported no semantic-missing entries
in its saved API trace. The observed first-to-next-request intervals were
approximately 390 ms in Mimic and 52 ms in Chrome; these are individual
intercepted replay measurements, not a live performance comparison.

This proves the presence of the cookie-and-navigation branch in both replays,
not equality of the encrypted cookie contents, VM instruction sequences or
all browser observations. No captured cookie or CAPTCHA credential was copied
into a live Mimic session. No additional production semantic fix is justified
by this capture alone.
