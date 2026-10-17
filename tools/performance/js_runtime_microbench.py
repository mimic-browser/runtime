"""Run synthetic JS/API microbenchmarks against existing loopback CDP servers.

Chrome reference runs require a directly launched headful frozen Chrome 152;
follow docs/oracle-policy.md and retain its launch receipt alongside the output.
Use --chrome only for a needed reference measurement, then reuse that capture.
No browser is launched and the fixture loads no network resources.
"""

import argparse
import asyncio
import hashlib
import json
from pathlib import Path
import statistics
import sys
from urllib.parse import urlsplit

import websockets

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from capture_cdp_protocol import Connection, read_endpoint, write_json


def loopback(value):
    parsed = urlsplit(value)
    if parsed.scheme != "http" or parsed.hostname not in {"127.0.0.1", "localhost", "::1"}:
        raise argparse.ArgumentTypeError("an HTTP loopback CDP endpoint is required")
    return value


async def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mimic", required=True, type=loopback)
    parser.add_argument("--chrome", type=loopback)
    parser.add_argument("--chrome-launch-receipt", type=Path)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--mode", choices=["standards", "quirks"], default="standards")
    parser.add_argument("--runs", type=int, default=9)
    parser.add_argument("--scale", type=int, default=1)
    parser.add_argument("--names", nargs="*")
    args = parser.parse_args()
    if args.chrome and args.chrome_launch_receipt is None:
        parser.error("--chrome-launch-receipt is required with --chrome")
    if args.runs < 2 or args.scale < 1:
        parser.error("at least two runs and a positive scale are required")
    if args.output.exists():
        parser.error("output exists; retain the previous capture and choose a new path")
    source = Path(__file__).with_suffix(".js").read_text(encoding="utf-8")
    receipt = {
        "fixtureSource": source,
        "fixtureSHA256": hashlib.sha256(source.encode()).hexdigest(),
        "mode": args.mode,
        "scale": args.scale,
        "names": args.names,
        "instrumentation": "CDP Runtime evaluation without Profiler; per-operation performance.now spans",
        "runtimes": {},
        "samples": [],
    }
    connections, targets, sockets = {}, {}, []

    def save():
        write_json(args.output, receipt)

    try:
        for name, endpoint in [("chrome", args.chrome), ("mimic", args.mimic)]:
            if endpoint is None:
                continue
            version = read_endpoint(endpoint, "/json/version")
            if name == "chrome" and version["Browser"] != "Chrome/152.0.7977.82":
                raise RuntimeError("the reference must be frozen Chrome 152.0.7977.82")
            browser_socket = await websockets.connect(version["webSocketDebuggerUrl"], max_size=None)
            sockets.append(browser_socket)
            browser = Connection(browser_socket)
            target = await browser.result("Target.createTarget", {"url": "about:blank"})
            targets[name] = browser, target
            tab = next(row for row in read_endpoint(endpoint, "/json/list") if row["id"] == target["targetId"])
            socket = await websockets.connect(tab["webSocketDebuggerUrl"], max_size=None)
            sockets.append(socket)
            client = Connection(socket)
            connections[name] = client
            if args.mode == "standards":
                await client.result("Runtime.evaluate", {"expression": "document.open();document.write('<!doctype html><html><head></head><body></body></html>');document.close()"})
            environment = await client.result("Runtime.evaluate", {
                "expression": "({webdriver:navigator.webdriver,descriptor:{get:Object.getOwnPropertyDescriptor(Navigator.prototype,'webdriver').get.toString(),configurable:Object.getOwnPropertyDescriptor(Navigator.prototype,'webdriver').configurable,enumerable:Object.getOwnPropertyDescriptor(Navigator.prototype,'webdriver').enumerable},ua:navigator.userAgent,languages:navigator.languages,compatMode:document.compatMode,viewport:{width:innerWidth,height:innerHeight,deviceScaleFactor:devicePixelRatio},window:{outerWidth,outerHeight},visibility:document.visibilityState,focus:document.hasFocus(),secureContextState:isSecureContext,isolationState:crossOriginIsolated})",
                "returnByValue": True,
            })
            if name == "chrome" and environment["result"]["value"]["webdriver"] is not False:
                raise RuntimeError("the reference exposes automation; correct its launch")
            receipt["runtimes"][name] = {"version": version, "environment": environment}
            if name == "chrome":
                launch = json.loads(args.chrome_launch_receipt.read_text(encoding="utf-8-sig"))
                observed = environment["result"]["value"]
                if launch.get("browserMode") != "headful" or not launch.get("arguments") or not launch.get("sha256"):
                    raise RuntimeError("the Chrome receipt must identify its headful launch arguments and executable hash")
                if any(flag.startswith(("--headless", "--enable-automation")) for flag in launch["arguments"]):
                    raise RuntimeError("automation/headless launch flags are not a normal reference")
                receipt["runtimes"][name]["captureMetadata"] = {
                    "chromeVersion": "152.0.7977.82",
                    "chromiumRevision": 1669021,
                    "chromiumCommit": "d04cdb24d67b081f6cf80200ffc5233f44b61109",
                    "v8Version": version["V8-Version"],
                    "platform": "windows-x64" if "Win64" in observed["ua"] else "linux-x64",
                    "browserMode": "headful",
                    "environmentProfileId": "chrome-152-headful-synthetic-js-local",
                    "commandLineFeatureOverrides": [flag for flag in launch["arguments"] if flag.startswith(("--enable-features", "--disable-features", "--js-flags", "--disable-blink-features"))],
                    "viewport": observed["viewport"],
                    "window": observed["window"],
                    "secureContextState": observed["secureContextState"],
                    "isolationState": observed["isolationState"],
                    "profileFreshness": launch.get("profileFreshness", "unspecified"),
                    "observations": observed,
                    "launchReceipt": launch,
                }
            save()
            result = await client.result("Runtime.evaluate", {"expression": source})
            if "exceptionDetails" in result:
                raise RuntimeError(result)
        options = json.dumps({"names": args.names, "scale": args.scale})
        for index in range(args.runs):
            names = list(connections)
            if index % 2:
                names.reverse()
            for name in names:
                result = await connections[name].result("Runtime.evaluate", {
                    "expression": "runRuntimeMicrobench(" + options + ")",
                    "returnByValue": True,
                })
                if "exceptionDetails" in result:
                    raise RuntimeError(result)
                receipt["samples"].append({"runtime": name, "index": index, "phase": "cold" if index == 0 else "warm", "rows": result["result"]["value"]})
                save()
        grouped = {}
        for sample in receipt["samples"]:
            if sample["phase"] == "warm":
                for row in sample["rows"]:
                    grouped.setdefault((sample["runtime"], row["name"]), []).append(row["ms"])
        summary = {}
        for (runtime, name), values in grouped.items():
            summary.setdefault(name, {})[runtime] = {"median_ms": statistics.median(values), "min_ms": min(values), "max_ms": max(values), "n": len(values)}
        receipt["summary"] = summary
        save()
        print(json.dumps(summary, indent=2))
    finally:
        for browser, target in targets.values():
            await browser.result("Target.closeTarget", target)
        for socket in sockets:
            await socket.close()


if __name__ == "__main__":
    asyncio.run(main())
