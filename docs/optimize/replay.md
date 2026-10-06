# Capture and local replay

A completed capture is a self-contained binary ZIP container with versioned metadata, bodies and integrity evidence. Recording spools response bytes directly to disk as the normal browser acquires them. Replay reads the required body rather than loading the entire capture into memory. Already acquired browser bodies remain available through normal cache and consumer paths.

Matching uses the request method, URL, headers, request-body identity and Context/request occurrence. Redirects, cookies, CORS and response consumers still execute normally. Browser-initiated requests also retain the initiating source and Page history phase, distinguishing ordinary SPA return navigation. Concurrent identical requests are supported when their complete recorded responses agree. Ambiguous repeated responses fail closed when requested; complete ambiguous evidence is retained for diagnosis.

A background response cancelled by its browser owner can contain only a received prefix. The capture records that prefix and cancellation boundary. Replay supplies the recorded headers/prefix, then waits for the real request Context to cancel; it never substitutes a successful end-of-body. Unexpected truncation, disk errors and arbitrary transport failures remain unsupported. This handles ordinary Page/Context teardown without requiring the workload to wait for irrelevant downloads.

Fresh public or immutable GET representations can be reused across non-Vary request headers. Authentication, payload-bearing GETs, Host, conditional/range requests, Vary, certificate policy and occurrence limits remain strict. This is HTTP representation reuse, not fuzzy matching of arbitrary APIs or silently replacing response data.

Optimizer replay never falls back to the internet. A missing request is an unsupported trial, not an invented empty response. The search may separately test an explicit block for that new fallback request; it must pass a new fully covered trial before acceptance.

When a supplied manual reference activates a new branch, managed training may record that reference once before search. Only new fresh immutable GET resources can extend the existing environment, and existing response bodies must agree. Both parent captures and their provenance are preserved; the resulting capture remains one self-contained binary file. Changed state or mutable new API responses cannot be combined this way. Explicit `--capture` inputs never trigger live recording. Advanced `--supplemental-capture FILE` supplies already recorded immutable branch evidence entirely offline.

A capture is limited to 512 MiB of recorded bodies and 10,000 requests; an individual replay body is limited to the loader’s 32 MiB boundary. Completed managed training artifacts are pruned to 20 entries or approximately 2 GiB, excluding active runs and installed profiles. Explicit input files are never pruned.

WebSocket replay, browser checkpoints, arbitrary external process state, virtual time and perfect random-number replay are not supported. Unsupported recording does not falsify ordinary live browser behavior.

Query timestamps are not evidence of incorrect page behavior. Exact replay matching can still lack their new values. `--volatile-query-key NAME` permits an explicitly identified nonsemantic query key to vary. Mimic does not guess which parameters are safe to ignore. Nonvolatile query order remains significant. Live profile application permits unknown requests through the general runtime.

Completed cached evidence is reused after fresh baseline replay. Uncovered background requests whose workload still passes are first explored as explicit exclusions locally. If the cached workload actually fails, managed training attempts one fresh live baseline before searching. Explicit `--capture` inputs never trigger this refresh. `--record` explicitly records a new environment. Advanced `--capture FILE` can supply multiple existing recorded states; it never triggers live recording.

Replay stability means the workload assertions pass repeatedly with covered requests. Background acquisition may still vary; small apparent savings then require another passing confirmation. Final measurement records the range rather than presenting noisy acquisition as an exact improvement.


An explicit manual reference may be probed separately after uncovered default
replay. Only fully covered passing reference trials can seed optimization; the
original unsupported run remains unsupported. Removing speculation can change
cookies or headers and require separate evidence. Mutable and Set-Cookie
responses are not silently merged as immutable resources. Installed guarded
profiles are validated again; uncovered final replay prevents installation.
