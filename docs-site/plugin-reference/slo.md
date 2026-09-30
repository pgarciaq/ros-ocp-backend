# slo

> **Last verified:** 2026-09-30

Package: [`internal/plugins/slo`](https://github.com/pgarciaq/ros-ocp-backend/blob/{{ git_branch }}/internal/plugins/slo)

**SLO rollup store** — persists bounded per-hosted-cluster histogram bucket rollups of hosted apiserver latency plus backend-derived worker-pressure summaries. Correlation input store for the HCP correlator (#625); ships no recommendations itself.

## Plugin metadata

| Property | Value |
|----------|-------|
| Name | `slo` |
| Phase | 1 (Produce) |
| Priority | 50 |
| CSV types | `slo` (SLO rollup CSV: `ros-openshift-slo-*.csv`) |
| Retention tables | `hosted_api_bucket_rollups`, `hosted_worker_pressure` (90 days, swept with history via `ROS_HISTORY_RETENTION_DAYS`) |

## Traits

| Trait | Supported |
|-------|-----------|
| CSVIngestor | Yes — parses `slo` CSV type, idempotent hourly upserts |
| RetentionProvider | Yes — sweeps both tables (partition drops) |
| APIProvider | No — no routes |
| TermProvider | No — store only, no recommendation windows |

## What it does

1. Ingest per-cycle cumulative histogram snapshots (`hc_cluster_id | window_start | window_end | verb_group | le | bucket_count | collected_at`) into `hosted_api_bucket_rollups`. Verb groups are `mutating` / `read` / `other` with verb regexes baked in at collection; `WATCH`/`CONNECT`/`PROXY` are excluded at source. Counts are stored verbatim; reset-aware deltas are computed at read (correlator).
2. Worker pressure (`hosted_worker_pressure`) is backend-derived from node digests (codes `12` `NODE_OVERCOMMITTED` / `74` `NODE_POD_SCHEDULING_LIMIT` + freshness gate) — no operator CSV. Absence/staleness never renders healthy.

## Load-bearing behavior

SLO ingest never permanently fails (skip-with-counters, always `Done`): a poisoned SLO file must not gate container recommendations via manifest completeness. The deferred recommendation switch needs no `slo` case — unknown types skip.

## Architecture

- Implements the #624 closed investigation (bucket/shape/writer/oracle/retention locks); canonical contract lives on [#644](https://github.com/pgarciaq/ros-ocp-backend/issues/644).
- Operator emission: `koku-metrics-operator` `internal/collector/slo_{queries,collector}.go` (External-gated hourly snapshot).
- Consumer: HCP correlator ([#625](https://github.com/pgarciaq/ros-ocp-backend/issues/625)).
