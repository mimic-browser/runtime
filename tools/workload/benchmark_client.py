"""Ordinary CDP assertions for the existing frozen benchmark workloads.

The page code, vendor bundles and expected results belong to benchmark/run.py;
this external process uses them unchanged through normal CDP, not a Mimic API.
"""

import argparse
import json
import os
from pathlib import Path
import sys
import time
import urllib.request

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "benchmark"))
from run import CDP, EXPECTED, wait_value


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("workload", choices=EXPECTED)
    parser.add_argument("url")
    args = parser.parse_args()
    endpoint = os.environ["MIMIC_ENDPOINT"]
    with urllib.request.urlopen(endpoint + "/json/version") as response:
        version = json.load(response)
    root = CDP(version["webSocketDebuggerUrl"])
    context = root.call("Target.createBrowserContext")["browserContextId"]
    target = root.call("Target.createTarget", dict(url="about:blank", browserContextId=context))["targetId"]
    page = CDP(endpoint.replace("http://", "ws://") + "/devtools/page/" + target)
    try:
        page.call("Page.enable")
        reply = page.call("Page.navigate", dict(url=args.url))
        assert not reply.get("errorText"), reply
        wait_value(page, "({ready:document.readyState,runner:typeof window.__benchRun})",
                   lambda value: value and value["ready"] == "complete" and value["runner"] == "function", 20)
        page.evaluate("void window.__benchRun()")
        result = wait_value(page, "window.__bench", lambda value: value and value.get("done"), 20)
        assert not result.get("error"), result
        assert result["result"] == EXPECTED[args.workload], result
        if os.environ.get("WORKLOAD_RESULT"):
            Path(os.environ["WORKLOAD_RESULT"]).write_text(json.dumps(result["result"]), encoding="utf-8")
        print("PASS", args.workload, json.dumps(result["result"]))
    finally:
        page.close()
        root.call("Target.disposeBrowserContext", dict(browserContextId=context))
        root.close()


if __name__ == "__main__":
    main()
