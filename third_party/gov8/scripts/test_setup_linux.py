import tempfile
import unittest
from pathlib import Path

from setup_linux import verify_archive_configuration


class ArchiveConfigurationTests(unittest.TestCase):
    def test_matching_multi_cage_archive_is_accepted(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            (output / 'obj').mkdir()
            archive = output / 'obj' / 'librusty_v8.a'
            archive.write_bytes(b'archive fixture')
            (output / 'args.gn').write_text('''v8_enable_pointer_compression = true
v8_enable_pointer_compression_shared_cage = false
v8_enable_sandbox = false
v8_monolithic_for_shared_library = true
''')
            metadata = verify_archive_configuration(archive)
            self.assertTrue(metadata['features']['v8_enable_pointer_compression'])
            self.assertEqual(len(metadata['archive_sha256']), 64)

    def test_mismatched_or_missing_switch_is_rejected(self):
        required = {
            'v8_enable_pointer_compression': 'true',
            'v8_enable_pointer_compression_shared_cage': 'false',
            'v8_enable_sandbox': 'false',
            'v8_monolithic_for_shared_library': 'true',
        }
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory)
            (output / 'obj').mkdir()
            archive = output / 'obj' / 'librusty_v8.a'
            archive.write_bytes(b'archive fixture')
            for name, value in required.items():
                for replacement in [None, 'false' if value == 'true' else 'true']:
                    with self.subTest(name=name, replacement=replacement):
                        settings = dict(required)
                        if replacement is None:
                            del settings[name]
                        else:
                            settings[name] = replacement
                        (output / 'args.gn').write_text(''.join(f'{key} = {item}\n' for key, item in settings.items()))
                        with self.assertRaisesRegex(RuntimeError, name):
                            verify_archive_configuration(archive)


if __name__ == '__main__':
    unittest.main()
