"""Fast iteration gate; imports the frozen workload runner without modifying it."""
import argparse
import concurrent.futures as futures
import datetime
import json
from pathlib import Path
import statistics
import subprocess
import sys
import threading
import time

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'benchmark'))
import run as frozen


def verify_binary(path, expected):
    actual = frozen.digest(path)
    if actual != expected:
        raise RuntimeError(f'Executable SHA-256 mismatch: expected {expected}, actual {actual}, path {path}')
    return actual


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--source', type=Path, default=ROOT)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--concurrency', type=int, nargs='+', default=[10, 25],
                        help='Page counts for static concurrency waves (default: 10 25)')
    args = parser.parse_args()
    if any(count < 1 for count in args.concurrency):
        parser.error('--concurrency counts must be positive')
    args.source = args.source.resolve()
    args.output = args.output.resolve()
    args.output.mkdir(parents=True, exist_ok=False)
    args.mimic = args.output / 'mimic.exe'
    args.timeout = 30
    # Always build here: a pre-existing executable can never become the baseline.
    command = ['go', 'build', '-o', str(args.mimic), './cmd/mimic']
    subprocess.run(command, cwd=args.source, check=True)
    expected = frozen.digest(args.mimic)
    fingerprint, files = frozen.harness_fingerprint()
    baseline=json.loads((ROOT/'benchmark/results/raw.json').read_text(encoding='utf-8'))
    if fingerprint != baseline['metadata']['harness_sha256']:
        raise RuntimeError('Frozen harness differs from immutable baseline')
    receipt = dict(command=command, source=str(args.source),
        revision=subprocess.check_output(['git','rev-parse','HEAD'],cwd=args.source,text=True).strip(),
        status=subprocess.check_output(['git','status','--porcelain'],cwd=args.source,text=True),
        executable=str(args.mimic), sha256=expected, harness_sha256=fingerprint,
        harness_files=files, built_at=datetime.datetime.now().astimezone().isoformat())
    (args.output/'build.json').write_text(json.dumps(receipt,indent=2),encoding='utf-8')
    data = dict(build=receipt, launches=[], rows=[], concurrency=[], memory=[])

    def save():
        (args.output/'raw.json').write_text(json.dumps(data,indent=2),encoding='utf-8')

    def launch(label):
        # Immediately before Runtime constructs the process; no alternate/default path.
        digest = verify_binary(args.mimic, expected)
        if frozen.harness_fingerprint()[0] != fingerprint:
            raise RuntimeError('Frozen harness changed')
        data['launches'].append(dict(label=label, executable=str(args.mimic), sha256=digest,
            verified_at=datetime.datetime.now().astimezone().isoformat()))
        save()
        return frozen.Runtime('mimic',args)

    def valid(rows):
        if any(row['status'] != 'VALID' for row in rows):
            raise RuntimeError('Mandatory workload correctness gate failed; see raw.json')

    def wave(runtime, work, n, index):
        servers=[frozen.Server(work) for _ in range(n)]
        before=runtime.tree.snapshot(); sample_index=len(runtime.tree.samples)
        barrier=threading.Barrier(n); release=threading.Event()
        finished=[threading.Event() for _ in range(n)]
        start=time.perf_counter(); stop=None; paging_since=None
        try:
            with futures.ThreadPoolExecutor(max_workers=n) as pool:
                jobs=[pool.submit(frozen.execute,runtime,work,barrier,(done,release),server)
                      for done,server in zip(finished,servers)]
                try:
                    while not all(done.is_set() for done in finished):
                        current=runtime.tree.snapshot()
                        if current.get('system_pages_input_s',0) is not None and current.get('system_pages_input_s',0)>1024:
                            paging_since=paging_since or time.perf_counter()
                            if time.perf_counter()-paging_since>=3:stop='sustained paging'
                        else:paging_since=None
                        if current['system_available']<max(2*1024**3,.15*frozen.psutil.virtual_memory().total):stop='memory pressure'
                        if time.perf_counter()-start>180:stop='wave timeout'
                        if stop:barrier.abort();break
                        time.sleep(.05)
                    completed=time.perf_counter(); active=runtime.tree.snapshot()
                finally:release.set()
                rows=[job.result() for job in jobs]
            after=runtime.tree.snapshot(); elapsed=time.perf_counter()-start
            time.sleep(.25); recovered=runtime.tree.snapshot()
            return dict(workload=work,n=n,wave=index,excluded=index<0,rows=rows,
                before_wave=before,active_memory=active,after_teardown=after,
                after_recovery=recovered,elapsed_s=elapsed,completion_window_s=completed-start,
                throughput=sum(row['status']=='VALID' for row in rows)/elapsed,
                marginal_rss_mib=(active['rss']-runtime.ready_memory['rss'])/n/2**20,
                marginal_private_mib=(active['private']-runtime.ready_memory['private'])/n/2**20,
                stop_reason=stop,samples=runtime.tree.samples[sample_index:])
        finally:
            release.set()
            for server in servers:server.close()

    try:
        # All six unchanged semantic workloads remain mandatory, including those not timed.
        runtime=launch('correctness')
        try:
            for work in frozen.WORKLOADS:
                row=frozen.execute(runtime,work); row.update(mode='correctness')
                data['rows'].append(row);save();valid([row])
        finally:runtime.close()
        for work in ['dom','static','react']:
            runtime=launch('warm '+work)
            try:
                for iteration in range(-1,5):
                    row=frozen.execute(runtime,work)
                    row.update(mode='warm',iteration=iteration,excluded=iteration<0)
                    time.sleep(.25);row['after_recovery']=runtime.tree.snapshot()
                    data['rows'].append(row);save();valid([row])
                    print(work,iteration,row['status'],round(row['completion_ms'],2),flush=True)
            finally:runtime.close()
        for n in args.concurrency:
            runtime=launch('static concurrency '+str(n))
            try:
                for index in range(-1,3):
                    row=wave(runtime,'static',n,index)
                    data['concurrency'].append(row);save();valid(row['rows'])
                    if row['stop_reason']:raise RuntimeError(row['stop_reason'])
                    print('static',n,index,round(row['throughput'],2),'sessions/s',flush=True)
            finally:runtime.close()
        for work in ['static','react']:
            runtime=launch('memory 10 '+work)
            try:
                row=wave(runtime,work,10,0)
                row['ready_memory']=runtime.ready_memory
                data['memory'].append(row);save();valid(row['rows'])
                if row['stop_reason']:raise RuntimeError(row['stop_reason'])
            finally:runtime.close()
        verify_binary(args.mimic,expected)
        if frozen.harness_fingerprint()[0]!=fingerprint:raise RuntimeError('Frozen harness changed')
        data['summary']={work:{key:statistics.median(r[key] for r in data['rows']
            if r['workload']==work and r['mode']=='warm' and not r['excluded'])
            for key in ['session_create_ms','execution_ms','completion_ms']}
            for work in ['dom','static','react']}
        data['finished']=datetime.datetime.now().astimezone().isoformat()
        print(json.dumps(data['summary'],indent=2),flush=True)
    except BaseException as error:
        data['failure']=repr(error)
        raise
    finally:save()

if __name__=='__main__':main()
