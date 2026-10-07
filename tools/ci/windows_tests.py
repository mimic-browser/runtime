"""Run a complete, disjoint shard of the Windows Browser and CDP tests."""
import argparse
import json
from pathlib import Path
import re
import subprocess
import time

ROOT = Path(__file__).resolve().parents[2]
BROWSER = 'github.com/moreveal/mimic/internal/browser'
CDP = 'github.com/moreveal/mimic/internal/cdp'
DEFAULT_TIMINGS = ROOT / 'tools/ci/browser_test_timings.json'
NAVIGATION_GATE = 'TestSuspendedNavigationDeadlineAndTeardown'


def package_directory(package):
    return ROOT / package.removeprefix('github.com/moreveal/mimic/')


def binary_path(directory, package):
    return directory.resolve() / (package.rsplit('/', 1)[1] + '.test.exe')


def run(*args, capture=False, cwd=ROOT):
    return subprocess.run(args, cwd=cwd, check=True, text=True, encoding='utf-8',
                          stdout=subprocess.PIPE if capture else None).stdout


def partition(names, shard, count, timings=None):
    if count < 1 or not 0 <= shard < count or len(names) != len(set(names)):
        raise ValueError('Invalid shard parameters or duplicate test names')
    timings = timings or {}
    unknown = max(timings.values(), default=1.0)
    assignments = [[] for _ in range(count)]
    totals = [0.0] * count
    for name in sorted(names, key=lambda item: (-timings.get(item, unknown), item)):
        target = min(range(count), key=lambda index: (totals[index], len(assignments[index]), index))
        assignments[target].append(name)
        totals[target] += timings.get(name, unknown)
    return sorted(assignments[shard])


def load_timings(path):
    if not path.exists():
        return {}
    data = json.loads(path.read_text(encoding='utf-8'))
    timings = data.get('tests', data)
    if not isinstance(timings, dict) or any(not isinstance(value, (int, float)) or value < 0
                                            for value in timings.values()):
        raise ValueError(f'Invalid test timing history: {path}')
    return timings


def root_tests(package, binary_dir=None):
    if binary_dir:
        listed = run(str(binary_path(binary_dir, package)), '-test.list=.',
                     capture=True, cwd=package_directory(package))
    else:
        listed = run('go', 'test', '-list', '.', package, capture=True)
    names = [line for line in listed.splitlines() if re.fullmatch(r'(Test|Example|Fuzz)\w*', line)]
    if not names or len(names) != len(set(names)):
        raise RuntimeError(f'Invalid test discovery for {package}')
    return names


def batches(names, size):
    if size < 1:
        raise ValueError('Batch size must be positive')
    for offset in range(0, len(names), size):
        yield names[offset:offset + size]


def package_batches(package, names):
    # Preserve the navigation gate's fresh process and original deadlines.
    ordinary = [name for name in names if package != CDP or name != NAVIGATION_GATE]
    yield from batches(ordinary, 25 if package == BROWSER else 10)
    if package == CDP and NAVIGATION_GATE in names:
        yield [NAVIGATION_GATE]


def test_command(package, names, binary_dir=None):
    expression = '^(' + '|'.join(re.escape(name) for name in names) + ')$'
    if binary_dir:
        return (['go', 'tool', 'test2json', '-t', '-p', package,
                 str(binary_path(binary_dir, package)), '-test.v=test2json',
                 '-test.parallel=4', '-test.timeout=10m', '-test.run=' + expression],
                package_directory(package))
    return (['go', 'test', '-json', '-parallel', '4', '-timeout', '10m',
             '-run', expression, package], ROOT)


def run_json(args, observed, cwd=ROOT):
    process = subprocess.Popen(args, cwd=cwd, text=True, encoding='utf-8', stdout=subprocess.PIPE,
                               stderr=subprocess.STDOUT, bufsize=1)
    assert process.stdout is not None
    for line in process.stdout:
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            print(line, end='', flush=True)
            continue
        output = event.get('Output')
        if output:
            print(output, end='', flush=True)
        name = event.get('Test', '')
        if event.get('Action') == 'pass' and name and '/' not in name:
            key = event['Package'] + '::' + name
            observed[key] = max(observed.get(key, 0.0), float(event.get('Elapsed', 0.0)))
    if process.wait() != 0:
        raise subprocess.CalledProcessError(process.returncode, args)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--shard', type=int, required=True)
    parser.add_argument('--count', type=int, default=3)
    parser.add_argument('--timings', type=Path, default=DEFAULT_TIMINGS)
    parser.add_argument('--binary-dir', type=Path)
    args = parser.parse_args()
    names = [package + '::' + name for package in (BROWSER, CDP)
             for name in root_tests(package, args.binary_dir)]
    timings = load_timings(args.timings)
    selected = partition(names, args.shard, args.count, timings)
    if not selected:
        raise RuntimeError('Empty test shard')
    output = ROOT / '.build/ci'
    output.mkdir(parents=True, exist_ok=True)
    unknown = max(timings.values(), default=1.0)
    plan = {'shard': args.shard, 'count': args.count, 'total': len(names),
            'estimatedSeconds': sum(timings.get(name, unknown) for name in selected),
            'selected': selected}
    (output / f'windows-tests-{args.shard}.json').write_text(
        json.dumps(plan, indent=2) + '\n', encoding='utf-8')
    print(f'Windows shard {args.shard + 1}/{args.count}: {len(selected)} of '
          f'{len(names)} Browser/CDP roots, all subtests included', flush=True)
    started = time.monotonic()
    observed = {}
    try:
        for package in (BROWSER, CDP):
            roots = [name.split('::', 1)[1] for name in selected if name.startswith(package + '::')]
            for offset, batch in enumerate(package_batches(package, roots)):
                print(f'{package} batch {offset + 1}: {len(batch)} root tests', flush=True)
                command, cwd = test_command(package, batch, args.binary_dir)
                run_json(command, observed, cwd)
    finally:
        # Retain partial timing and coverage evidence even when a test fails.
        result = {'shard': args.shard, 'wallSeconds': round(time.monotonic() - started, 3),
                  'tests': observed}
        (output / f'windows-test-results-{args.shard}.json').write_text(
            json.dumps(result, indent=2) + '\n', encoding='utf-8')
        print(f"Windows shard wall time: {result['wallSeconds']:.3f}s", flush=True)


if __name__ == '__main__':
    main()
