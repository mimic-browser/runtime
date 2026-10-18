# fhttp integration

The vendored base is v0.6.9 from
[bogdanfinn/fhttp](https://github.com/bogdanfinn/fhttp), retained with its upstream
license and tests. The module archive is pinned by Go's checksum
`h1:F9MsWR1K6JMRlXN4Q/MfjYmptr29nCdycQo+OPBG5U0=`.

Local HTTP/2 changes provide a connection-owned header priority controller and
browser-owned request priority metadata. Stream open and close notifications
share the connection's existing mutex; callbacks never execute application code.
The Chrome 152 profile supplies the dependency graph. Other profiles retain the
existing static HeaderPriority behavior. Author HTTP Priority headers remain
separate from the browser's scheduling metadata.

Initial resource metadata distinguishes parser-blocking scripts from async,
defer and dynamically inserted scripts; marked font requests use the highest
class. Supported element priority hints affect scheduling independently of an
author HTTP Priority header.

The current adapter assigns initial resource classes. It does not implement
viewport-based reprioritization or the complete lifecycle of Chrome priority
updates. Stream state is released on completion, cancellation or connection loss;
no Page or JavaScript state is retained by the dependency graph.
