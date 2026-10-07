"""Strict HTTP archive replay. This is an intercepted diagnostic, never an oracle control."""
from __future__ import annotations

import base64
import hashlib
import json
from pathlib import Path

from doctor_report import digest, verify


class Archive:
    def __init__(self, folder: Path):
        self.folder = folder.resolve()
        self.capture = verify(self.folder)
        if self.capture.get("state") != "finished":
            raise ValueError("Replay requires a finished source capture")
        self.identity = digest(self.folder / "inventory.json")
        self.rows = self.capture["requests"]
        self.used = set()
        self.hits = []

    def response(self, request):
        """Match exact URL, method and upload, in occurrence order for each key."""
        for index, row in enumerate(self.rows):
            if index in self.used or (row["url"], row["method"]) != (request["url"], request["method"]):
                continue
            recorded = row.get("request", {})
            post = recorded.get("postData", row.get("requestPostData", {}).get("postData"))
            if recorded.get("hasPostData") and post is None:
                raise ValueError("Archive upload bytes are missing")
            if request.get("hasPostData") and "postData" not in request:
                raise ValueError("Intercepted upload bytes are missing")
            # Absent and empty both encode zero upload bytes when hasPostData is false.
            if (post or "") != (request.get("postData") or ""):
                continue
            if row.get("completion") not in {"finished", "redirect"}:
                raise ValueError("Archive response did not finish")
            response = row.get("response", {})
            headers = response.get("headers", {})
            if any(k.lower() == "content-type" and "multipart/" in str(v).lower()
                   for k, v in recorded.get("headers", {}).items()):
                raise ValueError("Multipart upload bytes cannot be verified")
            status = row.get("status")
            if not isinstance(status, (int, float)) or int(status) != status or not 200 <= status <= 599:
                raise ValueError("Archive HTTP status is missing or unsupported")
            if status == 304:
                raise ValueError("A 304 archive response needs a preserved cache representation")
            raw = b""
            if row.get("bodyFile"):
                path = (self.folder / row["bodyFile"]).resolve()
                if not path.is_relative_to(self.folder):
                    raise ValueError("Unsafe body path")
                envelope = json.loads(path.read_text(encoding="utf-8"))
                raw = base64.b64decode(envelope["body"], validate=True) if envelope.get("base64Encoded") else envelope["body"].encode("utf-8")
                if hashlib.sha256(raw).hexdigest() != row.get("bodySha256"):
                    raise ValueError("Decoded body hash mismatch")
            elif row.get("completion") != "redirect" and status not in {204, 205} and row["method"] != "HEAD":
                raise ValueError("Archive response body is missing")
            # CDP bodies are decoded; transfer framing and compression headers cannot survive.
            omitted = {"content-length", "content-encoding", "transfer-encoding", "connection"}
            pairs = [{"name": k, "value": line} for k, v in headers.items() if k.lower() not in omitted
                     for line in str(v).split("\n")]
            pairs.append({"name": "Content-Length", "value": str(len(raw))})
            self.used.add(index)
            self.hits.append({"archiveIndex": index, "url": request["url"], "method": request["method"]})
            return {"responseCode": int(status), "responseHeaders": pairs,
                    "body": base64.b64encode(raw).decode("ascii")}
        raise ValueError(f"No unused archived response for {request['method']} {request['url']}")

    async def serve(self, recorder, params, session):
        try:
            response = self.response(params["request"])
            await recorder.cdp.call("Fetch.fulfillRequest", {"requestId": params["requestId"], **response}, session)
        except Exception as error:
            recorder.gap("replay-response-unavailable", str(error))
            # Never silently fall through to the live origin.
            await recorder.cdp.call("Fetch.failRequest", {"requestId": params["requestId"], "errorReason": "Failed"}, session)


async def reduce_actions(actions, reproduces, budget=30):
    """Bounded deletion reduction; caller defines and verifies the exact failure pair."""
    best = list(actions)
    attempts, width = 0, max(1, len(best) // 2)
    uncertain = False
    while best and attempts < budget:
        changed = False
        for start in range(0, len(best), width):
            proposal = best[:start] + best[start + width:]
            attempts += 1
            outcome = await reproduces(proposal)
            if outcome is None:
                uncertain = True
            if outcome is True:
                best, changed = proposal, True
                break
            if attempts >= budget:
                break
        if not changed:
            if attempts >= budget and start + width < len(best):
                return best, attempts, False
            if width == 1:
                return best, attempts, not uncertain
            width = max(1, width // 2)
        else:
            width = min(width, max(1, len(best)))
    return best, attempts, not best and not uncertain
