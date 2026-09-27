#!/usr/bin/env python3
"""Verify or apply Mimic's pinned V8 source patch before a native rebuild."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess


ROOT = Path(__file__).resolve().parents[1]


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def verify(source, apply=False):
    manifest = json.loads((ROOT / "patches/v8-native-observation.json").read_text())
    patch = ROOT / "patches" / manifest["patch"]
    if digest(patch) != manifest["patch_sha256"]:
        raise RuntimeError("V8 patch digest mismatch")
    field = "before_sha256" if apply else "after_sha256"
    for entry in manifest["files"]:
        if digest(source / entry["path"]) != entry[field]:
            raise RuntimeError(f"V8 source digest mismatch: {entry['path']}")
    if apply:
        subprocess.run(["git", "apply", "--check", str(patch)], cwd=source, check=True)
        subprocess.run(["git", "apply", str(patch)], cwd=source, check=True)
        verify(source)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("source", type=Path)
    parser.add_argument("--apply", action="store_true")
    args = parser.parse_args()
    verify(args.source.resolve(), args.apply)
    print("Verified pinned native V8 source patch")
