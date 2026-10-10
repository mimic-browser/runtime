# Mimic v0.2.7 — Public Beta

This release reduces bootstrap and installed-font memory retention while preserving native callable behavior, lazy operation identity, and Page isolation.

- Avoid redundant JavaScript wrappers around native platform operations.
- Preserve snapshot function state without retaining compiled installation bytecode; invalidate snapshots created with the previous policy.
- Decode immutable installed-font coverage once per resource on demand, keeping shaping faces and author fonts owned by each Page.
- Prevent CDP Pages from auto-attaching to their own targets and reject unsafe Console eager evaluations.

The [memory checkpoint](benchmark/runs/14-memory-20261010/public-summary.md) documents active workload memory, CPU, latency and fixed-concurrency throughput recovered from saved captures, with frozen Chrome 152 references and workload-specific limits. Generated lazy operations retain the established publication order required by semantic installers; Navigator, SVG, camera/WebRTC and ordinary/restored realm checks cover these boundaries.

The current public projection also records a separate Optimize on/off acquisition workload. Snapshot bytecode clearing reduced static batch throughput by approximately 7% in a separate paired diagnostic; Wikipedia workflow checks showed no material slowdown. Compatibility remains workload-dependent.

Known capacity limitation: the final React-100 memory wave failed with a native access violation. Two later alternating control/candidate pairs completed all their React-100 waves, so the failure was not reproduced or attributed. React-100 remains uncertified; failed observations are excluded from successful memory comparisons. Static and CPU completed 100-Page series, and React completed 50 Pages.
