# Completed navigation response timing

The snapshot navigation failure was not a missing response or a snapshot entry
identity error. A fresh reproduction retained the child's matching loader ID,
HTTP 200 response and complete body, but every transport phase and Duration
were zero. Fast local requests fit inside Go's coarse interrupt-time tick on
the reference host. Faster snapshot bootstrap made the symptom more apparent.

The loader now measures request, redirect, cache retrieval and transport phase
intervals using the existing platform-neutral `internal/monotime` abstraction,
also used by the Page scheduler. Response Duration and phase offsets share the
same transport start. Wall-clock cache expiration is unchanged. No minimum
responseEnd, artificial latency, snapshot-specific branch or OS dependency was
added to the network core; the existing clock provider handles platform details.

The retained frozen `TestPerformanceNetworkAndLifecycleMatchFrozenChrome`
passes in ordinary and restored realms after the change. A temporary diagnostic
also passed twelve repeated restored child navigations; raw before/after logs
remain in `.build/performance-navigation-{diagnostic,after}.txt`.
The permanent platform-neutral transport regression checks 32 short measured
operations: duration/completion must retain the elapsed interval and shifted
browser-visible completion cannot precede transport completion. Cache/timing
focused tests pass. Frozen navigation expectations and known streaming/opaque
boundaries were not modified.

The full suite, race checks and performance validation were not run for this
isolated change. Focused results do not substitute for those broader checks.
