> **Historical — Completed.** This plan has been fully implemented. Kept for reference.

# Phase 4: Pull Request Checklist

This document describes the PRs required to land Phase 4 (OOM Feedback and
Recommendation Quality) across all repositories, their merge order, and
dependencies.

## Merge Order (Strict)

PRs **must** be merged in this order. Merging out of order will break CI.

```
1. nise  ──────────────────►  merge + PyPI release
2. ros-ocp-backend  ───────►  merge (after nise is on PyPI)
3. iqe-ros-ocp-plugin  ───►  merge (after nise is on PyPI)
4. koku-metrics-operator  ─►  merge (independent, any time)
```

---

## PR 1: nise

| Field | Value |
|-------|-------|
| **Upstream repo** | `project-koku/nise` (GitHub) |
| **Fork** | `pgarciaq/nise` (GitHub) |
| **Source branch** | `pgarciaq-rosocp-superpowers-phase4` |
| **Target branch** | `main` |
| **Commits (4)** | See below |

### Commits

| SHA | Description |
|-----|-------------|
| `c92f444` | Add `oom_count` column to ROS container CSV generation |
| `b27d40c` | Add unit tests for `oom_count` in ROS CSV generation |
| `e1c40d9` | Support deterministic `oom_count` from static YAML |
| `7ccfe9a` | Omit OAuth `scope` parameter when `HCC_TOKEN_SCOPE` is empty |

### PR Description

```
COST-5691: Add oom_count to ROS CSV and fix OAuth scope for on-prem

- Add `oom_count` column to the ROS container CSV (`ocp_ros_usage`),
  with 90/10 weighted random generation (90% zero, 10% 1-3).
- Allow static YAML files to specify an explicit `oom_count` per pod
  for deterministic test data generation.
- Fix OAuth token acquisition for on-prem Keycloak: omit the `scope`
  parameter when `HCC_TOKEN_SCOPE` is empty, since on-prem Keycloak
  rejects `scope=api.console` with 400 Bad Request.
```

### Why first

The IQE OOM tests depend on `koku-nise` generating the `oom_count` CSV column.
IQE CI installs nise from PyPI, so this must be merged **and released to PyPI**
before the iqe-ros-ocp-plugin PR can pass CI.

### Post-merge action

**Release to PyPI** after merge so CI environments pick up the new version.

---

## PR 2: ros-ocp-backend

| Field | Value |
|-------|-------|
| **Upstream repo** | `RedHatInsights/ros-ocp-backend` (GitHub) |
| **Fork** | `pgarciaq/ros-ocp-backend` (GitHub) |
| **Source branch** | `pgarciaq-rosocp-superpowers-phase4` |
| **Target branch** | `pgarciaq-rosocp-superpowers-phase3` (or squash onto `main` if phase3 is already merged) |
| **Commits (20)** | See below |

### Commits

| SHA | Description |
|-----|-------------|
| `9620d3a` | Add Phase 4 OOM feedback plan and update docs for Phase 3 completion |
| `6bdd2f0` | Refine Phase 4 plan: CSV alignment, boxplot gap, partition error handling |
| `af5c644` | Defer recommendation_history to future phase |
| `95ee79c` | Add Phase 5 stub for deferred history, boxplots, and retention |
| `c40bf24` | Complete Phase 4 test plan with all missing test specifications |
| `92be01a` | Fix pipeline ordering for quality writer, mark quality as internal-only |
| `8183214` | Align native CSV parser columns with operator/nise output |
| `cf70553` | Add OOM bump to memory recommendations |
| `d9161ea` | Wire recommendation_quality writer with all 4 quality metrics |
| `70f6a3f` | Add E2E test for OOM pipeline and fix quality writer filter |
| `c2c2982` | Update phase-4 plan -- mark E2E test as done |
| `ba3b5ca` | Audit fixes for Phase 4 quality writer and tests |
| `c2292c3` | Fix compare tool to use operator column names and include oom_count |
| `495e357` | Harden Phase 4 with safety clamps, tuple filter, and test coverage |
| `cc0672b` | Code review fixes -- WorkloadType in quality keys, skip quality on read failure |
| `471530e` | Update plan with IQE test inventory for Phase 3 and 4 |
| `c6dab88` | Make legacy GORM query compatible with native engine rows |
| `c9a051a` | Auto-create digest partitions and document API contract |
| `3d8b384` | Always return notification_codes and notifications in API |
| `a20e76d` | Update plan docs for renamed test and omitempty removal |
| `22b6be6` | Update plan with IQE verification results and nise scope fix |

### PR Description

```
COST-5691: Phase 4 -- OOM feedback, recommendation quality, and API contract fixes

Engine changes:
- OOM bump in RecommendMemory: post-margin log-scale bump (15% at 1 OOM,
  capped at 60%), configurable via ROS_OOM_BASE_BUMP / ROS_OOM_MAX_BUMP
- recommendation_quality writer: all 4 metrics (oom_events_after_rec,
  stability_pct, adoption_detected, recommendation_age_hours)
- Auto-create digest partitions for historical data

API contract:
- Remove omitempty from notification_codes and notifications fields --
  always return [] and {} when empty
- Align native CSV parser columns with operator/nise output names

Tests:
- OOM bump unit tests (log-scale, max cap, zero OOM, custom params)
- Quality writer unit + integration tests (stability, adoption, partitions)
- E2E pipeline test (CSV -> digest -> recommendation -> quality)

Docs:
- Phase 4 plan with design decisions and implementation status
- Phase 5 stub for deferred history/boxplots
- Native engine notification API contract doc
```

### Dependency

No dependency on nise PyPI release. Backend changes are self-contained
and tested with Go-level fixtures.

---

## PR 3: iqe-ros-ocp-plugin

| Field | Value |
|-------|-------|
| **Upstream repo** | `insights-qe/iqe-ros-ocp-plugin` (GitLab CEE) |
| **Fork** | `pgarciaq/iqe-ros-ocp-plugin` (GitLab CEE) |
| **Source branch** | `pgarciaq-rosocp-superpowers-phase4` |
| **Target branch** | `pgarciaq-rosocp-superpowers-phase3` (or `master` if phase3 is already merged) |
| **Commits (5)** | See below |

### Commits

| SHA | Description |
|-----|-------------|
| `95b613f` | Add Phase 3 and Phase 4 IQE tests for native engine |
| `4dedc83` | Add cost_onprem environment support for ROS IQE tests |
| `7564f08` | Fix IQE test data generation and notification_codes tests |
| `463e7c4` | Use default fixture for notification_codes tests |
| `dd2d670` | Add skipped test_recommendation_quality_populated placeholder |

### PR Description

```
COST-5691: Phase 3+4 IQE tests for native engine and OOM feedback

Phase 3 tests (test_native_engine.py, 8 tests):
- Native response structure (top-level fields, deterministic UUID)
- Multi-term recommendations (short/medium/long, cost+performance engines)
- Engine fields (millicore and KiB units)
- Confidence level validation
- notification_codes and notifications always present

Phase 4 tests (test_oom.py, 3 tests + 1 skipped placeholder):
- OOM notification present (NotifOOMDetected code 3)
- OOM notification has correct Kruize-format entry
- OOM-bumped memory recommendation exceeds current config
- Skipped: recommendation_quality (not API-exposed yet)

Fixtures:
- ocp_static_report_ros_oom.yml with deterministic oom_count: 2
- n_days=16 for default fixture to ensure long_term recommendations
- cost_onprem environment config for Dynaconf

Verified: 11 passed, 1 skipped on SNO 4.21.8 aarch64.
```

### Dependency

**Requires nise on PyPI with `oom_count` CSV column.** The 3 OOM tests
(`test_oom_notification_present`, `test_oom_notification_has_correct_kruize_entry`,
`test_oom_bumped_memory_recommendation`) will fail if nise does not generate
the `oom_count` column.

The 8 Phase 3 tests are safe regardless of nise version.

---

## PR 4: koku-metrics-operator

| Field | Value |
|-------|-------|
| **Upstream repo** | `project-koku/koku-metrics-operator` (GitHub) |
| **Fork** | `pgarciaq/koku-metrics-operator` (GitHub) |
| **Source branch** | `pgarciaq-rosocp-superpowers-phase4` |
| **Target branch** | `main` |
| **Commits (2)** | See below |

### Commits

| SHA | Description |
|-----|-------------|
| `84055089` | Add OOM count PromQL query and CSV column for ROS containers |
| `39073e35` | Add unit tests for OOM count in ROS container CSV |

### PR Description

```
COST-5691: Add OOM count collection for ROS container reports

- Add PromQL query ros:oom_count_container_sum that joins
  increase(kube_pod_container_status_restarts_total) with
  kube_pod_container_status_last_terminated_reason{reason="OOMKilled"}
- Add OOMCount field to rosContainerRow, include in CSV header and row
- Unit tests for CSV header position and row formatting
```

### Dependency

**Independent.** The `oom_count` CSV column is additive. Old backends that
don't recognize it will ignore it (the ros-ocp-backend CSV parser handles
`oom_count` as optional via `if idx.oomCount >= 0`). Can be merged at any
time relative to the other PRs.

---

## Summary Table

| # | Repo | Fork branch | Target | Commits | Dependency |
|---|------|-------------|--------|---------|------------|
| 1 | nise | `pgarciaq-rosocp-superpowers-phase4` | `main` | 4 | None (merge first) |
| 2 | ros-ocp-backend | `pgarciaq-rosocp-superpowers-phase4` | phase3 or `main` | 20 | None |
| 3 | iqe-ros-ocp-plugin | `pgarciaq-rosocp-superpowers-phase4` | phase3 or `master` | 5 | Nise on PyPI |
| 4 | koku-metrics-operator | `pgarciaq-rosocp-superpowers-phase4` | `main` | 2 | Independent |

## Post-Merge Checklist

- [ ] Nise released to PyPI after PR 1 merges
- [ ] IQE CI passes after PR 3 merges (confirms nise PyPI has oom_count)
- [ ] Operator CI passes after PR 4 merges (Go tests, no external deps)
- [ ] Remove `pgarciaq-rosocp-superpowers-phase4` branches from all forks after merge
