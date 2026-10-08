<p align="center">
  <img src="docs/assets/readme-hero.png" alt="Mimic — Browser automation without Chromium. The blue Mimic mascot starts a simple web-to-JavaScript-to-data automation flow." width="1200">
</p>

<p align="center">
  <strong>Lightweight browser automation without Chromium.</strong><br>
  JavaScript automation and web scraping with Playwright, Puppeteer and CDP.
</p>

<p align="center">
  <a href="https://github.com/mimic-browser/runtime/releases/latest"><img src="https://img.shields.io/badge/status-public_beta-FFCA91?style=flat-square" alt="Public beta"></a>
  <a href="go.mod"><img src="https://img.shields.io/badge/Go-1.26.4%2B-00ADD8?style=flat-square&amp;logo=go&amp;logoColor=white" alt="Go 1.26.4+"></a>
  <a href="docs/getting-started.md"><img src="https://img.shields.io/badge/platforms-Windows%20%7C%20Linux-5988C7?style=flat-square" alt="Windows and Linux amd64"></a>
  <a href="docs/cdp-compatibility.md"><img src="https://img.shields.io/badge/automation-CDP-AC9FFF?style=flat-square" alt="CDP automation"></a>
</p>

<p align="center">
  <a href="#quick-start">Quick start</a> ·
  <a href="#benchmarks">Benchmarks</a> ·
  <a href="https://github.com/mimic-browser/runtime/releases/latest">Download</a> ·
  <a href="docs/getting-started.md">Documentation</a> ·
  <a href="docs/cdp-compatibility.md">CDP support</a>
</p>

Mimic runs browser automation without a Chromium process. It loads pages, executes
JavaScript in V8, and exposes DOM, CSS, navigation and page state over CDP. It is
built for extraction and automation where browser behavior matters but pixels do not.

**Use the tools you know.** The [Mimic SDK](https://github.com/mimic-browser/sdk)
manages the runtime and works alongside real Playwright, Puppeteer and other
framework objects. You can also start the binary yourself and connect with an
ordinary CDP client.

## Quick start

Install the Node SDK from GitHub and add Playwright as an optional client:

```sh
npm install git+https://github.com/mimic-browser/sdk.git playwright-core@1.63.0
```

Save this as `scrape.mjs` and run it with `node scrape.mjs`:

```javascript
import { launch } from "mimic-browser/playwright";

const session = await launch();
try {
  const context = await session.newContext();
  const page = await context.newPage();
  await page.goto("https://example.com");
  console.log(await page.title());
} finally {
  await session.close();
}
```

The first launch downloads a verified runtime for supported hosts; later launches
reuse the local cache. The SDK also has
[Python, Go, .NET and other integrations](https://github.com/mimic-browser/sdk#choose-your-language-and-client).

### Bring your own CDP client

[Download the standalone binary](https://github.com/mimic-browser/runtime/releases/latest)
and start its local endpoint (`mimic.exe` on Windows):

```sh
./mimic -listen 127.0.0.1:9222
```

Connect through Playwright’s `chromium.connectOverCDP()`, Puppeteer’s
`puppeteer.connect()`, or another CDP client. No SDK is required for this path.
See the [direct Playwright example](examples/playwright.mjs) and
[CDP support](docs/cdp-compatibility.md).

## Built for repeated work

Run independent Pages concurrently, use your own proxies and Contexts, and
keep familiar automation code. For a repeatable workload, `mimic optimize` can
learn a reusable resource profile from your existing assertions:

```sh
mimic optimize --name shop -- node scraper.js
mimic --profile shop
```

[How Optimize works](docs/optimize/index.md) ·
[Runnable examples](examples/README.md) ·
[Profile and proxy options](docs/environment-profiles.md)

## Benchmarks

<p align="center">
  <a href="benchmark/runs/13-rss-20260929/public-summary.md"><img src="docs/assets/benchmark-story.png" alt="September 29, 2026 local benchmark against Chrome 152: 8.3 times less ready RSS; at 50 static Pages, 6.0 times throughput and 5.5 times less active RSS." width="1200"></a>
</p>

At the September 29, 2026 checkpoint, Mimic used **8.3× less ready RSS**.
In the completed 50-Page static series it delivered **6.0× throughput** with
**5.5× less active RSS** than Chrome 152. All 12 correctness gates and 360
measured single-Page attempts passed. These are controlled, machine-specific
fixtures; Chrome is faster in some other workloads.

[Results and methodology](benchmark/runs/13-rss-20260929/public-summary.md) ·
[Reproduce](benchmark/README.md) ·
[Current performance notes](docs/performance/report.md)

## Know the boundary

Mimic targets observable Chrome 152 behavior. It is a public beta with
workload-dependent compatibility. It does not render screenshots or implement
the full Chromium/Web API surface. Test your workload against the
[compatibility notes](docs/compatibility.md); keep the unauthenticated CDP
endpoint on a trusted local interface.

[Getting started](docs/getting-started.md) ·
[Architecture](docs/architecture.md) ·
[Contributing](CONTRIBUTING.md) ·
[Website](https://mimic.boo)

## License

Mimic is source-available under the [Prosperity Public License 3.0.0](LICENSE).
Noncommercial use is free; commercial use has a 30-day trial.
Contributions require the [Mimic CLA](CLA.md).
