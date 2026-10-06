# Safety and generalization

A passing profile preserves behavior verified by the workload in the recorded states. It does not prove that future experiments, API responses, user state or bot challenges behave identically.

Route and request-input guards provide a useful general path for unknown environments. They are not universal rollback. Once a request has been blocked or author execution skipped, turning the profile off cannot restore its effects. HTML changes are accepted on recorded routes. Even an exactly matching request may become necessary in a new experiment, account state or bot challenge. This admission strategy deliberately trades exact-document rejection for useful empirical generalization; it does not detect every semantic site change. Input guards include the request body and explicit headers, but do not prove compatibility with changed implicit cookies or server state.

Unknown resources are acquired normally in live mode. Changed external script sources are executed normally. These recoverable admission decisions improve generalization but do not make every destructive removal safe. Apply profiles explicitly, retain ordinary assertions on live runs, and retrain when relevant states change.

Do not use a single successful navigation as evidence that extracted data is correct. Test the outputs and interactions you depend on. Record multiple representative states where practical. The current profile confidence is empirical and request-scoped; there is no universal JIT deoptimization system.

Captures contain real response headers and bodies, potentially including cookies, tokens and private data. Keep artifacts local unless you deliberately export them to an appropriately protected location.
