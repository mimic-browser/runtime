import argparse
import json
import os
from pathlib import Path
import statistics
import subprocess
import sys


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("control", type=Path)
    parser.add_argument("candidate", type=Path)
    parser.add_argument("output", type=Path)
    parser.add_argument("--pairs", type=int, default=3)
    parser.add_argument("--control-gogc", type=int)
    parser.add_argument("--candidate-gogc", type=int)
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=False)
    script = Path(__file__).with_name("measure_wikipedia_rss.py")
    rows = []
    for pair in range(1, args.pairs + 1):
        order = (("control", args.control), ("candidate", args.candidate))
        if pair % 2 == 0:
            order = tuple(reversed(order))
        for label, binary in order:
            directory = args.output / f"pair-{pair}-{label}"
            env = os.environ.copy()
            gogc = args.control_gogc if label == "control" else args.candidate_gogc
            if gogc is None:
                env.pop("GOGC", None)
            else:
                env["GOGC"] = str(gogc)
            with (args.output / "progress.log").open("a", encoding="utf-8") as log:
                subprocess.run([sys.executable, str(script), str(binary.resolve()), str(directory)],
                               check=True, stdout=log, stderr=subprocess.STDOUT, env=env)
            result = json.loads((directory / "result.json").read_text(encoding="utf-8"))
            result.pop("samples")
            result.update(pair=pair, label=label)
            result["gogc"] = gogc
            rows.append(result)
            (args.output / "summary.json").write_text(json.dumps(rows, indent=2), encoding="utf-8")
            print(pair, label, "peak RSS MiB", round(result["peaks"]["rss"] / 2**20, 2), flush=True)
    for label in ("control", "candidate"):
        selected = [row for row in rows if row["label"] == label]
        print(label, "median peak RSS MiB",
              statistics.median(row["peaks"]["rss"] / 2**20 for row in selected))


if __name__ == "__main__":
    main()
