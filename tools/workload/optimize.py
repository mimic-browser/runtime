"""Experimental external-process workload optimizer. No framework-specific oracle."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import secrets
import signal
import socket
import statistics
import subprocess
import sys
import time
import urllib.request
import urllib.parse
import zipfile

import psutil

ROOT = Path(__file__).resolve().parents[2]


def digest(path):
    checksum = hashlib.sha256()
    with Path(path).open("rb") as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b""):
            checksum.update(chunk)
    return checksum.hexdigest()


def free_port():
    with socket.socket() as sock:
        sock.bind(("127.0.0.1", 0))
        return sock.getsockname()[1]


class Process:
    """Own descendants, including a child that outlives its external runner."""

    def __init__(self, command, env, stdout, stderr):
        self.closed = False
        self.job = None
        if os.name == "nt":
            sys.path.insert(0, str(ROOT / "benchmark"))
            import windows_metrics as win
            self.win = win
            self.job = win.k.CreateJobObjectW(None, None)
            if not self.job:
                raise win.C.WinError(win.C.get_last_error())
            limits = win.ExtendedLimits()
            limits.basic.flags = 0x2000
            if not win.k.SetInformationJobObject(self.job, 9, win.C.byref(limits), win.C.sizeof(limits)):
                win.k.CloseHandle(self.job)
                raise win.C.WinError(win.C.get_last_error())
            startup = subprocess.STARTUPINFO()
            startup.dwFlags = subprocess.STARTF_USESHOWWINDOW
            startup.wShowWindow = 0
            try:
                self.process = subprocess.Popen(command, env=env, stdout=stdout, stderr=stderr,
                                                creationflags=0x4 | subprocess.CREATE_NEW_CONSOLE,
                                                startupinfo=startup)
                if not win.k.AssignProcessToJobObject(self.job, int(self.process._handle)):
                    raise win.C.WinError(win.C.get_last_error())
                if win.n.NtResumeProcess(int(self.process._handle)) != 0:
                    raise RuntimeError("cannot resume owned process")
            except BaseException:
                if hasattr(self, "process"):
                    self.process.kill()
                    self.process.wait()
                win.k.CloseHandle(self.job)
                raise
        else:
            self.process = subprocess.Popen(command, env=env, stdout=stdout, stderr=stderr, start_new_session=True)

    def sample(self):
        if self.job:
            win = self.win
            accounting = win.Accounting()
            if not win.k.QueryInformationJobObject(self.job, 1, win.C.byref(accounting), win.C.sizeof(accounting), None):
                raise win.C.WinError(win.C.get_last_error())
            cpu = (accounting.user + accounting.kernel) / 1e7
        else:
            cpu = 0
        rss = 0
        try:
            parent = psutil.Process(self.process.pid)
            for process in [parent] + parent.children(recursive=True):
                rss += process.memory_info().rss
                if not self.job:
                    times = process.cpu_times()
                    cpu += times.user + times.system
        except psutil.NoSuchProcess:
            pass
        return {"cpuSeconds": cpu, "rssBytes": rss}

    def close(self):
        if self.closed:
            return
        self.closed = True
        if self.job:
            self.win.k.TerminateJobObject(self.job, 1)
            self.process.wait(timeout=10)
            self.win.k.CloseHandle(self.job)
            self.job = None
        else:
            try:
                os.killpg(self.process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            self.process.wait(timeout=10)


def write_plan(path, policy, scripts, capture, binary):
    manifest = dict(format="mimic-workload-plan", version=1, offlineOnly=True,
                    captureSHA256=digest(capture), binarySHA256=digest(binary),
                    policy=policy, suppressClassic=sorted(scripts))
    with zipfile.ZipFile(path, "w", zipfile.ZIP_DEFLATED) as archive:
        archive.writestr("manifest.json", json.dumps(manifest, sort_keys=True))


class Runner:
    def __init__(self, args):
        self.args = args
        self.output = args.output.resolve()
        self.output.mkdir(parents=True, exist_ok=True)
        self.sequence = 0
        self.capture = self.output / "environment.mcap"
        self.baseline_result = None
        self.has_result = False
        self.encoded_costs = {}

    def run(self, label, policy=None, scripts=(), record=False, profile=None):
        self.sequence += 1
        directory = self.output / f"{self.sequence:04d}-{label}"
        directory.mkdir()
        endpoint = self.args.endpoint or f"http://127.0.0.1:{free_port()}"
        control = f"http://127.0.0.1:{free_port()}"
        token = secrets.token_hex(32)
        env = os.environ.copy()
        env["MIMIC_BOOTSTRAP_CACHE_DIR"] = ""  # keep background compilation out of trials
        env["MIMIC_WORKLOAD_TOKEN"] = token
        for name in self.args.endpoint_env:
            env[name] = endpoint
        result_path = Path(self.args.result_file).resolve() if self.args.result_file else None
        if self.args.result_env:
            result_path = directory / "result.json"
            env[self.args.result_env] = str(result_path)
        before_result = result_path.stat().st_mtime_ns if result_path and result_path.exists() else None
        command = [str(self.args.binary.resolve()), "-listen", endpoint.removeprefix("http://"),
                   "-engine", self.args.engine, "-chrome", "152", "-browser-mode", "headless",
                   "-workload-control", control.removeprefix("http://")]
        if not label.startswith("benchmark-"):
            command += ["-workload-inventory"]
        if record:
            command += ["-workload-capture", str(self.capture)]
            if self.args.volatile_query_key:
                command += ["-workload-volatile-query", ",".join(self.args.volatile_query_key)]
        else:
            command += ["-workload-replay", str(self.capture)]
        if profile:
            # The historical offline plan remains a research input. New live
            # validation exercises the normal installed profile entry point.
            flag = "-profile" if Path(profile).suffix == ".mprofile" else "-workload-plan"
            command += [flag, str(profile)]
        elif scripts:
            path = directory / "candidate.mplan"
            write_plan(path, policy or {}, scripts, self.capture, self.args.binary)
            command += ["-workload-plan", str(path)]
        elif policy:
            path = directory / "policy.json"
            path.write_text(json.dumps(policy), encoding="utf-8")
            command += ["-resource-policy", str(path)]
        row = dict(label=label, status="MIMIC_FAILURE", command=self.args.command,
                   browserCommand=command, endpoint=endpoint, directory=str(directory),
                   mode="record" if record else "offline-replay", engine=self.args.engine)
        browser = client = None
        started = time.perf_counter()

        def control_request(path):
            request = urllib.request.Request(control + path, data=b"", headers={"Authorization": "Bearer " + token})
            with urllib.request.urlopen(request, timeout=5) as response:
                return json.load(response) if path == "/snapshot" else response.read().decode("utf-8", errors="replace")

        with (directory / "mimic.stdout").open("wb") as mout, (directory / "mimic.stderr").open("wb") as merr, \
                (directory / "workload.stdout").open("wb") as out, (directory / "workload.stderr").open("wb") as err:
            try:
                browser = Process(command, env, mout, merr)
                row["mimicPid"] = browser.process.pid
                deadline = time.perf_counter() + 30
                while True:
                    if browser.process.poll() is not None:
                        raise RuntimeError(f"Mimic exited during startup: {browser.process.returncode}")
                    try:
                        with urllib.request.urlopen(endpoint + "/json/version", timeout=.3) as response:
                            row["browserVersion"] = json.load(response)
                        break
                    except (OSError, ValueError):
                        if time.perf_counter() > deadline:
                            raise TimeoutError("Mimic readiness timeout")
                        time.sleep(.02)
                row["startupMs"] = (time.perf_counter() - started) * 1000
                for name in self.args.websocket_env:
                    env[name] = row["browserVersion"]["webSocketDebuggerUrl"]
                before = browser.sample()
                peak = before["rssBytes"]
                work_start = time.perf_counter()
                client = Process(self.args.command, env, out, err)
                row["workloadPid"] = client.process.pid
                row["status"] = "TIMEOUT"
                while client.process.poll() is None:
                    if browser.process.poll() is not None:
                        row["status"] = "MIMIC_FAILURE"
                        row["reason"] = f"Mimic exited: {browser.process.returncode}"
                        break
                    peak = max(peak, browser.sample()["rssBytes"])
                    if time.perf_counter() - work_start > self.args.timeout:
                        row["reason"] = "external workload deadline exceeded"
                        break
                    time.sleep(.02)
                row["elapsedMs"] = (time.perf_counter() - work_start) * 1000
                after = browser.sample()
                row.update(browserCpuMs=(after["cpuSeconds"] - before["cpuSeconds"]) * 1000,
                           peakRssBytes=max(peak, after["rssBytes"]), retainedRssBytes=after["rssBytes"])
                code = client.process.poll()
                row["exitCode"] = code
                if code is not None and browser.process.poll() is None:
                    row["status"] = "PASS" if code == 0 else "CRASH" if code < 0 or code >= 0x80000000 else "WORKLOAD_FAILURE"
                row["workloadStatus"] = row["status"]
                if row["status"] != "PASS":
                    try:
                        (directory / "mimic-stacks.txt").write_text(control_request("/stacks"), encoding="utf-8")
                    except Exception:
                        pass
                client.close()
                snapshot = control_request("/snapshot")
                row.update(snapshot)
                if snapshot["metrics"]["violations"]:
                    row["status"] = "REPLAY_MISMATCH" if not record else "CAPTURE_FAILURE"
                elif snapshot["metrics"].get("captureMisses") or snapshot["metrics"].get("unsupported"):
                    row["status"] = "UNSUPPORTED_CAPTURE" if record else "UNSUPPORTED_REPLAY"
                elif snapshot.get("captureError"):
                    row["status"] = "UNSUPPORTED_CAPTURE"
                elif snapshot["metrics"]["active"]:
                    row["status"] = "UNSUPPORTED_REPLAY"
                    row["reason"] = "workload ended with unfinished transport requests"
                if result_path and row["status"] == "PASS":
                    if not result_path.exists() or result_path.stat().st_mtime_ns == before_result:
                        row["status"] = "RESULT_FAILURE"
                        row["reason"] = "structured result artifact was not produced by this run"
                    else:
                        row["result"] = json.loads(result_path.read_text(encoding="utf-8"))
                        if self.has_result and row["result"] != self.baseline_result:
                            row["status"] = "RESULT_FAILURE"
                            row["reason"] = "structured result differs from successful baseline"
                        elif row["status"] == "PASS" and not self.has_result:
                            self.baseline_result = row["result"]
                            self.has_result = True
                control_request("/stop")
                browser.process.wait(timeout=10)
            except Exception as error:
                row["controlError"] = str(error)
                row.setdefault("reason", str(error))
                if row["status"] == "PASS":
                    row["status"] = "MIMIC_FAILURE"
            finally:
                if client:
                    client.close()
                if browser:
                    browser.close()
        row["totalMs"] = (time.perf_counter() - started) * 1000
        row["stdout"] = "workload.stdout"
        row["stderr"] = "workload.stderr"
        (directory / "run.json").write_text(json.dumps(row, indent=2), encoding="utf-8")
        primary = f" (workload {row['workloadStatus']})" if row.get("workloadStatus") and row["workloadStatus"] != row["status"] else ""
        print(f"{self.sequence:04d} {label}: {row['status']}{primary} {row.get('elapsedMs', 0):.0f} ms", flush=True)
        return row


def exact_glob(url, volatile_keys=()):
    # ResourcePolicy uses Go path.Match (or its full-URL regex equivalent).
    escape = lambda text: "".join("\\" + char if char in "\\*?[" else char for char in text)
    if not volatile_keys or "?" not in url:
        return escape(url)
    prefix, query = url.split("?", 1)
    pieces = []
    for part in query.split("&"):
        key, sep, value = part.partition("=")
        pieces.append(escape(key + sep) + ("*" if sep and urllib.parse.unquote_plus(key) in volatile_keys else escape(value)))
    return escape(prefix + "?") + "&".join(pieces)


def compile_actions(actions):
    rules = []
    suppressed = []
    global_work = {}
    for action in actions:
        if action["action"] == "suppress":
            suppressed.append(action["script"])
        elif not action["match"]:
            global_work.update(action["work"])
        else:
            rules.append(dict(id=action["id"], match=action["match"], work=action["work"]))
    if global_work:
        rules.append(dict(id="processing", match={}, work=global_work))
    return {"rules": rules}, suppressed


def score(row):
    m = row["metrics"]
    # Internet acquisition is the objective. Retaining bytes already acquired,
    # decoded processing, JS CPU and RAM are diagnostics, not pruning rewards.
    return (m["encodedBodyBytes"], m["responseAcquisitions"])


class Search:
    def __init__(self, runner, baseline, manual):
        self.runner = runner
        self.baseline = baseline
        self.best = manual if manual["status"] == "PASS" and score(manual) < score(baseline) else baseline
        self.actions = []
        self.best_policy = runner.args.manual if self.best is manual else {}
        self.best_scripts = []
        self.memo = {}
        self.trials = []
        self.memo_hits = 0
        self.current = baseline

    def trial(self, additions):
        if len(self.trials) >= self.runner.args.max_trials:
            return False
        candidate = self.actions + additions
        policy, scripts = compile_actions(candidate)
        key = json.dumps([policy, sorted(scripts)], sort_keys=True)
        if key in self.memo:
            self.memo_hits += 1
            return self.memo[key]
        row = self.runner.run("search", policy, scripts)
        row["actions"] = candidate
        passing = row["status"] == "PASS"
        self.memo[key] = passing
        self.trials.append(row)
        if passing:
            self.actions = candidate
            self.current = row
            # Only measured cost counters choose the finalist. No single noisy
            # latency observation can beat a cheaper default/manual baseline.
            if score(row) < score(self.best) or (score(row) == score(self.best) and self.best is not self.baseline and self.best["label"] == "manual-validation"):
                self.best = row
                self.best_policy, self.best_scripts = policy, scripts
        return passing

    def eliminate(self, actions):
        if not actions or len(self.trials) >= self.runner.args.max_trials:
            return
        if self.trial(actions):
            return
        if len(actions) == 1:
            return
        midpoint = len(actions) // 2
        self.eliminate(actions[:midpoint])
        self.eliminate(actions[midpoint:])

    def run(self):
        kinds = sorted({resource["kind"] for resource in self.baseline["metrics"].get("resources", [])
                        if resource["kind"] != "document" and resource["url"].startswith(("http://", "https://"))})
        coarse = [dict(id="remove-kind-" + kind, action="block", match={"kinds": [kind]},
                       work={"cacheRead": False, "network": False}) for kind in kinds]
        self.eliminate(coarse)
        resources = {}
        for resource in self.current["metrics"].get("resources", []):
            if resource["kind"] == "document" or not resource["url"].startswith(("http://", "https://")):
                continue
            # URL rules cover all repeated uses, without guessing instance IDs.
            resources[resource["url"]] = resource
        actions = []
        for url, resource in sorted(resources.items(), key=lambda item: -self.runner.encoded_costs.get(item[0], 0)):
            actions.append(dict(id=f"remove-{len(actions)}", action="block",
                                match={"urlGlob": exact_glob(url, self.runner.args.volatile_query_key)}, work={"cacheRead": False, "network": False},
                                resource=resource))
        # Coarse provenance partitions make delta elimination useful without an
        # instruction-level graph: first try all, then split by kind/mechanism,
        # recursively bisect the failing groups. Successful removals accumulate.
        if actions and not self.trial(actions):
            groups = {}
            for action in actions:
                r = action["resource"]
                groups.setdefault((r["kind"], r["owner"], r["mechanism"]), []).append(action)
            for group in groups.values():
                self.eliminate(group)
        # Separate execution-stage search. Acquired source identity includes its
        # URL and bytes; this never treats network acquisition as JS execution.
        scripts = {script["id"]: script for script in self.current["metrics"].get("scripts", []) if script["external"]}
        self.eliminate([dict(id="suppress-" + sid, action="suppress", script=sid, url=s["url"])
                        for sid, s in sorted(scripts.items(), key=lambda item: -item[1]["bytes"])])
        return self.best_policy, self.best_scripts


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--manual-policy", type=Path, required=True)
    parser.add_argument("--endpoint-env", action="append", default=[])
    parser.add_argument("--websocket-env", action="append", default=[], help="existing workload variable expecting the browser WebSocket URL")
    parser.add_argument("--endpoint", help="fixed CDP HTTP endpoint for an unchanged client with a hardcoded port")
    parser.add_argument("--timeout", type=float, default=45)
    parser.add_argument("--replays", type=int, default=3)
    parser.add_argument("--repetitions", type=int, default=5)
    parser.add_argument("--max-trials", type=int, default=100)
    parser.add_argument("--engine", choices=["v8", "quickjs", "goja"], default="v8")
    parser.add_argument("--capture", type=Path, help="reuse an existing binary capture; never record live")
    parser.add_argument("--profile", type=Path, help="validate/benchmark an existing offline profile instead of searching")
    parser.add_argument("--result-file", help="existing workload JSON result output, compared semantically")
    parser.add_argument("--result-env", help="existing workload environment variable selecting its JSON result output")
    parser.add_argument("--volatile-query-key", action="append", default=[], help="explicitly declared nonsemantic timestamp/cache-buster query key; never inferred")
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    if args.command and args.command[0] == "--":
        args.command.pop(0)
    if not args.command or args.replays < 2 or args.repetitions < 1:
        parser.error("provide a workload command, at least two replays and one benchmark repetition")
    args.endpoint_env = args.endpoint_env or ["MIMIC_ENDPOINT", "PW_MIMIC_ENDPOINT"]
    args.manual = json.loads(args.manual_policy.read_text(encoding="utf-8"))
    runner = Runner(args)
    report = dict(binarySHA256=digest(args.binary), workloadCommand=args.command,
                  manualPolicy=args.manual, runs=[], supported="fresh Contexts, HTTP(S), external classic-script suppression",
                  cpuMethod="browser process tree CPU during external command; startup excluded",
                  memoryMethod="sampled browser process tree RSS (20 ms); retained RSS after command, not live heap",
                  trafficMethod="encoded HTTP body bytes actually read; excludes headers, TLS, framing, synthetic CDP fulfillment",
                  correctness="external process success, replay integrity, optional existing JSON result equality",
                  startedAt=time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()))

    def save():
        (runner.output / "report.json").write_text(json.dumps(report, indent=2), encoding="utf-8")

    if args.capture:
        import shutil
        if args.capture.resolve() != runner.capture:
            shutil.copyfile(args.capture, runner.capture)
    else:
        capture = runner.run("capture", record=True)
        report["runs"].append(capture)
        if capture["status"] != "PASS":
            report["conclusion"] = "baseline capture failed; optimization not attempted"
            save()
            return 1
    report["captureSHA256"] = digest(runner.capture)
    with zipfile.ZipFile(runner.capture) as archive:
        capture_manifest = json.loads(archive.read("manifest.json"))
        args.volatile_query_key = capture_manifest.get("volatileQuery", [])
        for entry in capture_manifest.get("entries") or []:
            runner.encoded_costs[entry["url"]] = runner.encoded_costs.get(entry["url"], 0) + entry["bodyBytes"]
    report["volatileQuery"] = args.volatile_query_key
    baselines = [runner.run("stability") for _ in range(args.replays)]
    report["runs"] += baselines
    # Cost-counter stability is conservative evidence, not inferred semantics.
    stable = all(row["status"] == "PASS" for row in baselines) and len({score(row) for row in baselines if row["status"] == "PASS"}) == 1
    report["replayStable"] = stable
    if not stable:
        report["conclusion"] = "unstable or invalid baseline replay; workload unsuitable for optimization"
        save()
        return 1
    manual = runner.run("manual-validation", args.manual)
    report["runs"].append(manual)
    if manual["status"] != "PASS":
        report["conclusion"] = "manual baseline fails the workload; choose a competent passing policy before comparing"
        save()
        return 1
    external_scripts = sorted({s["id"] for s in baselines[0]["metrics"].get("scripts", []) if s["external"]})
    if external_scripts and not args.profile:
        ablation = runner.run("runtime-ablation", {}, external_scripts)
        report["runtimeAblation"] = ablation
        report["runs"].append(ablation)
    started = time.perf_counter()
    if args.profile:
        plan = args.profile.resolve()
        with zipfile.ZipFile(plan) as archive:
            manifest = json.loads(archive.read("manifest.json"))
        policy, scripts = manifest["policy"], manifest.get("suppressClassic", [])
        report.update(selectedPolicy=policy, selectedSuppressClassic=scripts, selectedOrigin="supplied-profile")
    else:
        search = Search(runner, baselines[0], manual)
        policy, scripts = search.run()
        report["runs"] += search.trials
        report.update(optimizerTrials=len(search.trials), optimizerMemoHits=search.memo_hits,
                      optimizerWallMs=(time.perf_counter() - started) * 1000,
                      discoveredActions=search.actions, selectedPolicy=policy, selectedSuppressClassic=scripts,
                      selectedOrigin="manual" if search.best is manual else "default" if search.best is baselines[0] else "automatic")
        plan = runner.output / "optimized.mplan"
        write_plan(plan, policy, scripts, runner.capture, args.binary)
    report["profile"] = str(plan)
    matrix = {name: [] for name in ("Default", "Manual", "Auto")}
    for iteration in range(args.repetitions):
        # Rotate order to reduce systematic warming/background-load bias.
        order = list(matrix)
        order = order[iteration % 3:] + order[:iteration % 3]
        for name in order:
            row = runner.run("benchmark-" + name, args.manual if name == "Manual" else None,
                             profile=plan if name == "Auto" else None)
            matrix[name].append(row)
            report["runs"].append(row)
            save()
    report["benchmark"] = matrix
    report["summary"] = {}
    for name, rows in matrix.items():
        passing = [row for row in rows if row["status"] == "PASS"]
        summary = dict(passes=len(passing), trials=len(rows))
        if passing:
            for key in ["elapsedMs", "browserCpuMs", "peakRssBytes", "retainedRssBytes"]:
                summary[key] = statistics.median(row[key] for row in passing)
            for key in ["requests", "responseAcquisitions", "encodedBodyBytes", "processedBodyBytes", "scriptsAcquired", "classicScriptsExecuted", "classicScriptsSuppressed", "peakRetainedBodyBytes", "retainedBodyBytes"]:
                summary[key] = statistics.median(row["metrics"][key] for row in passing)
        report["summary"][name] = summary
    report["conclusion"] = "offline specialization validated" if all(row["status"] == "PASS" for rows in matrix.values() for row in rows) else "finalist failed validation; profile must not be used"
    save()
    print(json.dumps(report["summary"], indent=2))
    return 0 if report["conclusion"] == "offline specialization validated" else 1


if __name__ == "__main__":
    sys.exit(main())
