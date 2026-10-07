"""Focused correctness gates for evidence loss, reports, launch, and transport."""
import asyncio
import copy
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import AsyncMock, patch

from aiohttp import web
import aiohttp

from compat_doctor import demo_scenario, validate_scenario
from doctor_capture import Connection, Recorder, launch_arguments
from doctor_report import compare, exact, render_report, seal, verify, write_json


def observation(scenario):
    return {"state": "finished", "gaps": [], "checks": {
        check["name"]: {"available": True, "value": check["equals"]} for check in scenario["checks"]},
        "requests": [], "exceptions": [], "consoleProblems": [], "networkFailures": [],
        "initialEnvironment": {"webdriver": False}, "finalEnvironment": {"webdriver": False}, "finalCookies": []}


class ReportTests(unittest.TestCase):
    def setUp(self):
        self.scenario = demo_scenario(19363)
        self.reference = observation(self.scenario)

    def test_matching_declared_observations(self):
        result = compare(self.reference, copy.deepcopy(self.reference), self.scenario)
        self.assertEqual(result["status"], "no-observed-differences")
        self.assertTrue(result["captureCompleteWithinScope"])
        self.assertIn("not a full Chrome", result["limitations"][0])

    def test_same_failed_expectation_is_not_success(self):
        self.reference["checks"]["Application started"]["value"] = "Starting"
        result = compare(self.reference, copy.deepcopy(self.reference), self.scenario)
        self.assertEqual(result["status"], "inconclusive")

    def test_missing_check_is_not_equal_to_missing_check(self):
        self.reference["checks"].clear()
        result = compare(self.reference, copy.deepcopy(self.reference), self.scenario)
        self.assertEqual(result["status"], "inconclusive")
        self.assertFalse(any(c["matches"] for c in result["checks"]))

    def test_capture_loss_prevents_green(self):
        for kind in ("response-body-unavailable", "connection-lost", "child-startup-not-observed", "capture-tasks-incomplete"):
            with self.subTest(kind=kind):
                candidate = copy.deepcopy(self.reference)
                candidate["gaps"].append({"kind": kind, "detail": "lost"})
                result = compare(self.reference, candidate, self.scenario)
                self.assertEqual(result["status"], "inconclusive")
                self.assertFalse(result["captureCompleteWithinScope"])

    def test_difference_survives_incomplete_capture(self):
        candidate = copy.deepcopy(self.reference)
        candidate["checks"]["DOMException tag after prototype removal"]["value"] = "[object Error]"
        candidate["gaps"].append({"kind": "response-body-unavailable", "detail": "lost"})
        result = compare(self.reference, candidate, self.scenario)
        self.assertEqual(result["status"], "differences")
        self.assertFalse(result["captureCompleteWithinScope"])
        self.assertEqual(result["findings"][0]["runtimeCause"], "unproven")

    def test_no_checks_cannot_pass(self):
        scenario = validate_scenario({"url": "https://example.test/"})
        self.assertEqual(compare(self.reference, self.reference, scenario)["status"], "inconclusive")

    def test_exception_multiplicity_is_preserved(self):
        candidate = copy.deepcopy(self.reference)
        self.reference["exceptions"] = [{"text": "boom"}]
        candidate["exceptions"] = [{"text": "boom"}, {"text": "boom"}]
        result = compare(self.reference, candidate, self.scenario)
        self.assertEqual(result["findings"][0]["mimic"], 2)

    def test_types_and_absence_are_not_normalized(self):
        self.assertFalse(exact(True, 1))
        self.assertFalse(exact({"value": None}, {}))
        self.assertFalse(exact([1, 2], [2, 1]))

    def test_environment_and_headers_cannot_silently_match(self):
        for key, value in (("initialEnvironment", {"hasFocus": False}),
                           ("finalCookies", [{"name": "session", "value": "different"}])):
            with self.subTest(key=key):
                candidate = copy.deepcopy(self.reference)
                candidate[key] = value
                result = compare(self.reference, candidate, self.scenario)
                self.assertEqual(result["status"], "inconclusive")
                self.assertEqual(result["contextDifferences"][0]["kind"], key)
        candidate = copy.deepcopy(self.reference)
        self.reference["requests"] = [{"url": "http://example.test/", "request": {"headers": {"X-Test": "a"}}}]
        candidate["requests"] = [{"url": "http://example.test/", "request": {"headers": {"X-Test": "b"}}}]
        result = compare(self.reference, candidate, self.scenario)
        self.assertEqual(result["status"], "inconclusive")
        self.assertEqual(result["contextDifferences"][0]["kind"], "headers-and-upload-data")

    def test_html_escapes_site_content(self):
        candidate = copy.deepcopy(self.reference)
        candidate["checks"]["Application started"]["value"] = '</pre><script>alert("x")</script>'
        result = compare(self.reference, candidate, self.scenario)
        with tempfile.TemporaryDirectory() as directory:
            render_report(Path(directory), result, self.scenario)
            output = (Path(directory) / "report.html").read_text(encoding="utf-8")
        self.assertNotIn("<script>", output)
        self.assertIn("&lt;script&gt;", output)
        self.assertIn("default-src 'none'", output)


class EvidenceTests(unittest.TestCase):
    def test_seal_and_integrity_fail_closed(self):
        with tempfile.TemporaryDirectory() as directory:
            folder = Path(directory)
            write_json(folder / "capture.json", {"state": "finished"})
            seal(folder)
            self.assertEqual(verify(folder)["state"], "finished")
            with self.assertRaises(FileExistsError):
                seal(folder)
            write_json(folder / "capture.json", {"state": "modified"})
            with self.assertRaisesRegex(ValueError, "integrity"):
                verify(folder)

    def test_unlisted_evidence_rejected(self):
        with tempfile.TemporaryDirectory() as directory:
            folder = Path(directory)
            write_json(folder / "capture.json", {})
            seal(folder)
            (folder / "extra.txt").write_text("unverified")
            with self.assertRaisesRegex(ValueError, "file set"):
                verify(folder)

    def test_launch_has_no_automation_or_headless_overrides(self):
        command = launch_arguments("chrome", Path("chrome.exe"), 19361, str(Path(tempfile.gettempdir()).resolve()))
        self.assertEqual(len(command), 7)
        self.assertIn("--remote-debugging-port=19361", command)
        for forbidden in ("--enable-automation", "--headless", "AutomationControlled", "--disable-quic", "--proxy"):
            self.assertFalse(any(forbidden in value for value in command))
        with self.assertRaises(ValueError):
            launch_arguments("chrome", Path("chrome.exe"), 0, "/profile")

    def test_headless_launch_is_explicit_and_retains_original_identity(self):
        command = launch_arguments("chrome", Path("chrome.exe"), 19361, str(Path(tempfile.gettempdir()).resolve()), headless=True)
        self.assertIn("--headless=new", command)
        self.assertIn("--remote-debugging-port=19361", command)
        self.assertFalse(any("enable-automation" in arg or "AutomationControlled" in arg for arg in command))

    def test_scenario_rejects_ambiguous_or_ignored_fields(self):
        for value in ({"url": "file:///secret"}, {"url": "https://example.test", "unknown": True},
                      {"url": "https://example.test", "settleSeconds": float("nan")},
                      {"url": "https://example.test", "actions": [{"type": "eval", "code": "x"}]}):
            with self.subTest(value=value), self.assertRaises(ValueError):
                validate_scenario(value)


class RecorderTests(unittest.IsolatedAsyncioTestCase):
    async def test_oopif_navigation_finishes_on_its_new_session(self):
        with tempfile.TemporaryDirectory() as directory:
            recorder = Recorder("chrome", Path(directory), {})
            (Path(directory) / "bodies").mkdir()
            recorder.phase = "recording"
            recorder.sessions.update(parent={"targetId": "top"}, child={"targetId": "frame", "parentId": "top"})
            recorder.cdp = type("Peer", (), {"call": AsyncMock(return_value={"body": "iframe", "base64Encoded": False})})()
            recorder.event({"method": "Network.requestWillBeSent", "sessionId": "parent", "params": {
                "requestId": "navigation", "request": {"url": "https://child.test/", "method": "GET"}}})
            recorder.event({"method": "Network.responseReceived", "sessionId": "parent", "params": {
                "requestId": "navigation", "response": {"status": 200}}})
            recorder.event({"method": "Network.loadingFinished", "sessionId": "child", "params": {"requestId": "navigation"}})
            await recorder.drain()
            row = recorder.result["requests"][0]
            self.assertEqual((row["completion"], row["requestSession"], row["session"], row["bodyBytes"]), ("finished", "parent", "child", 6))
            self.assertEqual(recorder.result["gaps"], [])
            recorder.cdp.call.assert_awaited_once_with("Network.getResponseBody", {"requestId": "navigation"}, "child")

    async def test_same_request_id_on_unrelated_targets_is_not_merged(self):
        recorder = Recorder("chrome", Path("unused"), {})
        recorder.phase = "recording"
        recorder.sessions.update(a={"targetId": "one"}, b={"targetId": "two"})
        recorder.event({"method": "Network.requestWillBeSent", "sessionId": "a", "params": {
            "requestId": "1", "request": {"url": "https://one.test/", "method": "GET"}}})
        recorder.event({"method": "Network.loadingFailed", "sessionId": "b", "params": {"requestId": "1", "errorText": "Failed"}})
        self.assertEqual(recorder.result["requests"][0]["completion"], "pending")
        self.assertEqual(recorder.result["gaps"][0]["kind"], "request-start-missing")

    async def test_child_diagnostic_enables_domains_before_resuming(self):
        recorder = Recorder("chrome", Path("unused"), {}, pause_children=True)
        recorder.main_session = "main"
        recorder.phase = "recording"
        recorder.cdp = type("Peer", (), {"call": AsyncMock(return_value={})})()
        recorder.event({"method": "Target.attachedToTarget", "params": {"sessionId": "child",
            "waitingForDebugger": True, "targetInfo": {"targetId": "frame", "type": "iframe"}}})
        await recorder.drain()
        calls = recorder.cdp.call.await_args_list
        self.assertEqual(calls[-1].args[0], "Runtime.runIfWaitingForDebugger")
        self.assertTrue(next(c.args[1]["waitForDebuggerOnStart"] for c in calls if c.args[0] == "Target.setAutoAttach"))
        self.assertEqual(recorder.result["gaps"], [])
        self.assertTrue(recorder.result["scope"]["pauseOnStart"])

    async def test_drain_waits_for_pending_network_even_without_capture_tasks(self):
        recorder = Recorder("chrome", Path("unused"), {})
        recorder.result["requests"].append({"completion": "pending", "url": "https://example.test/"})
        async def finish():
            await asyncio.sleep(.02)
            recorder.result["requests"][0]["completion"] = "failed"
        task = asyncio.create_task(finish())
        await recorder.drain()
        await task
        self.assertEqual(recorder.result["gaps"], [])

    async def test_terminal_evaluation_selects_top_frame_default_context(self):
        recorder = Recorder("chrome", Path("unused"), {})
        recorder.main_session = "main"
        recorder.sessions["main"] = {"targetId": "top"}
        recorder.cdp = type("Peer", (), {"call": AsyncMock(return_value={"result": {"value": "top"}})})()
        for ident, frame, default in [(10, "top", True), (20, "child", True), (30, "top", False)]:
            recorder.event({"method": "Runtime.executionContextCreated", "sessionId": "main", "params": {
                "context": {"id": ident, "auxData": {"frameId": frame, "isDefault": default}}}})
        self.assertEqual(await recorder.evaluate("location.href"), "top")
        self.assertEqual(recorder.cdp.call.await_args.args[1]["contextId"], 10)
        recorder.event({"method": "Runtime.executionContextsCleared", "sessionId": "main", "params": {}})
        self.assertIsNone(recorder.default_context)

    async def test_final_presentation_records_actual_state_without_restoring(self):
        recorder = Recorder("chrome", Path("unused"), {})
        recorder.main_session = "main"
        recorder.sessions["main"] = {"targetId": "page"}
        recorder.result.update(launch={"pid": 123}, finalEnvironment={
            "visibilityState": "hidden", "window": {"outerWidth": 0}})
        recorder.cdp = type("Peer", (), {"call": AsyncMock(return_value={
            "windowId": 1, "bounds": {"windowState": "minimized"}})})()
        state = [{"handle": 456, "visible": True, "minimized": True}]
        with patch("doctor_capture.owned_windows", return_value=state) as windows:
            await recorder.observe_presentation()
        windows.assert_called_once_with(123)
        recorder.cdp.call.assert_awaited_once_with("Browser.getWindowForTarget", {"targetId": "page"})
        self.assertEqual(recorder.result["launch"]["finalNativeWindows"], state)
        self.assertEqual(recorder.result["gaps"][0]["kind"], "presentation-state-unverified")
        self.assertEqual(recorder.result["gaps"][0]["detail"]["nativeWindows"], state)

    async def test_final_window_query_failure_is_an_explicit_gap(self):
        recorder = Recorder("chrome", Path("unused"), {})
        recorder.main_session = "main"
        recorder.sessions["main"] = {"targetId": "page"}
        recorder.result.update(launch={"pid": 123}, finalEnvironment={
            "visibilityState": "visible", "window": {"outerWidth": 1280}})
        recorder.cdp = type("Peer", (), {"call": AsyncMock(side_effect=RuntimeError("lost window"))})()
        with patch("doctor_capture.owned_windows", return_value=[]) as windows:
            await recorder.observe_presentation()
        windows.assert_called_once_with(123)
        self.assertEqual(recorder.result["launch"]["finalNativeWindows"], [])
        self.assertEqual(recorder.result["gaps"][0]["kind"], "final-window-state-unavailable")

    async def test_late_body_event_and_child_attach_are_not_silent(self):
        recorder = Recorder("chrome", Path("unused"), {})
        recorder.phase = "recording"
        recorder.main_session = "main"
        recorder.event({"method": "Network.loadingFinished", "sessionId": "lost",
                        "params": {"requestId": "1"}})
        self.assertEqual(recorder.result["gaps"][0]["kind"], "request-start-missing")
        class Fake:
            async def call(self, *args, **kwargs):
                return {}
        recorder.cdp = Fake()
        recorder.event({"method": "Target.attachedToTarget", "params": {
            "sessionId": "worker", "targetInfo": {"targetId": "child", "type": "worker", "url": "worker.js"}}})
        await recorder.drain()
        self.assertTrue(any(g["kind"] == "child-startup-not-observed" for g in recorder.result["gaps"]))

    async def test_body_failure_remains_an_evidence_gap(self):
        class Broken:
            async def call(self, *_):
                raise RuntimeError("body evicted")
        with tempfile.TemporaryDirectory() as directory:
            recorder = Recorder("chrome", Path(directory), {})
            recorder.cdp = Broken()
            await recorder.body({"index": 0, "url": "https://example.test/", "method": "GET",
                                 "requestId": "1", "session": "main", "status": 200})
            self.assertEqual(recorder.result["gaps"][0]["kind"], "response-body-unavailable")

    async def test_redirect_chain_keeps_both_requests(self):
        recorder = Recorder("chrome", Path("unused"), {})
        recorder.phase = "recording"
        def event(params):
            recorder.event({"method": "Network.requestWillBeSent", "sessionId": "main", "params": params})
        event({"requestId": "1", "request": {"url": "https://example.test/a", "method": "GET"}})
        event({"requestId": "1", "redirectResponse": {"status": 302},
               "request": {"url": "https://example.test/b", "method": "GET"}})
        rows = recorder.result["requests"]
        self.assertEqual(len(rows), 2)
        self.assertEqual(rows[0]["completion"], "redirect")
        self.assertEqual(rows[1]["completion"], "pending")

    async def test_disconnect_resolves_pending_calls_and_keeps_raw_events(self):
        async def socket(request):
            ws = web.WebSocketResponse()
            await ws.prepare(request)
            await ws.receive_json()
            await ws.send_json({"method": "Runtime.exceptionThrown", "params": {"exceptionDetails": {"text": "boom"}}})
            await ws.close()
            return ws
        app = web.Application()
        app.router.add_get("/", socket)
        runner = web.AppRunner(app)
        await runner.setup()
        site = web.TCPSite(runner, "127.0.0.1", 0)
        await site.start()
        port = site._server.sockets[0].getsockname()[1]
        try:
            with tempfile.TemporaryDirectory() as directory:
                folder = Path(directory)
                recorder = Recorder("chrome", folder, {})
                recorder.phase = "recording"
                async with aiohttp.ClientSession() as http:
                    ws = await http.ws_connect(f"http://127.0.0.1:{port}/")
                    connection = Connection(ws, folder, recorder)
                    with self.assertRaises(ConnectionError):
                        await connection.call("Runtime.evaluate", timeout=2)
                    await connection.close()
                self.assertEqual(recorder.result["exceptions"][0]["text"], "boom")
                self.assertTrue(any(g["kind"] == "connection-lost" for g in recorder.result["gaps"]))
                self.assertIn("Runtime.exceptionThrown", (folder / "events.jsonl").read_text())
        finally:
            await runner.cleanup()


if __name__ == "__main__":
    unittest.main()
