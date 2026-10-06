"""Focused external-runner contract tests; no page assertion language."""

import argparse
import json
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import patch

import psutil

sys.path.insert(0, str(Path(__file__).resolve().parent))
from optimize import Runner, Search, exact_glob


@unittest.skipUnless(os.environ.get("MIMIC_WORKLOAD_TEST_BINARY"), "set MIMIC_WORKLOAD_TEST_BINARY to a freshly built Mimic")
class RunnerContract(unittest.TestCase):
    def runner(self, code, timeout=3, result_env=None):
        directory = tempfile.TemporaryDirectory(prefix="mimic-workload-test-")
        self.addCleanup(directory.cleanup)
        args = argparse.Namespace(binary=Path(os.environ["MIMIC_WORKLOAD_TEST_BINARY"]),
                                  output=Path(directory.name), endpoint_env=["MIMIC_ENDPOINT"],
                                  result_file=None, result_env=result_env, engine="v8", timeout=timeout,
                                  volatile_query_key=[], command=[sys.executable, "-c", code])
        args.endpoint = None
        args.websocket_env = []
        return Runner(args)

    def assert_cleaned(self, row):
        for name in ("mimicPid", "workloadPid"):
            self.assertFalse(psutil.pid_exists(row[name]), f"owned process leaked: {row[name]}")

    def test_pass_and_logs_then_repeat_against_empty_capture(self):
        runner = self.runner("import sys;print('stdout evidence');print('stderr evidence',file=sys.stderr)")
        recorded = runner.run("record", record=True)
        replay = runner.run("replay")
        self.assertEqual(recorded["status"], "PASS", recorded)
        self.assertEqual(replay["status"], "PASS", replay)
        directory = Path(replay["directory"])
        self.assertIn("stdout evidence", (directory / "workload.stdout").read_text())
        self.assertIn("stderr evidence", (directory / "workload.stderr").read_text())
        self.assert_cleaned(replay)

    def test_assertion_failure_is_not_pass(self):
        row = self.runner("assert False, 'ordinary external assertion'").run("assertion", record=True)
        self.assertEqual(row["status"], "WORKLOAD_FAILURE", row)
        self.assertNotEqual(row["exitCode"], 0)
        self.assert_cleaned(row)

    def test_timeout_kills_client_and_browser(self):
        row = self.runner("import subprocess,sys,time;child=subprocess.Popen([sys.executable,'-c','import time;time.sleep(10)']);print(child.pid,flush=True);time.sleep(10)", timeout=.3).run("timeout", record=True)
        self.assertEqual(row["status"], "TIMEOUT", row)
        self.assert_cleaned(row)
        child = int((Path(row["directory"]) / "workload.stdout").read_text().strip())
        self.assertFalse(psutil.pid_exists(child), f"client descendant leaked: {child}")

    def test_success_cleans_descendant_outliving_client(self):
        row = self.runner("import subprocess,sys;child=subprocess.Popen([sys.executable,'-c','import time;time.sleep(10)']);print(child.pid,flush=True)").run("orphan", record=True)
        self.assertEqual(row["status"], "PASS", row)
        self.assert_cleaned(row)
        child = int((Path(row["directory"]) / "workload.stdout").read_text().strip())
        self.assertFalse(psutil.pid_exists(child), f"client descendant leaked: {child}")

    def test_crash_has_its_own_status(self):
        code = "import sys;sys.exit(-1073741819)" if os.name == "nt" else "import os,signal;os.kill(os.getpid(),signal.SIGKILL)"
        row = self.runner(code).run("crash", record=True)
        self.assertEqual(row["status"], "CRASH", row)
        self.assert_cleaned(row)

    def test_existing_result_artifact_is_optional_additional_contract(self):
        runner = self.runner("import os,json;from pathlib import Path;Path(os.environ['RESULT_PATH']).write_text(json.dumps({'value':os.environ['VALUE']}))", result_env="RESULT_PATH")
        with patch.dict(os.environ, {"VALUE": "original"}):
            recorded = runner.run("record-result", record=True)
        with patch.dict(os.environ, {"VALUE": "changed"}):
            replay = runner.run("changed-result")
        self.assertEqual(recorded["status"], "PASS", recorded)
        self.assertEqual(replay["workloadStatus"], "PASS", replay)
        self.assertEqual(replay["status"], "RESULT_FAILURE", replay)
        self.assert_cleaned(replay)

    @unittest.skipUnless(os.name == "nt", "the existing frozen fixture server imports Windows benchmark infrastructure")
    def test_record_network_workload_then_replay_with_source_closed(self):
        sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "benchmark"))
        from run import Server

        server = Server("async")
        try:
            runner = self.runner("unused", timeout=30, result_env="WORKLOAD_RESULT")
            runner.args.command = [sys.executable, str(Path(__file__).with_name("benchmark_client.py")), "async", server.url]
            recorded = runner.run("record-async", record=True)
            self.assertEqual(recorded["status"], "PASS", recorded)
            self.assertEqual(len(server.requests), recorded["metrics"]["requests"])
            self.assertGreater(recorded["metrics"]["encodedBodyBytes"], 0)
        finally:
            server.close()
        replay = runner.run("source-closed-replay")
        self.assertEqual(replay["status"], "PASS", replay)
        self.assertEqual(recorded["metrics"]["encodedBodyBytes"], replay["metrics"]["encodedBodyBytes"])
        self.assert_cleaned(recorded)
        self.assert_cleaned(replay)


class EliminationContract(unittest.TestCase):
    def test_url_rules_preserve_semantic_keys_and_escape_glob_syntax(self):
        self.assertEqual(exact_glob("https://site.test/a[1]?id=42&time=123", ["time"]),
                         "https://site.test/a\\[1]\\?id=42&time=*")
        self.assertEqual(exact_glob("https://site.test/a?time=123"),
                         "https://site.test/a\\?time=123")

    def test_combines_safe_removals_without_removing_required_inputs(self):
        class Oracle:
            args = argparse.Namespace(max_trials=60, manual={}, volatile_query_key=[])

            def run(self, label, policy, scripts):
                removed = {rule["match"].get("urlGlob") for rule in policy["rules"]}
                passing = not ({"required-script", "required-data"} & removed)
                return dict(status="PASS" if passing else "WORKLOAD_FAILURE", label=label,
                            metrics=dict(encodedBodyBytes=100 - 20 * len(removed), responseAcquisitions=5 - len(removed)))

        baseline = dict(status="PASS", label="stability", metrics=dict(encodedBodyBytes=100, responseAcquisitions=5))
        manual = dict(status="PASS", label="manual-validation", metrics=dict(encodedBodyBytes=90, responseAcquisitions=4))
        search = Search(Oracle(), baseline, manual)
        actions = [dict(id=name, action="block", match={"urlGlob": name}, work={"network": False, "cacheRead": False})
                   for name in ["unused-css", "required-script", "unused-image", "required-data"]]
        search.eliminate(actions)
        self.assertEqual({action["id"] for action in search.actions}, {"unused-css", "unused-image"})
        self.assertEqual(search.best["status"], "PASS")
        self.assertLess(search.best["metrics"]["encodedBodyBytes"], manual["metrics"]["encodedBodyBytes"])
        self.assertLess(len(search.trials), 15)


if __name__ == "__main__":
    unittest.main()
