import argparse
import hashlib
import json
import os
from pathlib import Path
import queue
import re
import subprocess
import threading
import time
import urllib.request

import psutil


ROOT = Path(__file__).resolve().parents[2]
PORT = 9438


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def sample(root):
    totals = {"rss": 0, "private": 0, "uss": 0, "pss": 0, "cpu": 0.0}
    processes = [root]
    try:
        processes += root.children(recursive=True)
    except psutil.Error:
        pass
    for process in processes:
        try:
            info = process.memory_full_info()
            totals["rss"] += info.rss
            totals["private"] += getattr(info, "private", getattr(info, "uss", 0))
            totals["uss"] += getattr(info, "uss", 0)
            totals["pss"] += getattr(info, "pss", 0)
            cpu = process.cpu_times()
            totals["cpu"] += cpu.user + cpu.system
        except psutil.Error:
            continue
    return totals


def wait_ready(process):
    for _ in range(300):
        if process.poll() is not None:
            raise RuntimeError(f"Mimic exited before ready: {process.returncode}")
        try:
            with urllib.request.urlopen(f"http://127.0.0.1:{PORT}/json/version", timeout=0.2):
                return
        except Exception:
            time.sleep(0.02)
    raise TimeoutError("CDP not ready")


def measure(binary, output):
    output = Path(output)
    output.mkdir(parents=True, exist_ok=False)
    expected = digest(binary)
    env = os.environ.copy()
    env["PW_MIMIC_ENDPOINT"] = f"http://127.0.0.1:{PORT}"
    for key in tuple(env):
        if key.startswith("MIMIC_PROFILE_") or key == "MIMIC_DIAGNOSTICS":
            del env[key]
    command = [str(Path(binary).resolve()), "-listen", f"127.0.0.1:{PORT}"]
    start = time.perf_counter()
    server_log = (output / "server.log").open("w", encoding="utf-8")
    server = subprocess.Popen(command, cwd=ROOT, env=env, stdout=server_log, stderr=subprocess.STDOUT)
    root = psutil.Process(server.pid)
    records = []
    stages = []
    workload_log = []
    significant = {"CDP connection established", "New page created",
                   "Navigating to Wikipedia fixture", "Wikipedia main page loaded",
                   "Article DOM inspected", "Internal article link located",
                   "Internal link navigation completed",
                   "Back navigation restored JavaScript article", "Page closed normally"}
    try:
        wait_ready(server)
        stages.append({"name": "cdp_ready", "t": time.perf_counter() - start, **sample(root)})
        if digest(binary) != expected:
            raise RuntimeError("binary changed before workload")
        workload = subprocess.Popen(["node", "tools/runtimecheck/playwright_wikipedia_local.js"], cwd=ROOT, env=env, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True, bufsize=1)
        events = queue.Queue()

        def read_lines():
            for line in workload.stdout:
                events.put((time.perf_counter() - start, line.rstrip("\n")))

        thread = threading.Thread(target=read_lines, daemon=True)
        thread.start()
        while workload.poll() is None or not events.empty():
            now = time.perf_counter() - start
            records.append({"t": now, **sample(root)})
            while not events.empty():
                t, line = events.get_nowait()
                workload_log.append(line)
                match = re.search(r"\[\d+\] (.*?)(?: \{|$)", line)
                if match and match.group(1) in significant:
                    stages.append({"name": match.group(1), "t": t, **sample(root)})
            time.sleep(0.05)
        thread.join(timeout=1)
        while not events.empty():
            t, line = events.get_nowait()
            workload_log.append(line)
        exit_code = workload.wait(timeout=2)
        time.sleep(0.2)
        stages.append({"name": "after_close", "t": time.perf_counter() - start, **sample(root)})
        (output / "workload.log").write_text("\n".join(workload_log) + "\n", encoding="utf-8")
        peaks = {key: max(record[key] for record in records) for key in ("rss", "private", "uss", "pss")}
        result = {"binary": str(Path(binary).resolve()), "sha256": expected, "command": command,
                  "workload_sha256": digest(ROOT / "tools/runtimecheck/playwright_wikipedia_local.js"),
                  "exit_code": exit_code, "pass": exit_code == 0 and any("RESULT: PASS" in line for line in workload_log),
                  "stages": stages, "peaks": peaks, "samples": records}
        (output / "result.json").write_text(json.dumps(result, indent=2), encoding="utf-8")
        print(json.dumps({key: value for key, value in result.items() if key != "samples"}, indent=2))
        if not result["pass"]:
            raise RuntimeError("Wikipedia workload failed")
    finally:
        if server.poll() is None:
            server.terminate()
            try:
                server.wait(timeout=5)
            except subprocess.TimeoutExpired:
                server.kill()
                server.wait()
        server_log.close()


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("binary", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    measure(args.binary, args.output)
