#!/usr/bin/env python3
"""
假的 OpenAI 兼容生图服务，用于端到端验证。

返回一张 1x1 的 PNG（base64），形状和真上游一致：
  POST /v1/images/generations -> {"data":[{"b64_json":"..."}]}

用法：python3 scripts/fake-image-api.py [port]
"""
import base64
import json
import sys
from http.server import BaseHTTPRequestHandler, HTTPServer

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 18999

# 8x8 纯色 PNG，足够验证落盘与展示链路
PNG = base64.b64decode(
    "iVBORw0KGgoAAAANSUhEUgAAAAgAAAAICAYAAADED76LAAAAHElEQVQoz2Nk"
    "YPhfz0AEYBxVSF+FjAwMDAwMDAwAJfQH/0m6Q0YAAAAASUVORK5CYII="
)


class H(BaseHTTPRequestHandler):
    def _send(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_POST(self):
        n = int(self.headers.get("Content-Length", 0))
        raw = self.rfile.read(n)
        try:
            req = json.loads(raw or b"{}")
        except Exception:
            return self._send(400, {"error": {"message": "bad json"}})

        if not self.path.endswith("/images/generations"):
            return self._send(404, {"error": {"message": "not found: " + self.path}})
        if not req.get("prompt"):
            return self._send(400, {"error": {"message": "prompt required"}})

        # 按 n 返回多张，验证多图落盘
        cnt = min(int(req.get("n", 1) or 1), 4)
        self._send(200, {"data": [{"b64_json": base64.b64encode(PNG).decode()} for _ in range(cnt)]})

    def log_message(self, fmt, *args):
        sys.stderr.write("[fake-image] " + fmt % args + "\n")


if __name__ == "__main__":
    print(f"fake image api on http://127.0.0.1:{PORT}", flush=True)
    HTTPServer(("127.0.0.1", PORT), H).serve_forever()
