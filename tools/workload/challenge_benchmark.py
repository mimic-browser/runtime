"""Challenge the native optimizer on public dynamic applications.
The clients use ordinary assertions; this module contains no search engine.
Captured inputs are explicit, and every run stays offline after recording.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess

ROOT = Path(__file__).resolve().parents[2]
CASES = {
    "react-article": ("react-article.json", ["node", "tools/workload/dynamic_article_client.js", "react"]),
    "vue-guide": ("vue-guide.json", ["node", "tools/workload/vitepress_client.js"]),
    "realworld": ("realworld.json", ["node", "tools/workload/realworld_client.js"]),
    "books": ("document-only.json", ["node", "tools/workload/books_client.js"]),
}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--captures-json", type=Path, required=True,
                        help="Private mapping of case names to capture/supplementalCapture paths")
    parser.add_argument("--case", action="append", choices=CASES)
    parser.add_argument("--search-time", default="180s")
    parser.add_argument("--max-trials", type=int, default=24)
    args = parser.parse_args()
    inputs = json.loads(args.captures_json.read_text(encoding="utf-8-sig"))
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    receipts = []
    binary = args.binary.resolve()
    for name in args.case or CASES:
        manual, command = CASES[name]
        evidence = inputs[name]
        environment = os.environ.copy()
        data = output / name
        environment["MIMIC_DATA_DIR"] = str(data)
        argv = [str(binary), "optimize", "--name", name, "--listen", "127.0.0.1:0",
                "--browser-mode", "headless", "--manual-policy", str(ROOT / "tools/workload/manual" / manual),
                "--capture", str(Path(evidence["capture"]).resolve()),
                "--search-time", args.search_time, "--max-trials", str(args.max_trials), "--repetitions", "5"]
        if evidence.get("supplementalCapture"):
            argv += ["--supplemental-capture", str(Path(evidence["supplementalCapture"]).resolve())]
        # Sequential cases keep matched measurement away from other browser trials.
        completed = subprocess.run(argv + ["--"] + command, env=environment, cwd=ROOT)
        reports = sorted((data / "optimization").glob("run-*/report.json"), key=lambda p: p.stat().st_mtime)
        receipts.append({"workload": name, "exitCode": completed.returncode,
                         "report": str(reports[-1]) if reports else None,
                         "clientSHA256": hashlib.sha256((ROOT / command[1]).read_bytes()).hexdigest()})
        (output / "suite.json").write_text(json.dumps({"binarySHA256": hashlib.sha256(binary.read_bytes()).hexdigest(),
                                                      "cases": receipts}, indent=2), encoding="utf-8")
    return int(any(row["exitCode"] for row in receipts))


if __name__ == "__main__":
    raise SystemExit(main())
