# Crawlee + Mimic compatibility checkpoint

Mimic can serve as the local browser for PlaywrightCrawler through the
[`moreveal/crawlee` fork](https://github.com/moreveal/crawlee). The fork owns the
optional `mimicPath` launcher; Mimic itself remains a CDP runtime. This is a
tested integration, not an upstream Crawlee feature or a claim that every
Playwright workflow is supported.

## Use upstream Crawlee without the fork

Upstream Crawlee's existing `remoteBrowser` option can connect to a Mimic
process over CDP. Start Mimic separately:

```powershell
E:/GitHub/mimic/.build/mimic.exe -listen 127.0.0.1:9222
```

Then configure `PlaywrightCrawler` with
`remoteBrowser: { endpoint: 'http://127.0.0.1:9222' }`. Crawlee connects and
manages its browser pages; the caller starts and stops the Mimic process. The
fork's optional `mimicPath` option adds that process lifecycle to Crawlee's
browser launcher. It is not required to use Mimic with upstream Crawlee.

We checked this path with a locally served page: a `PlaywrightCrawler` using
`remoteBrowser` and no `mimicPath` connected through `connectOverCDP`, read the
page title and text, and completed one request with no failures. The fork does
not change Crawlee's `remoteBrowser` implementation. This is a focused
compatibility check, not a full upstream Crawlee test run.

## Run the fork's example

Build Mimic from this repository (see [getting started](../getting-started.md))
and build the Crawlee fork with `pnpm install` and `pnpm build`. From the root
of the fork, run its [bounded book crawler](https://github.com/moreveal/crawlee/blob/master/scripts/mimic-book-crawler.mjs):

```powershell
$env:MIMIC_PATH = 'E:/GitHub/mimic/.build/mimic.exe'
node scripts/mimic-book-crawler.mjs
```

On Unix shells, export `MIMIC_PATH` with an absolute binary path. The example
defaults to two catalogue pages and 20 books. `MAX_PAGES`, `MAX_BOOKS`, and
`OUTPUT_DIR` adjust its limits and JSONL output. To check the fork's integration
tests:

```powershell
pnpm exec vitest run test/integration/mimic-playwright-crawler.test.ts
```

The launcher keeps Crawlee's BrowserPool, queue, hooks, sessions, retries, and
browser retirement. Its current boundary is documented in the fork's
[PlaywrightCrawler README](https://github.com/moreveal/crawlee/blob/master/packages/playwright-crawler/README.md#using-mimic-instead-of-chromium):
Chromium Playwright only; no `remoteBrowser` or Crawlee proxy configuration
with `mimicPath`. Mimic provides approximate PNG screenshots through standard
CDP, with no guarantee of visual accuracy; see [approximate screenshots](../approximate-screenshots.md).

## What was checked

On Windows amd64 on September 22, 2026, all eight fork integration tests
passed against a freshly built Mimic. They cover routing and enqueueing,
request hooks, DOM interaction, isolated contexts, session cookies, network
interception, redirects, retries, downloads, and launch options. The book
example saved 25 unique books across two catalogue pages without failed
requests; a later bounded rerun saved five of five books. A PlaywrightCrawler
Hacker News example processed three pages with the same sampled posts and links
as headless Chrome. A three-page `crawlee.dev` link crawl succeeded in Chrome
and on a repeat Mimic run. Its first Mimic run timed out once at 25 seconds on
`/python`; the page subsequently completed alone and in the repeat crawl, so
that observation is not a diagnosed persistent defect.

Mimic's [Chrome 152 differential corpus](../../compatibility/corpus/index.json)
also reported 15/15 cases with no differences at this checkpoint. The corpus
measures specific observations; it does not establish a success rate across
websites. In particular, canvas focus-ring geometry is approximate and the
focused `Path2D` overload remains explicitly unsupported.

## Related external-site and anti-bot checks

These are separate Mimic compatibility investigations, not Crawlee test cases:

| Check | Recorded result and limit |
| --- | --- |
| [BrowserScan](../browserscan-current-result.md) | The automated Chrome control and Mimic were both classified `Robot`; the page loaded and 20 selected differential tests matched. This is not evidence of evasion. |
| [Voxel / Cloudflare challenge](../voxel-benchmark.md) | Mimic ran challenge resources and remained usable but did not obtain `cf_clearance` in the controlled checkpoint. Chrome and Mimic received different challenge branches, so their progress cannot be compared as a deterministic pass/fail result. |
| [Iroshop challenge](../compatibility.md) | A recorded Mimic attempt received a clearance cookie, but subsequent navigations still got challenge HTTP 403. Cookie issuance alone was not passage. |

The [anti-bot gate methodology](antibot-gate-methodology.md) explains how to
separate browser-semantic differences from server policy, IP reputation, and
changing challenge programs. No site- or challenge-vendor-specific branches
were added to Mimic for these checks.
