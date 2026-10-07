"""Low-interference CDP recording of owned, directly launched browser processes.

Normal controls never install page wrappers, pause targets, intercept responses,
or override identity. Explicit archive replay is a separate intercepted diagnostic.
"""
from __future__ import annotations

import asyncio
import base64
import contextlib
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import socket
import subprocess
import sys
import tempfile
import time

import aiohttp
import psutil

from doctor_report import digest, seal, write_json

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "compatibility"))
from oracle import PINNED_PRODUCT, PINNED_VERSION, CHROMIUM_REVISION, default_profile_id


IDENTITY = """(() => {
  const d = Object.getOwnPropertyDescriptor(Navigator.prototype, 'webdriver');
  return {
    webdriver: navigator.webdriver,
    webdriverDescriptor: d ? {enumerable: d.enumerable, configurable: d.configurable,
      getter: d.get ? Function.prototype.toString.call(d.get) : null,
      setter: d.set ? Function.prototype.toString.call(d.set) : null} : null,
    userAgent: navigator.userAgent, languages: Array.from(navigator.languages),
    platform: navigator.platform, secureContext: isSecureContext,
    crossOriginIsolated, visibilityState: document.visibilityState,
    hasFocus: document.hasFocus(),
    viewport: {width: innerWidth, height: innerHeight, deviceScaleFactor: devicePixelRatio},
    window: {x: screenX, y: screenY, outerWidth, outerHeight}
  };
})()"""


def launch_arguments(kind: str, binary: Path, port: int, profile: str | None = None, headless=False) -> list[str]:
    if not 1 <= port <= 65535:
        raise ValueError("The debugging port must be fixed and nonzero")
    if kind == "chrome":
        if not profile or not Path(profile).is_absolute():
            raise ValueError("Chrome requires a fresh absolute profile directory")
        return [str(binary), *(["--headless=new"] if headless else []), f"--remote-debugging-port={port}", f"--user-data-dir={profile}",
                "--no-first-run", "--no-default-browser-check", "--window-size=1280,800", "about:blank"]
    return [str(binary), "-listen", f"127.0.0.1:{port}"]


def available_port(port: int) -> None:
    with socket.socket() as listener:
        if os.name == "nt":
            listener.setsockopt(socket.SOL_SOCKET, socket.SO_EXCLUSIVEADDRUSE, 1)
        listener.bind(("127.0.0.1", port))


def stop_owned(proc: subprocess.Popen) -> None:
    """Only stop this launch and descendants. Never kill by image name or port."""
    if proc.poll() is not None:
        return
    try:
        parent = psutil.Process(proc.pid)
        children = parent.children(recursive=True)
    except psutil.NoSuchProcess:
        return
    processes = [*reversed(children), parent]
    for child in processes:
        with contextlib.suppress(psutil.NoSuchProcess):
            child.terminate()
    _, alive = psutil.wait_procs(processes, timeout=3)
    for child in alive:
        with contextlib.suppress(psutil.NoSuchProcess):
            child.kill()
    psutil.wait_procs(alive, timeout=3)
    with contextlib.suppress(subprocess.TimeoutExpired):
        proc.wait(timeout=3)


def verify_listener_owner(proc: subprocess.Popen, port: int) -> None:
    """Reject a port race with an unrelated browser, even if its version matches."""
    parent = psutil.Process(proc.pid)
    for process in [parent, *parent.children(recursive=True)]:
        try:
            if any(c.status == psutil.CONN_LISTEN and c.laddr.port == port
                   for c in process.net_connections(kind="tcp")):
                return
        except psutil.NoSuchProcess:
            continue
    raise RuntimeError("CDP listener is not owned by the launched process tree")


def owned_windows(pid, restore=False):
    """Measure only this launch's top-level windows; restore only for explicit input."""
    if os.name != "nt":
        return []
    import ctypes
    from ctypes import wintypes
    api = ctypes.WinDLL("user32", use_last_error=True)
    callback_type = ctypes.WINFUNCTYPE(wintypes.BOOL, wintypes.HWND, wintypes.LPARAM)
    api.GetWindowThreadProcessId.argtypes = [wintypes.HWND, ctypes.POINTER(wintypes.DWORD)]
    api.IsWindowVisible.argtypes = api.IsIconic.argtypes = [wintypes.HWND]
    api.ShowWindow.argtypes = [wintypes.HWND, ctypes.c_int]
    api.GetClassNameW.argtypes = [wintypes.HWND, wintypes.LPWSTR, ctypes.c_int]
    api.GetWindowLongW.argtypes = [wintypes.HWND, ctypes.c_int]
    api.EnumWindows.argtypes = [callback_type, wintypes.LPARAM]
    rows = []
    def visit(handle, _):
        owner = wintypes.DWORD()
        api.GetWindowThreadProcessId(handle, ctypes.byref(owner))
        if owner.value != pid:
            return True
        name = ctypes.create_unicode_buffer(256)
        api.GetClassNameW(handle, name, len(name))
        # Exclude owned tooltips, menus and extension popups: showing every
        # Chrome_WidgetWin_1 would itself change the application state.
        if name.value != "Chrome_WidgetWin_1" or api.GetWindowLongW(handle, -16) & 0x00C00000 != 0x00C00000:
            return True
        row = {"handle": int(handle), "visible": bool(api.IsWindowVisible(handle)), "minimized": bool(api.IsIconic(handle))}
        if restore and (not row["visible"] or row["minimized"]):
            api.ShowWindow(handle, 9)  # SW_RESTORE; never another process's window.
            row["restoreRequested"] = True
            row["visibleAfterRestore"] = bool(api.IsWindowVisible(handle))
        rows.append(row)
        return True
    callback = callback_type(visit)
    if not api.EnumWindows(callback, 0):
        raise OSError(ctypes.get_last_error(), "Cannot enumerate owned Chrome windows")
    return rows


class Connection:
    def __init__(self, ws, folder: Path, recorder):
        self.ws, self.recorder = ws, recorder
        self.pending, self.number = {}, 0
        self.origin = time.monotonic()
        self.events = (folder / "events.jsonl").open("w", encoding="utf-8")
        self.commands = (folder / "commands.jsonl").open("w", encoding="utf-8")
        self.closing = False
        self.reader = asyncio.create_task(self.read())

    def log(self, stream, message):
        stream.write(json.dumps({"elapsedSeconds": time.monotonic() - self.origin,
                                 "phase": self.recorder.phase, **message}, ensure_ascii=False) + "\n")
        stream.flush()

    async def read(self):
        try:
            async for message in self.ws:
                if message.type != aiohttp.WSMsgType.TEXT:
                    if message.type == aiohttp.WSMsgType.ERROR:
                        raise ConnectionError(str(self.ws.exception()))
                    continue
                value = json.loads(message.data)
                if "id" in value:
                    self.log(self.commands, {"direction": "received", **value})
                    future = self.pending.get(value["id"])
                    if future and not future.done():
                        future.set_result(value)
                else:
                    self.log(self.events, value)
                    self.recorder.event(value)
        except Exception as error:
            self.recorder.gap("reader-failure", str(error))
        finally:
            if not self.closing:
                self.recorder.gap("connection-lost", "CDP disconnected before recording finished")
            for future in self.pending.values():
                if not future.done():
                    future.set_exception(ConnectionError("CDP disconnected"))

    async def call(self, method, params=None, session=None, timeout=10):
        self.number += 1
        ident = self.number
        future = asyncio.get_running_loop().create_future()
        self.pending[ident] = future
        payload = {"id": ident, "method": method, "params": params or {}}
        if session:
            payload["sessionId"] = session
        try:
            self.log(self.commands, {"direction": "sent", **payload})
            await self.ws.send_json(payload)
            try:
                result = await asyncio.wait_for(future, timeout)
            except asyncio.TimeoutError as error:
                raise TimeoutError(f"{method} did not reply within {timeout} seconds") from error
            if "error" in result:
                raise RuntimeError(f"{method}: {result['error']}")
            return result.get("result", {})
        finally:
            self.pending.pop(ident, None)
            if not future.done():
                future.cancel()

    async def close(self):
        self.closing = True
        await self.ws.close()
        await self.reader
        self.events.close()
        self.commands.close()


class Recorder:
    def __init__(self, kind, folder, scenario, archive=None, diagnostic=False, pause_children=False):
        self.kind, self.folder, self.scenario = kind, folder, scenario
        self.phase = "setup"
        self.cdp = None
        self.main_session = None
        self.tasks = set()
        self.accept_tasks = True
        self.body_slots = asyncio.Semaphore(8)
        self.requests = {}
        self.sessions = {}
        self.load = asyncio.Event()
        self.archive = archive
        self.diagnostic = diagnostic
        self.scripts = []
        self.attaching_targets = set()
        self.pause_children = bool(pause_children or archive)
        self.default_context = None
        self.result = {"browser": kind, "state": "starting", "gaps": [], "checks": {},
                       "requests": [], "exceptions": [], "consoleProblems": [], "networkFailures": [],
                       "sessions": self.sessions, "recordingMode": "low-interference-cdp",
                       "scope": {"debuggerEnabled": False, "pauseOnStart": False, "pageWrappers": False,
                                 "requestInterception": False, "dynamicScriptSources": False,
                                 "caughtExceptions": False, "nativeInstructionTrace": False}}
        if archive:
            self.result["recordingMode"] = "intercepted-archive-replay"
            self.result["scope"]["requestInterception"] = True
            self.result["replay"] = {"sourceInventorySha256": archive.identity, "hits": archive.hits,
                "limitations": ["HTTP response replay changes timing, transport, cache and server interaction.",
                    "WebSocket and streaming protocols require their own recorded transport; HTTP misses fail closed."]}
        if diagnostic:
            self.result["diagnostic"] = {"scripts": self.scripts, "nativeTraceLimit": 8192,
                "description": "Chrome debugger source collection / Mimic runtime trace. No breakpoints or pause-on-exception."}
        if self.pause_children:
            self.result["recordingMode"] = "intercepted-archive-replay" if archive else "child-startup-diagnostic"
            self.result["scope"]["pauseOnStart"] = True
            self.result["scope"]["childStartup"] = "Targets pause until recording domains are ready; timing is instrumented."

    def gap(self, kind, detail):
        self.result["gaps"].append({"kind": kind, "detail": detail})

    def spawn(self, coro):
        if not self.accept_tasks:
            coro.close()
            return
        task = asyncio.create_task(coro)
        self.tasks.add(task)
        def done(finished):
            self.tasks.discard(finished)
            if not finished.cancelled() and finished.exception():
                self.gap("capture-operation-failed", str(finished.exception()))
        task.add_done_callback(done)

    def event(self, message):
        if self.phase in {"finished", "cleanup"}:
            return
        method, p, sid = message.get("method"), message.get("params", {}), message.get("sessionId")
        # Some CDP implementations deliver nested rather than flattened events.
        if method == "Target.receivedMessageFromTarget":
            nested = json.loads(p["message"])
            nested["sessionId"] = p["sessionId"]
            self.event(nested)
            return
        if method == "Target.attachedToTarget":
            child, info = p["sessionId"], p["targetInfo"]
            if child not in self.sessions:
                self.sessions[child] = info
                if self.main_session is not None:
                    if self.phase != "setup" and not p.get("waitingForDebugger"):
                        self.gap("child-startup-not-observed", f"{info.get('type')}: {info.get('url')}; attachment never pauses execution")
                    self.spawn(self.enable_child(child, p.get("waitingForDebugger", False)))
            return
        if method == "Runtime.executionContextCreated" and sid == self.main_session:
            context = p.get("context", {})
            aux = context.get("auxData", {})
            if aux.get("isDefault") and aux.get("frameId") == self.sessions.get(sid, {}).get("targetId"):
                self.default_context = context["id"]
        elif method == "Runtime.executionContextsCleared" and sid == self.main_session:
            self.default_context = None
        elif method == "Runtime.executionContextDestroyed" and sid == self.main_session and p.get("executionContextId") == self.default_context:
            self.default_context = None
        if method == "Target.targetCreated":
            info = p.get("targetInfo", {})
            if info.get("type") in {"worker", "shared_worker", "service_worker"}:
                self.spawn(self.attach_target(info))
            return
        if method == "Target.targetCrashed":
            self.gap("target-crashed", p)
        if method == "Page.loadEventFired" and sid == self.main_session:
            self.load.set()
        if method == "Fetch.requestPaused" and self.archive:
            self.spawn(self.archive.serve(self, p, sid))
            return
        if method in {"Debugger.scriptParsed", "Debugger.scriptFailedToParse"} and self.diagnostic:
            row = {"index": len(self.scripts), "session": sid, **p}
            self.scripts.append(row)
            self.spawn(self.script_source(row))
            return
        if self.phase not in {"recording", "actions", "final-observation", "draining"}:
            return
        key = (sid, p.get("requestId"))
        if method == "Network.requestWillBeSent":
            previous = self.requests.get(key)
            if p.get("redirectResponse") and not previous:
                self.gap("redirect-start-missing", p.get("requestId"))
            if p.get("redirectResponse") and previous:
                previous.update(status=p["redirectResponse"].get("status"), completion="redirect",
                                response=p["redirectResponse"])
            request = p["request"]
            row = {"index": len(self.result["requests"]), "session": sid, "requestId": p["requestId"],
                   "url": request["url"], "method": request["method"], "type": p.get("type"),
                   "request": request, "completion": "pending"}
            self.requests[key] = row
            self.result["requests"].append(row)
            if any(k.lower() == "content-type" and "multipart/" in str(v).lower()
                   for k, v in request.get("headers", {}).items()):
                self.gap("multipart-upload-bytes-unverified", row["url"])
            if request.get("hasPostData") and "postData" not in request:
                self.spawn(self.post_data(row))
        elif method == "Network.responseReceived":
            row = self.request_row(sid, p)
            if row is None:
                self.gap("request-start-missing", {"session": sid, "requestId": p.get("requestId")})
            else:
                row.update(status=p["response"].get("status"), response=p["response"])
        elif method == "Network.loadingFinished":
            row = self.request_row(sid, p)
            if row:
                row["completion"] = "finished"
                self.spawn(self.body(row))
            else:
                self.gap("request-start-missing", {"session": sid, "requestId": p.get("requestId")})
        elif method == "Network.loadingFailed":
            row = self.request_row(sid, p)
            if row:
                row.update(completion="failed", error=p.get("errorText"))
            else:
                self.gap("request-start-missing", {"session": sid, "requestId": p.get("requestId")})
            self.result["networkFailures"].append({"url": row.get("url") if row else None,
                                                   "error": p.get("errorText"), "canceled": p.get("canceled", False)})
        elif method == "Runtime.exceptionThrown":
            detail = p.get("exceptionDetails", {})
            self.result["exceptions"].append({"text": detail.get("text"), "url": detail.get("url"),
                "lineNumber": detail.get("lineNumber"), "columnNumber": detail.get("columnNumber"),
                "stackTrace": detail.get("stackTrace"),
                "description": detail.get("exception", {}).get("description")})
        elif method == "Runtime.consoleAPICalled" and p.get("type") in {"error", "warning", "assert"}:
            self.result["consoleProblems"].append({"type": p["type"], "args": [
                {k: a[k] for k in ("type", "value", "unserializableValue", "description") if k in a}
                for a in p.get("args", [])]})
        elif method in {"Network.webSocketCreated", "Network.eventSourceMessageReceived"}:
            self.gap("streaming-observation-boundary", {"event": method, "detail": p})

    def request_row(self, sid, params):
        """A navigation request can start on the parent and finish in an OOPIF."""
        key = (sid, params.get("requestId"))
        row = self.requests.get(key)
        if row is None and self.kind == "chrome":
            target = self.sessions.get(sid, {})
            ancestors = {target.get("parentId"), target.get("parentFrameId"), target.get("targetId")}
            candidates = [r for (session, ident), r in self.requests.items()
                          if ident == params.get("requestId") and r.get("completion") == "pending"
                          and self.sessions.get(session, {}).get("targetId") in ancestors]
            # Never merge ambiguous IDs or independently owned target requests.
            if len(candidates) == 1:
                row = candidates[0]
                self.requests[key] = row
        if row is not None:
            row.setdefault("requestSession", row["session"])
            row["session"] = sid
        return row

    async def enable_child(self, sid, waiting):
        # In replay this also installs Fetch interception before any child code
        # or request may run. A failed interceptor must never resume live traffic.
        await self.enable(sid, child=True)
        if waiting:
            await self.cdp.call("Runtime.runIfWaitingForDebugger", session=sid)

    async def enable(self, sid, child=False):
        params = {"maxTotalBufferSize": 200_000_000, "maxResourceBufferSize": 50_000_000,
                  "maxPostDataSize": 50_000_000, "enableDurableMessages": True} if self.kind == "chrome" else {}
        for method, args in (("Network.enable", params), ("Runtime.enable", {}),
                             ("Target.setAutoAttach", {"autoAttach": True, "waitForDebuggerOnStart": self.pause_children, "flatten": True})):
            try:
                await self.cdp.call(method, args, sid)
            except Exception as error:
                self.gap("domain-unavailable", str(error))
        if not child or self.sessions.get(sid, {}).get("type") in {"page", "iframe"}:
            await self.cdp.call("Page.enable", session=sid)
        if self.archive:
            # Failure aborts navigation: an unavailable interceptor must not become live replay.
            await self.cdp.call("Fetch.enable", {"patterns": [{"urlPattern": "*", "requestStage": "Request"}]}, sid)
        if self.diagnostic:
            try:
                if self.kind == "chrome":
                    await self.cdp.call("Debugger.enable", {"maxScriptsCacheSize": 100_000_000}, sid)
                    self.result["scope"].update(debuggerEnabled=True, dynamicScriptSources=True)
                elif not child:
                    await self.cdp.call("Mimic.startTrace", session=sid)
                    self.result["scope"]["nativeRuntimeEvents"] = True
            except Exception as error:
                self.gap("diagnostic-unavailable", str(error))

    async def attach_target(self, info):
        ident = info["targetId"]
        if ident in self.attaching_targets or any(s.get("targetId") == ident for s in self.sessions.values()):
            return
        self.attaching_targets.add(ident)
        try:
            response = await self.cdp.call("Target.attachToTarget", {"targetId": ident, "flatten": True})
            # Chrome emits attachedToTarget before the response. Some peers only
            # return the session, so feed the same session registration path.
            self.event({"method": "Target.attachedToTarget", "params": {"sessionId": response["sessionId"], "targetInfo": info}})
        finally:
            self.attaching_targets.discard(ident)

    async def prepare_background_targets(self):
        """Observe existing workers before the scenario, without claiming their boot history."""
        await self.cdp.call("Target.setDiscoverTargets", {"discover": True})
        # Let the fresh profile finish startup on about:blank. This preparation
        # precedes the scenario window; no site code or identity is patched.
        await asyncio.sleep(3)
        targets = await self.cdp.call("Target.getTargets")
        self.result["targetsBeforeScenario"] = targets.get("targetInfos", [])
        for info in targets.get("targetInfos", []):
            if info.get("type") in {"worker", "shared_worker", "service_worker"}:
                await self.attach_target(info)
        deadline = time.monotonic() + 10
        while self.tasks and time.monotonic() < deadline:
            await asyncio.wait(list(self.tasks), timeout=.1)
        if self.tasks:
            self.gap("background-setup-incomplete", len(self.tasks))
        self.result["scope"]["backgroundStartupBeforeScenario"] = "not observed; existing workers attach before navigation"

    async def script_source(self, row):
        data = await self.cdp.call("Debugger.getScriptSource", {"scriptId": row["scriptId"]}, row["session"])
        if not isinstance(data.get("scriptSource"), str):
            self.gap("script-source-unavailable", row.get("url"))
            return
        path = self.folder / f"script-{row['index']:06d}.json"
        write_json(path, data)
        row.update(sourceFile=path.name, sourceSha256=digest(path))

    async def finish_diagnostic(self):
        if not self.diagnostic or self.kind != "mimic":
            return
        try:
            await self.cdp.call("Mimic.stopTrace", session=self.main_session)
            value = await self.cdp.call("Mimic.getTrace", session=self.main_session)
            write_json(self.folder / "native-trace.json", value)
            events = value.get("events")
            if not isinstance(events, list):
                raise ValueError("Native trace events missing")
            self.result["diagnostic"]["nativeTraceEvents"] = len(events)
            if len(events) >= 8192:
                self.gap("native-trace-capacity-reached", "8192-event tail may omit earlier execution")
        except Exception as error:
            self.gap("native-trace-unavailable", str(error))

    async def post_data(self, row):
        data = await self.cdp.call("Network.getRequestPostData", {"requestId": row["requestId"]}, row["session"])
        row["requestPostData"] = data
        # Protocol may omit multipart file bytes. Preserve the limitation explicitly.
        content_type = next((str(v) for k, v in row["request"].get("headers", {}).items() if k.lower() == "content-type"), "")
        if "multipart/" in content_type:
            self.gap("multipart-upload-bytes-unverified", row["url"])

    async def body(self, row):
        async with self.body_slots:
            try:
                data = await self.cdp.call("Network.getResponseBody", {"requestId": row["requestId"]}, row["session"])
                if not isinstance(data.get("body"), str):
                    raise ValueError("Missing body field")
                relative = f"bodies/{row['index']:06d}.json"
                write_json(self.folder / relative, data)
                raw = base64.b64decode(data["body"], validate=True) if data.get("base64Encoded") else data["body"].encode("utf-8")
                row.update(bodyFile=relative, bodySha256=hashlib.sha256(raw).hexdigest(), bodyBytes=len(raw))
            except Exception as error:
                # No-content and HEAD responses have no representation body.
                if row.get("status") in {204, 205, 304} or row["method"] == "HEAD":
                    row["bodyAbsentByHTTP"] = True
                else:
                    self.gap("response-body-unavailable", {"url": row["url"], "error": str(error)})

    async def evaluate(self, expression):
        params = {"expression": expression, "returnByValue": True}
        if self.default_context is not None:
            params["contextId"] = self.default_context
        result = await self.cdp.call("Runtime.evaluate", params, self.main_session)
        if "exceptionDetails" in result:
            raise RuntimeError(f"Observation failed: {result['exceptionDetails']}")
        if "value" not in result.get("result", {}):
            raise RuntimeError("Observation did not return a JSON value")
        return result["result"]["value"]

    async def action(self, action):
        self.phase = "actions"
        if action["type"] == "wait":
            await asyncio.sleep(action["seconds"])
            return
        if self.kind == "chrome":
            if self.result.get("launch", {}).get("pid"):
                windows = owned_windows(self.result["launch"]["pid"], restore=True)
                self.result.setdefault("inputNativeWindows", []).append(windows)
            window = await self.cdp.call("Browser.getWindowForTarget", {"targetId": self.sessions[self.main_session]["targetId"]})
            if window.get("bounds", {}).get("windowState") == "minimized":
                # Activating a tab does not restore a minimized native window. This is
                # an explicit input prerequisite, not an identity/timing override.
                await self.cdp.call("Browser.setWindowBounds", {"windowId": window["windowId"], "bounds": {"windowState": "normal"}})
                self.result.setdefault("inputWindowRestorations", []).append(window)
        if action["type"] == "type":
            await self.cdp.call("Page.bringToFront", session=self.main_session)
            await self.cdp.call("Input.insertText", {"text": action["text"]}, self.main_session)
            return
        await self.cdp.call("Page.bringToFront", session=self.main_session)
        if action["type"] == "press":
            key = action["key"]
            codes = {"Enter": 13, "Tab": 9, "Escape": 27, "Backspace": 8, "Delete": 46,
                     "ArrowUp": 38, "ArrowDown": 40, "ArrowLeft": 37, "ArrowRight": 39}
            for kind in ("keyDown", "keyUp"):
                params = {"type": kind, "key": key, "code": key, "windowsVirtualKeyCode": codes[key], "nativeVirtualKeyCode": codes[key]}
                if key == "Enter" and kind == "keyDown":
                    params.update(text="\r", unmodifiedText="\r")
                await self.cdp.call("Input.dispatchKeyEvent", params, self.main_session)
            return
        root = await self.cdp.call("DOM.getDocument", {"depth": 0}, self.main_session)
        node = await self.cdp.call("DOM.querySelector", {"nodeId": root["root"]["nodeId"], "selector": action["selector"]}, self.main_session)
        if not node.get("nodeId"):
            raise ValueError(f"Action target not found: {action['selector']}")
        model = await self.cdp.call("DOM.getBoxModel", {"nodeId": node["nodeId"]}, self.main_session)
        points = model["model"]["content"]
        x, y = sum(points[::2]) / 4, sum(points[1::2]) / 4
        # No DOM click injection or forced scrolling. Coordinates and resulting checks are retained.
        for kind in ("mouseMoved", "mousePressed", "mouseReleased"):
            await self.cdp.call("Input.dispatchMouseEvent", {"type": kind, "x": x, "y": y,
                                "button": "none" if kind == "mouseMoved" else "left", "clickCount": 1}, self.main_session)

    async def observe(self):
        self.phase = "final-observation"
        for check in self.scenario["checks"]:
            selector, prop = json.dumps(check["selector"]), check["property"]
            if prop == "count":
                expression = f"({{available:true,value:document.querySelectorAll({selector}).length}})"
            else:
                expression = f"""(() => {{
                    const element = document.querySelector({selector});
                    if (!element) return {{ available: false }};
                    return {{ available: true, value: element[{json.dumps(prop)}] }};
                }})()"""
            try:
                value = await self.evaluate(expression)
                if not isinstance(value, dict) or type(value.get("available")) is not bool or (value["available"] and "value" not in value):
                    raise ValueError("Malformed terminal observation")
                self.result["checks"][check["name"]] = value
            except Exception as error:
                self.gap("terminal-observation-failed", {"name": check["name"], "error": str(error)})
        for name, method, params in (
            ("cookies.json", "Storage.getCookies", {}),
            ("targets-final.json", "Target.getTargets", {}),
        ):
            try:
                value = await self.cdp.call(method, params)
                write_json(self.folder / name, value)
                if name == "cookies.json":
                    if not isinstance(value.get("cookies"), list):
                        raise ValueError("Storage.getCookies omitted its cookie list")
                    self.result["finalCookies"] = value["cookies"]
                if name == "targets-final.json":
                    if not isinstance(value.get("targetInfos"), list):
                        raise ValueError("Target.getTargets omitted its target list")
                    observed = {s.get("targetId") for s in self.sessions.values()}
                    for target in value.get("targetInfos", []):
                        if target.get("type") in {"page", "iframe", "worker", "shared_worker", "service_worker"} and target.get("targetId") not in observed:
                            self.gap("target-not-recorded", target)
            except Exception as error:
                self.gap("final-evidence-unavailable", str(error))
        try:
            root = await self.cdp.call("DOM.getDocument", {"depth": 0}, self.main_session)
            dom = await self.cdp.call("DOM.getOuterHTML", {"nodeId": root["root"]["nodeId"]}, self.main_session)
            # Keep HTML as JSON, not a directly executable captured web page.
            write_json(self.folder / "dom.json", dom)
            self.result["finalEnvironment"] = await self.evaluate(IDENTITY)
            if "captureMetadata" in self.result:
                final = self.result["finalEnvironment"]
                self.result["captureMetadata"].update(viewport=final["viewport"], window=final["window"],
                    secureContextState=final["secureContext"], isolationState=final["crossOriginIsolated"],
                    contextObservation="Final document after the outcome window; initial identity is retained separately")
        except Exception as error:
            self.gap("final-dom-or-environment-unavailable", str(error))

    async def drain(self):
        self.phase = "draining"
        deadline = time.monotonic() + 12
        while time.monotonic() < deadline:
            if not self.tasks and not any(r["completion"] == "pending" for r in self.result["requests"]):
                break
            timeout = min(.1, max(.01, deadline - time.monotonic()))
            if self.tasks:
                await asyncio.wait(list(self.tasks), timeout=timeout)
            else:
                await asyncio.sleep(timeout)
        self.phase = "finished"
        self.accept_tasks = False
        unfinished = list(self.tasks)
        if unfinished:
            self.gap("capture-tasks-incomplete", len(unfinished))
            for task in unfinished:
                task.cancel()
            await asyncio.gather(*unfinished, return_exceptions=True)
        for row in self.result["requests"]:
            if row["completion"] == "pending":
                self.gap("request-incomplete-at-cutoff", row["url"])

    async def observe_presentation(self):
        """Retain final native/CDP state without restoring or activating the window."""
        if self.kind != "chrome" or self.result.get("launch", {}).get("browserMode") == "headless":
            return
        launch = self.result.get("launch", {})
        try:
            target = self.sessions[self.main_session]["targetId"]
            launch["finalObservedWindow"] = await self.cdp.call(
                "Browser.getWindowForTarget", {"targetId": target})
        except Exception as error:
            self.gap("final-window-state-unavailable", str(error))
        try:
            launch["finalNativeWindows"] = owned_windows(launch["pid"])
        except Exception as error:
            self.gap("final-native-window-state-unavailable", str(error))
        final = self.result.get("finalEnvironment", {})
        if final.get("visibilityState") != "visible" or final.get("window", {}).get("outerWidth", 0) <= 0:
            self.gap("presentation-state-unverified", {
                "document": final.get("visibilityState"),
                "window": final.get("window"),
                "cdpWindow": launch.get("finalObservedWindow"),
                "nativeWindows": launch.get("finalNativeWindows"),
            })


async def capture(kind: str, binary: Path, port: int, folder: Path, scenario: dict, archive=None, diagnostic=False, pause_children=False, headless=False) -> dict:
    folder.mkdir()
    (folder / "bodies").mkdir()
    recorder = Recorder(kind, folder, scenario, archive, diagnostic, pause_children)
    result = recorder.result
    result["startedUTC"] = datetime.now(timezone.utc).isoformat()
    result["scenario"] = scenario
    proc = cdp = None
    log = None
    try:
        available_port(port)
        if not binary.is_file():
            raise FileNotFoundError(binary)
        profile = tempfile.mkdtemp(prefix="mimic-doctor-chrome-") if kind == "chrome" else None
        command = launch_arguments(kind, binary, port, profile, headless)
        browser_mode = "headless" if headless else "headful"
        result["launch"] = {"binary": str(binary), "sha256": digest(binary), "arguments": command,
                            "profile": profile, "profileFreshness": "fresh-controlled" if profile else "fresh-process",
                            "browserMode": browser_mode if kind == "chrome" else "nonvisual-runtime",
                            "host": platform.platform(), "context": "persistent-default"}
        if kind == "mimic" and shutil.which("go"):
            try:
                # Inspect with the installed tool; metadata must not trigger a
                # module-driven toolchain download before the browser launch.
                build = subprocess.run(["go", "version", "-m", str(binary)], capture_output=True,
                                       text=True, encoding="utf-8", errors="replace", timeout=10,
                                       env={**os.environ, "GOTOOLCHAIN": "local"})
                result["launch"]["buildMetadata"] = {"exitCode": build.returncode, "stdout": build.stdout,
                                                      "stderr": build.stderr}
            except (OSError, subprocess.TimeoutExpired) as error:
                recorder.gap("build-metadata-unavailable", str(error))
        log = (folder / "process.log").open("wb")
        startup = None
        if os.name == "nt" and kind == "chrome" and not headless:
            # The parent terminal/tool host may itself inherit SW_HIDE. A headful
            # reference needs its own explicit visible GUI startup condition.
            startup = subprocess.STARTUPINFO()
            startup.dwFlags |= subprocess.STARTF_USESHOWWINDOW
            startup.wShowWindow = 1  # SW_SHOWNORMAL
            result["launch"]["startupWindowPolicy"] = "SW_SHOWNORMAL"
        # CREATE_NO_WINDOW hides only console windows; Chrome remains an ordinary headful GUI.
        proc = subprocess.Popen(command, cwd=ROOT, stdout=log, stderr=subprocess.STDOUT,
                                creationflags=subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0,
                                startupinfo=startup)
        result["launch"].update(pid=proc.pid, actualCommandLine=psutil.Process(proc.pid).cmdline())
        actual_binary = Path(psutil.Process(proc.pid).exe()).resolve()
        result["launch"]["actualExecutable"] = str(actual_binary)
        if actual_binary != binary.resolve():
            raise ValueError("Launched process executable differs from the requested binary")
        endpoint = f"http://127.0.0.1:{port}"
        async with aiohttp.ClientSession(trust_env=False) as http:
            version = None
            for _ in range(150):
                if proc.poll() is not None:
                    raise RuntimeError(f"Browser exited during startup: {proc.returncode}")
                try:
                    async with http.get(endpoint + "/json/version", timeout=aiohttp.ClientTimeout(total=1)) as response:
                        version = await response.json()
                    break
                except (aiohttp.ClientError, asyncio.TimeoutError):
                    await asyncio.sleep(.1)
            if not version:
                raise TimeoutError("CDP startup timed out")
            verify_listener_owner(proc, port)
            result["launch"]["listenerOwnershipVerified"] = True
            result["version"] = version
            if kind == "chrome" and version.get("Browser") != PINNED_PRODUCT:
                raise ValueError(f"Expected {PINNED_PRODUCT}, got {version.get('Browser')}")
            ws = await http.ws_connect(version["webSocketDebuggerUrl"], max_msg_size=256 * 1024 * 1024)
            cdp = recorder.cdp = Connection(ws, folder, recorder)
            targets = await cdp.call("Target.getTargets")
            pages = [t for t in targets.get("targetInfos", []) if t.get("type") == "page" and t.get("url") == "about:blank"]
            if pages:
                target = pages[0]
            else:
                target = await cdp.call("Target.createTarget", {"url": "about:blank"})
            attached = await cdp.call("Target.attachToTarget", {"targetId": target["targetId"], "flatten": True})
            recorder.main_session = attached["sessionId"]
            recorder.sessions[recorder.main_session] = target
            await recorder.enable(recorder.main_session)
            if kind == "chrome":
                await recorder.prepare_background_targets()
            identity = await recorder.evaluate(IDENTITY)
            result["initialEnvironment"] = identity
            if kind == "chrome":
                window = await cdp.call("Browser.getWindowForTarget", {"targetId": target["targetId"]})
                result["launch"]["observedWindow"] = window
                result["launch"]["nativeWindows"] = owned_windows(proc.pid)
                if identity.get("webdriver") is not False or not headless and "HeadlessChrome" in identity.get("userAgent", ""):
                    raise ValueError("Chrome launch identity is invalid; original webdriver must be false")
                result["captureMetadata"] = {
                    "chromeVersion": PINNED_VERSION, "chromiumRevision": CHROMIUM_REVISION,
                    "v8Version": version.get("V8-Version"), "platform": identity.get("platform"),
                    "browserMode": browser_mode, "commandLineFeatureOverrides": [],
                    "viewport": identity["viewport"], "window": identity["window"],
                    "secureContextState": identity["secureContext"], "isolationState": identity["crossOriginIsolated"],
                    "environmentProfileId": default_profile_id(browser_mode), "profileFreshness": "fresh-controlled",
                    "identityObservation": "about:blank before navigation; no overrides",
                }
                result["referenceAuthority"] = "non-authoritative headless diagnostic" if headless else "headful reference; presentation qualification retained"
            recorder.phase = "recording"
            result["state"] = "recording"
            navigation = await cdp.call("Page.navigate", {"url": scenario["url"]}, recorder.main_session, timeout=15)
            result["navigation"] = navigation
            if navigation.get("errorText"):
                recorder.gap("navigation-failed", navigation["errorText"])
            try:
                await asyncio.wait_for(recorder.load.wait(), scenario["loadTimeoutSeconds"])
            except asyncio.TimeoutError:
                recorder.gap("load-deadline", "Main page did not emit load before the deadline")
            await asyncio.sleep(scenario["settleSeconds"])
            result["actions"] = []
            for index, action in enumerate(scenario["actions"]):
                outcome = {"index": index, "action": action, "startedUTC": datetime.now(timezone.utc).isoformat()}
                result["actions"].append(outcome)
                try:
                    await recorder.action(action)
                    outcome["status"] = "dispatched"
                except Exception as error:
                    outcome.update(status="failed", error=str(error))
                    recorder.gap("action-failed", outcome.copy())
                    break
            await asyncio.sleep(scenario["settleSeconds"])
            result["observationWindowEndUTC"] = datetime.now(timezone.utc).isoformat()
            await recorder.observe()
            await recorder.observe_presentation()
            await recorder.finish_diagnostic()
            await recorder.drain()
            result["state"] = "finished"
            result["processExitBeforeCleanup"] = proc.poll()
            await cdp.close()
            cdp = None
    except asyncio.CancelledError:
        result["state"] = "cancelled"
        recorder.gap("run-cancelled", "Recording interrupted; partial evidence retained")
        raise
    except Exception as error:
        result["state"] = "failed"
        recorder.gap("run-failed", f"{type(error).__name__}: {error}")
    finally:
        recorder.phase = "cleanup"
        recorder.accept_tasks = False
        for task in list(recorder.tasks):
            task.cancel()
        if recorder.tasks:
            await asyncio.gather(*recorder.tasks, return_exceptions=True)
        if cdp:
            try:
                await cdp.close()
            except Exception as error:
                recorder.gap("connection-cleanup-failed", str(error))
        if proc:
            result.setdefault("processExitBeforeCleanup", proc.poll())
            try:
                await asyncio.to_thread(stop_owned, proc)
            except Exception as error:
                recorder.gap("process-cleanup-failed", str(error))
        if log:
            log.close()
        result["finishedUTC"] = datetime.now(timezone.utc).isoformat()
        write_json(folder / "capture.json", result)
        seal(folder)
    return result
