# 0308 — Auto-lower heavy API statement timeout in SaaS mode

**Status:** Accepted
**Date:** 2026-06-30
**Domain:** Performance / Operations
**Phase:** 15+
**Issue:** [#44](https://github.com/pgarciaq/ros-ocp-backend/issues/44)

## Context

The SaaS ingress/gateway at console.redhat.com enforces a ~30-second timeout on
HTTP responses. The `ROS_HEAVY_API_STATEMENT_TIMEOUT_MS` default was 45000ms,
meaning heavy queries (savings-summary, fleet-wide container list) could run for
up to 45 seconds — well past the point where the gateway has already returned a
504 to the client.

When a query outlives the gateway budget:
1. PostgreSQL continues executing a query whose result will never be delivered.
2. The connection remains occupied, reducing pool availability.
3. The client has already received a 504 and may retry, compounding load.

On-prem deployments without a gateway benefit from the higher 45s limit because
they serve responses directly without an intermediate timeout.

## Decision

Default `ROS_HEAVY_API_STATEMENT_TIMEOUT_MS` to **gateway_budget − 2000ms** when
the service detects it is running in SaaS mode (Clowder environment).

Detection uses `ACG_CONFIG` environment variable presence — the same mechanism
used by `clowder.IsClowderEnabled()` in the app-common-go library.

| Environment | Default | Rationale |
|-------------|---------|-----------|
| SaaS (`ACG_CONFIG` set) | 28000ms | 30s gateway − 2s margin |
| On-prem (no `ACG_CONFIG`) | 45000ms | No upstream gateway constraint |

The 2-second margin accounts for:
- Response serialization: ~200–500ms
- TCP delivery: ~50ms
- Gateway processing overhead: ~100ms
- Safety buffer: ~1000ms

An explicit `ROS_HEAVY_API_STATEMENT_TIMEOUT_MS` env var always overrides the
auto-detected default regardless of environment.

A startup log line reports which default was selected and why.

## Alternatives Considered

1. **Always use 28s:** Too restrictive for on-prem deployments without a gateway,
   where longer queries are acceptable and results are delivered directly.

2. **Always use 45s:** Wastes PostgreSQL resources in SaaS when queries outlive
   the gateway budget. Operators may not notice the mismatch until 504s appear.

3. **Documentation-only (no code change):** Relies on operators reading and
   correctly applying guidance. Experience shows defaults are rarely overridden
   until a problem surfaces in production.

4. **Read gateway budget from Clowder config:** Clowder does not expose the
   gateway timeout value. Hardcoding 30s minus margin is the pragmatic approach.

## Consequences

- **SaaS:** Heavy queries are cancelled by PostgreSQL before the gateway timeout,
  freeing the connection for other work. Clients receive a proper timeout error
  (translated to 504 by the API layer) instead of a disconnection.
- **On-prem:** No behavioral change. The 45s default remains.
- **Override:** Operators who set `ROS_HEAVY_API_STATEMENT_TIMEOUT_MS` explicitly
  are unaffected — the env var always wins.
- **Observability:** The startup log makes the effective timeout visible without
  inspecting environment variables.
