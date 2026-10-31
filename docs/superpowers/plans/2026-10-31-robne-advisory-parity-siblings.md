# robne advisory parity: zombie (#722), headroom (#723), api-tax (#724)

**STATUS: implementation complete 2026-10-31 — all gates green, commits held for owner go-ahead.**

Owner decision 2026-10-31: full parity, order #722 → #723 → #724, per-issue
commits held for the owner go-ahead. The skill is properly-implement-feature.

## Verified inputs (no plumbing gaps)

| Input | Source (robne today) |
|---|---|
| Hosted all-hours digests | `fl.containerDigests` (primary payload) |
| Management digests | context payload `csv.Load(...).Rows` — **new** capture in `loadAdvisoryContext` |
| Association namespaces | context topology `HCPSnapshotEntry.HCPNamespace` for the primary |
| SLO buckets | `csv.Load(...).SLORows` — **new** `fl.sloRows` (primary) + context |
| API-tax rows | `csv.Load(...).APITaxRows` — **new** `fl.apiTaxRows` (primary) |
| NodePool rows (short lane) | context `loaded.NodepoolRows` (already loaded for worker nodes) |
| Node pressure (N leg) | `recommendNodes(fl)` — `node.Rec.NotificationCodes` 12/74 |
| Still-on floor | `hcp.ControlPlaneCPUFloorMC` (100) |

## #722 zombie — `librobne/advisory/zombie.go`

- `ZombiePolicy{IdleWindowDays, IdleCPUFloorMC, ShortWindowDays, ShortRequiredDays, MinDataMinutes}` defaults 14/10/3/3/30 (`DefaultZombiePolicy` mirrors `DefaultPolicy`).
- Day sums: per-(day, ns, wl) CPU-p50 sums over hosted digests (row present = measured).
- Idle leg: port `evalZombieIdle` verbatim — full-window coverage, platform exclusions (compiled + synced), floor boundary (>= floor = active), one busy user day vetoes.
- Still-on: SUM `CPURequestP50MC` over assoc namespaces from mgmt digests, trailing day (`>= now-1d` date-based). Absent requests → unknown (medium); < 100 → known-not-on (silence); >= 100 → high.
- Snapshot freshness: 72h over context topology entries (complete, primary HC).
- Short lane (#674): NodePool rows for the HC; every covered day all-pools-zero (spec==0 AND status==0); >= required days coverage; legs over the short window; lane signals `lane:short-%dd`, `pool_zero_days:%d`.
- API context (never gates): per-day +Inf deltas over SLO rows, minSpan 30m.
- Details per #717: `peak_user_cpu_mc`, `idle_floor_cpu_mc`, `control_plane_cpu_request_mc`, `api_max_daily_requests` (only when known).
- Evidence = server signals verbatim. **No action copy in evidence** (server has none; copy branching is downstream). ADR-0333 table goes in the manpage + robne-cli.md (robne recommend output is JSON-only — no human-readable surface).
- Config: `zombie_advisory` YAML (window_days, idle_floor_mc, short_window_days, short_required_days, platform_namespaces).
- Tests: port `TestZombie_*` (fire/alive/spike/missing-days/medium/zero-CP/boundary/synced) + `TestZombieShort_*` (parked/partial/non-zero-veto/mid-transition/no-pool) + `TestEvalPoolParked_Matrix` as pure-function tests.

## #723 headroom — `librobne/advisory/headroom.go`

- `librobne/advisory/quantile.go`: verbatim port of `Bucket`/`Deltas`/`Quantile`/`MedianFloat64` + `Sample`/`WindowP99` (from evaluate.go, pure); port `quantile_test.go` cases.
- `HeadroomPolicy{HAbsoluteThresholdS, HBaselineMultiple, HWindowHours, HMinDataMinutes, CPUThresholdPct}` defaults 0.30/3/1/30/80.
- H: SLO rows (any payload) with `HCClusterID == primary`; window p99 + 7-day baseline median; threshold = max(abs, multiple×median).
- C: usage-p95/request-p50 ratio over assoc namespaces from mgmt digests (trailing day); > CPUThresholdPct = high.
- N: `recommendNodes(fl)` codes 12/74 → pressured; no recs = unknown.
- Rule: fire only H && C && !N with all legs known (port `Evaluate`); windowProof analogue = fresh complete snapshot for the primary in context topology.
- Details per #717: `hosted_api_p99_seconds`, `hosted_api_threshold_seconds`, `control_plane_capacity_ratio`.
- Config: `headroom_advisory` YAML (p99_threshold_s, baseline_multiple, window_hours, cp_cpu_pct).
- Tests: verdict matrix (silence on unknown legs + pressured N), leg boundary cases.
- Note: issue body mentions "codes 86/87" and `c_etcd_p99_s` — the shipped rule reads codes 12/74 and CPU ratio only; implement the shipped rule, note the discrepancy in the issue comment.

## #724 api-tax — `librobne/advisory/apitax.go`

- Verdict `tune_noisy_webhook`; consts p99 >= 2.0s, rejected >= 5% (server consts, not knobs — no YAML).
- Per-source coherence; per-webhook p99 via `WindowP99`; rejected rate from first/last totals deltas (reset-aware); hottest by p99; cluster-scoped (empty `hosted_cluster_id`).
- Window: now truncated to hour, minus HWindowHours (1h default) — same as server.
- Details per #717: `webhook_p99_seconds`, `webhook_p99_threshold_seconds`, `webhook_rejected_rate`, `webhook`.
- Tests: port `TestAPITax_*` (hot p99, rejected rate, quiet, no evidence).

## Shared wiring (each commit)

- `loadAdvisoryContext`: capture mgmt digests, assoc namespaces, SLO rows (primary + context), nodepool rows, snapshot freshness.
- `fileLoad`: add `sloRows`, `apiTaxRows`, `hcContextDigests`, `hcAssocNamespaces`, `hcSnapshotFresh`.
- `recommend.go`: wire `compute{Zombie,Headroom,APITax}Advisory` into both paths (file + pg) — append to `out.HCPAdvisories`, `sortAdvisoryOut`.
- Envelope version stays 13 (sibling exists from #721).
- `compare_parity.py`: `cmp_hcp_advisory` — compare details key-union instead of the fixed sleep field list.
- Manpage regen + docs-site robne-cli.md + changelog per issue.

## Gates per issue

`make test-short`, `make lint`, `make man && make man-check`, `make docs-lint docs-drift docs-sync-check`; issue comment with the completion-report table; commits held for owner go-ahead.
