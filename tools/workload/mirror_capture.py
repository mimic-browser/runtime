"""Serve captured same-origin resources unchanged through ordinary HTTP.

A corpus control, not the optimizer's replay implementation. Rebinding the
origin can affect page behavior and must be disclosed in benchmark provenance.
"""
import argparse
import gzip
import hashlib
import json
import threading
import zipfile
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlsplit

class Mirror:
    def __init__(self, capture, origin, port=0):
        self.archive = zipfile.ZipFile(capture)
        self.manifest = json.loads(self.archive.read("manifest.json"))
        self.origin = origin.rstrip("/")
        self.entries = {}
        for index, entry in enumerate(self.manifest["entries"]):
            if entry["url"].startswith(self.origin + "/") and entry["method"] == "GET":
                parts = urlsplit(entry["url"])
                self.entries[parts.path + ("?" + parts.query if parts.query else "")] = (index, entry)
        self.requests = []
        outer = self
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                outer.requests.append(self.path)
                found = outer.entries.get(self.path)
                if not found:
                    self.send_error(404); return
                index, entry = found
                body = outer.archive.read(f"bodies/{index}")
                if hashlib.sha256(body).hexdigest() != entry["bodySHA256"]:
                    self.send_error(500); return
                headers = entry["headers"]
                if headers.get("Content-Encoding") == ["gzip"]:
                    body = gzip.decompress(body)
                self.send_response(entry["status"])
                self.send_header("Content-Type", headers.get("Content-Type", ["application/octet-stream"])[0])
                self.send_header("Content-Length", str(len(body)))
                self.end_headers(); self.wfile.write(body)
            def log_message(self, *_): pass
        self.server = ThreadingHTTPServer(("127.0.0.1", port), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.base = f"http://127.0.0.1:{self.server.server_address[1]}"
    def close(self):
        self.server.shutdown(); self.server.server_close(); self.thread.join(); self.archive.close()

if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("capture"); parser.add_argument("origin"); parser.add_argument("--port", type=int, default=49360)
    args = parser.parse_args(); mirror = Mirror(args.capture, args.origin, args.port)
    print(mirror.base, flush=True)
    try: threading.Event().wait()
    except KeyboardInterrupt: pass
    finally: mirror.close()
