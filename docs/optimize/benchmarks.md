# Optimization benchmark methodology

Compare default Mimic, a competent manual configuration and the actual installed generated profile. Use the same executable, workload assertions, capture states, metrics and fresh state for every variant.

Validate the workload in default Mimic before treating it as optimization evidence. Never weaken assertions after observing search results. Validate three baseline replays, then rotate variant order for at least five matched measurements. Retain all failures and unsupported outcomes.

Encoded HTTP body bytes are acquired body bytes, not physical wire traffic. Acquired responses are transport responses, not every attempted browser request. CPU measurements are browser process-tree CPU. RSS is sampled process memory, not exact heap ownership. Local replay timing excludes live server latency and is not a forecast of live wall time.

Search inventories are disabled during final matched measurements. Search cost and training/validation cost are separate from steady-state savings. A reported break-even against default does not establish an advantage over the manual baseline.

The original PoC showed document-only ties on SSR extraction, no acquisition gain on required async/React resources, and useful script execution reduction in an intercepted Wikipedia control that acquired zero HTTP bytes. Those results do not establish a general dynamic-site advantage. New results must be reported separately without rewriting the original evidence.

## Implementation-coupled requests

A request can be unnecessary for the business data but necessary for the
application's current implementation. For example, a route resolver may wait
for comments even when the workload extracts only the article. Blocking that
request can prevent navigation. A failed whole-resource removal does not prove
that every byte or side effect in the resource is necessary. The current search
cannot prune arbitrary module initializers or fabricate substitute API results.
See the benchmark report's remaining-request analysis before interpreting a tie
as a global minimum.

## Multiple states and heavy workloads

Report acquisition and correctness separately per state. Mixture medians can
hide input regressions. Separate initial live training from later offline build
validation. Inspect remaining work after strong Manual: experimentally required,
removable, and unexpressible by the current action/admission model are different
results. A local win that falls back on the next live document is not a live
acquisition win. Failed default scenarios are ineligible controls. See
[results](results.md).
