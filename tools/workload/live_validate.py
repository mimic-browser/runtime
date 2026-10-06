"""Record one live research validation, without optimization or retries.

The feature itself uses the native runner. This helper reuses historical process
ownership/receipts to validate a normal installed profile against the live site.
Keep the output private: captures may contain credentials and personal data.
"""

import argparse
import json
from pathlib import Path

from optimize import Runner


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--profile", type=Path)
    parser.add_argument("--manual-policy", type=Path)
    parser.add_argument("--label", default="live-default")
    parser.add_argument("--timeout", type=float, default=45)
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    if args.command[:1] == ["--"]:
        args.command = args.command[1:]
    if not args.command:
        parser.error("provide an ordinary external workload after --")
    if args.profile and args.manual_policy:
        parser.error("choose a generated profile or a manual policy")
    if args.output.exists():
        parser.error("use a fresh output directory to preserve previous evidence")
    args.endpoint = None
    args.endpoint_env = ["MIMIC_CDP_URL", "MIMIC_ENDPOINT", "PW_MIMIC_ENDPOINT"]
    args.websocket_env = []
    args.volatile_query_key = []
    args.engine = "v8"
    args.result_file = None
    args.result_env = None
    policy = None
    if args.manual_policy:
        policy = json.loads(args.manual_policy.read_text(encoding="utf-8-sig"))
    runner = Runner(args)
    # Match the same cost-only instrumentation on each live comparison.
    row = runner.run("benchmark-" + args.label, policy=policy, profile=args.profile, record=True)
    print(json.dumps({key: row.get(key) for key in
                      ("status", "workloadStatus", "reason", "elapsedMs", "metrics")}, indent=2))
    return 0 if row.get("workloadStatus") == "PASS" else 1


if __name__ == "__main__":
    raise SystemExit(main())
