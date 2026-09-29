"""Reportable paired ten-Page controls using the unmodified frozen runner."""

import argparse
from concurrent.futures import ThreadPoolExecutor
import datetime
import json
import os
from pathlib import Path
import statistics
import sys
import threading
import time

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "benchmark"))
import run as frozen


def wave(runtime, work, n=10):
    servers = [frozen.Server(work) for _ in range(n)]
    barrier = threading.Barrier(n)
    release = threading.Event()
    finished = [threading.Event() for _ in range(n)]
    sample_index = len(runtime.tree.samples)
    start = time.perf_counter()
    try:
        with ThreadPoolExecutor(max_workers=n) as pool:
            jobs = [pool.submit(frozen.execute, runtime, work, barrier, (done, release), server)
                    for done, server in zip(finished, servers)]
            while not all(done.is_set() for done in finished):
                if time.perf_counter() - start > 180:
                    barrier.abort()
                    raise TimeoutError("ten-Page wave")
                time.sleep(0.05)
            active = runtime.tree.snapshot()
            release.set()
            rows = [job.result() for job in jobs]
        elapsed = time.perf_counter() - start
        time.sleep(0.25)
        recovered = runtime.tree.snapshot()
        if any(row["status"] != "VALID" for row in rows):
            raise RuntimeError("control workload failed: " + str(rows))
        return {"workload": work, "pages": n, "ready": runtime.ready_memory,
                "active": active, "recovered": recovered, "elapsed_s": elapsed,
                "throughput_pages_per_s": n / elapsed, "rows": rows,
                "samples": runtime.tree.samples[sample_index:]}
    finally:
        release.set()
        for server in servers:
            server.close()


def run(binary, work, gogc):
    class Args:
        mimic = binary
        timeout = 30
    expected = frozen.digest(binary)
    previous = os.environ.get("GOGC")
    if gogc is None:
        os.environ.pop("GOGC", None)
    else:
        os.environ["GOGC"] = str(gogc)
    try:
        runtime = frozen.Runtime("mimic", Args)
    finally:
        if previous is None:
            os.environ.pop("GOGC", None)
        else:
            os.environ["GOGC"] = previous
    try:
        result = wave(runtime, work)
        if frozen.digest(binary) != expected:
            raise RuntimeError("binary changed during measurement")
        result["binary"] = str(binary)
        result["sha256"] = expected
        result["gogc"] = gogc
        return result
    finally:
        runtime.close()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("control", type=Path)
    parser.add_argument("candidate", type=Path)
    parser.add_argument("output", type=Path)
    parser.add_argument("--pairs", type=int, default=3)
    parser.add_argument("--control-gogc", type=int)
    parser.add_argument("--candidate-gogc", type=int)
    args = parser.parse_args()
    if args.output.exists():
        raise FileExistsError(args.output)
    args.output.mkdir(parents=True)
    expected_harness = json.loads((ROOT / "benchmark/results/raw.json").read_text(encoding="utf-8"))["metadata"]["harness_sha256"]
    actual_harness, _ = frozen.harness_fingerprint()
    if actual_harness != expected_harness:
        raise RuntimeError("frozen harness changed")
    receipts = {"at": datetime.datetime.now().astimezone().isoformat(),
                "harness_sha256": actual_harness, "runs": []}
    for work in ("static", "react"):
        for pair in range(args.pairs):
            order = (("control", args.control), ("candidate", args.candidate))
            if pair % 2:
                order = tuple(reversed(order))
            for label, binary in order:
                row = run(binary.resolve(), work,
                          args.control_gogc if label == "control" else args.candidate_gogc)
                row["label"], row["pair"] = label, pair + 1
                receipts["runs"].append(row)
                (args.output / "raw.json").write_text(json.dumps(receipts, indent=2), encoding="utf-8")
                print(work, pair + 1, label, round(row["throughput_pages_per_s"], 2),
                      round(row["active"]["rss"] / 2**20, 2), flush=True)
    for work in ("static", "react"):
        for label in ("control", "candidate"):
            rows = [r for r in receipts["runs"] if r["workload"] == work and r["label"] == label]
            print(work, label, "median throughput", statistics.median(r["throughput_pages_per_s"] for r in rows),
                  "median active RSS MiB", statistics.median(r["active"]["rss"] / 2**20 for r in rows))


if __name__ == "__main__":
    main()
