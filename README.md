# Mimic: lightweight browser automation without Chromium

<p align="center">
  <img src="docs/assets/readme-hero.png" alt="Mimic — Run the web. Skip the rendering." width="1200">
</p>

<p align="center">
  <strong>Lightweight browser automation without Chromium.</strong><br>
  JavaScript automation and web scraping with Playwright, Puppeteer and CDP.
</p>

<p align="center">
  <a href="https://github.com/moreveal/mimic/releases/latest"><img src="https://img.shields.io/badge/status-public_beta-FFCA91?style=flat-square" alt="Public beta"></a>
  <a href="go.mod"><img src="https://img.shields.io/badge/Go-1.26.4%2B-00ADD8?style=flat-square&amp;logo=go&amp;logoColor=white" alt="Go 1.26.4+"></a>
  <a href="docs/getting-started.md"><img src="https://img.shields.io/badge/platforms-Windows%20%7C%20Linux-5988C7?style=flat-square" alt="Windows and Linux amd64"></a>
  <a href="docs/cdp-compatibility.md"><img src="https://img.shields.io/badge/automation-CDP-AC9FFF?style=flat-square" alt="CDP automation"></a>
</p>

<p align="center">
  <a href="#quick-start">Quick start</a> ·
  <a href="#benchmarks">Benchmarks</a> ·
  <a href="https://github.com/moreveal/mimic/releases/latest">Download</a> ·
  <a href="docs/getting-started.md">Documentation</a> ·
  <a href="docs/cdp-compatibility.md">CDP support</a>
</p>

## What is Mimic?

Mimic is a source-available public beta for JavaScript automation and web scraping.
It is a lightweight browser execution runtime that loads resources, executes JavaScript in V8, maintains DOM/style/layout state and
exposes it through the Chrome DevTools Protocol—without embedding Chromium,
opening a window or rendering pixels.

MIMIC stands for “Mimic Implements Modern Internet Compatibility.”

- Use familiar Playwright, Puppeteer and CDP clients.
- Run independent Pages concurrently with one event loop per Page.
- Observe canonical DOM, navigation, CSSOM and geometry state.
- Use native HTTP, HTTPS and SOCKS5 proxy profiles.
- Ship one standalone executable; users do not need Chrome, Go or Rust.

Mimic targets observable Chrome 152 behavior. It is a public beta: compatibility
is workload-dependent, and unsupported browser surfaces remain explicit.

## Quick start

Download a binary from [the latest release](https://github.com/moreveal/mimic/releases/latest),
then start the CDP endpoint:

```powershell
./mimic.exe -listen 127.0.0.1:9222
```

```sh
./mimic -listen 127.0.0.1:9222
```

Or run a clean source checkout on Windows or Linux amd64:

```sh
git clone https://github.com/moreveal/mimic.git
cd mimic
go run ./tools/runmimic -listen 127.0.0.1:9222
```

The source build requires Go 1.26.4+, Rust/Cargo, CGO and a C compiler. The
released executable has no Go/Rust/Cargo dependency. See
[setup and platform requirements](docs/getting-started.md) or the
[release build guide](tools/release/README.md).

### Connect with Playwright

```javascript
import { chromium } from "playwright-core";

const browser = await chromium.connectOverCDP("http://127.0.0.1:9222");
const context = browser.contexts()[0];
const page = await context.newPage();

await page.goto("https://books.toscrape.com/", { waitUntil: "load" });
console.log(await page.locator("h1").innerText());

await browser.close();
```

This example reads `All products` from [Books to Scrape](https://books.toscrape.com/),
a public web-scraping sandbox. External sites can change or become unavailable;
check the response and DOM if the expected element is missing.

### Advanced example: 100 separate identities

The [runnable profile example](examples/profile-contexts.mjs) processes 100 URLs
through separate Contexts. Each job gets a freshly generated profile, its own
cookies and optionally its own proxy. It closes its Context after extraction.
Eight live Pages is the default. Set `CONCURRENCY=100 HOLD_ALL_LIVE=1` to hold all
100 Pages simultaneously until every extraction finishes; this requires much
more RAM.

```javascript
// Inside a CDP browser connection; see examples/profile-contexts.mjs for the
// complete connection, navigation, extraction and error-handling code.
const jobs = Array.from({ length: 100 }, (_, index) => ({
  cookie: { name: "session", value: accountSessions[index], url: targetURL },
  proxy: proxies[index], // { server: "socks5://host:1080", username, password }
}));
await Promise.all(jobs.map(async (job) => {
  const { browserContextId, profileId } = await cdp.send("Mimic.createContext", {
    ...(job.proxy ? { proxy: job.proxy } : {}),
    disposeOnDetach: true,
  });
  try {
    await cdp.send("Storage.setCookies", {
      browserContextId,
      cookies: [job.cookie],
    });
    await scrapeInContext(cdp, browserContextId, profileId);
  } finally {
    await cdp.send("Target.disposeBrowserContext", { browserContextId });
  }
}));
```

Provide one proxy per job if every route must differ. `profile` is an opaque,
portable token that can be saved and reused; manual settings require explicit
`Mimic.importProfile` with `mode: "manual"`. See the
[profile contract and limits](docs/environment-profiles.md). Generated profiles
select paired GPU/font observation recipes, while Chrome 152 identity and the
network wire recipe remain fixed. Different seeds can select the same recipe.

See the [runnable examples](examples/README.md),
[supported CDP surface](docs/cdp-compatibility.md), and
[snapshot/capture guide](docs/getting-started.md).
The [Crawlee compatibility checkpoint](docs/compatibility/crawlee-playwright.md)
links to the tested fork and its bounded crawler example.

## Benchmarks

September 29 checkpoint against headless Chrome 152.0.7977.82 on Windows 11
x64, Intel i7-14700KF, 31.83 GiB RAM:

<p align="center">
  <a href="benchmark/runs/13-rss-20260929/public-summary.md"><img src="docs/assets/benchmark-story-20260929.png" alt="September 29 benchmark: 8.3 times less ready RSS, 6.0 times static throughput, and 5.5 times less active RSS than Chrome at 50 Pages" width="1200"></a>
</p>

**8.3× less ready RSS. 5.5× less active RSS and 6.0× throughput at 50 static
Pages.** All 12 correctness gates and 360 measured single-Page attempts passed.
The headline compares the completed 50-Page static series. These are fixture-
and machine-specific results, not universal claims; the full matrix includes
workloads where Chrome is faster.

[Results and methodology](benchmark/runs/13-rss-20260929/public-summary.md) ·
[Full report](benchmark/runs/13-rss-20260929/report.md) ·
[Reproduce](benchmark/README.md) ·
[Performance history](docs/performance/report.md)

## Current boundaries

Mimic reproduces browser observations needed by automation; it is not a visual
browser. It does not render screenshots or provide a complete Chromium/Web API
surface. Host fonts and OS services can affect observable results. CDP has no
authentication and is intended for trusted local clients.

Evaluate your workload against the
[compatibility notes](docs/compatibility.md) and
[CDP matrix](docs/cdp-compatibility.md).

## Documentation

- [Getting started and troubleshooting](docs/getting-started.md)
- [Product vision](docs/product-vision.md)
- [Architecture](docs/architecture.md)
- [CDP compatibility](docs/cdp-compatibility.md)
- [Chrome oracle policy](docs/oracle-policy.md)
- [Performance report](docs/performance/report.md)
- [Contributing](CONTRIBUTING.md)

Website: [mimic.boo](https://mimic.boo)

- [Run Playwright without Chromium](https://mimic.boo/playwright-without-chromium/)
- [Mimic versus Chromium for nonvisual extraction](https://mimic.boo/mimic-vs-chromium/)

Feedback: DM **`moreveal`** on Discord.

## License

Mimic is source-available under the [Prosperity Public License 3.0.0](LICENSE).
Noncommercial use is free; commercial use has a 30-day trial. Third-party
components retain their own licenses. Contributions require acceptance of the
[Mimic CLA](CLA.md).
