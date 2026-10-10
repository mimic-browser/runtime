"""Project saved captures into current public workload metrics, without startup claims."""
import argparse
import hashlib
import json
from pathlib import Path
from statistics import median


def load_capture(directory):
    path = directory / 'raw.json'
    expected = json.loads((directory / 'manifest.json').read_text(encoding='utf-8'))['sha256']['raw.json']
    digest = hashlib.sha256(path.read_bytes()).hexdigest()
    if digest != expected:
        raise ValueError('Capture integrity mismatch')
    data = json.loads(path.read_text(encoding='utf-8'))
    if not data.get('finished'):
        raise ValueError('Incomplete capture')
    return data, digest


def percentile(values, q):
    values = sorted(values)
    position = (len(values) - 1) * q
    low = int(position)
    high = min(low + 1, len(values) - 1)
    return values[low] + (values[high] - values[low]) * (position - low)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('candidate', type=Path)
    parser.add_argument('chrome', type=Path)
    parser.add_argument('output', type=Path)
    args = parser.parse_args()
    candidate, candidate_hash = load_capture(args.candidate)
    chrome, chrome_hash = load_capture(args.chrome)
    for field in ('harness_sha256', 'fixture_sha256'):
        if candidate['metadata'][field] != chrome['metadata'][field]:
            raise ValueError(f'Reference mismatch: {field}')
    output = json.loads(args.output.read_text(encoding='utf-8'))
    output.pop('ready', None)
    for series in output['single']:
        work = series['workload']
        rows = [r for s in candidate['single'] if s['workload'] == work for r in s['rows'] if not r['excluded']]
        controls = [r for r in chrome['rows'] if r['system'] == 'chrome' and r['workload'] == work and r['mode'] == 'warm' and not r['excluded']]
        for prefix, source in [('', rows), ('chrome_', controls)]:
            if not source or any(r['status'] != 'VALID' for r in source):
                raise ValueError('Invalid warm series')
            cpu = [(r['after_teardown']['cpu_s'] - r['before_session']['cpu_s']) * 1000 for r in source]
            series[prefix + 'cpu_per_page_ms'] = median(cpu)
            series[prefix + 'completion_p50_ms'] = median(r['completion_ms'] for r in source)
            series[prefix + 'completion_p95_ms'] = percentile([r['completion_ms'] for r in source], .95)
    for series in output['density']:
        source = next(s for s in candidate['density'] if s['workload'] == series['workload'] and s['n'] == series['n'])
        waves = [w for w in source['waves'] if not w['excluded']]
        series['throughput_pages_s'] = None
        series['cpu_per_page_ms'] = None
        series['latency_p50_ms'] = None
        series['latency_p95_ms'] = None
        if not series['failure'] and series['valid'] == series['attempts']:
            rows = [r for w in waves for r in w['rows']]
            series['batch_elapsed_s'] = sum(w['elapsed_s'] for w in waves)
            series['throughput_pages_s'] = len(rows) / series['batch_elapsed_s']
            series['cpu_per_page_ms'] = 1000 * sum(w['after_teardown']['cpu_s'] - w['before']['cpu_s'] for w in waves) / len(rows)
            series['latency_p50_ms'] = median(r['session_latency_ms'] for r in rows)
            series['latency_p95_ms'] = percentile([r['session_latency_ms'] for r in rows], .95)
        control = next((s for s in chrome['concurrency'] if s['system'] == 'chrome' and s['workload'] == series['workload'] and s['n'] == series['n']), None)
        series['chrome_throughput_pages_s'] = None
        series['chrome_cpu_per_page_ms'] = None
        if control and not control.get('stop_reason'):
            cw = [w for w in control['waves'] if not w['excluded']]
            cr = [r for w in cw for r in w['rows']]
            if cw and all(r['status'] == 'VALID' for r in cr):
                series['chrome_throughput_pages_s'] = len(cr) / sum(w['elapsed_s'] for w in cw)
                series['chrome_cpu_per_page_ms'] = 1000 * sum(w['cpu_s'] for w in cw) / len(cr)
                series['chrome_latency_p50_ms'] = median(r['session_latency_ms'] for r in cr)
                series['chrome_latency_p95_ms'] = percentile([r['session_latency_ms'] for r in cr], .95)
                series['chrome_timing_attempts'] = len(cr)
    output['optimize'] = {
        'workload': 'Books SSR extraction', 'date': '2026-10-05',
        'baseline': 'Mimic Default, Optimize disabled',
        'optimized': 'Mimic installed Auto profile',
        'baseline_encoded_body_bytes': 284591, 'optimized_encoded_body_bytes': 5276,
        'reduction_percent': (1 - 5276 / 284591) * 100,
        'baseline_responses': 29, 'optimized_responses': 1,
        'valid': 5, 'attempts': 5,
        'limitations': 'Encoded acquired HTTP body bytes, not physical wire traffic. One recorded state; Manual ties Auto. Training and validation took 27.1 seconds.',
        'methodology': 'docs/performance/workload-optimization-feature.md#books-ssr',
    }
    output['methodology'].update(
        scope='active memory, warm latency/CPU and fixed-concurrency batch throughput; separate Optimize acquisition evidence',
        candidate_raw_sha256=candidate_hash, chrome_timing_raw_sha256=chrome_hash,
        projector_sha256=hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        optimize_report_sha256=hashlib.sha256((Path(__file__).resolve().parents[2] / 'docs/performance/workload-optimization-feature.md').read_bytes()).hexdigest(),
        chrome_timing_reference_date=chrome['metadata']['date'],
        aggregation='Throughput = total valid pages / total measured wave duration. CPU = summed process-tree wave user+kernel time / successes. Latency quantiles use linear interpolation. Excluded warmups and failed series are not successes.',
        timing_boundary='Page create to final teardown; server setup and 250 ms recovery excluded. Same frozen execute, fixture, navigation barrier and 50 ms completion polling. Candidate adapter includes the initial Job Object snapshot in elapsed time; reference starts immediately after it and includes the final snapshot. This small instrumentation difference remains in the comparison. Guard is 120 s versus 180 s, neither reached; runs are not paired and background load can vary.',
    )
    args.output.write_bytes((json.dumps(output, indent=2) + '\n').encode('utf-8'))
    row = next(r for r in output['density'] if r['workload'] == 'static' and r['n'] == 50)
    print(json.dumps(row, indent=2))


if __name__ == '__main__':
    main()
