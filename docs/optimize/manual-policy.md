# Manual policy and generated specialization

Manual ResourcePolicy remains supported. Generated resource actions compile into the same typed rule representation and decision engine. Execution-stage author suppression has a separate admission point; it is not disguised as a network permission.

An explicitly matching manual rule takes precedence over generated resource rules. This includes explicit allow rules. Manual updates preserve the installed generated plan and in-flight immutable policy snapshots.

For an honest comparison, `--manual-policy FILE` supplies a competent configuration for the workload. Without it, the CLI labels the comparison Reference: the visual/speculative presets are a convenient reference, not a claim that they are the strongest human policy. If those presets fail, the reference becomes normal Mimic.

SSR extraction may already be optimal with document-only. Automatic optimization should report that tie. Dynamic workloads may need some scripts and endpoints while safely excluding others. The optimizer searches exact resources, grouped removals, bounded combinations and classic execution exclusions rather than only trying presets.

Search starts from the passing reference and examines its remaining acquisition.
It ranks newly recorded fallback branches ahead of large required bundles,
recursively splits failing groups and reserves part of its budget for execution
stages and combinations. A passing candidate that acquires more data is not an
improvement. Fetch status/headers may be kept without acquiring its body when
body consumption is unnecessary; reading an excluded body errors explicitly.
Whole-request elimination is preferred whenever the workload permits it.

An uncovered manual comparison is not automatically a semantic failure. If
coverage exclusions are needed, the table explicitly labels it Manual+repair.
That adjusted comparison must not be presented as an advantage over the original
competent manual policy. Recorded immutable branch evidence can make the original
reference measurable without altering its rules.
