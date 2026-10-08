"""Offline regressions for schema completeness, wire types and support claims."""

from pathlib import Path
import tempfile
import unittest
import json

import generate_cdp as generator


class CDPGenerationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.source, cls.provenance = generator.read_source()

    def test_source_is_complete_and_resolves_all_refs(self):
        types = generator.validate_schema(self.source)
        self.assertEqual(len(types), 611)
        self.assertIn("Runtime.CallArgument", types)
        self.assertIn("Schema.Domain", types)
        self.assertIn("Network.CookiePartitionKey", types)

    def test_unknown_ref_and_duplicate_field_are_rejected(self):
        for command in [
            {"name": "test", "parameters": [{"name": "x", "$ref": "Missing"}]},
            {"name": "test", "parameters": [{"name": "x", "type": "integer"}] * 2},
            {"name": "test", "parameters": [{"name": "x", "type": "array"}]},
        ]:
            with self.assertRaises(ValueError):
                generator.validate_schema({"domains": [{"domain": "Test", "commands": [command]}]})

    def test_alias_cycle_rejected_but_recursive_objects_survive(self):
        bad = {"domains": [{"domain": "Test", "types": [
            {"id": "A", "$ref": "B"}, {"id": "B", "$ref": "A"},
        ]}]}
        with self.assertRaisesRegex(ValueError, "alias cycle"):
            generator.validate_schema(bad)
        good = {"domains": [{"domain": "Test", "types": [
            {"id": "A", "type": "object", "properties": [{"name": "next", "$ref": "A", "optional": True}]},
        ]}]}
        generator.validate_schema(good)

    def test_inventory_never_claims_generated_means_implemented(self):
        manifest = {"Runtime.evaluate": {"status": "partial", "notes": "Execution contexts remain limited", "tests": ["TestEvaluate"]}}
        output = generator.inventory(self.source, self.provenance, manifest)
        entries = {row["name"]: row for row in output["entries"]}
        self.assertEqual(entries["Runtime.evaluate"]["support"], manifest["Runtime.evaluate"])
        self.assertEqual(entries["Runtime.getHeapUsage"]["support"]["status"], "unsupported")
        self.assertTrue(entries["Runtime.getHeapUsage"]["wireSchemaGenerated"])
        with self.assertRaises(ValueError):
            generator.inventory(self.source, self.provenance, {"Storage.enable": {"status": "implemented"}})
        with self.assertRaises(ValueError):
            generator.inventory(self.source, self.provenance, {"Runtime.evaluate": {"status": "verified"}})
        with self.assertRaisesRegex(ValueError, "scope/limitations"):
            generator.inventory(self.source, self.provenance, {"Runtime.evaluate": {"status": "partial"}})
        with self.assertRaisesRegex(ValueError, "regression evidence"):
            generator.inventory(self.source, self.provenance, {"Runtime.evaluate": {"status": "implemented", "notes": "Declared scope"}})

    def test_mimic_extensions_do_not_modify_frozen_wire_counts(self):
        manifest = {"Mimic.setMediaProfile": {"status": "partial", "notes": "Context-owned media catalog", "tests": ["TestMediaProfile"]}}
        output = generator.inventory(self.source, self.provenance, manifest)
        self.assertEqual(len(output["entries"]), 899)
        self.assertEqual(sum(output["summary"]["methods"].values()), 665)
        self.assertEqual(output["extensions"][0]["support"], manifest["Mimic.setMediaProfile"])
        self.assertFalse(output["extensions"][0]["wireSchemaGenerated"])
        with self.assertRaises(ValueError):
            generator.inventory(self.source, self.provenance, {"Mimic.bad-name": manifest["Mimic.setMediaProfile"]})

    def test_offline_check_is_deterministic_and_detects_drift(self):
        # Running twice in fresh output directories catches ordering instability;
        # no network modules or browsers participate in either projection.
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for relative in (generator.SOURCE, generator.SOURCE.with_suffix(".source.json"), generator.MIMIC_SOURCE, generator.MIMIC_SOURCE.with_suffix(".source.json"), Path("chrome/152/target.json")):
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes((generator.ROOT / relative).read_bytes())
            generator.generate(root)
            before = {p.name: p.read_bytes() for p in (root / "internal/cdp").glob("*_generated.*")}
            generator.generate(root, check=True)
            generator.generate(root)
            self.assertEqual(before, {p.name: p.read_bytes() for p in (root / "internal/cdp").glob("*_generated.*")})
            coverage = root / "docs/cdp-coverage-generated.md"
            self.assertIn("665 commands", coverage.read_text(encoding="utf-8"))
            coverage.write_bytes(coverage.read_bytes() + b"\n")
            with self.assertRaisesRegex(ValueError, "artifact drift"):
                generator.generate(root, check=True)
            generator.generate(root)
            registry = root / "internal/cdp/protocol_registry_generated.go"
            registry.write_bytes(registry.read_bytes() + b"\n")
            with self.assertRaisesRegex(ValueError, "artifact drift"):
                generator.generate(root, check=True)
            retained = root / generator.SOURCE
            retained.write_bytes(retained.read_bytes() + b"\n")
            with self.assertRaisesRegex(ValueError, "source hash mismatch"):
                generator.read_source(root)

    def test_sdk_snapshot_projects_all_shapes_without_promoting_semantics(self):
        mimic, provenance = generator.read_mimic_source()
        output = generator.inventory(self.source, self.provenance, {}, mimic, provenance)
        self.assertEqual(len(output["entries"]), 899)
        self.assertEqual(len(output["extensions"]), len(mimic["commands"]))
        for row in output["extensions"]:
            self.assertTrue(row["wireSchemaGenerated"])
            self.assertEqual(row["support"]["status"], "unsupported")
            self.assertEqual(row["schemaSHA256"], provenance["sha256"])
        command = next(row for row in output["extensions"] if row["name"] == "Mimic.configureContext")
        self.assertEqual(command["requiredParameters"], ["browserContextId"])
        self.assertNotIn("disposeOnDetach", command["optionalParameters"])


if __name__ == "__main__":
    unittest.main()
