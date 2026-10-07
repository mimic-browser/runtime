"""Retain a camera diagnostic from directly launched frozen headful Chrome 152."""
import asyncio
import hashlib
import http.server
import json
import os
from pathlib import Path
import subprocess
import tempfile
import threading
import urllib.request

import websockets

ROOT = Path(__file__).resolve().parents[2]
PROBE = ROOT / "internal/browser/testdata/camera_capture_oracle.js"
OUTPUT = ROOT / "internal/browser/testdata/camera_capture_chrome152.json"
PORT = 19571


async def main():
    if OUTPUT.exists():
        raise FileExistsError(OUTPUT)
    binary = ROOT / "compatibility/.chrome-for-testing/152.0.7977.82/chrome-win64/chrome.exe"

    class Handler(http.server.BaseHTTPRequestHandler):
        def do_GET(self):
            self.send_response(200)
            self.send_header("Content-Type", "text/html")
            self.end_headers()
            self.wfile.write(b"<!doctype html><body>Camera diagnostic</body>")

        def log_message(self, *args):
            pass

    server = http.server.ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    origin = f"http://127.0.0.1:{server.server_port}"
    profile = tempfile.mkdtemp(prefix="mimic-camera-chrome152-")
    command = [str(binary), f"--remote-debugging-port={PORT}",
               f"--user-data-dir={profile}", "--no-first-run",
               "--no-default-browser-check", "--window-size=1280,800", "about:blank"]
    startup = subprocess.STARTUPINFO()
    startup.dwFlags |= subprocess.STARTF_USESHOWWINDOW
    startup.wShowWindow = 0
    process = subprocess.Popen(command, startupinfo=startup,
                               stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        endpoint = f"http://127.0.0.1:{PORT}"
        for _ in range(100):
            try:
                version = json.load(urllib.request.urlopen(endpoint + "/json/version", timeout=.3))
                break
            except OSError:
                await asyncio.sleep(.1)
        if version["Browser"] != "Chrome/152.0.7977.82":
            raise RuntimeError(version)
        request = urllib.request.Request(endpoint + "/json/new?" + origin, method="PUT")
        page = json.load(urllib.request.urlopen(request))
        async with websockets.connect(page["webSocketDebuggerUrl"], max_size=32*1024*1024) as socket:
            sequence = 0

            async def call(method, params):
                nonlocal sequence
                sequence += 1
                await socket.send(json.dumps({"id": sequence, "method": method, "params": params}))
                while True:
                    message = json.loads(await socket.recv())
                    if message.get("id") == sequence:
                        if "error" in message:
                            raise RuntimeError(message)
                        return message["result"]

            identity = await call("Runtime.evaluate", {"expression": "JSON.stringify({webdriver:navigator.webdriver,descriptor:Object.getOwnPropertyDescriptor(Navigator.prototype,'webdriver')?.get?.toString(),ua:navigator.userAgent,languages:navigator.languages,origin:location.origin,secure:isSecureContext,outer:[outerWidth,outerHeight],inner:[innerWidth,innerHeight],dpr:devicePixelRatio,visibility:document.visibilityState,focus:document.hasFocus()})", "returnByValue": True})
            identity = json.loads(identity["result"]["value"])
            if identity["webdriver"] is not False:
                raise RuntimeError("Unmodified navigator.webdriver is not false")
            # Preserve launch/identity before changing any permission state.
            metadata = {"chrome": version["Browser"], "v8": version["V8-Version"],
                        "chromium": "d04cdb24d67b081f6cf80200ffc5233f44b61109",
                        "browserMode": "headful", "windowHidden": True,
                        "environmentProfileId": "chrome-152-windows-x64-headful-controlled-v1",
                        "platform": "Windows x64", "origin": origin,
                        "launchArguments": command, "profile": profile,
                        "binarySha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
                        "probeSha256": hashlib.sha256(PROBE.read_bytes()).hexdigest(),
                        "identity": identity,
                        "experiment": "diagnostic: CDP-granted camera, OBS Virtual Camera; no API overrides"}
            evidence_dir = ROOT / ".build/camera-oracle-20261007"
            evidence_dir.mkdir(parents=True, exist_ok=False)
            (evidence_dir / "launch.json").write_text(json.dumps(metadata, indent=2) + "\n")
            await call("Browser.setPermission", {"permission": {"name": "camera"}, "setting": "granted", "origin": origin})
            result = await asyncio.wait_for(call("Runtime.evaluate", {"expression": PROBE.read_text(), "awaitPromise": True, "returnByValue": True}), 30)
            capture = {"captureMetadata": metadata, "raw": result}
            if "exceptionDetails" in result:
                (evidence_dir / "failure.json").write_text(json.dumps(capture, indent=2) + "\n")
                raise RuntimeError(result)
            capture["observation"] = result["result"]["value"]
            OUTPUT.write_text(json.dumps(capture, indent=2) + "\n")
            (evidence_dir / "capture.json").write_bytes(OUTPUT.read_bytes())
            (evidence_dir / "sha256.txt").write_text(hashlib.sha256(OUTPUT.read_bytes()).hexdigest() + "  capture.json\n")
            print(json.dumps(capture["observation"]))
    finally:
        process.terminate()
        process.wait(timeout=10)
        server.shutdown()
        server.server_close()


if __name__ == "__main__":
    asyncio.run(main())
