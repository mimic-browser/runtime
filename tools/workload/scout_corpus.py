"""Record public application inventories once, without optimizer search.

Scouting is not correctness validation and is never counted as a passing
optimization benchmark. Preserve all captures, including failures.
"""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[2]
CASES = {
    "supabase": "https://supabase.com/docs/guides/database/overview",
    "nextjs": "https://nextjs.org/docs/app/getting-started/installation",
    "discourse": "https://meta.discourse.org/latest",
    "gitlab": "https://gitlab.com/gitlab-org/gitlab",
    "shadcn": "https://ui.shadcn.com/docs/installation",
    "react-native": "https://reactnative.dev/docs/getting-started",
}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--case", choices=CASES, action="append")
    args = parser.parse_args()
    output = args.output.resolve()
    if output.exists():
        parser.error("use a fresh directory; earlier evidence must be preserved")
    output.mkdir(parents=True)
    rows = []
    for name in args.case or CASES:
        command = [sys.executable, str(ROOT / "tools/workload/live_validate.py"),
                   "--binary", str(args.binary.resolve()), "--output", str(output / name),
                   "--label", "scout", "--timeout", "65", "--", "node",
                   "tools/workload/scout_client.js", CASES[name]]
        completed = subprocess.run(command, cwd=ROOT)
        receipts = sorted((output / name).glob("*/run.json"))
        receipt = json.loads(receipts[-1].read_text()) if receipts else {}
        rows.append({"name": name, "url": CASES[name], "exitCode": completed.returncode,
                     "receipt": str(receipts[-1]) if receipts else None,
                     "status": receipt.get("status"), "workloadStatus": receipt.get("workloadStatus"),
                     "metrics": receipt.get("metrics"), "reason": receipt.get("reason")})
        (output / "suite.json").write_text(json.dumps({
            "purpose": "scouting-not-correctness-validation",
            "binarySHA256": hashlib.sha256(args.binary.read_bytes()).hexdigest(),
            "cases": rows}, indent=2), encoding="utf-8")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
