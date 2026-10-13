"""Build and run one focused Go package from a persistent executable path."""

import argparse
from contextlib import contextmanager
import errno
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[2]


def binary_directory():
    if os.name == "nt":
        local = os.environ.get("LOCALAPPDATA")
        if not local:
            raise RuntimeError("LOCALAPPDATA is required for a stable Windows test path")
        return Path(local) / "Mimic" / "test-binaries"
    return Path(os.environ.get("XDG_CACHE_HOME", Path.home() / ".cache")) / "mimic" / "test-binaries"


@contextmanager
def executable_lock(path):
    # Different worktrees share the path. Hold the lock through both build and
    # execution, so another checkout cannot replace the binary being tested.
    with path.open("a+b") as lock:
        lock.seek(0, os.SEEK_END)
        if lock.tell() == 0:
            lock.write(b"\0")
            lock.flush()
        waiting = False
        while True:
            lock.seek(0)
            try:
                if os.name == "nt":
                    import msvcrt

                    msvcrt.locking(lock.fileno(), msvcrt.LK_NBLCK, 1)
                else:
                    import fcntl

                    fcntl.flock(lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
                break
            except OSError as error:
                if error.errno not in (errno.EACCES, errno.EAGAIN, errno.EDEADLK):
                    raise
                if not waiting:
                    print(f"Waiting for test executable: {path.stem}", file=sys.stderr, flush=True)
                    waiting = True
                time.sleep(0.25)
        try:
            yield
        finally:
            lock.seek(0)
            if os.name == "nt":
                msvcrt.locking(lock.fileno(), msvcrt.LK_UNLCK, 1)
            else:
                fcntl.flock(lock.fileno(), fcntl.LOCK_UN)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("package", help="One local Go package, for example ./internal/browser")
    parser.add_argument("--run", required=True, help="Focused Go test regular expression")
    parser.add_argument("--race", action="store_true")
    parser.add_argument("--json", action="store_true", help="Emit standard go test JSON events")
    parser.add_argument("--count", type=int, default=1)
    parser.add_argument("--parallel", type=int, default=4)
    parser.add_argument("--timeout", default="3m")
    args = parser.parse_args()
    if "..." in args.package or args.count < 1 or args.parallel < 1:
        parser.error("Select one package and positive count/parallel values")
    metadata = json.loads(subprocess.check_output(
        ["go", "list", "-json", args.package], cwd=ROOT, text=True, encoding="utf-8",
    ))
    if not metadata.get("TestGoFiles") and not metadata.get("XTestGoFiles"):
        parser.error("The selected package has no Go tests")
    package_dir = Path(metadata["Dir"]).resolve()
    try:
        relative = package_dir.relative_to(ROOT)
    except ValueError:
        parser.error("The package must belong to this checkout")
    # Use its module import identity, not a worktree, date, run or build mode.
    name = re.sub(r"[^A-Za-z0-9_-]", "-", metadata["ImportPath"])
    directory = binary_directory()
    directory.mkdir(parents=True, exist_ok=True)
    binary = directory / (name + (".exe" if os.name == "nt" else ""))
    with executable_lock(directory / (name + ".lock")):
        build = ["go", "test", "-c", "-o", str(binary)]
        if args.race:
            build.append("-race")
        build.append(args.package)
        print(f"Building {relative} -> {binary}", file=sys.stderr, flush=True)
        subprocess.run(build, cwd=ROOT, check=True, stdout=sys.stderr)
        command = [str(binary), "-test.v=test2json" if args.json else "-test.v",
                   "-test.run=" + args.run, "-test.count=" + str(args.count),
                   "-test.parallel=" + str(args.parallel), "-test.timeout=" + args.timeout]
        if args.json:
            command = ["go", "tool", "test2json", "-t", "-p", metadata["ImportPath"], *command]
        return subprocess.run(command, cwd=package_dir).returncode


if __name__ == "__main__":
    try:
        sys.exit(main())
    except subprocess.CalledProcessError as error:
        sys.exit(error.returncode)
