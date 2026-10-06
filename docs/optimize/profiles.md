# Profile lifecycle

A named `.mprofile` contains a compiled specialization plus validation metadata. It does not include the capture bodies and does not make live browsing offline. Users inspect it with `mimic optimize --inspect NAME`; generated internal rules are not a configuration language to edit manually.

Profiles currently bind to the exact Mimic executable, engine and browser mode that validated them. A different build requires re-optimization. The artifact format and runtime identity are validated before installation; container integrity is checked.

A Page admits specialization after a known document URL, status and body digest match. Unknown documents and unknown request methods, URLs, kinds, bodies or explicitly supplied headers use ordinary Mimic. Requests bind to the originating document body and recorded source URL, including SPA hash/path states. New source states take the general path. Navigation revokes the previous Page admission. Current specialization is restricted to top-level document clients; child realms and worker clients use the general path. Classic execution exclusions additionally match the external script source identity. Changed script bytes execute normally.

Re-run Optimize with the same name to replace the profile after successful matched validation. An interrupted or rejected training does not replace the existing profile. Use `--output` for deployment exports. The exact build requirement also applies to exported files.

The CLI prints how many recorded states were validated. One state is evidence about one environment, not a promise of generalization. See [safety](safety.md).

A changed document can legitimately fall back even when the workload still
passes: the current guard compares the complete decoded document digest. Nonces,
dynamic navigation metadata and content can change it. General fallback can lose
trained acquisition savings; it does not restore effects already skipped before
a later mismatch. Multiple states expand evidence, not a proof of future states.
See [current results](results.md).
