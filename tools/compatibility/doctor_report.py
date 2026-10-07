"""Offline, deterministic comparison and self-contained Compatibility Doctor report."""
from __future__ import annotations

from collections import Counter
import hashlib
import html
import json
from pathlib import Path


LIMITATIONS = [
    "This is an observation report for one scenario, not a full Chrome conformance verdict.",
    "Recording can affect timing. No page API wrappers, debugger pauses, response interception, or identity overrides are used.",
    "Caught exceptions, every JavaScript/API call, internal task ordering, and unexecuted branches are not covered.",
    "Live responses, session state, scheduling, and machine state can differ. Differences are candidates, not automatically proven runtime defects.",
    "Final checks and DOM reads run after the timed observation window and are explicitly recorded as observations.",
]


def write_json(path: Path, value) -> None:
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def digest(path: Path) -> str:
    value = hashlib.sha256()
    with path.open("rb") as source:
        for block in iter(lambda: source.read(1024 * 1024), b""):
            value.update(block)
    return value.hexdigest()


def seal(folder: Path) -> dict:
    """Never overwrite a sealed capture. The inventory covers all finalized evidence."""
    path = folder / "inventory.json"
    if path.exists():
        raise FileExistsError(f"Capture already sealed: {folder}")
    inventory = {
        file.relative_to(folder).as_posix(): {"sha256": digest(file), "bytes": file.stat().st_size}
        for file in sorted(folder.rglob("*")) if file.is_file()
    }
    write_json(path, inventory)
    return inventory


def verify(folder: Path) -> dict:
    inventory = json.loads((folder / "inventory.json").read_text(encoding="utf-8"))
    if not isinstance(inventory, dict) or "capture.json" not in inventory:
        raise ValueError("Capture inventory is malformed")
    if any(p.is_symlink() for p in folder.rglob("*")):
        raise ValueError("Capture contains a symbolic link")
    files = {p.relative_to(folder).as_posix() for p in folder.rglob("*") if p.is_file()}
    if files != set(inventory) | {"inventory.json"}:
        raise ValueError("Capture inventory does not match its file set")
    for name, expected in inventory.items():
        if not isinstance(expected, dict) or type(expected.get("bytes")) is not int or not isinstance(expected.get("sha256"), str):
            raise ValueError("Capture inventory entry is malformed")
        path = (folder / name).resolve()
        if not path.is_relative_to(folder.resolve()) or path.is_symlink():
            raise ValueError("Capture contains an unsafe evidence path")
        if path.stat().st_size != expected["bytes"] or digest(path) != expected["sha256"]:
            raise ValueError(f"Capture integrity mismatch: {name}")
    capture = json.loads((folder / "capture.json").read_text(encoding="utf-8"))
    if not isinstance(capture, dict):
        raise ValueError("Capture must be an object")
    return capture


def exact(left, right) -> bool:
    """Do not equate JSON true with 1, or absent values with null."""
    if type(left) is not type(right):
        return False
    if isinstance(left, dict):
        return left.keys() == right.keys() and all(exact(left[k], right[k]) for k in left)
    if isinstance(left, list):
        return len(left) == len(right) and all(exact(a, b) for a, b in zip(left, right))
    return left == right


def compare(reference: dict, candidate: dict, scenario: dict) -> dict:
    findings, gaps = [], []
    for label, capture in (("Chrome", reference), ("Mimic", candidate)):
        for field in ("gaps", "requests", "exceptions", "consoleProblems", "networkFailures"):
            if not isinstance(capture.get(field), list):
                raise ValueError(f"{label}: missing or malformed evidence field {field}")
        if not isinstance(capture.get("checks"), dict):
            raise ValueError(f"{label}: missing or malformed checks")
        for item in capture["gaps"]:
            if not isinstance(item, dict) or not isinstance(item.get("kind"), str) or "detail" not in item:
                raise ValueError(f"{label}: malformed evidence gap")
        for item in capture["requests"]:
            if not isinstance(item, dict) or not isinstance(item.get("request", {}), dict):
                raise ValueError(f"{label}: malformed network observation")
        gaps.extend({"browser": label, **item} for item in capture.get("gaps", []))
        if capture.get("state") != "finished":
            gaps.append({"browser": label, "kind": "run-incomplete", "detail": capture.get("state")})
        for field, expected_type in (("initialEnvironment", dict), ("finalEnvironment", dict), ("finalCookies", list)):
            if not isinstance(capture.get(field), expected_type):
                gaps.append({"browser": label, "kind": "context-evidence-missing", "detail": field})
    if not scenario.get("checks"):
        gaps.append({"browser": "Both", "kind": "no-success-contract", "detail": "No terminal checks were declared."})
    checks = []
    for check in scenario.get("checks", []):
        name = check["name"]
        a, b = reference.get("checks", {}).get(name), candidate.get("checks", {}).get(name)
        valid = lambda value: isinstance(value, dict) and type(value.get("available")) is bool and (not value["available"] or "value" in value)
        complete = valid(a) and valid(b)
        matches = complete and exact(a, b)
        expected = check.get("equals")
        reference_pass = valid(a) and a.get("available") is True and exact(a.get("value"), expected)
        candidate_pass = valid(b) and b.get("available") is True and exact(b.get("value"), expected)
        checks.append({"name": name, "expected": expected, "chrome": a, "mimic": b,
                       "matches": matches, "referencePass": reference_pass, "candidatePass": candidate_pass})
        if not reference_pass:
            gaps.append({"browser": "Chrome", "kind": "reference-check-failed", "detail": name})
        if not complete:
            gaps.append({"browser": "Both", "kind": "check-missing", "detail": name})
        elif not matches:
            findings.append({"kind": "terminal-check", "name": name, "chrome": a, "mimic": b,
                             "confidence": "observed difference", "runtimeCause": "unproven"})

    # Keep duplicates and exact strings. No guessed session/timestamp normalization.
    for key in ("exceptions", "consoleProblems", "networkFailures"):
        a = Counter(json.dumps(v, sort_keys=True, ensure_ascii=False) for v in reference.get(key, []))
        b = Counter(json.dumps(v, sort_keys=True, ensure_ascii=False) for v in candidate.get(key, []))
        for value in sorted(a.keys() | b.keys()):
            if a[value] != b[value]:
                findings.append({"kind": key, "name": value, "chrome": a[value], "mimic": b[value],
                                 "confidence": "investigate", "runtimeCause": "unproven"})

    def network(capture):
        return [{key: item.get(key) for key in ("url", "method", "status", "bodySha256", "error")}
                for item in capture.get("requests", [])]

    a, b = network(reference), network(candidate)
    if not exact(a, b):
        first = next(index for index in range(max(len(a), len(b)))
                     if index >= len(a) or index >= len(b) or not exact(a[index], b[index]))
        findings.append({"kind": "network-sequence", "name": "Requests, responses or body content differ",
                         "chrome": a, "mimic": b, "confidence": "investigate",
                         "firstDifference": {"index": first, "chrome": a[first] if first < len(a) else None,
                                             "mimic": b[first] if first < len(b) else None},
                         "requestCounts": {"chrome": len(a), "mimic": len(b)},
                         "runtimeCause": "unproven; independent live sessions may differ"})
    context = []
    for key in ("initialEnvironment", "finalEnvironment", "finalCookies"):
        a, b = reference.get(key), candidate.get(key)
        if not exact(a, b):
            context.append({"kind": key, "chrome": a, "mimic": b})
    # Header differences are retained separately so environment/server variation
    # remains visible without masquerading as a proven browser-semantic defect.
    headers = lambda capture: [{"url": row.get("url"), "request": row.get("request", {}).get("headers"),
        "response": row.get("response", {}).get("headers"), "postData": row.get("request", {}).get("postData"),
        "retrievedPostData": row.get("requestPostData")} for row in capture.get("requests", [])]
    if not exact(headers(reference), headers(candidate)):
        context.append({"kind": "headers-and-upload-data", "chrome": headers(reference), "mimic": headers(candidate)})
    if candidate.get("processExitBeforeCleanup") is not None:
        findings.append({"kind": "process-exit", "name": "Mimic exited during observation",
                         "chrome": reference.get("processExitBeforeCleanup"),
                         "mimic": candidate["processExitBeforeCleanup"], "confidence": "observed failure"})
    status = "differences" if findings else "inconclusive" if gaps or context else "no-observed-differences"
    modes = {"chrome": reference.get("recordingMode"), "mimic": candidate.get("recordingMode")}
    limits = list(LIMITATIONS)
    browser_mode = reference.get("launch", {}).get("browserMode")
    if browser_mode == "headless":
        limits.append("HEADLESS DIAGNOSTIC: not an authoritative normal desktop Chrome reference; mode-sensitive observations cannot become generic expectations.")
    if reference.get("scope", {}).get("pauseOnStart"):
        limits.append("Child targets were paused until recording domains were ready. Child startup timing is instrumented.")
    if "intercepted-archive-replay" in modes.values():
        limits[1] = "INTERCEPTED DIAGNOSTIC: archived HTTP responses replace live responses. Timing, transport and server interaction differ from normal controls."
    if reference.get("diagnostic") or candidate.get("diagnostic"):
        limits.append("Deep diagnostic enabled: Chrome Debugger source collection and Mimic bounded runtime events add observation overhead. These are different trace surfaces, not equivalent instruction traces.")
    for finding in findings:
        identity = json.dumps([scenario.get("url"), finding["kind"], finding["name"]], ensure_ascii=False, separators=(",", ":"))
        finding["id"] = "finding-" + hashlib.sha256(identity.encode("utf-8")).hexdigest()[:20]
        key = {"terminal-check": "checks", "network-sequence": "requests", "process-exit": "processExitBeforeCleanup"}.get(finding["kind"], finding["kind"])
        pointer = "/" + key
        if finding["kind"] == "terminal-check":
            pointer += "/" + finding["name"].replace("~", "~0").replace("/", "~1")
        finding["evidenceRefs"] = [{"file": f"{browser}/capture.json", "pointer": pointer} for browser in ("chrome", "mimic")]
    return {"status": status, "recordingModes": modes, "scope": "Declared terminal checks, emitted exceptions/console problems and recorded HTTP sequence",
            "referenceBrowserMode": browser_mode, "referenceAuthority": reference.get("referenceAuthority"),
            "diagnostics": {"chrome": reference.get("diagnostic"), "mimic": candidate.get("diagnostic")},
            "captureCompleteWithinScope": not gaps, "checks": checks, "findings": findings,
            "gaps": gaps, "contextDifferences": context, "limitations": limits}


def render_report(folder: Path, report: dict, scenario: dict) -> None:
    esc = lambda value: html.escape(str(value), quote=True)
    pretty = lambda value: esc(json.dumps(value, ensure_ascii=False, indent=2))
    title = {"differences": "Differences found", "inconclusive": "Insufficient evidence",
             "no-observed-differences": "Checks agree within observed scope"}[report["status"]]
    rows = "".join(
        f'<tr><td>{esc(c["name"])}</td><td><pre>{pretty(c["expected"])}</pre></td>'
        f'<td><pre>{pretty(c["chrome"])}</pre></td><td><pre>{pretty(c["mimic"])}</pre></td>'
        f'<td>{"Agree" if c["matches"] else "Differ"}</td></tr>' for c in report["checks"])
    findings = "".join(
        f'<details><summary>{esc(f["kind"])} · {esc(f["name"])}</summary><pre>{pretty(f)}</pre></details>'
        for f in report["findings"])
    gaps = "".join(f'<li><b>{esc(g["browser"])} · {esc(g["kind"])}</b>: {esc(g["detail"])}</li>' for g in report["gaps"])
    limits = "".join(f'<li>{esc(item)}</li>' for item in report["limitations"])
    context = "".join(f'<details><summary>{esc(item["kind"])}</summary><pre>{pretty(item)}</pre></details>'
                      for item in report["contextDifferences"])
    source = f'''<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<meta http-equiv="Content-Security-Policy" content="default-src 'none'; style-src 'unsafe-inline'">
<title>Compatibility Doctor · {esc(scenario['name'])}</title>
<style>
* {{ box-sizing: border-box; }} body {{ margin: 0; background: #101620; color: #e5eaf1;
font: 16px/1.6 system-ui, sans-serif; }} main {{ max-width: 1120px; margin: auto; padding: 52px 28px; }}
.eyebrow {{ color: #86dcca; letter-spacing: .13em; font-size: 12px; text-transform: uppercase; }}
h1 {{ font-size: 38px; line-height: 1.2; margin-bottom: 10px; }} h2 {{ margin-top: 34px; }}
.muted {{ color: #acb7c8; }} .stats {{ display: flex; gap: 16px; flex-wrap: wrap; margin: 28px 0; }}
.card {{ background: #1b2533; border: 1px solid #334257; border-radius: 12px; padding: 20px; flex: 1; min-width: 200px; }}
.number {{ font-size: 30px; font-weight: 700; }} .warning {{ color: #ffd488; }}
table {{ width: 100%; border-collapse: collapse; }} th,td {{ text-align: left; vertical-align: top;
padding: 12px; border-bottom: 1px solid #334257; }} .table {{ overflow-x: auto; }}
pre {{ white-space: pre-wrap; overflow-wrap: anywhere; font-size: 13px; margin: 0; }}
details {{ background: #1b2533; padding: 16px; border-radius: 8px; margin: 10px 0; }}
summary {{ cursor: pointer; overflow-wrap: anywhere; }} details pre {{ margin-top: 14px; }}
a {{ color: #86dcca; }} li {{ margin: 8px 0; }} .url {{ overflow-wrap: anywhere; }}
</style></head><body><main><div class="eyebrow">Mimic / Compatibility Doctor / Evidence first</div>
<h1>{title}</h1><p class="muted">{esc(scenario['name'])}</p><p class="url">{esc(scenario['url'])}</p>
<div class="stats"><div class="card"><div class="number">{len(report['findings'])}</div>Difference candidates</div>
<div class="card"><div class="number">{len(report['checks'])}</div>Terminal checks</div>
<div class="card"><div class="number warning">{len(report['gaps'])}</div>Evidence gaps</div></div>
<p>{esc(report['scope'])}. A difference is not automatically a confirmed runtime defect.</p>
<h2>Scenario outcome</h2><div class="table"><table><thead><tr><th>Check</th><th>Expected</th><th>Chrome 152</th><th>Mimic</th><th>Comparison</th></tr></thead><tbody>{rows}</tbody></table></div>
<h2>Differences</h2>{findings or '<p>No differences in the recorded checks. Read coverage limits below.</p>'}
<h2>Evidence quality</h2><ul>{gaps or '<li>No detected collection gaps within the declared scope.</li>'}</ul>
<h2>Comparison context</h2><p>Environment, cookies, headers and uploads are kept exact. Differences here require classification;
they are not automatically browser defects and prevent an unqualified agreement result.</p>{context or '<p>No recorded context differences.</p>'}
<h2>Recording boundaries</h2><ul>{limits}</ul>
<h2>Evidence files</h2><p><a href="report.json">Machine-readable report</a> · <a href="scenario.json">Scenario</a> ·
<a href="chrome/capture.json">Chrome capture</a> · <a href="mimic/capture.json">Mimic capture</a></p>
<p class="muted">Each capture includes raw protocol events, commands, response bodies, process provenance and a SHA-256 inventory.
Private captures may contain credentials and session data.</p></main></body></html>'''
    (folder / "report.html").write_text(source, encoding="utf-8")
