"""Agent-facing CLI and evidence access contracts, without browser launches."""
import copy
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

from compat_doctor import demo_scenario
from doctor_agent import read_events, read_evidence
from doctor_capture import PINNED_PRODUCT
from doctor_report import compare, seal, write_json
from doctor_replay import reduce_actions
from test_compat_doctor import observation


class AgentTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.folder = Path(self.temp.name)
        self.scenario = demo_scenario(19363)
        write_json(self.folder / "scenario.json", self.scenario)
        for kind in ("chrome", "mimic"):
            folder = self.folder / kind
            folder.mkdir()
            value = observation(self.scenario)
            value.update(browser=kind, scenario=self.scenario, version={"Browser": PINNED_PRODUCT},
                         initialEnvironment={"webdriver": False})
            if kind == "mimic":
                value["checks"]["Application started"]["value"] = "Broken"
            write_json(folder / "capture.json", value)
            write_json(folder / "native-trace.json", {"events": [{"name": "task", "sequence": n} for n in range(4)]})
            (folder / "events.jsonl").write_text("\n".join(json.dumps({"method": method, "params": {"index": index}})
                for index, method in enumerate(["A", "B", "A", "A"])) + "\n", encoding="utf-8")
            seal(folder)

    def cli(self, *args):
        process = subprocess.run([sys.executable, str(Path(__file__).with_name("compat_doctor.py")), *args, "--json"],
                                 capture_output=True, encoding="utf-8", timeout=10)
        return process, json.loads(process.stdout)

    def test_inspect_is_read_only_and_findings_resolve(self):
        process, data = self.cli("inspect", str(self.folder))
        self.assertEqual(process.returncode, 0)
        self.assertFalse((self.folder / "report.json").exists())
        finding = data["result"]["findings"][0]
        _, detail = self.cli("inspect", str(self.folder), "--finding", finding["id"])
        self.assertEqual(detail["result"]["finding"]["id"], finding["id"])
        for ref in finding["evidenceRefs"]:
            browser, filename = ref["file"].split("/", 1)
            selected = read_evidence(self.folder, browser, filename, ref["pointer"])
            self.assertIsInstance(selected["value"], dict)

    def test_report_stdout_is_one_json_with_difference_exit_code(self):
        process, data = self.cli("report", str(self.folder))
        self.assertEqual(process.returncode, 1)
        self.assertEqual(data["exitCode"], 1)
        self.assertEqual(data["result"]["status"], "differences")
        self.assertIn("Report:", process.stderr)

    def test_errors_are_json_and_nonzero(self):
        process, data = self.cli("inspect", str(self.folder), "--finding", "unknown")
        self.assertEqual(process.returncode, 2)
        self.assertEqual(data["error"]["type"], "ValueError")
        self.assertNotIn("Traceback", process.stderr)

    def test_usage_errors_are_also_machine_readable(self):
        process, data = self.cli("events", str(self.folder), "--browser", "wrong")
        self.assertEqual(process.returncode, 2)
        self.assertEqual(data["error"]["type"], "UsageError")

    def test_json_flag_before_subcommand_is_supported(self):
        process, data = self.cli("--json", "inspect", str(self.folder))
        self.assertEqual(process.returncode, 0)
        self.assertEqual(data["command"], "inspect")

    def test_both_missing_context_cannot_be_green(self):
        before = observation(self.scenario)
        for key in ("initialEnvironment", "finalEnvironment", "finalCookies"):
            sample = copy.deepcopy(before)
            del sample[key]
            self.assertEqual(compare(sample, sample, self.scenario)["status"], "inconclusive")

    def test_filtered_event_pagination_does_not_lose_or_duplicate_rows(self):
        first = read_events(self.folder, "chrome", "A", limit=2)
        second = read_events(self.folder, "chrome", "A", offset=first["nextOffset"], limit=2)
        self.assertEqual([item["line"] for item in first["items"] + second["items"]], [1, 3, 4])
        self.assertIsNone(second["nextOffset"])

    def test_native_trace_pages_have_resolvable_json_pointers(self):
        page = read_events(self.folder, "mimic", "task", offset=1, limit=2, source="native-trace")
        self.assertEqual(page["nextOffset"], 3)
        self.assertEqual([row["pointer"] for row in page["items"]], ["/events/1", "/events/2"])
        self.assertNotIn("line", page["items"][0])
        for row in page["items"]:
            self.assertEqual(read_evidence(self.folder, "mimic", "native-trace.json", row["pointer"])["value"], row["event"])

    def test_evidence_rejects_traversal_and_invalid_pointer(self):
        for filename, pointer in (("../scenario.json", ""), ("capture.json", "checks"), ("capture.json", "/absent")):
            with self.subTest(filename=filename, pointer=pointer), self.assertRaises((ValueError, KeyError)):
                read_evidence(self.folder, "chrome", filename, pointer)

    def test_tampered_evidence_never_reaches_agent(self):
        (self.folder / "chrome/events.jsonl").write_text("{}\n")
        with self.assertRaisesRegex(ValueError, "integrity"):
            read_events(self.folder, "chrome")

    def test_finding_id_does_not_depend_on_other_findings_or_values(self):
        before = observation(self.scenario)
        after = copy.deepcopy(before)
        after["checks"]["Application started"]["value"] = "failure1"
        first = compare(before, after, self.scenario)["findings"][0]["id"]
        after["checks"]["Application started"]["value"] = "failure2"
        after["exceptions"] = [{"text": "boom"}]
        self.assertEqual(compare(before, after, self.scenario)["findings"][0]["id"], first)


class ReductionEvidenceTests(unittest.IsolatedAsyncioTestCase):
    async def test_unobserved_deletion_is_not_proof_of_minimality(self):
        async def missing(_):
            return None
        best, _, minimal = await reduce_actions(["click"], missing)
        self.assertEqual(best, ["click"])
        self.assertFalse(minimal)


if __name__ == "__main__":
    unittest.main()
