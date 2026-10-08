import io
import json
import tarfile
import unittest
from unittest.mock import patch

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


if __name__ == "__main__": unittest.main()
