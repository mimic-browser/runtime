#!/usr/bin/env python3
"""Verify published runtime bytes and optionally dispatch SDK compatibility checks."""
import argparse
import hashlib
import io
import json
import os
from pathlib import Path
import re
import tarfile
import urllib.request


def sha(data):
    return hashlib.sha256(data).hexdigest()


def fetch(url):
    request = urllib.request.Request(url, headers={"User-Agent": "mimic-runtime-sdk-qualification"})
    with urllib.request.urlopen(request, timeout=120) as response:
        return response.read()


def verified_payload(version):
    if not re.fullmatch(r"v\d+\.\d+\.\d+(?:-beta\.\d+)?", version):
        raise ValueError("Require an exact published runtime tag")
    base = "https://github.com/mimic-browser/runtime/releases/download/" + version
    sums = {}
    for line in fetch(base + "/SHA256SUMS").decode("utf-8").splitlines():
        match = re.fullmatch(r"([a-f0-9]{64})  ([^/\\]+)", line)
        if not match or match[2] in sums:
            raise ValueError("Malformed or duplicate published checksum")
        sums[match[2]] = match[1]
    raw = fetch(base + "/release-manifest.json")
    if sha(raw) != sums.get("release-manifest.json"):
        raise ValueError("Published manifest checksum mismatch")
    manifest = json.loads(raw)
    if manifest.get("version") != version or not re.fullmatch(r"[a-f0-9]{40}", manifest.get("sourceRevision", "")):
        raise ValueError("Published manifest identity mismatch")
    artifacts = [item for item in manifest["artifacts"] if item["platform"] == "linux-amd64"]
    if len(artifacts) != 1:
        raise ValueError("Require one official Linux artifact")
    artifact = artifacts[0]
    archive_name = f"mimic-{version}-linux-amd64.tar.gz"
    if artifact["archive"] != archive_name or artifact["binaryVersion"] != version:
        raise ValueError("Linux release artifact identity mismatch")
    archive = fetch(base + "/" + archive_name)
    archive_sha = sha(archive)
    if archive_sha != artifact["sha256"] or archive_sha != sums.get(archive_name) or len(archive) != artifact["size"]:
        raise ValueError("Published archive checksum/size mismatch")
    binary_name = f"mimic-{version}-linux-amd64/mimic"
    with tarfile.open(fileobj=io.BytesIO(archive), mode="r:gz") as package:
        members = [member for member in package.getmembers() if member.name == binary_name]
        if len(members) != 1 or not members[0].isfile():
            raise ValueError("Require one regular runtime executable in published archive")
        binary_sha = sha(package.extractfile(members[0]).read())
    if binary_sha != artifact["binarySha256"]:
        raise ValueError("Published executable checksum mismatch")
    return {"runtime_url": base + "/" + archive_name,
            "runtime_archive_sha256": archive_sha, "runtime_binary_sha256": binary_sha,
            "runtime_manifest_sha256": sha(raw), "runtime_source_revision": manifest["sourceRevision"]}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--execute", action="store_true", help="CI-only cross-repository dispatch; never SDK publication")
    args = parser.parse_args()
    if args.execute and (os.getenv("CI") != "true" or os.getenv("GITHUB_REPOSITORY") != "mimic-browser/runtime"):
        raise ValueError("Dispatch is restricted to the official runtime CI")
    payload = verified_payload(args.version)
    receipt = {"kind": "sdk-qualification-dispatch", "runtime": payload, "status": "prepared"}
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(receipt, indent=2) + "\n", encoding="utf-8")
    if args.execute:
        token = os.getenv("SDK_QUALIFICATION_TOKEN")
        if not token:
            receipt["status"] = "not-configured"
            print("::warning::SDK qualification token is not configured; verified runtime was not dispatched for SDK qualification")
        else:
            request = urllib.request.Request("https://api.github.com/repos/mimic-browser/sdk/dispatches",
                data=json.dumps({"event_type": "runtime-released", "client_payload": payload}).encode(),
                headers={"Authorization": "Bearer " + token, "Accept": "application/vnd.github+json",
                         "X-GitHub-Api-Version": "2026-03-10", "Content-Type": "application/json"}, method="POST")
            with urllib.request.urlopen(request, timeout=30) as response:
                if response.status != 204: raise ValueError("SDK qualification dispatch was not accepted")
            receipt["status"] = "dispatched"
        args.output.write_text(json.dumps(receipt, indent=2) + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
