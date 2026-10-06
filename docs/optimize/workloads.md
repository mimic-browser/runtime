# Workload contract

Use your ordinary assertions and exit codes. Assertions may occur anywhere in the program. No Mimic assertion API, language SDK or Playwright dependency is required.

```js
const result = await doWork();
assert.equal(result.price, expectedPrice);
assert.ok(result.products.length >= 20);
```

An exit status of zero is PASS. A nonzero exit, assertion failure, crash or deadline is failure. A replay request outside the recorded environment is UNSUPPORTED even if the client exits successfully. Corrupt artifacts are invalid evidence, not a semantic workload failure.

Mimic cannot determine whether your assertions cover the business requirement. A workload that only navigates successfully permits removal of everything else. Add assertions for data, interactions and navigation that actually matter.

Optimize tries three fresh baseline replays before searching. You do not need to certify full determinism beforehand. Concrete assertion failures or unsupported replay coverage prevent training and retain diagnostics. Passing workloads with varying background acquisition can proceed; small apparent savings require confirmation and final measurements report the range.

The runner chooses a private free port and sets `MIMIC_CDP_URL`, `MIMIC_ENDPOINT` and `PW_MIMIC_ENDPOINT`. `--endpoint-env NAME` supplies a different existing HTTP endpoint variable; `--websocket-env NAME` supplies the browser WebSocket URL. Normal Mimic can stay running on `:9222`. A client hardcoded to an endpoint must use `--listen` with that address (which must be free) or expose its existing endpoint setting. A successful client that contacted another instance is not a valid trial.

## Multiple workload states

If the workload already accepts inputs through environment variables, supply an optional states file:

```json
{
  "states": [
    { "name": "Product A", "env": { "PRODUCT_ID": "A" } },
    { "name": "Product B", "env": { "PRODUCT_ID": "B" } }
  ]
}
```

```sh
mimic optimize --name shop --states-file states.json -- node scraper.js
```

The command remains the same. Mimic records and caches each environment independently, validates three fresh replays per state, and tests each candidate across all states. Structured result baselines are also independent. Final matched measurements reject acquisition regressions in any state, even if another state improves. The report includes per-state results.

The optional file supports up to 32 named states. It does not reset external databases or generate meaningful test inputs for you. State inputs are passed only to the external command; endpoint/control variables belong to the runner. Treat input files as private if they contain secrets. More observed states increase evidence, not a guarantee of generalization.

For explicit offline evidence, provide one `--capture` per state in the same order. No live recording occurs with explicit captures. Without a states file, repeated `--capture` inputs repeat the same command inputs against different recorded environments.

If your program already writes JSON results, `--result-file PATH` or `--result-env NAME` enables baseline comparison. This is optional and does not replace assertions. Fresh browser state does not reset files, databases or services owned by the external workload.

Only network performed by Mimic is captured and replayed. The runner does not install an OS network sandbox around the client process. Direct HTTP calls made by your program or external services are outside the capture; use local fixtures for those operations during offline training.

Windows batch launchers need their ordinary interpreter, for example `mimic optimize -- cmd /d /c npm test`. Native executables, Node and Python processes run directly. Signal/native-fault crashes are distinguished when the platform exposes them; interpreter exceptions and other nonzero exits are workload FAIL.
