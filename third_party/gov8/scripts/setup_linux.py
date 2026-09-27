#!/usr/bin/env python3
"""Build and package Mimic's pinned Linux amd64 V8 shim (no Chromium build).

Requires Linux x86_64, Python 3.11+, Go, Rust/cargo, binutils and glibc headers.
The pinned crate includes the matching libc++ headers and Clang updater.
"""
import argparse
import fcntl
import gzip
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys
import tarfile
import tempfile
import tomllib
import urllib.request
from verify_native_source import verify

ROOT = Path(__file__).resolve().parents[1]
VERSION = "152.2.0"
ARTIFACT = "librusty_v8_release_x86_64-unknown-linux-gnu.a.gz"
ARTIFACT_SHA = "b6683e9afcb77fbd8cb2c3acf45d34d52029ab216b332522c95179c12f0fed3c"
CRATE_SHA = "a10fe1a92da5c32c7c7f838ce36c0ccfcfd5edf0865b58bdde820aa64cea9888"


def sha(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def acquire(path, url, digest):
    if not path.exists():
        print("Downloading", url, flush=True)
        temporary = path.with_suffix(path.suffix + ".part")
        with urllib.request.urlopen(url, timeout=120) as source, temporary.open("wb") as dest:
            shutil.copyfileobj(source, dest)
        if sha(temporary) != digest:
            raise RuntimeError(f"SHA-256 mismatch: {temporary}")
        temporary.replace(path)
    if sha(path) != digest:
        raise RuntimeError(f"SHA-256 mismatch: {path}")


def pin(lock, name, expected):
    packages = tomllib.loads(lock.read_text())["package"]
    if not any(p["name"] == name and p["version"] == expected for p in packages):
        raise RuntimeError(f"{lock} does not pin {name} {expected}")


def run(*args, **kwargs):
    subprocess.run([str(arg) for arg in args], check=True, **kwargs)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--build-dir", type=Path, default=ROOT / "build/linux-amd64")
    parser.add_argument("--native-source", type=Path, required=True)
    parser.add_argument("--native-archive", type=Path, required=True)
    args = parser.parse_args()
    if platform.system() != "Linux" or platform.machine() != "x86_64":
        parser.error("run on Linux x86_64 (including WSL2)")
    build = args.build_dir.resolve()
    build.mkdir(parents=True, exist_ok=True)
    with (build / ".setup.lock").open("w") as lock:
        fcntl.flock(lock, fcntl.LOCK_EX)
        pin(ROOT / "rust-oracle/Cargo.lock", "v8", VERSION)
        pin(ROOT / "rust-oracle/Cargo.lock", "temporal_capi", "0.2.6")
        pin(ROOT / "internal/shim/temporal/Cargo.lock", "temporal_capi", "0.2.6")
        native_source = args.native_source.resolve()
        verify(native_source)
        native_archive = args.native_archive.resolve(strict=True)
        crate = build / "v8.crate"
        acquire(crate, f"https://static.crates.io/crates/v8/v8-{VERSION}.crate", CRATE_SHA)
        # Fresh verified headers each run; never trust a stale extracted header tree.
        with tempfile.TemporaryDirectory(prefix="headers-", dir=build) as extracted:
            with tarfile.open(crate) as tar:
                tar.extractall(extracted, filter="data")
            source = Path(extracted) / f"v8-{VERSION}"
            # Shim headers must match the patched archive, not the stock crate.
            for entry in json.loads((ROOT / "patches/v8-native-observation.json").read_text())["files"]:
                shutil.copyfile(native_source / entry["path"], source / entry["path"])
            compiler_root = build / "compiler"
            # The updater is from the hash-verified crate and pins the Clang revision.
            run(sys.executable, source / "tools/clang/scripts/update.py", "--output-dir", compiler_root)
            compiler = compiler_root / "bin/clang++"
            with native_archive.open("rb") as src, (build / "v8.a").open("wb") as dst:
                shutil.copyfileobj(src, dst)
            env = dict(os.environ, CARGO_TARGET_DIR=str(build / "temporal"))
            run("cargo", "build", "--release", "--locked", "--manifest-path", ROOT / "internal/shim/temporal/Cargo.toml", env=env)
            with tempfile.TemporaryDirectory(prefix="link-", dir=build) as staging:
                obj = Path(staging) / "shim.o"
                library = Path(staging) / "libgov8_shim.so"
                run(compiler, "-std=c++20", "-O2", "-fPIC", "-fno-rtti", "-fvisibility=hidden",
                    "-DNDEBUG", "-DV8_GN_HEADER", '-DV8_EMBEDDER_STRING="-rusty"',
                    "-D_LIBCPP_HARDENING_MODE=_LIBCPP_HARDENING_MODE_NONE", "-nostdinc++",
                    "-isystem", source / "third_party/libc++/src/include",
                    "-isystem", source / "buildtools/third_party/libc++",
                    "-I", ROOT / "internal/shim", "-I", source / "v8/include",
                    "-c", ROOT / "internal/shim/shim.cc", "-o", obj)
                run(compiler, "-fuse-ld=lld", "-nostdlib++", "-shared", "-Wl,-z,defs",
                    "-Wl,--exclude-libs,ALL", "-Wl,--as-needed", "-o", library, obj,
                    "-Wl,--start-group", build / "v8.a", build / "temporal/release/libtemporal_link.a",
                    "-Wl,--end-group", "-pthread", "-ldl", "-lm")
                run("strip", "--strip-unneeded", library)
                destination = build / "libgov8_shim.so"
                library.replace(destination)
        run("go", "run", "./internal/cmd/package-shim", "-input", destination,
            "-output", "internal/prebuilt/linux_amd64/libgov8_shim.so.gz", cwd=ROOT)
        metadata = ROOT / "internal/prebuilt/prebuilt_linux_amd64.go"
        metadata.write_text('//go:build linux && amd64\n\npackage prebuilt\n\nimport _ "embed"\n\n'
            f'const (\n ABI = 44\n Size = int64({destination.stat().st_size})\n SHA256 = "{sha(destination)}"\n'
            ' fileName = "libgov8_shim.so"\n)\n\n//go:embed linux_amd64/libgov8_shim.so.gz\nvar compressed []byte\n')
        run("gofmt", "-w", metadata)
        print("Packaged Linux shim. Validate with: go test ./...", flush=True)


if __name__ == "__main__":
    main()
