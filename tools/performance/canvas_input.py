"""Diagnostic CDP input probe during canvas animation; no live networking."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import statistics
import subprocess
import time
import urllib.request

from evaluate_series import Client


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--kind", choices=["chrome", "mimic"], required=True)
    parser.add_argument("--port", type=int, required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if args.port <= 0:
        parser.error("Use a fixed nonzero CDP port")
    out = args.output.resolve()
    out.mkdir(parents=True, exist_ok=False)
    binary = args.binary.resolve()
    command = [str(binary)]
    if args.kind == "chrome":
        command += [
            f"--remote-debugging-port={args.port}",
            f"--user-data-dir={out / 'profile'}",
            "--no-first-run",
            "--no-default-browser-check",
            "--window-size=1280,800",
            "about:blank",
        ]
    else:
        command += ["-listen", f"127.0.0.1:{args.port}"]
    env = os.environ.copy()
    for key in list(env):
        if key.startswith("MIMIC_PROFILE_"):
            del env[key]
    env["MIMIC_DIAGNOSTICS"] = "0"
    workload = Path(__file__).with_suffix(".js").read_text(encoding="utf-8")
    journal, timings = [], []
    data = dict(
        kind=args.kind,
        command=command,
        binary=str(binary),
        binarySha256=hashlib.sha256(binary.read_bytes()).hexdigest(),
        workloadSha256=hashlib.sha256(workload.encode()).hexdigest(),
        classification="instrumented local canvas animation diagnostic",
        journal=journal,
        timings=timings,
    )
    log = (out / "process.log").open("w")
    startup = None
    if args.kind == "chrome" and os.name == "nt":
        startup = subprocess.STARTUPINFO()
        startup.dwFlags |= subprocess.STARTF_USESHOWWINDOW
        startup.wShowWindow = 1
        data["windowsShowWindow"] = "SW_SHOWNORMAL"
    process = subprocess.Popen(
        command, env=env, stdout=log, stderr=log, startupinfo=startup
    )
    client = None

    def evaluate(expression):
        response = client.call(
            "Runtime.evaluate", dict(expression=expression, returnByValue=True)
        )
        if "error" in response or "exceptionDetails" in response.get("result", {}):
            raise RuntimeError(response)
        return response["result"]["result"].get("value")

    try:
        for attempt in range(150):
            try:
                with urllib.request.urlopen(
                    f"http://127.0.0.1:{args.port}/json/version"
                ) as r:
                    data["version"] = json.load(r)
                with urllib.request.urlopen(f"http://127.0.0.1:{args.port}/json") as r:
                    targets = json.load(r)
                target = next(t for t in targets if t["type"] == "page")
                break
            except Exception:
                if process.poll() is not None:
                    raise RuntimeError("Browser exited before CDP was ready")
                time.sleep(0.1)
        else:
            raise TimeoutError("CDP startup")
        client = Client(target["webSocketDebuggerUrl"], journal)
        if args.kind == "chrome":
            client.call("Page.bringToFront")
            data["presentationControl"] = (
                "Page.bringToFront before the unmodified navigator observation"
            )
        data["presentation"] = evaluate("""({
  webdriver: navigator.webdriver,
  userAgent: navigator.userAgent,
  languages: navigator.languages,
  inner: [innerWidth, innerHeight],
  outer: [outerWidth, outerHeight],
  dpr: devicePixelRatio,
  focus: document.hasFocus(),
  visibility: document.visibilityState,
  secure: isSecureContext,
  isolated: crossOriginIsolated,
  webdriverDescriptor: Object.getOwnPropertyDescriptor(
    Navigator.prototype,
    'webdriver',
  )?.get?.toString(),
});""")
        if args.kind == "chrome":
            if data["version"].get("Browser") != "Chrome/152.0.7977.82":
                raise RuntimeError("Expected frozen Chrome 152")
            if data["presentation"]["webdriver"] is not False:
                raise RuntimeError("Unexpected automation exposure")
        client.call("Runtime.enable")
        client.call("Network.enable")
        client.call("Page.enable")
        evaluate(workload)
        time.sleep(2)
        data["beforeInput"] = evaluate("""({
  visibility: document.visibilityState,
  focus: document.hasFocus(),
  state: canvasProbe.state(),
});""")
        if (
            data["beforeInput"]["visibility"] != "visible"
            or not data["beforeInput"]["state"]["frames"]
        ):
            data["validity"] = (
                "inactive animation: capture retained, not latency comparison evidence"
            )
            raise RuntimeError(data["validity"])
        data["validity"] = "active animation diagnostic"
        for click in range(4):
            for event in ("mouseMoved", "mousePressed", "mouseReleased"):
                params = dict(type=event, x=150 + click * 60, y=180)
                if event != "mouseMoved":
                    params.update(
                        button="left",
                        buttons=1 if event == "mousePressed" else 0,
                        clickCount=1,
                    )
                started = time.perf_counter()
                response = client.call("Input.dispatchMouseEvent", params)
                timings.append(
                    dict(
                        event=params,
                        ms=(time.perf_counter() - started) * 1000,
                        response=response,
                    )
                )
                if "error" in response:
                    raise RuntimeError(response)
                time.sleep(0.05)
        data["state"] = evaluate("canvasProbe.running = false;\ncanvasProbe.state();")
        values = [item["ms"] for item in timings]
        data["summary"] = dict(
            meanMs=statistics.mean(values),
            medianMs=statistics.median(values),
            maxMs=max(values),
            totalMs=sum(values),
        )
        print(json.dumps(data["summary"]))
    finally:
        data["captureMetadata"] = dict(
            browserMode="headful" if args.kind == "chrome" else "runtime",
            environmentProfileId=(
                "chrome-152-windows-x64-headful-controlled-v1"
                if args.kind == "chrome"
                else "mimic-local-canvas-diagnostic"
            ),
            observedVersion=data.get("version"),
            observedPresentation=data.get("presentation"),
            launchArguments=command,
            executableSha256=data["binarySha256"],
            featureOverrides=[],
            instrumented=True,
            networkModified=False,
        )
        (out / "raw.json").write_text(json.dumps(data, indent=2), encoding="utf-8")
        if client:
            client.ws.close()
        process.terminate()
        try:
            process.wait(timeout=10)
        except subprocess.TimeoutExpired:
            process.kill()
            process.wait()
        log.close()


if __name__ == "__main__":
    main()
