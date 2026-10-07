#!/usr/bin/env python3
"""Record Chrome 152 and Mimic, then report observed compatibility differences.

Run --demo for a local, deterministic scenario with a documented DOMException
branding boundary. No AI service or external website is used by the demo.
"""
from __future__ import annotations

import argparse
import asyncio
from contextlib import contextmanager, redirect_stdout
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import math
from pathlib import Path
import shutil
import sys
import threading
from urllib.parse import urlparse

from doctor_capture import ROOT, PINNED_PRODUCT, capture
from doctor_report import compare, digest, render_report, verify, write_json
from doctor_report import exact
from doctor_replay import Archive, reduce_actions
from doctor_agent import inspect_report, read_events, read_evidence

for stream in (sys.stdout, sys.stderr):
    if hasattr(stream, "reconfigure"):
        stream.reconfigure(encoding="utf-8")


class ArgumentParser(argparse.ArgumentParser):
    def error(self, message):
        if "--json" in sys.argv[1:]:
            print(json.dumps({"exitCode": 2, "error": {"type": "UsageError", "message": message}}))
            self.exit(2)
        super().error(message)


def bounded_number(value, minimum, maximum):
    return type(value) in (int, float) and math.isfinite(value) and minimum <= value <= maximum


def validate_scenario(raw: dict) -> dict:
    if not isinstance(raw, dict):
        raise ValueError("Scenario must be an object")
    if set(raw) - {"name", "url", "settleSeconds", "loadTimeoutSeconds", "actions", "checks"}:
        raise ValueError("Unknown scenario fields")
    result = {"name": raw.get("name", "Compatibility scenario"), "url": raw.get("url"),
              "settleSeconds": raw.get("settleSeconds", 2), "loadTimeoutSeconds": raw.get("loadTimeoutSeconds", 30),
              "actions": raw.get("actions", []), "checks": raw.get("checks", [])}
    if not isinstance(result["name"], str) or not result["name"]:
        raise ValueError("Scenario name must be a nonempty string")
    url = urlparse(result["url"] if isinstance(result["url"], str) else "")
    if url.scheme not in {"http", "https"} or not url.hostname or url.username or url.password:
        raise ValueError("Scenario URL must be HTTP(S), without embedded credentials")
    if not bounded_number(result["settleSeconds"], 0, 60) or not bounded_number(result["loadTimeoutSeconds"], 1, 120):
        raise ValueError("Invalid observation duration")
    if not isinstance(result["actions"], list) or len(result["actions"]) > 100:
        raise ValueError("At most 100 actions are supported")
    for action in result["actions"]:
        if not isinstance(action, dict):
            raise ValueError("Action must be an object")
        kind = action.get("type")
        fields = {"click": {"type", "selector"}, "type": {"type", "text"}, "wait": {"type", "seconds"}, "press": {"type", "key"}}
        if not isinstance(kind, str) or kind not in fields or set(action) != fields[kind]:
            raise ValueError("Actions are click(selector), type(text), press(key), or wait(seconds)")
        if kind == "wait" and not bounded_number(action["seconds"], 0, 60):
            raise ValueError("Wait duration must be between zero and 60 seconds")
        if kind == "press" and action["key"] not in ("Enter", "Tab", "Escape", "Backspace", "Delete", "ArrowUp", "ArrowDown", "ArrowLeft", "ArrowRight"):
            raise ValueError("Unsupported input key")
        if kind in {"click", "type"} and not isinstance(action["selector" if kind == "click" else "text"], str):
            raise ValueError("Action target/text must be a string")
    if not isinstance(result["checks"], list) or len(result["checks"]) > 100:
        raise ValueError("At most 100 checks are supported")
    names = set()
    for check in result["checks"]:
        if not isinstance(check, dict) or set(check) != {"name", "selector", "property", "equals"}:
            raise ValueError("Each check requires name, selector, property and equals")
        if not isinstance(check["name"], str) or not check["name"] or check["name"] in names:
            raise ValueError("Check names must be nonempty and unique")
        names.add(check["name"])
        if not isinstance(check["selector"], str) or not check["selector"]:
            raise ValueError("Check selector must be nonempty")
        if not isinstance(check["property"], str) or check["property"] not in {"textContent", "value", "count"}:
            raise ValueError("Check property must be textContent, value or count")
        if check["property"] == "count":
            if type(check["equals"]) is not int or check["equals"] < 0:
                raise ValueError("Expected count must be a nonnegative integer")
        elif not isinstance(check["equals"], str):
            raise ValueError("Expected text/value must be a string")
    return result


def demo_scenario(port):
    return validate_scenario({"name": "Local browser semantics · DOMException branding",
        "url": f"http://127.0.0.1:{port}/", "settleSeconds": 1, "loadTimeoutSeconds": 10,
        "actions": [{"type": "click", "selector": "#run"}],
        "checks": [
            {"name": "Application started", "selector": "#ready", "property": "textContent", "equals": "Ready"},
            {"name": "Real input reached the page", "selector": "#clicked", "property": "textContent", "equals": "Clicked"},
            {"name": "DOMException tag after prototype removal", "selector": "#brand", "property": "textContent", "equals": "[object Object]"},
        ]})


@contextmanager
def demo_server(port):
    fixtures = Path(__file__).parent / "doctor-fixtures"
    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            filename = {"/": "index.html", "/app.js": "app.js"}.get(self.path)
            if filename:
                body = (fixtures / filename).read_bytes()
                self.send_response_only(200)
                self.send_header("Content-Type", "text/javascript; charset=utf-8" if filename.endswith(".js") else "text/html; charset=utf-8")
            else:
                body = b""
                self.send_response_only(204)
            self.send_header("Content-Length", str(len(body)))
            self.send_header("Cache-Control", "no-store")
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, *_):
            pass
    server = ThreadingHTTPServer(("127.0.0.1", port), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        yield
    finally:
        server.shutdown()
        server.server_close()
        thread.join(timeout=3)


def private_output(path: Path):
    if path.is_relative_to(ROOT):
        allowed = [ROOT / ".build", ROOT / "compatibility/private-captures"]
        if not any(path.is_relative_to(parent) and path != parent for parent in allowed):
            raise ValueError("Store private runs under .build/ or compatibility/private-captures/, or outside the repository")
    path.mkdir(parents=True, exist_ok=False)


def preserve_tool(out):
    sources = [Path(__file__).with_name(name) for name in
               ("compat_doctor.py", "doctor_capture.py", "doctor_report.py", "doctor_replay.py", "doctor_agent.py")]
    (out / "tool-source").mkdir()
    for source in sources:
        shutil.copy2(source, out / "tool-source" / source.name)
    return {p.name: digest(p) for p in sources}


def load_report(folder: Path):
    scenario = validate_scenario(json.loads((folder / "scenario.json").read_text(encoding="utf-8")))
    reference, candidate = verify(folder / "chrome"), verify(folder / "mimic")
    if reference.get("scenario") != scenario or candidate.get("scenario") != scenario:
        raise ValueError("Capture scenarios differ; an offline comparison cannot silently relabel them")
    if reference.get("browser") != "chrome" or candidate.get("browser") != "mimic":
        raise ValueError("Reference/candidate browser identities are invalid")
    if reference.get("state") == "finished" and (reference.get("version", {}).get("Browser") != PINNED_PRODUCT
            or reference.get("initialEnvironment", {}).get("webdriver") is not False):
        raise ValueError("Finished Chrome evidence has invalid version or original webdriver provenance")
    report = compare(reference, candidate, scenario)
    report["evidence"] = {kind: digest(folder / kind / "inventory.json") for kind in ("chrome", "mimic")}
    return report, scenario


def build_report(folder: Path):
    report, scenario = load_report(folder)
    write_json(folder / "report.json", report)
    render_report(folder, report, scenario)
    print(f"{report['status']}: {len(report['findings'])} difference candidates, {len(report['gaps'])} evidence gaps, {len(report['contextDifferences'])} context differences")
    print(f"Report: {folder / 'report.html'}")
    return 1 if report["findings"] else 2 if report["status"] == "inconclusive" else 0


async def run(args, scenario):
    out = args.out.resolve()
    saved = None
    if args.reference:
        saved = verify(args.reference.resolve())
        if saved.get("browser") != "chrome" or saved.get("scenario") != scenario:
            raise ValueError("Saved Chrome reference must have the identical scenario, including origin")
        if saved.get("initialEnvironment", {}).get("webdriver") is not False:
            raise ValueError("Saved Chrome reference did not verify its original webdriver value")
        if saved.get("version", {}).get("Browser") != PINNED_PRODUCT:
            raise ValueError("Saved reference is not the pinned Chrome product")
    private_output(out)
    write_json(out / "scenario.json", scenario)
    write_json(out / "tool.json", {"files": preserve_tool(out),
        "referenceReused": bool(saved), "referenceSource": str(args.reference.resolve()) if args.reference else None})
    if saved:
        print("Reusing sealed Chrome evidence; no Chrome launch.", flush=True)
        shutil.copytree(args.reference.resolve(), out / "chrome")
    else:
        print("Recording Chrome 152 with a fresh dedicated profile…", flush=True)
        saved = await capture("chrome", args.chrome.resolve(), args.chrome_port, out / "chrome", scenario,
                              pause_children=getattr(args, "pause_children", False), headless=getattr(args, "headless", False))
        print(f"Chrome capture sealed: {saved['state']}, {len(saved['gaps'])} evidence gaps.", flush=True)
    print("Recording a dedicated Mimic process…", flush=True)
    candidate = await capture("mimic", args.mimic.resolve(), args.mimic_port, out / "mimic", scenario)
    print(f"Mimic capture sealed: {candidate['state']}, {len(candidate['gaps'])} evidence gaps.", flush=True)
    return build_report(out)


async def replay_pair(args, folder, scenario):
    archive = Archive(args.archive)
    if archive.capture.get("browser") != "chrome" or archive.capture.get("version", {}).get("Browser") != PINNED_PRODUCT:
        raise ValueError("Replay source must be a pinned Chrome capture")
    private_output(folder)
    write_json(folder / "tool.json", {"files": preserve_tool(folder)})
    write_json(folder / "scenario.json", scenario)
    write_json(folder / "diagnostic.json", {"mode": "intercepted-archive-replay",
        "source": str(args.archive.resolve()), "sourceInventorySha256": digest(args.archive / "inventory.json")})
    for kind in ("chrome", "mimic"):
        print(f"Replaying archived HTTP responses in {kind}: {folder.name}", flush=True)
        await capture(kind, getattr(args, kind).resolve(), getattr(args, kind + "_port"),
                      folder / kind, scenario, Archive(args.archive), diagnostic=getattr(args, "diagnostic", False),
                      headless=getattr(args, "headless", False))
    return build_report(folder)


async def minimize(args):
    """Reduce scenario actions while preserving an exact observed terminal difference."""
    source = args.folder.resolve()
    scenario = validate_scenario(json.loads((source / "scenario.json").read_text(encoding="utf-8")))
    original = {kind: verify(source / kind) for kind in ("chrome", "mimic")}
    target = args.check
    pair = {kind: original[kind].get("checks", {}).get(target) for kind in original}
    if any(not isinstance(value, dict) or value.get("available") is not True for value in pair.values()) or exact(pair["chrome"], pair["mimic"]):
        raise ValueError("--check must name an available, differing terminal check")
    if any(original[kind].get("scenario") != scenario for kind in original):
        raise ValueError("Original scenario does not match its captures")
    args.archive = source / "chrome"
    out = args.out.resolve()
    private_output(out)
    write_json(out / "tool.json", {"files": preserve_tool(out)})
    attempt = 0
    history = []

    async def reproduces(actions):
        nonlocal attempt
        trial = {**scenario, "actions": actions}
        # Every accepted candidate must reproduce twice in both engines.
        for repeat in range(2):
            attempt += 1
            folder = out / f"attempt-{attempt:04d}"
            await replay_pair(args, folder, trial)
            captures = {kind: verify(folder / kind) for kind in original}
            complete = all(c["state"] == "finished" and not c["gaps"] and target in c["checks"] for c in captures.values())
            accepted = all(exact(c["checks"].get(target), pair[kind]) for kind, c in captures.items()) if complete else None
            history.append({"folder": folder.name, "actions": actions, "repeat": repeat + 1, "reproduced": accepted})
            write_json(out / "attempts.json", history)
            if not accepted:
                return accepted
        return True

    if not await reproduces(scenario["actions"]):
        write_json(out / "reduction.json", {"status": "inconclusive", "reason": "Original exact pair did not survive repeated archive replay"})
        return 2
    actions, attempts, minimal = await reduce_actions(scenario["actions"], reproduces, args.budget)
    write_json(out / "scenario.json", {**scenario, "actions": actions})
    write_json(out / "reduction.json", {"status": "reproduced", "check": target, "exactPair": pair,
        "originalActions": len(scenario["actions"]), "remainingActions": len(actions), "proposals": attempts,
        "oneActionDeletionMinimal": minimal, "budgetExhausted": attempts >= args.budget and not minimal,
        "inconclusiveAttempts": sum(item["reproduced"] is None for item in history),
        "scope": "Action deletion only; not JavaScript/source minimization or proof of runtime root cause.",
        "sourceInventories": {kind: digest(source / kind / "inventory.json") for kind in original}})
    print(f"Reproducer saved: {out / 'scenario.json'} ({len(actions)} actions)")
    return 0


def main():
    parser = ArgumentParser(description=__doc__)
    parser.add_argument("--json", action="store_true", help="Emit machine-readable JSON")
    commands = parser.add_subparsers(dest="command", required=True)
    record = commands.add_parser("run", help="Record one scenario and create an HTML/JSON report")
    source = record.add_mutually_exclusive_group(required=True)
    source.add_argument("--demo", action="store_true")
    source.add_argument("--scenario", type=Path)
    source.add_argument("--url")
    record.add_argument("--expect-selector")
    record.add_argument("--expect-text")
    record.add_argument("--out", type=Path, required=True, help="New private run directory")
    record.add_argument("--chrome", type=Path, default=ROOT / "compatibility/.chrome-for-testing/152.0.7977.82/chrome-win64/chrome.exe")
    record.add_argument("--mimic", type=Path, default=ROOT / ".build/mimic-compat-doctor.exe")
    record.add_argument("--reference", type=Path, help="Reuse a sealed chrome/ capture; do not launch Chrome")
    record.add_argument("--pause-children", action="store_true", help="Labelled diagnostic: pause new child targets until recording is ready")
    record.add_argument("--headless", action="store_true", help="Use a labelled, non-authoritative headless Chrome diagnostic")
    record.add_argument("--chrome-port", type=int, default=19361)
    record.add_argument("--mimic-port", type=int, default=19362)
    record.add_argument("--fixture-port", type=int, default=19363)
    offline = commands.add_parser("report", help="Verify preserved evidence and regenerate its report without browsers")
    offline.add_argument("folder", type=Path)
    inspect = commands.add_parser("inspect", help="Read verified findings without browsers or file mutation")
    inspect.add_argument("folder", type=Path)
    inspect.add_argument("--finding", help="Stable finding ID from an inspect result")
    events = commands.add_parser("events", help="Read a bounded page of verified raw protocol events")
    events.add_argument("folder", type=Path)
    events.add_argument("--method")
    events.add_argument("--offset", type=int, default=0)
    events.add_argument("--limit", type=int, default=100)
    events.add_argument("--source", choices=("events", "commands", "native-trace"), default="events")
    evidence = commands.add_parser("evidence", help="Read a verified JSON evidence file or JSON pointer")
    evidence.add_argument("folder", type=Path)
    evidence.add_argument("--file", required=True)
    evidence.add_argument("--pointer", default="")
    for command in (events, evidence):
        command.add_argument("--browser", choices=("chrome", "mimic"), required=True)
    replay = commands.add_parser("replay", help="Paired intercepted diagnostic using archived HTTP responses")
    replay.add_argument("archive", type=Path, help="Sealed Chrome capture directory")
    replay.add_argument("--scenario", type=Path, help="Optional alternative scenario at the same origin")
    replay.add_argument("--diagnostic", action="store_true", help="Also collect Chrome script sources and Mimic's bounded internal trace")
    reduction = commands.add_parser("minimize", help="Reduce actions while preserving an exact terminal difference twice")
    reduction.add_argument("folder", type=Path, help="Existing paired run directory")
    reduction.add_argument("--check", required=True)
    reduction.add_argument("--budget", type=int, default=20, help="Maximum deletion proposals; each runs at most two pairs")
    for command in (replay, reduction):
        command.add_argument("--headless", action="store_true", help="Use labelled headless Chrome diagnostics")
        command.add_argument("--out", type=Path, required=True)
        command.add_argument("--chrome", type=Path, default=record.get_default("chrome"))
        command.add_argument("--mimic", type=Path, default=record.get_default("mimic"))
        command.add_argument("--chrome-port", type=int, default=19361)
        command.add_argument("--mimic-port", type=int, default=19362)
    for command in (record, offline, replay, reduction, inspect, events, evidence):
        command.add_argument("--json", action="store_true", default=argparse.SUPPRESS, help="Emit one JSON result on stdout; progress goes to stderr")
    args = parser.parse_args()
    try:
        with redirect_stdout(sys.stderr):
            if args.command == "inspect":
                report, _ = load_report(args.folder.resolve())
                payload = inspect_report(args.folder.resolve(), report, args.finding)
                code = 0
            elif args.command == "events":
                payload = read_events(args.folder.resolve(), args.browser, args.method, args.offset, args.limit, args.source)
                code = 0
            elif args.command == "evidence":
                payload = read_evidence(args.folder.resolve(), args.browser, args.file, args.pointer)
                code = 0
            else:
                code = execute(args)
                folder = (args.folder if args.command == "report" else args.out).resolve()
                artifact = folder / ("reduction.json" if args.command == "minimize" else "report.json")
                payload = json.loads(artifact.read_text(encoding="utf-8"))
                payload["artifactDirectory"] = str(folder)
        if args.json or args.command in {"inspect", "events", "evidence"}:
            print(json.dumps({"command": args.command, "exitCode": code, "result": payload}, ensure_ascii=False))
        return code
    except (OSError, ValueError, KeyError, IndexError, TypeError) as error:
        if args.json:
            print(json.dumps({"command": args.command, "exitCode": 2,
                              "error": {"type": type(error).__name__, "message": str(error)}}, ensure_ascii=False))
        else:
            print(f"Compatibility Doctor: {error}", file=sys.stderr)
        return 2


def execute(args):
    try:
        if args.command == "report":
            return build_report(args.folder.resolve())
        if args.command in {"replay", "minimize"}:
            if not (1 <= args.chrome_port <= 65535 and 1 <= args.mimic_port <= 65535) or args.chrome_port == args.mimic_port:
                raise ValueError("Ports must be distinct, fixed, and nonzero")
            if args.command == "minimize":
                if not 1 <= args.budget <= 100:
                    raise ValueError("Reduction budget must be between 1 and 100")
                return asyncio.run(minimize(args))
            archive = Archive(args.archive)
            scenario = validate_scenario(json.loads(args.scenario.read_text(encoding="utf-8")) if args.scenario else archive.capture["scenario"])
            if urlparse(scenario["url"])[:2] != urlparse(archive.capture["scenario"]["url"])[:2]:
                raise ValueError("Replay scenario must retain the archived origin")
            return asyncio.run(replay_pair(args, args.out.resolve(), scenario))
        ports = [args.chrome_port, args.mimic_port] + ([args.fixture_port] if args.demo else [])
        if any(not 1 <= port <= 65535 for port in ports) or len(set(ports)) != len(ports):
            raise ValueError("Ports must be distinct, fixed, and nonzero")
        if bool(args.expect_selector) != (args.expect_text is not None):
            raise ValueError("--expect-selector and --expect-text must be used together")
        if (args.expect_selector or args.expect_text is not None) and not args.url:
            raise ValueError("Inline expectations require --url")
        if args.demo:
            scenario = demo_scenario(args.fixture_port)
            with demo_server(args.fixture_port):
                return asyncio.run(run(args, scenario))
        if args.scenario:
            scenario = validate_scenario(json.loads(args.scenario.read_text(encoding="utf-8")))
        else:
            checks = [{"name": "Expected page content", "selector": args.expect_selector,
                       "property": "textContent", "equals": args.expect_text}] if args.expect_selector else []
            scenario = validate_scenario({"url": args.url, "checks": checks})
        return asyncio.run(run(args, scenario))
    except KeyboardInterrupt:
        raise ValueError("Run interrupted; inspect partial capture evidence in the output directory")


if __name__ == "__main__":
    raise SystemExit(main())
