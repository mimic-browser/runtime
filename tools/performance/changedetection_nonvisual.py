"""Bounded local monitoring evaluation using actual upstream HTML filters.

This is not the full changedetection application or stock screenshot fetcher.
Run with Python deps: psutil, loguru, beautifulsoup4==4.14.3,
inscriptis==2.7.5, lxml==6.1.3, elementpath==5.1.1.
"""
import argparse
import hashlib
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import importlib
import json
import os
from pathlib import Path
import statistics
import subprocess
import sys
import tempfile
import threading
import time
from urllib.parse import parse_qs, urlparse
import urllib.request

import psutil

REVISION = "0e0566721b1c483dcf7ae548210ee10532d9b181"
REPO = Path(__file__).resolve().parents[2]
CSS = "#product .name, #product .price, #product .stock"
XPATH = "//*[@id='product']/*[@class='name' or @class='price' or @class='stock']"
EXPECTED = [
    "Widget\n\n  USD 49.99\n\n  Out of stock",
    "Widget\n\n  USD 49.99\n\n  Out of stock",
    "Widget\n\n  USD 39.99\n\n  Out of stock",
    "Widget\n\n  USD 39.99\n\n  In stock",
    "Widget\n\n  USD 39.99\n\n  In stock",
    "Widget edition B\n\n  USD 39.99\n\n  In stock",
]


class Fixture(BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def do_GET(self):
        parsed = urlparse(self.path)
        if parsed.path == "/watch":
            body = (REPO / "tools/performance/changedetection_fixture.html").read_bytes()
            content_type = "text/html; charset=utf-8"
        elif parsed.path == "/changedetection_fixture.js":
            body = (REPO / "tools/performance/changedetection_fixture.js").read_bytes()
            content_type = "application/javascript"
        elif parsed.path == "/state":
            revision = int(parse_qs(parsed.query)["revision"][0])
            state = {"product": {
                "name": "Widget edition B" if revision == 3 else "Widget",
                "price": "USD 49.99" if revision == 0 else "USD 39.99",
                "stock": "In stock" if revision >= 2 else "Out of stock",
            }, "catalog": [{"name": f"Unwatched item {i}", "description": "Other catalog text " * 12} for i in range(100)]}
            body = json.dumps(state).encode()
            content_type = "application/json"
        else:
            self.send_error(404)
            return
        self.send_response(200)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.end_headers()
        self.wfile.write(body)


class TreeMetrics:
    """Sample only the launched backend and descendants; RSS may double count shared pages."""
    def __init__(self, pid):
        self.root = psutil.Process(pid)
        self.known = {}
        self.cpu = {}
        self.rss_peak = 0
        self.last_rss = 0
        self.samples = 0
        self.errors = 0
        self.stop = threading.Event()
        self.thread = threading.Thread(target=self.poll, daemon=True)
        self.thread.start()

    def poll(self):
        while not self.stop.is_set():
            try:
                processes = [self.root] + self.root.children(recursive=True)
                rss = 0
                for process in processes:
                    try:
                        key = (process.pid, process.create_time())
                        self.known[key] = process
                        memory = process.memory_info()
                        cpu = process.cpu_times()
                        self.cpu[key] = max(self.cpu.get(key, 0), cpu.user + cpu.system)
                        rss += memory.rss
                    except (psutil.NoSuchProcess, psutil.AccessDenied):
                        self.errors += 1
                self.last_rss = rss
                self.rss_peak = max(self.rss_peak, rss)
                self.samples += 1
            except (psutil.NoSuchProcess, psutil.AccessDenied):
                self.errors += 1
            self.stop.wait(0.025)

    def summary(self):
        return {"treeCpuSecondsSampled": sum(self.cpu.values()),
                "peakTreeRssBytes": self.rss_peak, "lastTreeRssBytes": self.last_rss,
                "processesSeen": len(self.known), "samples": self.samples, "samplingErrors": self.errors}

    def close(self):
        self.stop.set()
        self.thread.join(timeout=2)


def prepare_upstream(output, cache):
    namespace = output / "upstream/changedetectionio"
    namespace.mkdir(parents=True)
    source_files = ["changedetectionio/html_tools.py", "changedetectionio/strtobool.py"]
    for name in source_files:
        cached = cache / name.replace("/", "__") if cache else None
        if cached and cached.exists():
            data = cached.read_bytes()
        else:
            url = f"https://raw.githubusercontent.com/dgtlmoon/changedetection.io/{REVISION}/{name}"
            data = urllib.request.urlopen(url, timeout=20).read()
        (namespace / Path(name).name).write_bytes(data)
    # Namespace package only: the full app __init__ is not bootstrapped. The
    # two upstream modules above are executed unchanged, without AST extraction.
    sys.path.insert(0, str(namespace.parent))
    return importlib.import_module("changedetectionio.html_tools")


def terminate(process):
    if os.name == "nt":
        subprocess.run(["taskkill", "/PID", str(process.pid), "/T", "/F"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    else:
        for child in psutil.Process(process.pid).children(recursive=True):
            child.terminate()
        process.terminate()
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        process.kill()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mimic", type=Path, required=True)
    parser.add_argument("--chrome", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--source-cache", type=Path)
    parser.add_argument("--repeats", type=int, default=3)
    parser.add_argument("--gate-only", action="store_true")
    parser.add_argument("--chrome-mode", choices=["headful", "headless"], default="headful")
    args = parser.parse_args()
    if not 1 <= args.repeats <= 3:
        parser.error("repeats must be between 1 and 3")
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=False)
    upstream = prepare_upstream(output, args.source_cache)
    # Browser-process cold starts are measured with the shared Python filtering
    # dependencies already imported, equally for both backends.
    prewarm_start = time.perf_counter()
    prewarm_html = '<section id="product"><p class="name">Widget</p></section>'
    upstream.html_to_text(upstream.include_filters(CSS, prewarm_html, True))
    upstream.html_to_text(upstream.xpath_filter(XPATH, prewarm_html, True))
    prewarm_ms = (time.perf_counter() - prewarm_start) * 1000
    server = ThreadingHTTPServer(("127.0.0.1", 9240), Fixture)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    binary = {"mimic": args.mimic.resolve(), "chrome": args.chrome.resolve()}
    metadata = {"upstreamRevision": REVISION, "python": sys.version, "platform": sys.platform,
                "binaryHashes": {name: hashlib.sha256(path.read_bytes()).hexdigest() for name, path in binary.items()},
                "screenshots": False, "css": CSS, "xpath": XPATH, "sequence": EXPECTED,
                "chromeMode": args.chrome_mode,
                "filterDependenciesPrewarmMs": prewarm_ms,
                "mode": "local fixture, standalone opt-in fetch plus unchanged upstream filtering",
                "memoryBoundary": "sampled backend process-tree RSS sum; shared pages may be counted repeatedly",
                "cpuBoundary": "sampled backend process-tree accumulated CPU; short-lived descendants may be missed"}
    (output / "metadata.json").write_text(json.dumps(metadata, indent=2))
    rows = []
    try:
        for repeat in range(args.repeats):
            for name in (["chrome", "mimic"] if repeat % 2 == 0 else ["mimic", "chrome"]):
                run = output / f"{repeat}-{name}"
                run.mkdir()
                profile = tempfile.mkdtemp(prefix="changedetection-local-") if name == "chrome" else None
                launch_args = [str(binary[name])]
                if name == "chrome":
                    launch_args += ["--remote-debugging-port=9336", "--user-data-dir=" + profile,
                                    "--no-first-run", "--no-default-browser-check", "--window-size=1280,800", "about:blank"]
                    if args.chrome_mode == "headless":
                        launch_args.insert(-1, "--headless=new")
                    port = 9336
                else:
                    launch_args += ["-listen", "127.0.0.1:9233", "-navigation-timeout", "20s"]
                    port = 9233
                (run / "launch.json").write_text(json.dumps({"arguments": launch_args, "profile": profile,
                    "browserMode": args.chrome_mode if name == "chrome" else "renderer-free", "osWindow": "hidden startup request"}, indent=2))
                startup = None
                if os.name == "nt":
                    startup = subprocess.STARTUPINFO()
                    startup.dwFlags |= subprocess.STARTF_USESHOWWINDOW
                    startup.wShowWindow = 0
                backend_log = (run / "backend.log").open("w")
                started = time.perf_counter()
                process = subprocess.Popen(launch_args, startupinfo=startup, stdout=backend_log, stderr=backend_log)
                metrics = TreeMetrics(process.pid)
                client = None
                watchdog = None
                try:
                    deadline = time.monotonic() + 20
                    endpoint = f"http://127.0.0.1:{port}"
                    while True:
                        try:
                            version = json.loads(urllib.request.urlopen(endpoint + "/json/version", timeout=1).read())
                            break
                        except Exception:
                            if process.poll() is not None or time.monotonic() >= deadline:
                                raise
                            time.sleep(0.05)
                    ready_ms = (time.perf_counter() - started) * 1000
                    ready_rss = metrics.last_rss
                    client = subprocess.Popen(["node", "tools/runtimecheck/changedetection_nonvisual.cjs", endpoint,
                        "http://127.0.0.1:9240", str(run)], cwd=REPO, stdout=subprocess.PIPE,
                        stderr=(run / "client-errors.log").open("w"), stdin=subprocess.PIPE, text=True)
                    watchdog = threading.Timer(45, lambda watched=client: terminate(watched) if watched.poll() is None else None)
                    watchdog.start()
                    snapshots = []
                    teardown = None
                    with (run / "client-events.jsonl").open("w") as events:
                        for line in client.stdout:
                            events.write(line)
                            event = json.loads(line)
                            if event["type"] == "teardown":
                                teardown = event
                            if event["type"] == "snapshot":
                                event["elapsedSinceLaunchMs"] = (time.perf_counter() - started) * 1000
                                filter_start = time.perf_counter()
                                html = (run / f"snapshot-{event['index']}.html").read_text(encoding="utf-8")
                                filtered = {}
                                for method in ["css", "xpath"]:
                                    selected = upstream.include_filters(CSS, html, True) if method == "css" else upstream.xpath_filter(XPATH, html, True)
                                    filtered[method] = upstream.html_to_text(selected).strip()
                                event["upstreamFilterMs"] = (time.perf_counter() - filter_start) * 1000
                                event["filtered"] = filtered
                                expected = EXPECTED[event["index"]]
                                if event["errors"] or event["status"] != 200 or any(value != expected for value in filtered.values()):
                                    raise RuntimeError(f"Correctness failure {name}: {event}")
                                event["changed"] = event["index"] > 0 and filtered["css"] != EXPECTED[event["index"] - 1]
                                if event["changed"] != [False, False, True, True, False, True][event["index"]]:
                                    raise RuntimeError("Unexpected change-detection sequence")
                                event["checkMs"] = event["totalMs"] + event["upstreamFilterMs"]
                                event["backendMetrics"] = metrics.summary()
                                snapshots.append(event)
                                client.stdin.write("continue\n")
                                client.stdin.flush()
                    if client.wait(timeout=5) != 0 or len(snapshots) != 6:
                        raise RuntimeError(f"Incomplete client run: {name}; inspect {run}")
                    watchdog.cancel()
                    if not teardown or teardown["initialPageCount"] != teardown["remainingPageCount"]:
                        raise RuntimeError("Page teardown count did not return to baseline")
                    time.sleep(0.1)  # At least four metric samples after all check Pages closed.
                    retained_rss = metrics.last_rss
                    row = {"backend": name, "repeat": repeat, "readyMs": ready_ms, "readyTreeRssBytes": ready_rss,
                           "coldFirstFetchMs": snapshots[0]["elapsedSinceLaunchMs"],
                           "retainedTreeRssBytes": retained_rss, "version": version,
                           "teardown": teardown,
                           "metrics": metrics.summary(), "snapshots": snapshots}
                    (run / "result.json").write_text(json.dumps(row, indent=2))
                    rows.append(row)
                    print(json.dumps({"backend": name, "repeat": repeat, "correct": 6, "readyMs": round(ready_ms),
                        "warmMedianMs": round(statistics.median(x["totalMs"] for x in snapshots[1:])),
                        "peakTreeRssMiB": round(row["metrics"]["peakTreeRssBytes"] / 1048576, 1)}), flush=True)
                finally:
                    if watchdog:
                        watchdog.cancel()
                    if client and client.poll() is None:
                        terminate(client)
                    metrics.close()
                    terminate(process)
                    backend_log.close()
            if args.gate_only:
                break
    finally:
        server.shutdown()
        server.server_close()
    summary = {}
    for name in binary:
        selected = [row for row in rows if row["backend"] == name]
        warm = [snapshot for row in selected for snapshot in row["snapshots"][1:]]
        summary[name] = {"correctSnapshots": sum(len(row["snapshots"]) for row in selected),
            "coldMsMedian": statistics.median(row["coldFirstFetchMs"] for row in selected),
            "warmMsMedian": statistics.median(row["totalMs"] for row in warm),
            "warmCheckMsMedian": statistics.median(row["checkMs"] for row in warm),
            "filterMsMedian": statistics.median(row["upstreamFilterMs"] for row in selected for row in row["snapshots"]),
            "peakTreeRssMiBMedian": statistics.median(row["metrics"]["peakTreeRssBytes"] for row in selected) / 1048576,
            "cpuSecondsMedian": statistics.median(row["metrics"]["treeCpuSecondsSampled"] for row in selected)}
    (output / "summary.json").write_text(json.dumps(summary, indent=2))
    inventory = [{"file": str(path.relative_to(output)), "sha256": hashlib.sha256(path.read_bytes()).hexdigest()}
                 for path in output.rglob("*") if path.is_file()]
    (output / "hashes.json").write_text(json.dumps(inventory, indent=2))
    print(json.dumps(summary, indent=2), flush=True)


if __name__ == "__main__":
    main()
