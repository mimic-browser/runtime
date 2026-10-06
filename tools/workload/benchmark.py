"""Matched Default/Manual/Auto experiments; reuse the existing frozen fixtures."""

import argparse
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "benchmark"))
from run import Server


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--repetitions", type=int, default=5)
    parser.add_argument("--max-trials", type=int, default=80)
    parser.add_argument("--wikipedia-script", type=Path, help="unchanged existing Wikipedia client with its saved private fixture")
    parser.add_argument("--skip-books", action="store_true")
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    receipts = []

    def run(name, manual, command, result=False):
        argv = [sys.executable, str(ROOT / "tools/workload/optimize.py"), "--binary", str(args.binary.resolve()),
                "--output", str(args.output.resolve() / name), "--manual-policy", str(ROOT / "tools/workload/manual" / manual),
                "--repetitions", str(args.repetitions), "--max-trials", str(args.max_trials)]
        if result:
            argv += ["--result-env", "WORKLOAD_RESULT"]
        argv += ["--"] + command
        completed = subprocess.run(argv)
        receipts.append(dict(workload=name, exitCode=completed.returncode))
        (args.output / "suite.json").write_text(json.dumps(receipts, indent=2), encoding="utf-8")

    if not args.skip_books:
        run("books-ssr", "document-only.json", ["node", str(ROOT / "tools/workload/books_client.js")], True)
    for workload in ["async", "react"]:
        server = Server(workload)
        try:
            run("existing-" + workload, "interactive.json",
                [sys.executable, str(ROOT / "tools/workload/benchmark_client.py"), workload, server.url], True)
            report_path = args.output / ("existing-" + workload) / "report.json"
            report = json.loads(report_path.read_text(encoding="utf-8"))
            capture_runs = [row for row in report["runs"] if row.get("mode") == "record"]
            expected = capture_runs[0].get("metrics", {}).get("requests") if capture_runs else None
            proof = dict(sourceRequests=len(server.requests), recordedTransportRequests=expected,
                         noNetworkDuringReplay=expected is not None and len(server.requests) == expected)
            (report_path.parent / "source-network-proof.json").write_text(json.dumps(proof, indent=2), encoding="utf-8")
        finally:
            server.close()
    if args.wikipedia_script:
        run("wikipedia-unchanged", "document-only.json", ["node", str(args.wikipedia_script.resolve())])
    return int(any(receipt["exitCode"] for receipt in receipts))


if __name__ == "__main__":
    sys.exit(main())
