#!/usr/bin/env python3
"""RBAC stub for deterministic error injection (#921).

A tiny HTTP service impersonating the insights-rbac v1 access endpoint so
the ROS API's RBAC client (internal/api/middleware/rbac.go) can be driven
through its error paths on demand: the #532 pagination-error leg, the #543
truncated-and-never-cached leg, and the #546 429/408-not-403 leg.

Deployed via the manifest next to this file (ConfigMap + stock UBI python
image); the ROS API twin points RBACHOST at this service. The response
shapes mirror internal/api/middleware/rbac_test.go's fixtures — the
contract the unit layer already pins.

Control protocol (used by the IQE tests):
  PUT /__control/mode   {"mode": "<name>"}   — set the response mode
  GET  /__control/hits  — {"access_hits": N} — access-endpoint hit counter
  PUT  /__control/reset — zero the counters, mode back to normal

Modes:
  normal           two pages of canned ACLs, then Links.Next clears
  error-first-429  the first access request answers 429 (#546)
  error-first-408  the first access request answers 408 (#546)
  error-page2-429  page 1 serves, page 2 answers 429 (#532: the client
                   must fail, not serve page 1's partial set)
  truncated        every page keeps Links.Next set — the client stops at
                   maxRBACPages and serves the partial WITHOUT caching (#543)
  deny-403         the first access request answers 403 (the authoritative
                   denial path — served as 403, distinct from #546's 503)
"""
import json
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

MODES = (
    "normal",
    "error-first-429",
    "error-first-408",
    "error-page2-429",
    "truncated",
    "deny-403",
)

# Canned ACLs: wildcard cluster/node/project read for any caller (the error
# legs exercise transport semantics, not scoping — that is #925's domain,
# covered against the real rbac). Empty resourceDefinitions aggregate to
# the "*" resource set in the ROS client.
def canned_acls():
    return [
        {"permission": "cost-management:openshift.cluster:read"},
        {"permission": "cost-management:openshift.node:read"},
        {"permission": "cost-management:openshift.project:read"},
        {"permission": "cost-management:settings:read"},
    ]


class State:
    def __init__(self):
        self.lock = threading.Lock()
        self.mode = "normal"
        self.access_hits = 0


STATE = State()


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, fmt, *args):  # keep the pod log readable
        print(f"rbacstub: {self.command} {self.path} mode={STATE.mode}", flush=True)

    # -- helpers ---------------------------------------------------------
    def _json(self, code, obj):
        body = json.dumps(obj).encode()
        self.send_response(code)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def _access_page(self, next_link):
        """One RBAC access page: the canned ACLs plus the given Links.Next."""
        self._json(
            200,
            {
                "meta": {"count": 4},
                "links": {
                    "first": "/api/rbac/v1/access/?offset=0",
                    "next": next_link,
                    "previous": None,
                    "last": "/api/rbac/v1/access/?offset=100",
                },
                "data": canned_acls(),
            },
        )

    # -- control plane ----------------------------------------------------
    def _control(self):
        if self.path == "/__control/hits":
            with STATE.lock:
                self._json(200, {"access_hits": STATE.access_hits})
            return True
        if self.path == "/__control/reset":
            with STATE.lock:
                STATE.mode = "normal"
                STATE.access_hits = 0
            self._json(200, {"ok": True})
            return True
        return False

    def do_PUT(self):
        if self.path == "/__control/mode":
            length = int(self.headers.get("Content-Length", 0))
            mode = json.loads(self.rfile.read(length) or b"{}").get("mode")
            if mode not in MODES:
                self._json(400, {"error": f"unknown mode {mode!r}", "modes": MODES})
                return
            with STATE.lock:
                STATE.mode = mode
            self._json(200, {"mode": mode})
            return
        self._json(404, {"error": "not found"})

    # -- the RBAC v1 access endpoint ---------------------------------------
    def do_GET(self):
        if self._control():
            return
        if not self.path.startswith("/api/rbac/v1/access/"):
            self._json(404, {"error": "not found"})
            return

        with STATE.lock:
            STATE.access_hits += 1
            mode = STATE.mode
            page = 1 if "offset" not in self.path else 2

        if mode == "deny-403":
            self._json(403, {"errors": [{"detail": "denied (stub)"}]})
            return
        if mode == "error-first-429":
            self._json(429, {"errors": [{"detail": "rate limited (stub)"}]})
            return
        if mode == "error-first-408":
            self._json(408, {"errors": [{"detail": "timeout (stub)"}]})
            return
        if mode == "error-page2-429":
            if page == 1:
                self._access_page("/api/rbac/v1/access/?application=cost-management&limit=100&offset=100")
            else:
                self._json(429, {"errors": [{"detail": "rate limited mid-pagination (stub)"}]})
            return
        if mode == "truncated":
            # Links.Next never clears: the client stops at maxRBACPages and
            # serves the partial without caching it (#543).
            self._access_page("/api/rbac/v1/access/?application=cost-management&limit=100&offset=100")
            return
        # normal
        if page == 1:
            self._access_page("/api/rbac/v1/access/?application=cost-management&limit=100&offset=100")
        else:
            self._access_page("")


def main():
    server = ThreadingHTTPServer(("0.0.0.0", 8080), Handler)
    print("rbacstub: listening on :8080", flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
