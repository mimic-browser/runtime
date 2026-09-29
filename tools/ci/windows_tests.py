"""Run a complete, disjoint shard of the Windows reference test suite."""
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


def run(*args, capture=False):
    return subprocess.run(args, cwd=ROOT, check=True, text=True,
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
        raise ValueError(f'Invalid browser timing history: {path}')
    return timings


def root_tests(package):
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


def run_json(args, observed):
    process = subprocess.Popen(args, cwd=ROOT, text=True, stdout=subprocess.PIPE,
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
            observed[name] = max(observed.get(name, 0.0), float(event.get('Elapsed', 0.0)))
    if process.wait() != 0:
        raise subprocess.CalledProcessError(process.returncode, args)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--shard', type=int, required=True)
    parser.add_argument('--count', type=int, default=2)
    parser.add_argument('--timings', type=Path, default=DEFAULT_TIMINGS)
    args = parser.parse_args()
    names = root_tests(BROWSER)
    timings = load_timings(args.timings)
    selected = partition(names, args.shard, args.count, timings)
    if not selected:
        raise RuntimeError('Empty browser test shard')
    output = ROOT / '.build/ci'
    output.mkdir(parents=True, exist_ok=True)
    unknown = max(timings.values(), default=1.0)
    plan = {'shard': args.shard, 'count': args.count, 'total': len(names),
            'estimatedSeconds': sum(timings.get(name, unknown) for name in selected),
            'selected': selected}
    (output / f'windows-tests-{args.shard}.json').write_text(json.dumps(plan, indent=2) + '\n')
    print(f'Windows browser shard {args.shard + 1}/{args.count}: '
          f'{len(selected)} of {len(names)} discovered root tests, all their subtests included', flush=True)
    # Bound command-line length and native V8 pressure on the hosted Windows
    # runner. Keep every test and its own assertion/context deadline unchanged.
    started = time.monotonic()
    observed = {}
    for offset, batch in enumerate(batches(selected, 25)):
        expression = '^(' + '|'.join(re.escape(name) for name in batch) + ')$'
        print(f'Browser batch {offset + 1}: {len(batch)} root tests', flush=True)
        run_json(['go', 'test', '-json', '-parallel', '4', '-timeout', '10m', '-run', expression, BROWSER], observed)
    if args.shard == 0:
        packages = [line for line in run('go', 'list', './...', capture=True).splitlines()
                    if line != BROWSER]
        print(f'Windows remaining packages: {len(packages)}', flush=True)
        # CDP's short first-response gates must run in a fresh process rather
        # than after the accumulated native state of its other root tests.
        # Keep every root and subtest, with all original assertion deadlines.
        if CDP in packages:
            packages.remove(CDP)
            cdp_names = root_tests(CDP)
            # Keep the navigation responsiveness gate independent of native
            # state accumulated by other CDP roots.
            navigation_gate = 'TestSuspendedNavigationDeadlineAndTeardown'
            has_navigation_gate = navigation_gate in cdp_names
            if has_navigation_gate:
                cdp_names.remove(navigation_gate)
            for offset, batch in enumerate(batches(cdp_names, 10)):
                expression = '^(' + '|'.join(re.escape(name) for name in batch) + ')$'
                print(f'CDP batch {offset + 1}: {len(batch)} root tests', flush=True)
                run('go', 'test', '-timeout', '10m', '-run', expression, CDP)
            if has_navigation_gate:
                run('go', 'test', '-timeout', '10m', '-run', '^' + navigation_gate + '$', CDP)
        # Behavioral deadlines must not compete with unrelated package builds
        # and engine initializations on the small hosted runner.
        run('go', 'test', '-p', '1', *packages)
    result = {'shard': args.shard, 'wallSeconds': round(time.monotonic() - started, 3),
              'tests': observed,
              'slowest': [{'name': name, 'seconds': seconds}
                          for name, seconds in sorted(observed.items(), key=lambda item: (-item[1], item[0]))[:20]]}
    (output / f'windows-test-results-{args.shard}.json').write_text(
        json.dumps(result, indent=2) + '\n', encoding='utf-8')
    print(f"Windows shard wall time: {result['wallSeconds']:.3f}s", flush=True)
    for row in result['slowest'][:10]:
        print(f"  {row['seconds']:8.3f}s  {row['name']}", flush=True)


if __name__ == '__main__':
    main()
