# Adversarial Reviews

Adversarial reviews are structured security and engineering audits conducted against
ros-ocp-backend using a "red team" mindset. Reviewers assume attacker access to the
API port, inspect code for compound failure chains, and validate fixes across
deployment postures (SaaS, on-prem chart, SNO/dev).

## Methodology

Each review combines:

- **Static code review** — source-level inspection of handlers, middleware, SQL, and config
- **Architecture analysis** — threat modeling (STRIDE-lite) across Kafka ingestion, API, and DB layers
- **Operational failure-mode analysis** — what happens when services degrade, misconfigure, or restart under load
- **Cross-system integration checks** — NetworkPolicy gaps, env var wiring across Helm charts, cross-service auth

Findings are classified by severity (Critical / High / Medium / Low / Info) and
tracked through remediation with commit-level verification.

## Review Cycles

| Review | Date | Scope | Findings | Resolved |
|--------|------|-------|----------|----------|
| v1.0 | 2026-06-11 | Full engine + API + ingestion + auth | 30 | 30/30 |
| v2.0 | 2026-06-11 | Follow-up: internal auth, tag wiring, GPU memory | 15 | 15/15 |
| v3.0 | 2026-06-11 | Follow-up: recalc storms, staleness, analytics mode | 10 | 10/10 |
| v4.0 | 2026-06-11 | Follow-up: keyset pagination, savings integer, migrations | 10 | 10/10 |
| v5.0 | 2026-06-11 | Cumulative integration validation | 5 | 5/5 |
| Phase 13 v1 | 2026-06-14 | Remediation commits: worker pools, Helm security, koku-ui | 22 | 22/22 |
| Phase 13 v2 | 2026-06-15 | Incremental: tag push end-to-end, NetworkPolicy, statement timeout | 7 | 7/7 |

**Total findings across all cycles: 85+ (all resolved).**

## Deployment Postures

Findings are assessed against three deployment postures — many issues only apply
when gateway/RBAC compensating controls are absent:

| Posture | Auth | RBAC | Risk profile |
|---------|------|------|--------------|
| **SaaS** (console.redhat.com) | 3scale JWT | Enabled | Lowest — gateway + platform controls |
| **On-prem chart** (default) | Envoy gateway + JWKS | Enabled | Low — NetworkPolicy + chart defaults |
| **SNO/dev overrides** | None (direct API) | Disabled | Highest — no compensating controls |

## Top Issues Found and Fixed

The following represent the most impactful findings across all review cycles.

### Critical / High Severity

| # | Finding | Risk | Resolution |
|---|---------|------|------------|
| 1 | **Kafka commit before processing** — offset committed before CSV fully processed; crash loses data silently | Data loss on processor crash | Per-file `report_file_status` tracking (migration 000140), error surfacing, recommendation gating, `ros_ingestion_file_failures_total` metric |
| 2 | **Ingestion errors swallowed** — CSV parse failures logged but not propagated; stale recommendations served | Stale recommendations without alerting | Error propagation to manifest status, strict analytics mode (default on), `rosocp_analytics_incomplete_total` counter |
| 23 | **Tag push not end-to-end wired** — ROS configured for HTTP push but Koku workers missing env vars; NetworkPolicy blocked callbacks | Silent loss of tag filtering in production | Wired `ROS_TAGS_ENABLED=true` + `ROS_TAGS_SOURCE=api` on Koku workers; added `cost-worker` to `ros-api-access` NetworkPolicy |
| 24 | **Masu NetworkPolicy blocks ROS API** — `masu-access` policy omitted ros-api from allowlist; GPU savings and business-hours rates fail | Zero savings values under NetworkPolicy enforcement | Added `ros-api` to masu ingress allowlist |
| 37 | **Internal tags endpoint unauthenticated** — `/internal/tags/sync` accepted any caller without bearer validation | Cross-tenant tag injection | Bearer auth via `ROS_INTERNAL_TAGS_AUTH_REQUIRED` (default `true`); service-account allowlist validation |
| 31 | **Pagination filter bypass** — `workload_type` filter not applied to keyset page-key query; returned unfiltered rows | Data leakage across filter boundaries | Fixed keyset query to propagate all active filters |
| 27 | **Internal auth disable non-fatal** — `ROS_INTERNAL_TAGS_AUTH_REQUIRED=false` only warned at startup, never blocked | Misconfigured production clusters run without internal auth | `ValidateSecurityConfig()` now returns fatal error in non-dev mode |

### Medium Severity

| # | Finding | Risk | Resolution |
|---|---------|------|------------|
| 8 | **Unbounded memory during grouped ingest** — entire cluster's digest rows held in memory map | OOM on large clusters (10k+ containers) | Incremental digest flush with streaming batch (500 containers) |
| 12 | **SSRF via CSV URL** — processor fetched presigned URLs without host allowlist; attacker-controlled manifests could probe internal network | Internal network scanning | `ROS_CSV_ALLOWED_HOSTS` allowlist + private-network deny; auto-derived from chart S3 endpoint |
| 13 | **SQL injection via ILIKE wildcards** — `%` and `_` in filter values not escaped | Query manipulation / performance DoS | `EscapeILIKE()` utility applied to all user-facing ILIKE filters |
| 14 | **Unbounded offset pagination** — `offset=999999999` forced full sequential scan | DoS via expensive query plans | Offset cap (10,000); keyset pagination as primary path |
| 21 | **No statement timeout** — long-running queries could hold connections indefinitely | Connection pool exhaustion under load | `SET LOCAL statement_timeout` per transaction; global 25s default; per-endpoint heavy timeout (45s) |
| 25 | **CSV allowlist normalization** — `https://host:443` didn't match `u.Hostname()` from presigned URLs | Ingestion failure after misconfigured endpoint | Helm helper strips scheme and port; startup validation warning |
| 11 | **Recalculation storms** — settings change triggered unbounded parallel goroutines per cluster | CPU/memory spike, connection exhaustion | Worker pool with configurable concurrency cap; `ctx.Done()` propagation |

### Security Hardening

| # | Finding | Risk | Resolution |
|---|---------|------|------------|
| 15 | **Dev token accepted in production** | Auth bypass if DEVELOPMENT=true leaks to prod | Dev token blocked when `DEVELOPMENT != true` |
| 16 | **Empty SA allowlist accepts all service accounts** | Cross-tenant invocation of internal endpoints | Startup validation rejects empty allowlist in `api` tag mode |
| 20 | **PII in error logs** — CSV row content logged on parse failure | Data exposure in log aggregation | Redacted to field names + row index only |
| 22 | **GPU/node endpoints loaded full result in memory** — no SQL pagination | OOM on large GPU fleets | SQL-backed pagination for GPU MIG, time-slicing, and node endpoints |

## Scorecard (Latest)

| Dimension | Rating | Notes |
|-----------|--------|-------|
| Data integrity | Strong | Per-file tracking, strict analytics, error propagation |
| Authentication | Strong | Gateway-delegated; internal endpoints SA-authenticated |
| Authorization | Strong | RBAC enabled by default; cluster/node scoping |
| API security | Strong | SSRF allowlist, ILIKE escape, offset cap, statement timeout |
| Database & connections | Strong | Unified pool, metrics, connection budget |
| Memory & performance | Strong | Streaming ingest, SQL pagination, bounded batches |
| Operational resilience | Strong | Graceful shutdown, DLQ, readiness probes |
| Engineering governance | Strong | 162+ ADRs, CHANGELOG, migration lint CI |

## Internal Documentation

Full review documents with source-level findings, code locations, and commit
references are maintained in `docs/audits/` (not published externally):

- `docs/audits/adversarial-review.md` — v1–v5 cumulative (85 findings)
- `docs/audit/adversarial-review-phase13-2026-06-14.md` — Phase 13 v1
- `docs/audit/adversarial-review-phase13-v2-2026-06-15.md` — Phase 13 v2
