"""Archive, reduction, subprocess CLI and collector fault-injection regression gates."""
import asyncio
import base64
import copy
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import AsyncMock, patch
import psutil

from compat_doctor import build_report, demo_scenario, minimize
from doctor_capture import PINNED_PRODUCT
from doctor_capture import Recorder, capture
from doctor_replay import Archive, reduce_actions
from doctor_report import compare, seal, verify, write_json
from test_compat_doctor import observation


class ArchiveTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.folder = Path(self.temp.name)
        self.request = {"url": "https://example.test/path?q=1", "method": "GET"}
        self.raw = b"\x00\xffexact response\r\n"
        write_json(self.folder / "body.json", {"body": base64.b64encode(self.raw).decode(), "base64Encoded": True})
        self.row = {**self.request, "request": self.request, "completion": "finished", "status": 200,
            "response": {"headers": {"Content-Encoding": "gzip", "Content-Length": "99", "Set-Cookie": "a=1\nb=2"}},
            "bodyFile": "body.json", "bodySha256": hashlib.sha256(self.raw).hexdigest()}

    def archive(self, rows=None):
        write_json(self.folder / "capture.json", {"state": "finished", "requests": rows or [self.row]})
        seal(self.folder)
        return Archive(self.folder)

    def test_binary_body_headers_and_cookie_multiplicity(self):
        result = self.archive().response(self.request)
        self.assertEqual(base64.b64decode(result["body"]), self.raw)
        self.assertEqual(result["responseHeaders"], [{"name": "Set-Cookie", "value": "a=1"},
            {"name": "Set-Cookie", "value": "b=2"}, {"name": "Content-Length", "value": str(len(self.raw))}])

    def test_occurrence_consumption_never_reuses_a_response(self):
        archive = self.archive([self.row, copy.deepcopy(self.row)])
        archive.response(self.request)
        archive.response(self.request)
        with self.assertRaisesRegex(ValueError, "No unused"):
            archive.response(self.request)
        self.assertEqual([h["archiveIndex"] for h in archive.hits], [0, 1])

    def test_origin_query_method_and_upload_match_exactly(self):
        archive = self.archive()
        for request in ({**self.request, "url": "http://example.test/path?q=1"},
                        {**self.request, "url": "https://example.test/path?q=2"},
                        {**self.request, "method": "POST"}, {**self.request, "postData": "x"}):
            with self.subTest(request=request), self.assertRaises(ValueError):
                archive.response(request)
        self.assertEqual(archive.used, set())

    def test_missing_body_is_not_an_empty_success(self):
        self.row.pop("bodyFile")
        with self.assertRaisesRegex(ValueError, "body is missing"):
            self.archive().response(self.request)

    def test_empty_get_upload_matches_absent_upload(self):
        self.assertEqual(self.archive().response({**self.request, "postData": ""})["responseCode"], 200)

    def test_corrupt_decoded_hash_rejected(self):
        self.row["bodySha256"] = "bad"
        with self.assertRaisesRegex(ValueError, "hash mismatch"):
            self.archive().response(self.request)

    def test_304_is_not_replayed_without_cache(self):
        self.row["status"] = 304
        with self.assertRaisesRegex(ValueError, "cache"):
            self.archive().response(self.request)

    def test_missing_upload_is_not_treated_as_empty(self):
        self.row["request"] = {**self.request, "hasPostData": True}
        with self.assertRaisesRegex(ValueError, "upload bytes"):
            self.archive().response(self.request)

    async def test_miss_fails_request_and_never_continues_live(self):
        archive = self.archive()
        recorder = Recorder("mimic", self.folder, demo_scenario(19363))
        recorder.cdp = type("Peer", (), {"call": AsyncMock(return_value={})})()
        await archive.serve(recorder, {"requestId": "1", "request": {**self.request, "url": "https://missing.test/"}}, "s")
        recorder.cdp.call.assert_awaited_once_with("Fetch.failRequest", {"requestId": "1", "errorReason": "Failed"}, "s")
        self.assertEqual(recorder.result["gaps"][0]["kind"], "replay-response-unavailable")

    async def test_replay_enable_failure_is_fatal(self):
        recorder = Recorder("mimic", self.folder, demo_scenario(19363), self.archive())
        async def call(method, *args, **kwargs):
            if method == "Fetch.enable":
                raise RuntimeError("unsupported")
            return {}
        recorder.cdp = type("Peer", (), {"call": staticmethod(call)})()
        with self.assertRaisesRegex(RuntimeError, "unsupported"):
            await recorder.enable("s")
        self.assertEqual(recorder.result["recordingMode"], "intercepted-archive-replay")


class ReductionTests(unittest.IsolatedAsyncioTestCase):
    async def test_reduces_only_irrelevant_actions(self):
        async def reproduces(actions):
            return "trigger" in actions
        actions, attempts, minimal = await reduce_actions(["noise", "trigger", "wait", "noise"], reproduces)
        self.assertEqual(actions, ["trigger"])
        self.assertTrue(minimal)
        self.assertGreater(attempts, 1)

    async def test_budget_keeps_verified_candidate_without_claiming_minimal(self):
        gate = AsyncMock(return_value=False)
        actions, attempts, minimal = await reduce_actions([1, 2, 3], gate, 1)
        self.assertEqual(actions, [1, 2, 3])
        self.assertEqual(attempts, 1)
        self.assertFalse(minimal)

    async def test_empty_reproducer_is_allowed(self):
        actions, _, minimal = await reduce_actions([1], AsyncMock(return_value=True))
        self.assertEqual(actions, [])
        self.assertTrue(minimal)


class InputTests(unittest.IsolatedAsyncioTestCase):
    async def test_minimized_owned_window_is_restored_before_keyboard_input(self):
        recorder = Recorder("chrome", Path("unused"), {})
        recorder.main_session = "main"
        recorder.sessions["main"] = {"targetId": "target"}
        calls = []
        async def call(method, params=None, session=None):
            calls.append((method, params))
            if method == "Browser.getWindowForTarget":
                return {"windowId": 7, "bounds": {"windowState": "minimized"}}
            return {}
        recorder.cdp = type("Peer", (), {"call": staticmethod(call)})()
        await recorder.action({"type": "press", "key": "Enter"})
        self.assertEqual([item[0] for item in calls], ["Browser.getWindowForTarget", "Browser.setWindowBounds",
            "Page.bringToFront", "Input.dispatchKeyEvent", "Input.dispatchKeyEvent"])
        self.assertEqual(calls[3][1]["key"], "Enter")
        self.assertEqual(calls[3][1]["text"], "\r")
        self.assertEqual(calls[4][1]["type"], "keyUp")
        self.assertEqual(len(recorder.result["inputWindowRestorations"]), 1)

    async def test_keyboard_input_in_mimic_does_not_require_a_native_window(self):
        recorder = Recorder("mimic", Path("unused"), {})
        recorder.main_session = "main"
        recorder.cdp = type("Peer", (), {"call": AsyncMock(return_value={})})()
        await recorder.action({"type": "press", "key": "Tab"})
        methods = [call.args[0] for call in recorder.cdp.call.await_args_list]
        self.assertEqual(methods, ["Page.bringToFront", "Input.dispatchKeyEvent", "Input.dispatchKeyEvent"])


class DiagnosticTests(unittest.IsolatedAsyncioTestCase):
    async def test_native_trace_capacity_is_an_explicit_gap(self):
        with tempfile.TemporaryDirectory() as directory:
            recorder = Recorder("mimic", Path(directory), {}, diagnostic=True)
            recorder.main_session = "main"
            recorder.cdp = type("Peer", (), {"call": AsyncMock(side_effect=[{}, {"events": [{}] * 8192}])})()
            await recorder.finish_diagnostic()
            self.assertEqual(recorder.result["gaps"][0]["kind"], "native-trace-capacity-reached")
            self.assertTrue((Path(directory) / "native-trace.json").is_file())

    async def test_dynamic_source_saved_as_inert_json(self):
        with tempfile.TemporaryDirectory() as directory:
            recorder = Recorder("chrome", Path(directory), {}, diagnostic=True)
            recorder.cdp = type("Peer", (), {"call": AsyncMock(return_value={"scriptSource": "throw new Error('probe');"})})()
            row = {"index": 0, "scriptId": "12", "session": "s", "url": ""}
            await recorder.script_source(row)
            source = json.loads((Path(directory) / row["sourceFile"]).read_text())
            self.assertEqual(source["scriptSource"], "throw new Error('probe');")
            self.assertEqual(len(row["sourceSha256"]), 64)


class BackgroundTargetTests(unittest.IsolatedAsyncioTestCase):
    async def test_existing_worker_is_enabled_before_scenario_without_claiming_its_boot(self):
        recorder = Recorder("chrome", Path("unused"), {})
        recorder.main_session = "main"
        info = {"targetId": "worker", "type": "service_worker", "url": "https://example.test/worker.js"}
        calls = []
        async def call(method, *args, **kwargs):
            calls.append(method)
            if method == "Target.getTargets":
                return {"targetInfos": [info]}
            if method == "Target.attachToTarget":
                return {"sessionId": "worker-session"}
            return {}
        recorder.cdp = type("Peer", (), {"call": staticmethod(call)})()
        with patch("doctor_capture.asyncio.sleep", new=AsyncMock()):
            await recorder.prepare_background_targets()
        self.assertIn("worker-session", recorder.sessions)
        self.assertIn("Network.enable", calls)
        self.assertEqual(recorder.result["gaps"], [])
        self.assertIn("not observed", recorder.result["scope"]["backgroundStartupBeforeScenario"])

    async def test_new_worker_during_scenario_keeps_startup_gap_and_attaches_once(self):
        recorder = Recorder("chrome", Path("unused"), {})
        recorder.main_session = "main"
        recorder.phase = "recording"
        recorder.cdp = type("Peer", (), {"call": AsyncMock(return_value={"sessionId": "worker-session"})})()
        info = {"targetId": "worker", "type": "worker", "url": "https://example.test/worker.js"}
        await recorder.attach_target(info)
        await recorder.attach_target(info)
        await recorder.drain()
        calls = [item.args[0] for item in recorder.cdp.call.await_args_list]
        self.assertEqual(calls.count("Target.attachToTarget"), 1)
        self.assertEqual(recorder.result["gaps"][0]["kind"], "child-startup-not-observed")


class RobustnessTests(unittest.IsolatedAsyncioTestCase):
    def test_missing_evidence_lists_never_pass(self):
        scenario = demo_scenario(19363)
        for key in ("gaps", "requests", "exceptions", "consoleProblems", "networkFailures", "checks"):
            for value in (None, "lost", 42):
                with self.subTest(key=key, value=value):
                    sample = observation(scenario)
                    sample[key] = value
                    with self.assertRaises(ValueError):
                        compare(sample, sample, scenario)

    def test_malformed_terminal_values_are_inconclusive(self):
        scenario = demo_scenario(19363)
        for value in (None, [], "Ready", {"available": True}, {"available": 1, "value": "Ready"}):
            sample = observation(scenario)
            sample["checks"]["Application started"] = value
            self.assertEqual(compare(sample, sample, scenario)["status"], "inconclusive")

    async def test_failed_launch_keeps_sealed_partial_evidence(self):
        with tempfile.TemporaryDirectory() as directory:
            folder = Path(directory) / "capture"
            result = await capture("mimic", Path(directory) / "missing.exe", 19379, folder, demo_scenario(19363))
            self.assertEqual(result["state"], "failed")
            self.assertEqual(verify(folder), result)
            self.assertIn("FileNotFoundError", result["gaps"][0]["detail"])

    async def test_process_crash_is_retained_and_owned_process_is_reaped(self):
        with tempfile.TemporaryDirectory() as directory:
            folder = Path(directory) / "capture"
            with patch("doctor_capture.launch_arguments", return_value=[sys.executable, "-c", "raise SystemExit(7)"]):
                result = await capture("mimic", Path(sys.executable), 19379, folder, demo_scenario(19363))
            self.assertEqual(result["state"], "failed")
            self.assertEqual(result["processExitBeforeCleanup"], 7)
            self.assertEqual(verify(folder), result)

    async def test_cancellation_seals_evidence_and_stops_owned_process(self):
        with tempfile.TemporaryDirectory() as directory:
            folder = Path(directory) / "capture"
            with patch("doctor_capture.launch_arguments", return_value=[sys.executable, "-c", "import time; time.sleep(30)"]):
                task = asyncio.create_task(capture("mimic", Path(sys.executable), 19379, folder, demo_scenario(19363)))
                while not (folder / "process.log").exists() and not task.done():
                    await asyncio.sleep(.01)
                await asyncio.sleep(.05)
                task.cancel()
                with self.assertRaises(asyncio.CancelledError):
                    await task
            result = verify(folder)
            self.assertEqual(result["state"], "cancelled")
            self.assertFalse(psutil.pid_exists(result["launch"]["pid"]))

    def test_malformed_gap_and_network_rows_are_rejected(self):
        scenario = demo_scenario(19363)
        for field, value in (("gaps", [None]), ("gaps", [{"kind": "lost"}]),
                             ("requests", [None]), ("requests", [{"request": []}])):
            sample = observation(scenario)
            sample[field] = value
            with self.subTest(field=field, value=value), self.assertRaises(ValueError):
                compare(sample, sample, scenario)

    def test_cli_invalid_evidence_returns_two_without_traceback(self):
        with tempfile.TemporaryDirectory() as directory:
            proc = subprocess.run([sys.executable, str(Path(__file__).with_name("compat_doctor.py")), "report", directory],
                                  capture_output=True, text=True, timeout=10)
        self.assertEqual(proc.returncode, 2)
        self.assertNotIn("Traceback", proc.stderr)

    def test_replay_report_is_visibly_distinct(self):
        scenario = demo_scenario(19363)
        sample = observation(scenario)
        sample["recordingMode"] = "intercepted-archive-replay"
        report = compare(sample, sample, scenario)
        self.assertIn("INTERCEPTED DIAGNOSTIC", report["limitations"][1])


class WorkflowTests(unittest.IsolatedAsyncioTestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.scenario = demo_scenario(19363)
        self.name = "DOMException tag after prototype removal"

    def pair(self, folder, scenario, differs=True, gap=False):
        folder.mkdir()
        write_json(folder / "scenario.json", scenario)
        for kind in ("chrome", "mimic"):
            capture_folder = folder / kind
            capture_folder.mkdir()
            sample = observation(scenario)
            sample.update(browser=kind, scenario=scenario, version={"Browser": PINNED_PRODUCT},
                          initialEnvironment={"webdriver": False})
            if kind == "mimic" and differs:
                sample["checks"][self.name]["value"] = "[object Error]"
            if gap:
                sample["gaps"].append({"kind": "injected-loss", "detail": "Synthetic fault"})
            write_json(capture_folder / "capture.json", sample)
            seal(capture_folder)

    def test_cli_all_three_verdict_exit_codes(self):
        for expected, differs, gap in ((0, False, False), (1, True, False), (2, False, True)):
            folder = self.root / str(expected)
            self.pair(folder, self.scenario, differs, gap)
            process = subprocess.run([sys.executable, str(Path(__file__).with_name("compat_doctor.py")), "report", str(folder)],
                                     capture_output=True, text=True, timeout=10)
            self.assertEqual(process.returncode, expected, process.stderr)
            self.assertTrue((folder / "report.html").is_file())

    async def test_minimize_workflow_retains_exact_failure_and_all_attempts(self):
        self.scenario["actions"] += [{"type": "wait", "seconds": 0}]
        source = self.root / "source"
        self.pair(source, self.scenario)
        output = self.root / "reduced"
        args = SimpleNamespace(folder=source, out=output, check=self.name, budget=10)
        async def trial(args, folder, scenario):
            self.pair(folder, scenario, differs=any(action["type"] == "click" for action in scenario["actions"]))
        with patch("compat_doctor.replay_pair", side_effect=trial):
            self.assertEqual(await minimize(args), 0)
        reduced = json.loads((output / "scenario.json").read_text())
        summary = json.loads((output / "reduction.json").read_text())
        attempts = json.loads((output / "attempts.json").read_text())
        self.assertEqual(reduced["actions"], [{"type": "click", "selector": "#run"}])
        self.assertTrue(summary["oneActionDeletionMinimal"])
        self.assertGreaterEqual(len(attempts), 5)
        for row in attempts:
            verify(output / row["folder"] / "chrome")
            verify(output / row["folder"] / "mimic")

    async def test_flaky_baseline_blocks_reduction(self):
        source = self.root / "source"
        self.pair(source, self.scenario)
        output = self.root / "reduced"
        args = SimpleNamespace(folder=source, out=output, check=self.name, budget=10)
        calls = 0
        async def trial(args, folder, scenario):
            nonlocal calls
            calls += 1
            self.pair(folder, scenario, differs=calls == 1)
        with patch("compat_doctor.replay_pair", side_effect=trial):
            self.assertEqual(await minimize(args), 2)
        self.assertEqual(calls, 2)
        self.assertFalse((output / "scenario.json").exists())
        self.assertEqual(json.loads((output / "reduction.json").read_text())["status"], "inconclusive")


if __name__ == "__main__":
    unittest.main()
