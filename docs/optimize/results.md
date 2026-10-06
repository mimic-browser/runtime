# Multi-state and heavy workload results

The 2026-10-06 checkpoint validates React across three input states: Quick Start,
Adding Interactivity and Managing State. Auto saves 30,060 encoded HTTP body bytes
(5.4%) and two responses beyond strong Manual in every state, with 15/15 passing
installed-profile measurements. Initial live training acquired 5.68 MB; acquisition
break-even versus Manual is about 190 runs, excluding CPU/time/disk training costs.

GitLab project navigation leaves 1,110,148 bytes after strengthened Manual.
Installed Auto acquires 1,013,774 bytes: 8.68% less, with 5/5 local passes.
It removes a shared analytics chunk and avoids two unused API response bodies.
A separate live check passed assertions but changed HTML rejected profile
admission: general fallback acquired more than Manual. Reliable heavy-site live
savings are not yet established. No consistent CPU or latency win is claimed.

Supabase search identified removable work, but final guarded replay encountered
an unseen telemetry POST. No profile was installed. Next.js, React Native,
Discourse and shadcn/ui asserted scenarios failed default interaction and were
not optimized. These failures and model limits are retained, not counted as wins.

Default matched replay is unavailable for the new dynamic cases. No held-out or
future-state generalization is claimed. See the
[full new report](https://github.com/mimic-browser/runtime/blob/main/docs/performance/workload-optimization-multistate-heavy.md).

## Previous checkpoint


Optimize has demonstrated one narrow gain over a strong manual policy, not a
general dynamic-site advantage. The 2026-10-05 evaluation uses actual public
React documentation, Vue/VitePress, RealWorld/Angular and a Books SSR control.

| Workload | Strong Manual encoded bodies | Installed Auto encoded bodies | Auto versus Manual |
| --- | ---: | ---: | --- |
| React documentation | 556,903 bytes | 526,843 bytes | 30,060 bytes / 5.4% less; two fewer responses |
| Vue guide | 88,870 bytes | 88,870 bytes | Tie |
| RealWorld | 159,719 bytes | 159,719 bytes | Tie |
| Books SSR | 5,276 bytes | 5,276 bytes | Tie with document-only |

Every Manual/Auto variant passed five matched local trials. React's separate live
activation reproduced the same acquisition difference, with both workloads
passing. It was slower in that single live observation, so no live latency gain
is claimed. Encoded bodies are not physical wire traffic.

React's manual editor exclusion activated two non-obvious first-party fallback
chunks. After recording their immutable environment once, branch-aware local
search removed both by its second candidate trial. Training took approximately
239 seconds, including a 180-second search. The original default plus reference
captures cost approximately 1.93 MB of encoded acquisition; at the demonstrated
gain versus Manual, that preparation breaks even in about 65 workload runs by
encoded bytes. CPU/time/disk training costs remain separate.

The ties are also useful. Vue's apparently unnecessary sponsor module is a
static application dependency: blocking it prevents required theme hydration.
RealWorld's unobserved comments request gates the article resolver. These are
limitations of the current execution model, not proof that every remaining byte
is intrinsically required. Arbitrary module/function pruning and substitute API
results are not implemented.

Default local replay remained uncovered on these dynamic captures because of
changing analytics identities or ambiguous repeated visual responses. The
Default matched columns are unavailable, not replaced with an already optimized
baseline. Fresh default live workloads passed; those single observations are
reported separately. Books has a complete matched Default/Manual/Auto comparison.

An early RealWorld apparent win accepted loading/error text as article content.
It was discarded; the final assertions verify API content and reject Markdown
removal. Failed storefront interaction and MDN coverage cases remain negative
evidence rather than being counted as wins. One recorded state per workload does
not establish generalization to future site changes.

See [methodology](benchmarks.md), [safety](safety.md), and the
[full research report](https://github.com/mimic-browser/runtime/blob/main/docs/performance/workload-optimization-feature.md).
The report and website preparation remain local until separately published.
