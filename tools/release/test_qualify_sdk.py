import io
import json
import os
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import MagicMock, patch

import qualify_sdk


class SdkDispatchTests(unittest.TestCase):
    def assets(self):
        binary = b"verified executable bytes"
        stream = io.BytesIO()
        with tarfile.open(fileobj=stream, mode="w:gz") as archive:
            member = tarfile.TarInfo("mimic-v0.1.0-linux-amd64/mimic")
            member.size = len(binary); archive.addfile(member, io.BytesIO(binary))
        archive = stream.getvalue()
        manifest = json.dumps({"version":"v0.1.0", "sourceRevision":"a" * 40, "artifacts":[{
            "platform":"linux-amd64", "archive":"mimic-v0.1.0-linux-amd64.tar.gz",
            "binaryVersion":"v0.1.0", "binarySha256":qualify_sdk.sha(binary),
            "sha256":qualify_sdk.sha(archive), "size":len(archive)}]}).encode()
        assets = {"release-manifest.json":manifest, "mimic-v0.1.0-linux-amd64.tar.gz":archive}
        assets["SHA256SUMS"] = "".join(qualify_sdk.sha(data) + "  " + name + "\n" for name, data in assets.items()).encode()
        return assets

    def test_dispatch_payload_binds_verified_archive_and_binary(self):
        assets = self.assets()
        with patch.object(qualify_sdk, "fetch", side_effect=lambda url: assets[url.rsplit("/", 1)[1]]):
            result = qualify_sdk.verified_payload("v0.1.0")
        self.assertEqual(result["runtime_archive_sha256"], qualify_sdk.sha(assets["mimic-v0.1.0-linux-amd64.tar.gz"]))
        self.assertEqual(result["runtime_binary_sha256"], qualify_sdk.sha(b"verified executable bytes"))

    def test_changed_published_bytes_are_rejected_without_dispatch(self):
        assets = self.assets(); assets["mimic-v0.1.0-linux-amd64.tar.gz"] += b"tampered"
        with patch.object(qualify_sdk, "fetch", side_effect=lambda url: assets[url.rsplit("/", 1)[1]]):
            with self.assertRaisesRegex(ValueError, "checksum/size"):
                qualify_sdk.verified_payload("v0.1.0")

    def test_untrusted_tag_is_rejected_before_network(self):
        with patch.object(qualify_sdk, "fetch") as fetch:
            with self.assertRaises(ValueError): qualify_sdk.verified_payload("../../main")
            fetch.assert_not_called()

    def test_workflow_prefers_dedicated_token_and_allows_explicit_release(self):
        workflow = (Path(__file__).resolve().parents[2] / ".github/workflows/qualify-sdk.yml").read_text()
        self.assertIn("${{ secrets.SDK_QUALIFICATION_TOKEN || secrets.PERSONAL_ACCESS_TOKEN }}", workflow)
        self.assertIn("${{ inputs.version || github.event.release.tag_name }}", workflow)

    def dispatch(self, *, token=None, status=204):
        assets = self.assets()
        environment = {"CI": "true", "GITHUB_REPOSITORY": "mimic-browser/runtime"}
        if token is not None:
            environment["SDK_QUALIFICATION_TOKEN"] = token
        response = MagicMock()
        response.__enter__.return_value.status = status
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "receipt.json"
            argv = ["qualify_sdk.py", "--version", "v0.1.0", "--output", str(output), "--execute"]
            with patch.dict(os.environ, environment, clear=True), patch("sys.argv", argv), \
                    patch.object(qualify_sdk, "fetch", side_effect=lambda url: assets[url.rsplit("/", 1)[1]]), \
                    patch.object(qualify_sdk.urllib.request, "urlopen", return_value=response) as request:
                if status == 204:
                    qualify_sdk.main()
                else:
                    with self.assertRaisesRegex(ValueError, "not accepted"):
                        qualify_sdk.main()
            return json.loads(output.read_text()), request

    def test_accepted_dispatch_contains_only_verified_compatibility_payload(self):
        receipt, request = self.dispatch(token="test-token")
        self.assertEqual(receipt["status"], "dispatched")
        request.assert_called_once()
        sent = request.call_args.args[0]
        self.assertEqual(sent.full_url, "https://api.github.com/repos/mimic-browser/sdk/dispatches")
        self.assertEqual(sent.get_method(), "POST")
        self.assertEqual(sent.get_header("Authorization"), "Bearer test-token")
        self.assertEqual(json.loads(sent.data), {"event_type": "runtime-released", "client_payload": receipt["runtime"]})
        self.assertNotIn("test-token", json.dumps(receipt))

    def test_missing_token_does_not_dispatch(self):
        with patch("builtins.print"):
            receipt, request = self.dispatch()
        self.assertEqual(receipt["status"], "not-configured")
        request.assert_not_called()

    def test_unaccepted_dispatch_is_not_recorded_as_success(self):
        receipt, request = self.dispatch(token="test-token", status=200)
        self.assertEqual(receipt["status"], "prepared")
        request.assert_called_once()


if __name__ == "__main__": unittest.main()
