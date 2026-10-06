# Safety and generalization

A passing profile preserves behavior verified by the workload in the recorded states. It does not prove that future experiments, API responses, user state or bot challenges behave identically.

Document and request guards provide a useful general path for unknown environments. They are not universal rollback. Once a request has been blocked or author execution skipped, turning the profile off cannot restore its effects. An unchanged document can still refer to changed resources or receive different API data. Input guards include the request body and explicit headers, but do not prove compatibility with changed implicit cookies or server state.

Unknown resources are acquired normally in live mode. Changed external script sources are executed normally. These recoverable admission decisions improve generalization but do not make every destructive removal safe. Apply profiles explicitly, retain ordinary assertions on live runs, and retrain when relevant states change.

Do not use a single successful navigation as evidence that extracted data is correct. Test the outputs and interactions you depend on. Record multiple representative states where practical. The current profile confidence is empirical and document-guarded; there is no universal JIT deoptimization system.

Captures contain real response headers and bodies, potentially including cookies, tokens and private data. Keep artifacts local unless you deliberately export them to an appropriately protected location.
