"""Loopback-only CDP queue probe; launch the supplied binary directly.

Requires websockets. Chrome must be the frozen headful 152 binary. Captures
are diagnostic (scripted observations), not passive live-site acceptance runs.
"""
import argparse
import asyncio
import hashlib
import http.server
import json
import os
from pathlib import Path
import socket
import statistics
import subprocess
import threading
import time
import urllib.request

import websockets

HTML = Path(__file__).with_suffix('.html').read_bytes()
BUSY = Path(__file__).with_name('cdp_queue_busy.js').read_text(encoding='utf-8')
BODY = b'local retained response body'


class Fixture(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        body = BODY if self.path == '/body' else HTML
        self.send_response(200)
        self.send_header('Content-Type', 'text/plain' if self.path == '/body' else 'text/html')
        self.send_header('Content-Length', str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass


class CDP:
    def __init__(self, wire):
        self.wire, self.sequence, self.pending, self.events = wire, 0, {}, []
        self.reader = asyncio.create_task(self.read())

    async def read(self):
        async for raw in self.wire:
            message = json.loads(raw)
            if 'id' in message:
                self.pending.pop(message['id']).set_result(message)
            else:
                self.events.append(message)

    async def call(self, method, **params):
        self.sequence += 1
        sequence = self.sequence
        future = asyncio.get_running_loop().create_future()
        self.pending[sequence] = future
        started = time.perf_counter()
        await self.wire.send(json.dumps({'id': sequence, 'method': method, 'params': params}))
        message = await asyncio.wait_for(future, 15)
        elapsed = (time.perf_counter() - started) * 1000
        if 'error' in message:
            raise RuntimeError(message)
        return message['result'], elapsed


async def run(args):
    output = args.output.resolve()
    if output.exists():
        raise FileExistsError(output)
    output.parent.mkdir(parents=True, exist_ok=True)
    binary = args.binary.resolve()
    chrome = binary.name.lower() == 'chrome.exe'
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        port = sock.getsockname()[1]
    profile = output.parent / (output.stem + '-profile')
    if chrome:
        profile.mkdir(exist_ok=False)
    command = ([str(binary), f'--remote-debugging-port={port}', f'--user-data-dir={profile}',
                '--no-first-run', '--no-default-browser-check', '--window-size=1280,800', 'about:blank']
               if chrome else [str(binary), '-listen', f'127.0.0.1:{port}'])
    env = dict(os.environ)
    env['MIMIC_PROFILE_CDP'] = '1' if args.profile else '0'
    server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Fixture)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    process = subprocess.Popen(command, env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL,
                               creationflags=subprocess.CREATE_NO_WINDOW if os.name == 'nt' else 0)
    result = {'binary': str(binary), 'sha256': hashlib.sha256(binary.read_bytes()).hexdigest(),
              'command': command, 'profile': str(profile) if chrome else None,
              'instrumented': args.profile, 'workload': 'loopback retained body and 300ms JavaScript tasks',
              'fixture_sha256': hashlib.sha256(HTML + BODY + BUSY.encode()).hexdigest(),
              'tool_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(), 'rows': []}
    try:
        endpoint = f'http://127.0.0.1:{port}'
        for _ in range(300):
            try:
                with urllib.request.urlopen(endpoint + '/json/version', timeout=.3) as response:
                    result['version'] = json.load(response)
                with urllib.request.urlopen(endpoint + '/json/list', timeout=.3) as response:
                    target = next(x for x in json.load(response) if x['type'] == 'page')
                break
            except (OSError, StopIteration):
                await asyncio.sleep(.1)
        else:
            raise TimeoutError('CDP target did not become available')
        async with websockets.connect(target['webSocketDebuggerUrl'], max_size=64 << 20) as wire:
            cdp = CDP(wire)
            result['events'] = cdp.events
            observed, _ = await cdp.call('Runtime.evaluate', expression="JSON.stringify({webdriver:navigator.webdriver,descriptor:Object.getOwnPropertyDescriptor(Navigator.prototype,'webdriver')?.get?.toString(),ua:navigator.userAgent,languages:navigator.languages,outerWidth,outerHeight,innerWidth,innerHeight,devicePixelRatio,visibility:document.visibilityState,focus:document.hasFocus()})", returnByValue=True)
            result['environment'] = json.loads(observed['result']['value'])
            if chrome:
                assert result['version']['Browser'] == 'Chrome/152.0.7977.82'
                assert result['environment']['webdriver'] is False
            await cdp.call('Network.enable')
            if args.profile and not chrome:
                await cdp.call('Mimic.startTrace')
            await cdp.call('Page.navigate', url=f'http://127.0.0.1:{server.server_port}/')
            for _ in range(100):
                ready, _ = await cdp.call('Runtime.evaluate', expression="document.readyState==='complete' && typeof order!=='undefined'", returnByValue=True)
                if ready['result'].get('value') is True:
                    break
                await asyncio.sleep(.02)
            await cdp.call('Runtime.evaluate', expression="fetch('/body').then(r=>r.text())", awaitPromise=True, returnByValue=True)
            request = next(e['params']['requestId'] for e in cdp.events if e.get('method') == 'Network.responseReceived' and e['params']['response']['url'].endswith('/body'))
            for iteration in range(8):
                _, idle = await cdp.call('Network.getResponseBody', requestId=request)
                busy = asyncio.create_task(cdp.call('Runtime.evaluate', expression=BUSY, returnByValue=True))
                await asyncio.sleep(.04)
                body, loaded = await cdp.call('Network.getResponseBody', requestId=request)
                assert body['body'] == BODY.decode() and body['base64Encoded'] is False
                evaluation, busy_ms = await busy
                assert evaluation['result']['value'] == 42
                inputs = []
                for kind in ['mouseMoved', 'mousePressed', 'mouseReleased']:
                    _, elapsed = await cdp.call('Input.dispatchMouseEvent', type=kind, x=30, y=30, button='left', clickCount=1)
                    inputs.append(elapsed)
                result['rows'].append({'iteration': iteration, 'idle_body_ms': idle, 'loaded_body_ms': loaded, 'busy_evaluate_ms': busy_ms, 'input_ms': inputs})
            state, _ = await cdp.call('Runtime.evaluate', expression='JSON.stringify(order)', returnByValue=True)
            result['input_order'] = json.loads(state['result']['value'])
            expected = [item for kind in ['mousedown', 'mouseup', 'click'] for item in [kind, kind + ':microtask']] * 8
            assert result['input_order'] == expected, result['input_order']
            if args.profile and not chrome:
                result['trace'], _ = await cdp.call('Mimic.getTrace')
            result['events'] = cdp.events
            cdp.reader.cancel()
        result['medians'] = {key: statistics.median(row[key] for row in result['rows']) for key in ['idle_body_ms', 'loaded_body_ms', 'busy_evaluate_ms']}
        print(json.dumps(result['medians']))
    except BaseException as error:
        result['failure'] = repr(error)
        raise
    finally:
        result['captureMetadata'] = {
            'browserMode': 'headful' if chrome else 'runtime',
            'environmentProfileId': ('chrome-152-windows-x64-headful-controlled-v1' if chrome else 'mimic-loopback-queue-diagnostic'),
            'launchArguments': command,
            'executableSha256': result['sha256'],
            'observedVersion': result.get('version'),
            'observedPresentation': result.get('environment'),
            'classification': 'scripted loopback diagnostic',
            'instrumented': True,
            'networkModified': False,
            'featureOverrides': [],
        }
        output.write_text(json.dumps(result, indent=2) + '\n', encoding='utf-8')
        process.terminate()
        process.wait(timeout=10)
        server.shutdown()
        server.server_close()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--profile', action='store_true')
    asyncio.run(run(parser.parse_args()))
