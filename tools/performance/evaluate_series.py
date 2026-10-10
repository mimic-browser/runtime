"""Sequential CDP evaluation probe; retain every response and event before analysis."""

import argparse
import hashlib
import http.server
import json
import os
from pathlib import Path
import statistics
import subprocess
import time
import threading
import urllib.request

import websocket


class Client:
    def __init__(self, url, journal):
        self.ws = websocket.create_connection(url, timeout=15, suppress_origin=True)
        self.id = 0
        self.url = url
        self.journal = journal

    def send(self, method, params=None):
        self.id += 1
        message = dict(id=self.id, method=method, params=params or {})
        self.journal.append(
            dict(
                socket=self.url,
                direction="send",
                at=time.perf_counter(),
                message=message,
            )
        )
        self.ws.send(json.dumps(message))
        return self.id

    def receive(self, command):
        while True:
            message = json.loads(self.ws.recv())
            self.journal.append(
                dict(
                    socket=self.url,
                    direction="receive",
                    at=time.perf_counter(),
                    message=message,
                )
            )
            if message.get("method") == "Fetch.requestPaused":
                self.send(
                    "Fetch.continueRequest",
                    dict(requestId=message["params"]["requestId"]),
                )
            if message.get("id") == command:
                return message

    def call(self, method, params=None):
        return self.receive(self.send(method, params))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--kind", choices=["chrome", "mimic"], required=True)
    parser.add_argument("--port", type=int, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument(
        "--trace",
        action="store_true",
        help="Mimic diagnostic control; not passive latency evidence",
    )
    parser.add_argument(
        "--navigation-url",
        help="Optional controlled fixture for navigation/Fetch overlap",
    )
    parser.add_argument(
        "--local-navigation",
        action="store_true",
        help="Use a delayed local script with Fetch interception",
    )
    parser.add_argument(
        "--small-only",
        action="store_true",
        help="Skip large payloads in diagnostic/follow-up captures",
    )
    args = parser.parse_args()
    if args.port <= 0:
        parser.error("Use a fixed nonzero debugging port")
    if args.trace and args.kind != "mimic":
        parser.error("--trace enables Mimic-specific diagnostics")
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
    data = dict(
        command=command,
        binary_sha256=hashlib.sha256(binary.read_bytes()).hexdigest(),
        kind=args.kind,
        diagnostic=args.trace,
        rows=[],
        journal=[],
        traces=[],
    )
    root = Path(__file__).resolve().parents[2]
    data["source"] = dict(
        revision=subprocess.check_output(
            ["git", "rev-parse", "HEAD"], cwd=root, text=True
        ).strip(),
        status=subprocess.check_output(
            ["git", "status", "--porcelain"], cwd=root, text=True
        ),
        probe_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
    )
    data["presentation"] = dict(
        mode="headful" if args.kind == "chrome" else "no rendering",
        os_window_visibility="not measured",
    )

    def save():
        temporary = out / "raw.json.tmp"
        temporary.write_text(json.dumps(data, separators=(",", ":")), encoding="utf-8")
        temporary.replace(out / "raw.json")

    save()
    env = os.environ.copy()
    env["MIMIC_PROFILE_CDP"] = "1" if args.trace else "0"
    log = (out / "process.log").open("w")
    process = subprocess.Popen(command, stdout=log, stderr=log, env=env)
    clients = []
    try:
        endpoint = f"http://127.0.0.1:{args.port}"
        deadline = time.monotonic() + 60
        while True:
            try:
                version = json.load(urllib.request.urlopen(endpoint + "/json/version"))
                targets = json.load(urllib.request.urlopen(endpoint + "/json"))
                if not any(t["type"] == "page" for t in targets):
                    raise RuntimeError("Waiting for initial Page")
                break
            except Exception:
                if time.monotonic() > deadline or process.poll() is not None:
                    raise
                time.sleep(0.1)
        data["version"] = version
        if args.kind == "chrome" and "152.0.7977.82" not in version["Browser"]:
            raise RuntimeError("Wrong Chrome reference")
        target = next(t for t in targets if t["type"] == "page")
        client = Client(target["webSocketDebuggerUrl"], data["journal"])
        clients.append(client)

        def series(label, expression, count=64, by_value=True, await_promise=True):
            rows = []
            for index in range(count):
                started = time.perf_counter()
                response = client.call(
                    "Runtime.evaluate",
                    dict(
                        expression=expression,
                        returnByValue=by_value,
                        awaitPromise=await_promise,
                        objectGroup="evaluate-series",
                    ),
                )
                row = dict(
                    case=label,
                    index=index + 1,
                    command_id=response["id"],
                    ms=(time.perf_counter() - started) * 1000,
                    response=response,
                )
                data["rows"].append(row)
                rows.append(row)
                if "error" in response or "exceptionDetails" in response.get(
                    "result", {}
                ):
                    raise RuntimeError(response)
            values = [r["ms"] for r in rows]
            print(
                label,
                "total",
                round(sum(values), 3),
                "p50",
                round(statistics.median(values), 3),
                "max",
                round(max(values), 3),
                flush=True,
            )
            client.call(
                "Runtime.releaseObjectGroup", dict(objectGroup="evaluate-series")
            )
            if args.trace:
                # The Page recorder is bounded. Drain between cases before
                # another series can evict early command attribution.
                data["traces"].append(
                    dict(case=label, response=client.call("Mimic.getTrace"))
                )
                client.call("Mimic.clearTrace")
            save()

        if args.trace:
            client.call("Mimic.startTrace")
        series("fresh-target", "({value:1})")
        probe = """({
  webdriver: navigator.webdriver,
  descriptor: Object.getOwnPropertyDescriptor(Navigator.prototype, 'webdriver')?.get?.toString(),
  ua: navigator.userAgent,
  languages: navigator.languages,
  outer: [outerWidth, outerHeight],
  inner: [innerWidth, innerHeight],
  dpr: devicePixelRatio,
  focus: document.hasFocus(),
  visibility: document.visibilityState,
})"""
        data["environment"] = client.call(
            "Runtime.evaluate", dict(expression=probe, returnByValue=True)
        )
        save()
        if (
            args.kind == "chrome"
            and data["environment"]["result"]["result"]["value"]["webdriver"]
        ):
            raise RuntimeError("Automation-exposed control")
        expressions = dict(
            short="({ value: 1 })",
            traversal="""JSON.stringify(
  Object.entries({
    a: { value: 1 },
    b: { value: 2 },
    c: { value: 3 },
  }).map(([key, value]) => ({
    key,
    keys: Object.keys(value),
    value: value.value,
  })),
)""",
            large="""Array.from({ length: 4096 }, (_, i) => ({
  index: i,
  value: 'abcdefghijklmnop',
}))""",
        )
        for label, expression in expressions.items():
            if args.small_only and label == "large":
                continue
            for by_value in [False, True]:
                for await_promise in [False, True]:
                    series(
                        f"{label}/value={by_value}/await={await_promise}",
                        expression,
                        by_value=by_value,
                        await_promise=await_promise,
                    )
            series(
                label + "/batched64",
                f"Array.from({{length:64}},()=>({expression}))",
                count=1,
            )
        # A local receive deadline does not cancel a CDP request. Keep the socket
        # open and discard its eventual response while matching subsequent IDs.
        client.ws.settimeout(0.05)
        pending = client.send(
            "Runtime.evaluate",
            dict(
                expression="new Promise(r=>setTimeout(()=>r(1),300))", awaitPromise=True
            ),
        )
        try:
            client.receive(pending)
            data["timeout_observed"] = False
        except websocket.WebSocketTimeoutException:
            data["timeout_observed"] = True
        client.ws.settimeout(15)
        series("after-client-timeout", "({value:1})")
        client.ws.settimeout(0.05)
        pending = client.send(
            "Runtime.evaluate",
            dict(expression="""(() => {
  const end = Date.now() + 300;
  while (Date.now() < end) {}
  return 1;
})()"""),
        )
        try:
            client.receive(pending)
        except websocket.WebSocketTimeoutException:
            pass
        client.ws.settimeout(15)
        series("after-synchronous-client-timeout", "({value:1})")
        browser = Client(version["webSocketDebuggerUrl"], data["journal"])
        clients.append(browser)
        context = browser.call("Target.createBrowserContext")["result"][
            "browserContextId"
        ]
        new_target = browser.call(
            "Target.createTarget", dict(url="about:blank", browserContextId=context)
        )["result"]["targetId"]
        client = Client(
            f"ws://127.0.0.1:{args.port}/devtools/page/{new_target}", data["journal"]
        )
        clients.append(client)
        if args.trace:
            client.call("Mimic.startTrace")
        series("fresh-context-target", "({value:1})")
        fixture = None
        if args.local_navigation:

            class Handler(http.server.BaseHTTPRequestHandler):
                def do_GET(self):
                    if self.path == "/slow.js":
                        time.sleep(0.5)
                        body = b"globalThis.fixtureLoaded=true"
                    else:
                        body = b'<html><head><script src="/slow.js"></script></head><body>fixture</body></html>'
                    self.send_response(200)
                    self.send_header(
                        "Content-Type",
                        "text/javascript" if self.path.endswith(".js") else "text/html",
                    )
                    self.send_header("Content-Length", str(len(body)))
                    self.end_headers()
                    self.wfile.write(body)

                def log_message(self, *unused):
                    pass

            fixture = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
            threading.Thread(target=fixture.serve_forever, daemon=True).start()
            args.navigation_url = f"http://127.0.0.1:{fixture.server_port}/"
        if args.navigation_url:
            client.call("Network.enable")
            client.call("Fetch.enable", dict(patterns=[dict(urlPattern="*")]))
            client.send("Page.navigate", dict(url=args.navigation_url))
            series("navigation-fetch", "({value:1})")
            terminal = (
                """new Promise((resolve) => {
  const poll = () => {
    if (globalThis.fixtureLoaded) resolve(true);
    else setTimeout(poll, 10);
  };
  poll();
})"""
                if args.local_navigation
                else "document.readyState"
            )
            data["navigation_terminal"] = client.call(
                "Runtime.evaluate",
                dict(
                    expression=terminal,
                    awaitPromise=True,
                    returnByValue=True,
                    timeout=10000,
                ),
            )
            client.call("Fetch.disable")
        if args.trace:
            data["trace"] = client.call("Mimic.getTrace")
            data["initial_trace"] = clients[0].call("Mimic.getTrace")
        if fixture:
            fixture.shutdown()
        save()
    finally:
        save()
        for client in clients:
            client.ws.close()
        process.terminate()
        process.wait(timeout=15)
        log.close()
        inventory = {
            p.name: hashlib.sha256(p.read_bytes()).hexdigest()
            for p in out.iterdir()
            if p.is_file()
        }
        (out / "hashes.json").write_text(
            json.dumps(inventory, indent=2), encoding="utf-8"
        )


if __name__ == "__main__":
    main()
