#!/usr/bin/env python3
"""Fake webhook consumer for Medora smoke tests."""
from __future__ import annotations

import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

HOST = "0.0.0.0"
PORT = 9090

_events: list[dict] = []
_lock = threading.Lock()


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        return

    def _json(self, code: int, body):
        data = json.dumps(body).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self):
        if self.path.rstrip("/") == "/events":
            with _lock:
                out = list(_events)
            self._json(200, out)
            return
        self._json(404, {"error": "not found"})

    def do_DELETE(self):
        if self.path.rstrip("/") == "/events":
            with _lock:
                _events.clear()
            self.send_response(204)
            self.end_headers()
            return
        self._json(404, {"error": "not found"})

    def do_POST(self):
        if self.path.rstrip("/") != "/hook":
            self._json(404, {"error": "not found"})
            return
        length = int(self.headers.get("Content-Length", "0"))
        raw = self.rfile.read(length) if length else b""
        body = None
        if raw:
            try:
                body = json.loads(raw.decode("utf-8"))
            except json.JSONDecodeError:
                body = raw.decode("utf-8", errors="replace")
        headers = {k: v for k, v in self.headers.items()}
        event = {"path": self.path, "headers": headers, "body": body}
        with _lock:
            _events.append(event)
        self._json(200, {"ok": True})


def main():
    httpd = ThreadingHTTPServer((HOST, PORT), Handler)
    print(f"webhook-stub listening on {HOST}:{PORT}", flush=True)
    httpd.serve_forever()


if __name__ == "__main__":
    main()
