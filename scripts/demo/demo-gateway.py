#!/usr/bin/env python3
"""demo-gateway.py — dev-only reverse proxy splitting UI API traffic.

Mirrors the on-prem gateway split without any cluster:
  /api/cost-management/v1/recommendations/* -> ros-ocp API (default :8002)
  /api/*                                     -> Koku (default :8000)

ros-api requires x-rh-identity, which browsers never send: injects a static
demo identity (env DEMO_IDENTITY_B64, else the baked-in org-1234567 admin)
for recommendation paths only. Never use outside local demos.

Usage: python3 scripts/demo-gateway.py   # listens 127.0.0.1:8080 (DEMO_GATEWAY_PORT)
"""
import os
import urllib.request
import urllib.error
from http.server import BaseHTTPRequestHandler, HTTPServer

ROS_API = os.environ.get("DEMO_ROS_API", "http://localhost:8002")
KOKU_API = os.environ.get("DEMO_KOKU_API", "http://localhost:8000")
PORT = int(os.environ.get("DEMO_GATEWAY_PORT", "8080"))
IDENTITY_FILE = os.environ.get("DEMO_IDENTITY_FILE", "")

DEFAULT_IDENTITY = (
    "eyJpZGVudGl0eSI6eyJhY2NvdW50X251bWJlciI6IjEwMDAxIiwib3JnX2lkIjoiMTIzNDU2NyIs"
    "InR5cGUiOiJVc2VyIiwidXNlciI6eyJ1c2VybmFtZSI6InVzZXJfZGV2IiwiZW1haWwiOiJ1c2Vy"
    "X2RldkBmb28uY29tIiwiaXNfb3JnX2FkbWluIjp0cnVlLCJhY2Nlc3MiOnt9fX0sImVudGl0bGVt"
    "ZW50cyI6eyJjb3N0X21hbmFnZW1lbnQiOnsiaXNfZW50aXRsZWQiOnRydWV9fX0="
)

ROUTES = [
    ("/api/cost-management/v1/recommendations/", ROS_API),
    ("/api/", KOKU_API),
]


def demo_identity():
    if IDENTITY_FILE:
        with open(IDENTITY_FILE) as f:
            return f.read().strip()
    return DEFAULT_IDENTITY


class Handler(BaseHTTPRequestHandler):
    def _proxy(self):
        target_base = next((t for p, t in ROUTES if self.path.startswith(p)), None)
        if target_base is None:
            self.send_response(404)
            self.end_headers()
            return
        url = target_base + self.path
        headers = {k: v for k, v in dict(self.headers).items() if k.lower() != "host"}
        if target_base == ROS_API and "x-rh-identity" not in {k.lower(): 1 for k in headers}:
            headers["x-rh-identity"] = demo_identity()
        length = int(self.headers.get("Content-Length", 0) or 0)
        body = self.rfile.read(length) if length else None
        try:
            req = urllib.request.Request(url, data=body, headers=headers, method=self.command)
            with urllib.request.urlopen(req, timeout=120) as resp:
                data = resp.read()
                self.send_response(resp.status)
                for k, v in resp.getheaders():
                    if k.lower() not in ("transfer-encoding", "content-length", "connection"):
                        self.send_header(k, v)
                self.send_header("Content-Length", str(len(data)))
                self.end_headers()
                self.wfile.write(data)
        except urllib.error.HTTPError as e:
            data = e.read()
            self.send_response(e.code)
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
        except Exception as e:  # noqa: BLE001 - dev proxy must never hang the UI
            msg = ("gateway error: %s" % e).encode()
            self.send_response(502)
            self.send_header("Content-Length", str(len(msg)))
            self.end_headers()
            self.wfile.write(msg)

    do_GET = do_POST = do_PUT = do_DELETE = do_PATCH = _proxy

    def log_message(self, *a):
        pass


if __name__ == "__main__":
    print("demo gateway on 127.0.0.1:%d (ros=%s, koku=%s)" % (PORT, ROS_API, KOKU_API), flush=True)
    HTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
