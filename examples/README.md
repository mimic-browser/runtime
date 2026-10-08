# Run real automation against Mimic

These examples use the released executable. No Chrome download or browser
installation is needed. Install Node.js 22+ for the clients.

From this directory:

```sh
npm ci
```

Start Mimic in another terminal (adjust the executable path):

```sh
# Linux
/path/to/mimic --listen 127.0.0.1:9222
```

```powershell
# Windows
C:\path\to\mimic.exe --listen 127.0.0.1:9222
```

Start the local demo shop and leave it running:

```sh
npm run fixture
```

In another terminal, choose a workflow:

| Command | What it does |
| --- | --- |
| `npm run playwright` | Fills a quantity, clicks a button, waits for fetch and DOM updates; prints `126 USD`. |
| `npm run puppeteer` | Reads content and verifies language/viewport and explicit CDP timezone/theme emulation. |
| `npm run concurrency` | Runs 10 pages concurrently, checks separate window state, collects fetch results, closes every page. |
| `npm run profiles` | Scrapes 100 jobs with separate generated Context profiles and cookies over raw CDP, at most 8 live contexts; streams results and disposes each context. |

`CONCURRENCY=100 HOLD_ALL_LIVE=1` keeps all 100 contexts live together until
every extraction completes and needs much more RAM. Without the barrier,
`CONCURRENCY=100` only sets the upper limit.
On PowerShell, run `$env:CONCURRENCY='100'; $env:HOLD_ALL_LIVE='1'; npm run profiles`.
The default cookie is a unique demonstration value for each task.
`COOKIES_JSON` accepts 100 arrays of CDP cookies, one per task, for real account
sessions; cookies without `url` use `TARGET_URL`. Cookie values are not printed.
`FINGERPRINT_SEED_PREFIX` optionally makes generated profiles reproducible;
without it each Context gets a fresh random seed. The example retries a rare
generated-profile collision so its 100 generated profile IDs are distinct.
`PROXIES_JSON` accepts an array of `{server, username?, password?}` objects;
job `i` uses entry `i % proxies.length`, so provide 100 entries for 100 distinct
routes. `RESOURCE_POLICY=dataExtraction` optionally blocks image/font/media
requests and can change page observations. `PROFILE_MODE=manual` demonstrates
explicit manual import (the same manual environment for every isolated context).
Generated profiles vary window position/size, preferences and output audio rate,
and select among paired GPU/font observation recipes. Chrome 152 identity and
the network wire recipe remain fixed. See
[profile limits](../docs/environment-profiles.md).

The fixture uses localhost so the examples are reproducible without a third-party
website. `MIMIC_URL` changes the CDP endpoint. `TARGET_URL` changes the fixture
address; the form examples expect the same demo-shop page structure.

## Verify an installation in one command

This starts its own local fixture and Mimic process on available ports, uses a
fresh native cache, runs all three examples, and cleans up afterward. It does
not need either of the manually started processes above.

```sh
npm run verify -- /absolute/path/to/mimic
```

```powershell
npm run verify -- C:\absolute\path\to\mimic.exe
```

Clients are pinned in `package-lock.json`: Playwright Core 1.63.0 and Puppeteer
Core 25.10.0. These scenarios are verified on Windows amd64 and Ubuntu 24.04
amd64 under WSL2 for the beta release. They do not establish full Playwright or
Puppeteer compatibility. PNG screenshots are available through standard CDP,
with no guarantee of visual accuracy; see [approximate screenshots](../docs/approximate-screenshots.md).
Rendered PDFs are not supported.

The files in this directory are [MIT licensed](LICENSE) so you can adapt them.
The Mimic executable is governed by the authoritative project [LICENSE](../LICENSE).

For a separate Crawlee PlaywrightCrawler example and its measured limits, see
the [Crawlee compatibility checkpoint](../docs/compatibility/crawlee-playwright.md).
