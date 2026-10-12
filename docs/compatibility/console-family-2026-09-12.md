# Console family compatibility, Chrome 152

The cross-realm follow-up fixes native brand classification at the reference
owner: browser-owned wrappers are not author Proxies. Function, Date, RegExp and
Error arguments passed between live consoles now receive the same message-text
conversion as local values. Author Proxies, including wrappers around foreign
functions, remain uncoerced. No object is cloned to discover its brand.

Frozen A/B controls also show that retained values from a removed iframe still
work in a live console, while the removed iframe's own console methods stop
before argument conversion. Console checks the existing Realm.inactive state;
no public DOM property or duplicate lifecycle state supplies that decision.
The cross-realm fixture covers both argument origins, null receivers, retained
console methods and native/proxy values. Ordinary and restored regressions pass.
Private independent matrices are retained in `.build/console-residual-delegated/`:
`before/` had 48 differing leaves; `verified/` has zero for connected receivers;
`retained/` exposed 96 inactive-receiver differences, and `lifecycle/` has zero
after the lifecycle guard. Every native A/B control has zero differences.

The Console namespace now owns its methods with Chrome's enumerable/writable/
configurable descriptors, zero arity, native names and `[object console]` tag.
Its immediate prototype is an empty object. The same implementation serves
Window, iframe and dedicated worker realms. Each realm owns its count/timer maps.

The package adds `dir`, `dirxml`, `table`, `trace`, `assert`, `clear`, and the
ordinary time/timeLog/timeEnd family, alongside existing logging, groups and
counts. Directory/table/groupEnd arguments do not undergo format substitutions;
trace/logging/group labels do. Empty logging calls emit nothing; trace and clear
have their standard default messages. Failed count/timer label conversion still
operates on the default label and then rethrows the original exception. Timers
use the existing realm monotonic clock; this is not a performance API change.

## Correction to the earlier VM diagnosis

Custom `toString` observation in `SbVZ3` is **not confined to Runtime.enable**.
Two fresh frozen headful Chrome 152.0.7977.82 processes, launched without any
remote-debugging flag or CDP connection, executed the independent fixture and
returned results by local HTTP POST. Both called ToString for function, Date,
RegExp and native Error console arguments; ordinary objects, arrays, maps, boxed
numbers and proxies were not coerced. Exceptions while producing console message
text were suppressed. Explicit `%s`/numeric format conversions still propagate
their original failures. The separate edge controls confirmed proxy traps stay
untouched and string-hint primitive conversion ordering.

Therefore ordinary console message storage performs the measured coercion.
V8 native brand predicates classify those values without inspecting author
properties or proxies; JS never guesses from a spoofable class name. Internal
classification failures propagate through the host callback. Other engines keep
the bounded script fallback and are not claimed equivalent to native V8 brands.

Runtime console preview is a second observation: with Runtime enabled, Chrome
materializes the native lazy Error.stack in the returned preview, which reads
Error.name once. Author stack accessors remain accessors, and object-valued
stacks are not coerced; frozen A/B controls verify both. A disabled / enabled /
disabled-again matrix separates this effect. This remains evidence of an
instrumented Chrome artifact, not the current Mimic contract. Under
[the passive-tooling rule](../oracle-policy.md), Mimic deliberately does not
materialize lazy Error stacks for Runtime inspection. Runtime's domain state
controls console event delivery; enabling it adds no author accessor, Proxy
trap or stack-formatting calls. Console format arguments remain live remote
values.

## Evidence and validation

`internal/browser/testdata/console_family_oracle.js` is synthetic public API code,
with no saved target program or payload tokens. The accompanying Chrome JSON
retains exact binary hash, version, observed viewport/window, origin, visibility,
secure/isolation state, fresh-profile provenance and probe hash. The no-CDP A/B
results are identical. The native/CDP matrix also has zero Chrome A/B differences;
after excluding three explicitly out-of-scope profiling members, all captured
shape/return/coercion/label observations agree with Mimic.

Tests compare the retained oracle in ordinary and restored bootstrap modes for
Window, iframe and worker. Additional tests cover native brands, throwing
descriptions, untouched proxies/plain objects, failed timer-label state
transitions. Current regressions compare unattached execution with Runtime
disabled, enabled, disabled again and re-enabled, including multiple sessions,
author accessors, Proxy traps, lazy stacks and microtask ordering. Existing
console count, formatting, group and live-object debugger tests pass; the broader bootstrap
observational-equivalence and worker-fetch checks pass. The old trace-only
`[function]` expectation was replaced with the measured function text, while its
plain-object non-coercion and explicit format-conversion assertions remain.

Private raw runs/scripts are retained outside the removable worktree under
`.build/console-compat-delegated/` in the original checkout: `no_cdp.py`,
`no-cdp-a.json`, `no-cdp-b.json`, `edges/`, `stack/`, and `verified/`. Initial no-CDP attempts
did not reach the fixture because the network service restarted during startup;
they provide no observations. The successful harness starts a local file that
delays navigation to the local HTTP fixture by three seconds. No sandbox or
feature override was added. HTTP teardown connection resets are transport
shutdown diagnostics, not fixture failures.

## Deliberate limits

`profile`, `profileEnd`, and `timeStamp` instrumentation are not added by this
package. Console table rendering, DevTools UI formatting, full remote previews
and stackTrace protocol enrichment remain outside this bounded repair. Numeric
timer durations are not frozen expectations. The Error.name preview regression
is superseded by passive inspection regressions. Unsafe serialization and Proxy
property traversal reject rather than execute author hooks. No target-specific
branch or payload answer is embedded in the implementation.
