#!/usr/bin/env python3
"""Healthy llama-server stand-in so Matchora skips downloading GGUFs in smoke tests."""
from __future__ import annotations

import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

HOST = "0.0.0.0"
PORT = 8080
EMBED = "all-MiniLM-L6-v2-Q4_K_M.gguf"
MODELS = json.dumps({"data": [{"id": EMBED, "object": "model"}]}).encode()
OK = json.dumps({"ok": True}).encode()


class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):
        return

    def _send(self, body: bytes, content_type: str = "application/json"):
        self.send_response(200)
        self.send_header("Content-Type", content_type)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        path = self.path.split("?", 1)[0]
        if path in ("/v1/models", "/models"):
            self._send(MODELS)
            return
        self._send(OK)

    def do_POST(self):
        self._send(OK)


def main():
    httpd = ThreadingHTTPServer((HOST, PORT), Handler)
    print(f"llama-stub listening on {HOST}:{PORT}", flush=True)
    httpd.serve_forever()


if __name__ == "__main__":
    main()
