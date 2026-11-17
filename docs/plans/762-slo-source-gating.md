# #762 SLO Source Gating — Implementation Plan

> **For agentic workers:** superpowers:test-driven-development governs every step. Owner go-ahead 2026-11-07. Implementation in ros-ocp-backend (the server correlate + the offline librobne advisory); the gate is the owner-locked #644 amendment (migration 000202) — no new design decisions.

**Goal:** the headroom latency leg (H) gates on `source == "kubernetes"` per the #644 contract; different sources' histogram schemas never combine in the quantile path.

**Mechanism (verified at HEAD, zero drift):** three drop points — `evaluate.go` `loadBucketSamples` (no source column selected), `windowP99`/`WindowP99` (map keys `(verb, le)` only → latest-wins across sources), `headroom.go` `evalHeadroom` (builds `Sample` without the `Source` the `csv.SLORow` already carries). The audit's proof: calm Kubernetes + later metrics-server → `do_not_add_workers_first, high, p99 2.98` vs `0.3`.

**Fix:**
1. `librobne/advisory/quantile.go`: `Sample` gains `Source string`; `WindowP99`'s `latest`/`latestBefore` keys include Source (schema mixing structurally impossible for every caller; apitax's samples share the empty source — inert).
2. `librobne/advisory/headroom.go`: `evalHeadroom` skips rows with `r.Source != "kubernetes"` (the gate; legacy `''` and other jobs recorded, non-gating).
3. `internal/engine/correlate/evaluate.go`: `bucketSample` gains `source`; `loadBucketSamples` selects it; `windowP99` keys include source; `evalH` filters `all` to kubernetes right after load — the single slice feeds BOTH the fire window and the daily baselines, so the coherent-source-policy criterion holds by construction.

**Tests:**
- RED (both layers): the audit's repro — calm Kubernetes snapshots + coherent metrics-server one minute later → the advisory must stay silent (baseline: fires with the filed numbers). Server-level via the correlate evaluation harness; offline via `EvaluateHeadroom` with SLORows.
- Controls: reverse timing (metrics-server first); differing bucket schemas (disjoint `le` sets — no phantom boundaries); genuine Kubernetes pain still fires (the gate must not anesthetize).
- Quantile unit tests: source-keyed maps (two sources, same (group, le), different collected times — no cross-source replacement).

**Constraints:** no broader multi-source design; no threshold changes; apitax untouched (its own bucketSample/query); commits wait for owner go-ahead.
