"""Attach to a directly launched frozen Chrome and retain a native camera sample.

Never launches a browser. The launch record must describe the existing dedicated
Chrome process and fresh profile. Every completed phase is saved before the next
measurement. Output records device metadata and pixel summaries, not images.
"""

import argparse
import asyncio
import hashlib
import json
from pathlib import Path
import urllib.request

import websockets

ROOT = Path(__file__).resolve().parents[2]
PROBE = ROOT / "tools/compatibility/media_device_profile_probe.js"


async def capture(args):
    output = Path(args.output).resolve()
    output.mkdir(parents=True, exist_ok=False)
    launch_path = Path(args.launch_record)
    launch = json.loads(launch_path.read_text(encoding="utf-8"))
    (output / "launch.json").write_bytes(launch_path.read_bytes())
    (output / "probe.js").write_bytes(PROBE.read_bytes())
    (output / "recorder.py").write_bytes(Path(__file__).read_bytes())
    with urllib.request.urlopen(args.endpoint.rstrip("/") + "/json/version", timeout=3) as response:
        version = json.load(response)
    if version["Browser"] != "Chrome/152.0.7977.82":
        raise RuntimeError("The reference must be frozen Chrome 152.0.7977.82")
    binary = Path(launch["launchArguments"][0])
    arguments = launch["launchArguments"]
    if any(arg == "--headless" or arg.startswith("--headless=") or
           arg.startswith("--enable-automation") or arg.startswith("--disable-blink-features")
           for arg in arguments):
        raise RuntimeError("The launch record is not a normal-browser control")
    expected_port = args.endpoint.rstrip("/").rsplit(":", 1)[1]
    if expected_port == "0" or f"--remote-debugging-port={expected_port}" not in arguments:
        raise RuntimeError("A matching fixed nonzero CDP port is required")
    origin = args.origin.rstrip("/")
    request = urllib.request.Request(args.endpoint.rstrip("/") + "/json/new?about:blank", method="PUT")
    with urllib.request.urlopen(request, timeout=3) as response:
        page = json.load(response)
    metadata = {
        **launch,
        "chrome": version,
        "binarySha256": hashlib.sha256(binary.read_bytes()).hexdigest(),
        "probeSha256": hashlib.sha256(PROBE.read_bytes()).hexdigest(),
        "recorderSha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        "origin": origin,
        "experiment": "diagnostic: CDP camera/microphone grants; physical camera constraints, frame readback summaries and same-browser WebRTC; userGesture evaluations; no API overrides" if args.case=="complete" else "diagnostic: CDP capture grants, physical camera clone constraint transition before/after frame presentation; userGesture evaluations; no API overrides",
    }
    records = {}
    sequence = 0
    wire_path = output / "cdp.jsonl"

    def retain(name, value):
        records[name] = value
        (output / f"{name}.json").write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        (output / "capture.json").write_text(json.dumps({"captureMetadata": metadata, "observations": records}, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        hashes = []
        for path in sorted(output.iterdir()):
            if path.is_file() and path.name != "sha256.txt":
                hashes.append(f"{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}")
        (output / "sha256.txt").write_text("\n".join(hashes) + "\n", encoding="utf-8")

    async with websockets.connect(page["webSocketDebuggerUrl"], max_size=32 * 1024 * 1024) as socket:
        async def call(method, params=None):
            nonlocal sequence
            sequence += 1
            message = {"id": sequence, "method": method, "params": params or {}}
            with wire_path.open("a", encoding="utf-8") as wire:
                wire.write(json.dumps({"direction": "send", "message": message}) + "\n")
            await socket.send(json.dumps(message))
            while True:
                message = json.loads(await socket.recv())
                with wire_path.open("a", encoding="utf-8") as wire:
                    wire.write(json.dumps({"direction": "receive", "message": message}) + "\n")
                if message.get("id") == sequence:
                    if "error" in message:
                        raise RuntimeError(message)
                    return message["result"]

        async def evaluate(expression):
            result = await asyncio.wait_for(call("Runtime.evaluate", {
                "expression": expression, "awaitPromise": True,
                "returnByValue": True, "userGesture": True,
            }), 30)
            if "exceptionDetails" in result:
                raise RuntimeError(result)
            return result["result"].get("value")

        try:
            await call("Network.enable")
            await call("Page.enable")
            await call("Page.navigate", {"url": origin})
            for _ in range(100):
                if await evaluate("location.origin === " + json.dumps(origin) + " && document.readyState === 'complete'"):
                    break
                await asyncio.sleep(.05)
            else:
                raise RuntimeError("Fixture navigation did not complete")
            identity = await evaluate("({webdriver:navigator.webdriver,descriptor:Object.getOwnPropertyDescriptor(Navigator.prototype,'webdriver').get.toString(),ua:navigator.userAgent,languages:navigator.languages,origin:location.origin,secure:isSecureContext,outer:[outerWidth,outerHeight],inner:[innerWidth,innerHeight],dpr:devicePixelRatio,visibility:document.visibilityState,focus:document.hasFocus()})")
            metadata["identity"] = identity
            retain("identity", identity)
            if identity["webdriver"] is not False:
                raise RuntimeError("Unmodified navigator.webdriver must be false")
            devices_expression = "navigator.mediaDevices.enumerateDevices().then(devices => devices.map(device => device.toJSON()))"
            retain("before-grants", await evaluate(devices_expression))
            for kind in ["camera", "microphone"]:
                await call("Browser.setPermission", {"permission": {"name": kind}, "setting": "granted", "origin": origin})
            devices = await evaluate(devices_expression)
            retain("devices", devices)
            candidates = [d for d in devices if d["kind"] == "videoinput" and "obs" not in d["label"].lower() and "virtual" not in d["label"].lower()]
            if args.camera_label:
                candidates = [d for d in devices if d["kind"] == "videoinput" and d["label"] == args.camera_label]
            if len(candidates) != 1:
                raise RuntimeError("Choose --camera-label from retained devices.json; physical source selection is ambiguous")
            retain("selected-device", candidates[0])
            await evaluate(PROBE.read_text(encoding="utf-8"))
            retain("supported-constraints", await evaluate("navigator.mediaDevices.getSupportedConstraints()"))
            retain("initial", await evaluate("mediaProfileProbe.open(" + json.dumps(candidates[0]["deviceId"]) + ")"))
            if args.case == "clone-transition":
                retain("clone-immediate", await evaluate("mediaProfileProbe.cloneStart()"))
                retain("clone-delayed", await evaluate("mediaProfileProbe.cloneDelay()"))
                retain("clone-attached", await evaluate("mediaProfileProbe.cloneAttach()"))
            else:
                for index, (width, height, fps) in enumerate([(640, 480, 30), (1280, 720, 30), (1920, 1080, 30), (1280, 720, 60), (320, 240, 15), (10000, 10000, 30)]):
                    constraints = {"width": {"exact": width}, "height": {"exact": height}, "frameRate": {"exact": fps}}
                    retain(f"constraints-{index}", await evaluate("mediaProfileProbe.constraints(" + json.dumps(constraints) + ")"))
                retain("restore", await evaluate("mediaProfileProbe.constraints({width:{exact:1280},height:{exact:720},frameRate:{exact:30}})"))
                retain("clone", await evaluate("mediaProfileProbe.clone()"))
                retain("local-frames", await evaluate("mediaProfileProbe.frames(mediaProfileProbe.video, 90)"))
                retain("transport", await evaluate("mediaProfileProbe.transport()"))
            retain("final", await evaluate("mediaProfileProbe.observe()"))
            print(json.dumps({"output": str(output), "device": candidates[0]["label"], "completedPhases": list(records)}, ensure_ascii=False))
        except Exception as error:
            retain("failure", {"error": str(error)})
            raise
        finally:
            await evaluate("globalThis.mediaProfileProbe?.close()")
            retain("closed", True)
            await call("Page.close")
            retain("page-closed", True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--endpoint", required=True)
    parser.add_argument("--origin", required=True)
    parser.add_argument("--launch-record", required=True)
    parser.add_argument("--output", required=True)
    parser.add_argument("--camera-label")
    parser.add_argument("--case", choices=["complete", "clone-transition"], default="complete")
    asyncio.run(capture(parser.parse_args()))
