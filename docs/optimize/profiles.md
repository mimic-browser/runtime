# Profile lifecycle

A named `.mprofile` contains a compiled specialization plus validation metadata. It does not include the capture bodies and does not make live browsing offline. Users inspect it with `mimic optimize --inspect NAME`; generated internal rules are not a configuration language to edit manually.

Profiles currently bind to the exact Mimic executable, engine and browser mode that validated them. A different build requires re-optimization. The artifact format and runtime identity are validated before installation; container integrity is checked.

A Page admits specialization after a recorded document URL and HTTP status match. The HTML body may change: text, prices, markup and nonces do not revoke all learned network decisions. Each generated decision is still limited to a recorded originating document URL, source URL (including SPA path/hash state), request URL, method, kind, body and explicitly supplied headers. Unknown requests take the ordinary path individually; they do not disable other known decisions. No URL path similarity, automatic query stripping or payload/header normalization is inferred. Only explicitly declared volatile query keys are normalized.

Document digests remain capture provenance, not live admission conditions. Navigation revokes admission before loading another document. Child realms and workers take the general path. Classic execution exclusions additionally require recorded script coverage on this route and an exact external script URL/source identity; changed script bytes execute normally. This does not prove that an unchanged resource remains unnecessary after a site change. Keep assertions in live workloads.

Re-run Optimize with the same name to replace the profile after successful matched validation. An interrupted or rejected training does not replace the existing profile. Use `--output` for deployment exports. The exact build requirement also applies to exported files.

The CLI prints how many recorded states were validated. One state is evidence about one environment, not a promise of generalization. See [safety](safety.md).

Changing HTML alone no longer loses trained savings. A new route, status, source state or request input can still fall back. This is intentionally narrower than applying broad learned categories to every future request. Previously generated document-guarded profiles must be retrained; they are not silently reinterpreted. See [safety](safety.md) and [the dated evaluation](results.md), whose historical live results used the earlier admission rule.
