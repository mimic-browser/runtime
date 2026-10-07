"""Retain one bilateral Mimic/frozen Chrome 152 camera transport diagnostic.

The executable is supplied by the caller so Windows checks can reuse an
already-approved application path. No runtime download or permission UI occurs.
"""
import argparse
import asyncio
import hashlib
import http.server
import json
from pathlib import Path
import subprocess
import tempfile
import threading
import urllib.request

import websockets

ROOT = Path(__file__).resolve().parents[2]
PROBE = ROOT / 'tools/compatibility/camera_webrtc_interop.js'
DOCUMENT = b'<!doctype html><body>Camera transport diagnostic</body>'


class CDP:
    def __init__(self, socket):
        self.socket, self.sequence, self.events = socket, 0, []

    async def call(self, method, params=None):
        self.sequence += 1
        await self.socket.send(json.dumps({'id': self.sequence, 'method': method, 'params': params or {}}))
        while True:
            message = json.loads(await self.socket.recv())
            if message.get('id') == self.sequence:
                if 'error' in message:
                    raise RuntimeError(message)
                return message['result']
            self.events.append(message)

    async def evaluate(self, expression):
        result = await asyncio.wait_for(self.call('Runtime.evaluate', {
            'expression': expression, 'returnByValue': True, 'awaitPromise': True, 'userGesture': True,
        }), 30)
        if 'exceptionDetails' in result:
            raise RuntimeError(result)
        return result['result'].get('value')


async def endpoint(port):
    for _ in range(100):
        try:
            return json.load(urllib.request.urlopen(f'http://127.0.0.1:{port}/json/version', timeout=.3))
        except OSError:
            await asyncio.sleep(.1)
    raise TimeoutError(port)


async def main(args):
    probe = PROBE if args.media == 'camera' else ROOT / 'tools/compatibility/microphone_webrtc_interop.js'
    directory = Path(args.output).resolve()
    directory.mkdir(parents=True, exist_ok=False)
    chrome = ROOT / 'compatibility/.chrome-for-testing/152.0.7977.82/chrome-win64/chrome.exe'
    mimic = Path(args.mimic).resolve()

    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            self.send_response(200)
            self.send_header('Content-Type', 'text/html')
            self.end_headers()
            self.wfile.write(DOCUMENT)

        def log_message(self, *args):
            pass

    server = http.server.ThreadingHTTPServer(('127.0.0.1', 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    origin = f'http://127.0.0.1:{server.server_port}'
    profile = tempfile.mkdtemp(prefix='mimic-webrtc-chrome152-')
    chrome_command = [str(chrome), '--remote-debugging-port=19573',
                      f'--user-data-dir={profile}', '--no-first-run',
                      '--no-default-browser-check', '--window-size=1280,800', 'about:blank']
    mimic_command = [str(mimic), '-listen', '127.0.0.1:19574']
    startup = subprocess.STARTUPINFO()
    startup.dwFlags |= subprocess.STARTF_USESHOWWINDOW
    startup.wShowWindow = 0
    (directory / 'probe.js').write_bytes(probe.read_bytes())
    (directory / 'document.html').write_bytes(DOCUMENT)
    processes, sockets, logs = [], [], []
    try:
        for name, command in [('chrome', chrome_command), ('mimic', mimic_command)]:
            log = (directory / f'{name}.log').open('wb')
            logs.append(log)
            processes.append(subprocess.Popen(command, startupinfo=startup, stdout=log, stderr=log))
        chrome_version, mimic_version = await asyncio.gather(endpoint(19573), endpoint(19574))
        if chrome_version['Browser'] != 'Chrome/152.0.7977.82':
            raise RuntimeError(chrome_version)
        clients = []
        for port in [19573, 19574]:
            request = urllib.request.Request(f'http://127.0.0.1:{port}/json/new?about:blank', method='PUT')
            page = json.load(urllib.request.urlopen(request))
            socket = await websockets.connect(page['webSocketDebuggerUrl'], max_size=32*1024*1024)
            sockets.append(socket)
            client = CDP(socket)
            clients.append(client)
            await client.call('Network.enable')
            await client.call('Page.enable')
            await client.call('Page.navigate', {'url': origin})
            for _ in range(100):
                if await client.evaluate("location.origin === " + json.dumps(origin) + " && document.readyState === 'complete'"):
                    break
                await asyncio.sleep(.05)
        chrome_client, mimic_client = clients
        identity = await chrome_client.evaluate("({webdriver:navigator.webdriver,descriptor:Object.getOwnPropertyDescriptor(Navigator.prototype,'webdriver').get.toString(),ua:navigator.userAgent,languages:navigator.languages,origin:location.origin,secure:isSecureContext,outer:[outerWidth,outerHeight],inner:[innerWidth,innerHeight],dpr:devicePixelRatio,visibility:document.visibilityState,focus:document.hasFocus()})")
        if identity['webdriver'] is not False:
            raise RuntimeError(identity)
        metadata = {'chrome': chrome_version, 'mimic': mimic_version,
                    'chromium': 'd04cdb24d67b081f6cf80200ffc5233f44b61109',
                    'browserMode': 'headful', 'windowHidden': True, 'profile': profile,
                    'environmentProfileId': 'chrome-152-windows-x64-headful-controlled-v1',
                    'platform': 'Windows x64', 'origin': origin, 'identity': identity,
                    'launchArguments': chrome_command, 'mimicArguments': mimic_command,
                    'binarySha256': hashlib.sha256(chrome.read_bytes()).hexdigest(),
                    'mimicBinarySha256': hashlib.sha256(mimic.read_bytes()).hexdigest(),
                    'probeSha256': hashlib.sha256(probe.read_bytes()).hexdigest(),
                    'experiment': f'diagnostic: CDP {args.media} grants, native capture, bilateral WebRTC, userGesture evaluations and trusted key input for audio activation, no API overrides'}
        (directory / 'launch.json').write_text(json.dumps(metadata, indent=2)+'\n')
        for client in clients:
            await client.call('Browser.setPermission', {'permission': {'name': args.media}, 'setting': 'granted', 'origin': origin})
            if args.media == 'microphone':
                await client.call('Input.dispatchKeyEvent', {'type': 'keyDown', 'key': 'a', 'code': 'KeyA'})
                await client.call('Input.dispatchKeyEvent', {'type': 'keyUp', 'key': 'a', 'code': 'KeyA'})
            await client.evaluate(probe.read_text())
        offer = await mimic_client.evaluate('cameraInterop.offer()')
        (directory / 'offer.json').write_text(json.dumps(offer, indent=2)+'\n')
        answer = await chrome_client.evaluate('cameraInterop.answer('+json.dumps(offer)+')')
        (directory / 'answer.json').write_text(json.dumps(answer, indent=2)+'\n')
        await mimic_client.evaluate('cameraInterop.peer.setRemoteDescription('+json.dumps(answer)+')')
        observations = await asyncio.gather(chrome_client.evaluate('cameraInterop.observe()'), mimic_client.evaluate('cameraInterop.observe()'))
        if args.media == 'microphone':
            after_close = await asyncio.gather(*(client.evaluate('cameraInterop.cleanup()') for client in clients))
            for observation, state in zip(observations, after_close):
                observation['remoteAfterClose'] = state
        capture = {'captureMetadata': metadata, 'chrome': observations[0], 'mimic': observations[1]}
        (directory / 'capture.json').write_text(json.dumps(capture, indent=2)+'\n')
        for name, client in zip(['chrome', 'mimic'], clients):
            (directory / f'{name}-events.json').write_text(json.dumps(client.events, indent=2)+'\n')
        (directory / 'sha256.txt').write_text(''.join(hashlib.sha256(path.read_bytes()).hexdigest()+'  '+path.name+'\n' for path in sorted(directory.iterdir()) if path.is_file() and path.suffix != '.log'))
        for name, observation in zip(['chrome', 'mimic'], observations):
            if observation['connection'] != 'connected' or (args.media == 'camera' and not all(observation['dimensions'])) or observation.get('errors'):
                raise RuntimeError(observation)
            print(name, observation.get('dimensions', observation.get('peak')), 'connected', observation['remoteAfterClose'])
    except Exception as error:
        (directory / 'failure.txt').write_text(str(error))
        raise
    finally:
        for name, client in zip(['chrome', 'mimic'], locals().get('clients', [])):
            (directory / f'{name}-events.json').write_text(json.dumps(client.events, indent=2)+'\n')
        (directory / 'sha256.txt').write_text(''.join(hashlib.sha256(path.read_bytes()).hexdigest()+'  '+path.name+'\n' for path in sorted(directory.iterdir()) if path.is_file() and path.name != 'sha256.txt' and path.suffix != '.log'))
        for socket in sockets:
            await socket.close()
        for process in processes:
            process.terminate()
            process.wait(timeout=10)
        for log in logs:
            log.close()
        server.shutdown()
        server.server_close()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--mimic', required=True)
    parser.add_argument('--media', choices=['camera', 'microphone'], default='camera')
    parser.add_argument('--output', required=True)
    asyncio.run(main(parser.parse_args()))
