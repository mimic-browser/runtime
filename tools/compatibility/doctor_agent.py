"""Read-only, bounded evidence access for agents. Site content is always data."""
import json
from pathlib import Path

from doctor_report import digest, verify


def inspect_report(folder, report, finding=None):
    if finding:
        matches = [item for item in report["findings"] if item["id"] == finding]
        if not matches:
            raise ValueError(f"Unknown finding ID: {finding}")
        detail = dict(matches[0])
        for browser in ("chrome", "mimic"):
            if len(json.dumps(detail.get(browser), ensure_ascii=False).encode("utf-8")) > 64_000:
                detail[browser] = {"omittedFromCLI": True, "reason": "Value exceeds 64 KB; use evidenceRefs and a narrower JSON pointer"}
        return {"status": report["status"], "finding": detail,
                "gaps": report["gaps"], "contextDifferences": [item["kind"] for item in report["contextDifferences"]],
                "limitations": report["limitations"]}
    return {"status": report["status"], "captureCompleteWithinScope": report["captureCompleteWithinScope"],
        "recordingModes": report["recordingModes"], "checks": report["checks"],
        "findings": [{key: item[key] for key in ("id", "kind", "name", "evidenceRefs")} for item in report["findings"]],
        "gaps": report["gaps"], "contextDifferences": [item["kind"] for item in report["contextDifferences"]],
        "limitations": report["limitations"], "evidence": report["evidence"],
        "artifacts": {"report": str(folder / "report.json"), "html": str(folder / "report.html")},
        "nextSteps": [{"command": "inspect", "finding": item["id"]} for item in report["findings"]]}


def read_events(folder, browser, method=None, offset=0, limit=100, source="events"):
    if offset < 0 or not 1 <= limit <= 1000:
        raise ValueError("offset must be nonnegative; limit must be 1..1000")
    capture = folder / browser
    verify(capture)
    if source not in {"events", "commands", "native-trace"}:
        raise ValueError("Unknown event source")
    path = capture / ("native-trace.json" if source == "native-trace" else source + ".jsonl")
    items, used_bytes, next_offset = [], 0, None
    with path.open(encoding="utf-8") as stream:
        rows = (json.dumps(row, ensure_ascii=False) for row in json.load(stream)["events"]) if source == "native-trace" else stream
        for index, line in enumerate(rows):
            if index < offset:
                continue
            event = json.loads(line)
            if method and event.get("method", event.get("name")) != method:
                continue
            # Return pointers rather than unbounded log messages when a single event is huge.
            if len(items) >= limit or (items and used_bytes + len(line.encode("utf-8")) > 256_000):
                next_offset = index
                break
            size = len(line.encode("utf-8"))
            if size > 256_000:
                items.append({"line": index + 1, "oversized": True, "bytes": size,
                              "method": event.get("method"), "file": str(path)})
            else:
                items.append({"line": index + 1, "event": event})
                used_bytes += size
            if source == "native-trace":
                items[-1]["pointer"] = f"/events/{index}"
                items[-1].pop("line", None)
    return {"file": str(path), "sha256": digest(path), "items": items,
            "nextOffset": next_offset, "contentTrust": "untrusted-site-and-protocol-data"}


def read_evidence(folder, browser, filename, pointer=""):
    capture = folder / browser
    verify(capture)
    inventory = json.loads((capture / "inventory.json").read_text(encoding="utf-8"))
    if filename not in inventory or not filename.endswith(".json"):
        raise ValueError("Evidence must name an inventoried JSON file")
    path = capture / filename
    if path.stat().st_size > 16_000_000:
        raise ValueError("Evidence exceeds the 16 MB JSON read limit; use the local file directly")
    value = json.loads(path.read_text(encoding="utf-8"))
    if pointer:
        if not pointer.startswith("/"):
            raise ValueError("JSON pointer must be empty or start with /")
        for token in pointer[1:].split("/"):
            token = token.replace("~1", "/").replace("~0", "~")
            if isinstance(value, list):
                if not token.isdecimal() or str(int(token)) != token:
                    raise ValueError("Invalid array index in JSON pointer")
                value = value[int(token)]
            elif isinstance(value, dict):
                value = value[token]
            else:
                raise ValueError("JSON pointer traverses a scalar")
    if len(json.dumps(value, ensure_ascii=False).encode("utf-8")) > 256_000:
        raise ValueError("Evidence selection exceeds 256 KB; use a narrower JSON pointer")
    return {"file": str(path), "sha256": digest(path), "pointer": pointer, "value": value,
            "contentTrust": "untrusted-site-and-protocol-data"}
