"""Evaluate the native product workflow, retaining negative controls.
No search implementation lives here; all training runs through mimic optimize.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import threading
import time

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "benchmark"))
from run import Server
from mirror_capture import Mirror


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--books-capture", type=Path, required=True)
    parser.add_argument("--vue-source", type=Path, required=True)
    args = parser.parse_args()
    output = args.output.resolve(); output.mkdir(parents=True, exist_ok=True)
    evidence = []
    def run(name, manual, command, capture=None, server=None, env=None):
        data = output / name
        environment = os.environ.copy(); environment["MIMIC_DATA_DIR"] = str(data)
        environment.update(env or {})
        stopped = threading.Event(); receipt = {}
        def close_after_baseline():
            while not stopped.wait(0.05):
                for path in (data / "optimization").glob("run-*/0001-baseline/run.json"):
                    try: baseline = json.loads(path.read_text())
                    except (OSError, ValueError): continue
                    if baseline["status"] == "PASS":
                        receipt["sourceRequestsBeforeClose"] = len(server.requests)
                        server.close(); receipt["sourceClosedBeforeSearch"] = True
                        return
        monitor = None
        if server:
            monitor = threading.Thread(target=close_after_baseline, daemon=True); monitor.start()
        argv = [str(args.binary.resolve()), "optimize", "--name", name, "--browser-mode", "headless", "--listen", "127.0.0.1:0", "--manual-policy", str(ROOT / "tools/workload/manual" / manual), "--repetitions", "5", "--max-trials", "32", "--search-time", "120s", "--timeout", "30s"]
        if capture: argv += ["--capture", str(capture.resolve())]
        try:
            completed = subprocess.run(argv + ["--"] + command, env=environment)
        finally:
            stopped.set()
            if monitor: monitor.join(10)
            if server and not receipt.get("sourceClosedBeforeSearch"): server.close()
        reports = sorted((data / "optimization").glob("run-*/report.json"), key=lambda p:p.stat().st_mtime)
        receipt.update(workload=name, exitCode=completed.returncode, report=str(reports[-1]) if reports else None)
        if server: receipt["sourceRequestsTotal"] = len(server.requests)
        evidence.append(receipt)
        (output / "suite.json").write_text(json.dumps(evidence, indent=2))
    run("books", "document-only.json", ["node", str(ROOT / "tools/workload/books_client.js")], capture=args.books_capture)
    for workload in ["async", "react"]:
        server = Server(workload)
        run(workload, "interactive.json", [sys.executable, str(ROOT / "tools/workload/benchmark_client.py"), workload, server.url], server=server)
    mirror = Mirror(args.vue_source, "https://todomvc.com", port=49360)
    run("vue-todomvc-mirror", "todomvc.json", ["node", str(ROOT / "tools/workload/todomvc_client.js")], server=mirror, env={"WORKLOAD_URL":mirror.base+"/examples/vue/dist/"})
    (output / "provenance.json").write_text(json.dumps({"binarySHA256":hashlib.sha256(args.binary.read_bytes()).hexdigest(),"vueSourceCaptureSHA256":hashlib.sha256(args.vue_source.read_bytes()).hexdigest(),"vueOriginRebound":True,"vueBodyChanges":False,"vueAnalytics": "official unchanged base.js only enables analytics on todomvc.com; local origin follows its normal branch"},indent=2))
    return int(any(row["exitCode"] for row in evidence))

if __name__ == "__main__": sys.exit(main())
