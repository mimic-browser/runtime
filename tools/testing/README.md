# Focused local tests

Run one package and an explicit test expression:

```text
python tools/testing/run_focused.py ./internal/browser --run '^TestRTCNativeDataSDPMatchesFrozenChrome152$'
python tools/testing/run_focused.py ./internal/network --run '^TestRTCProxyRelayEgressMatrix$' --race --json
```

On Windows the executable is rebuilt at a fixed package-specific path under
`%LOCALAPPDATA%/Mimic/test-binaries`. All checkouts use that same path, including
race builds. Windows Firewall can therefore reuse an existing permission for
the executable instead of asking again for each temporary Go test directory.
An initial permission request may still occur. The runner does not change
firewall settings.

The package binary is locked during compilation and execution so concurrent
worktrees cannot overwrite it. Each run rebuilds from the calling checkout and
executes from the package directory, preserving relative fixture paths. Other
package binaries can run independently. Linux and macOS use the corresponding
fixed directory under `$XDG_CACHE_HOME/mimic/test-binaries` or
`~/.cache/mimic/test-binaries`.

Use `--json` for standard Go JSON test events, `--race` for race instrumentation,
and `--timeout` to set the test deadline. Only focused tests belong in local
iterations; CI owns the full suite. Existing `go test` examples in semantic
documentation identify packages and test expressions; on a local Windows host,
pass that package and expression to this runner.
