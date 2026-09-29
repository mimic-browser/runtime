import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch
from prepare import digest, resolve_unbundled_links, run, validate_document_links
from publish import CHECKS, verified_archive


class PublicationGateTests(unittest.TestCase):
    def test_release_commands_decode_utf8_independent_of_host_locale(self):
        with patch('prepare.subprocess.run') as execute:
            execute.return_value.stdout = 'Mimic — verified'
            self.assertEqual(run('go', 'list', capture=True), 'Mimic — verified')
            self.assertEqual(execute.call_args.kwargs['encoding'], 'utf-8')

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.archive = self.root / 'mimic-v0.1.0-beta.1-windows-amd64.zip'
        self.archive.write_bytes(b'checked archive bytes')
        self.receipt = {
            'version': 'v0.1.0-beta.1', 'platform': 'windows-amd64',
            'sourceRevision': 'source',
            'verified': True, 'checks': list(CHECKS), 'archive': self.archive.name,
            'sha256': digest(self.archive), 'size': self.archive.stat().st_size,
        }

    def check(self):
        (self.root / 'windows-amd64.receipt.json').write_text(json.dumps(self.receipt))
        return verified_archive(self.root, 'v0.1.0-beta.1', 'windows', 'source')

    def test_verified_matching_archive(self):
        self.assertEqual(self.check()[0], self.archive)

    def test_reused_binary_receipt_keeps_distinct_packaging_revision(self):
        self.receipt['sourceRevision'] = 'binary-source'
        self.receipt['packagingRevision'] = 'release-commit'
        (self.root / 'windows-amd64.receipt.json').write_text(json.dumps(self.receipt))
        self.assertEqual(verified_archive(self.root, 'v0.1.0-beta.1', 'windows',
                                          'release-commit')[0], self.archive)
        with self.assertRaisesRegex(RuntimeError, 'Stale or unverified'):
            verified_archive(self.root, 'v0.1.0-beta.1', 'windows', 'other-commit')

    def test_modified_archive_rejected(self):
        self.archive.write_bytes(b'changed archive bytes')
        with self.assertRaisesRegex(RuntimeError, 'Archive changed'):
            self.check()

    def test_stale_source_or_examples_and_missing_checks_rejected(self):
        for key, value in [('sourceRevision', 'old'), ('verified', False),
                           ('checks', ['runtimecheck:v8'])]:
            with self.subTest(key=key):
                original = self.receipt[key]
                self.receipt[key] = value
                with self.assertRaisesRegex(RuntimeError, 'Stale or unverified'):
                    self.check()
                self.receipt[key] = original

    def test_outside_archive_path_rejected(self):
        self.receipt['archive'] = '../private-source.zip'
        with self.assertRaisesRegex(RuntimeError, 'Unexpected archive name'):
            self.check()

    def test_bundled_document_link_cannot_be_missing_or_escape(self):
        doc = self.root / 'README.md'
        doc.write_text('[Results](benchmarks/results.json)')
        with self.assertRaisesRegex(RuntimeError, 'Missing bundled document link'):
            validate_document_links(self.root)
        (self.root / 'benchmarks').mkdir()
        (self.root / 'benchmarks/results.json').write_text('{}')
        validate_document_links(self.root)
        doc.write_text('[Outside](../)')
        with self.assertRaisesRegex(RuntimeError, 'Missing bundled document link'):
            validate_document_links(self.root)

    def test_repository_document_link_is_bundled_for_archive(self):
        stage = self.root / 'package'
        (stage / 'examples').mkdir(parents=True)
        document = stage / 'examples' / 'README.md'
        document.write_text('[Guide](../docs/compatibility/crawlee-playwright.md)')
        guide = stage / 'docs/compatibility/crawlee-playwright.md'
        guide.parent.mkdir(parents=True)
        guide.write_text('# Crawlee guide\n')
        resolve_unbundled_links(stage, 'revision')
        self.assertEqual(document.read_text(),
                         '[Guide](../docs/compatibility/crawlee-playwright.md)')
        self.assertTrue((stage / 'docs/compatibility/crawlee-playwright.md').is_file())
        validate_document_links(stage)


if __name__ == '__main__':
    unittest.main()
