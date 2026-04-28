# Agent Memory Dump — ROS OCP Native Engine Development

**Date:** 2026-04-29  
**Purpose:** Complete context for a new AI agent to resume work. Read this entire file before doing anything.

---

## Table of Contents

1. [Project Goal](#1-project-goal)
2. [Repository Map and Branch State](#2-repository-map-and-branch-state)
3. [Phase Summary — What Was Built](#3-phase-summary--what-was-built)
4. [Plan Documents Index](#4-plan-documents-index)
5. [Feature Implementation Status](#5-feature-implementation-status)
6. [Apollo Test Cluster Details](#6-apollo-test-cluster-details)
7. [Current E2E State on Apollo](#7-current-e2e-state-on-apollo)
8. [Immediate Pending Work](#8-immediate-pending-work)
9. [GPU Recommendations — Technical Details](#9-gpu-recommendations--technical-details)
10. [Cost Impact / Savings Estimation — Technical Details](#10-cost-impact--savings-estimation--technical-details)
11. [Nise GPU Data Generation](#11-nise-gpu-data-generation)
12. [Koku Backend Changes](#12-koku-backend-changes)
13. [Operator Changes](#13-operator-changes)
14. [Database Schema State](#14-database-schema-state)
15. [Key Bugs Found and Fixed](#15-key-bugs-found-and-fixed)
16. [E2E Testing Playbook](#16-e2e-testing-playbook)
17. [Known Issues and Gaps](#17-known-issues-and-gaps)
18. [Development Environment Notes](#18-development-environment-notes)
19. [What NOT to Do](#19-what-not-to-do)

---

## 1. Project Goal

Replace the Kruize (Java) recommendation engine in ros-ocp-backend with a **native Go engine** that:

- Computes CPU, memory, and GPU recommendations directly from daily digest data
- Uses decay-weighted percentiles (not Kruize's absolute-max approach) — intentionally ~10-16% less memory than Kruize for stable workloads, with OOM feedback closing the safety gap
- Provides cost impact / savings estimates by querying Koku's cost model rates
- Supports replica count awareness (pod_count_min/max/avg)
- Tracks recommendation history and quality metrics over time
- Offers namespace-level recommendations with boxplot visualizations
- Has custom timeframe settings (configurable term windows per org)
- Supports GPU workload classification, MIG right-sizing, and GPU savings estimation
- Works both on-prem (PostgreSQL-only) and SaaS (with Trino)
- Is at least as good as competitors (KubeCost, Utilyze) for GPU recommendations

The native engine has been developed over **6 phases** across **86 commits** on the `pgarciaq-rosocp-superpowers-phase6` branch.

---

## 2. Repository Map and Branch State

### ros-ocp-backend (PRIMARY — 86 commits, ~12,500 lines added)
- **Path:** `/home/pgarciaq/dev/koku/ros-ocp-backend/`
- **Branch:** `pgarciaq-rosocp-superpowers-phase6`
- **Remote:** `pgarciaq` → `git@github.com:pgarciaq/ros-ocp-backend.git`
- **Origin:** `https://github.com/RedHatInsights/ros-ocp-backend.git`
- **Phase branches exist for:** phase0 through phase6 (each extends the previous)
- **Latest commit:** `40237f5 Add comprehensive agent memory dump for session continuity`

### koku (backend — effective_rates endpoint + AGENTS.md)
- **Path:** `/home/pgarciaq/dev/koku/koku/`
- **Branch:** `pgarciaq-rosocp-superpowers`
- **Remote:** `pgarciaq` → `git@github.com:pgarciaq/koku.git`
- **Our commits (4 new, 2 are ours):**
  - `6fe59f939` Include all distributed cost types in effective-rates endpoint
  - `d10909686` Add effective-rates masu endpoint for ROS savings estimates
  - `faf88a188` Document additional cost-onprem deployment pitfalls
  - `23c0dc53b` Document cost-onprem aarch64 SNO deployment pitfalls in AGENTS.md

### koku-metrics-operator (operator — DCGM metrics + OOM + pod count)
- **Path:** `/home/pgarciaq/dev/koku/koku-metrics-operator/`
- **Branch:** `pgarciaq-rosocp-superpowers-phase4`
- **Remote:** `pgarciaq` → `git@github.com:pgarciaq/koku-metrics-operator.git`
- **Our commits (4 new):**
  - `924de0f4` Replace misleading DCGM DEV_ metrics with PROF_ profiling metrics
  - `1001a815` Add workload_pod_count PromQL query and CSV column for ROS containers
  - `603b37b0` COST-5691: Add unit tests for OOM count in ROS container CSV
  - `3383cf6c` Add OOM count PromQL query and CSV column for ROS containers

### nise (test data generator)
- **Path:** `/home/pgarciaq/dev/koku/nise/`
- **Branch:** `pgarciaq-rosocp-superpowers-phase4`
- **Remote:** `pgarciaq` → `https://github.com/pgarciaq/nise.git`
- **Our commits (7 new):**
  - `1057bbe` Add GPU profiling metrics to ROS container CSV generation
  - `d67a6e1` Add workload_pod_count column to ROS container CSV generation
  - `19b5024` Add ROS test data generation example for ros-ocp-backend
  - `7ccfe9a` COST-5691: Omit OAuth scope parameter when HCC_TOKEN_SCOPE is empty
  - `e1c40d9` COST-5691: Support deterministic oom_count from static YAML
  - `b27d40c` COST-5691: Add unit tests for oom_count in ROS CSV generation
  - `c92f444` Add oom_count column to ROS container CSV generation

### koku-ui (frontend — NO changes yet)
- **Path:** `/home/pgarciaq/dev/koku/koku-ui/`
- **Branch:** `main`
- **Status:** Waiting for Stefan (UX designer) to provide mockups

---

## 3. Phase Summary — What Was Built

### Phase 0: Critical Robustness Fixes
**12 bugs fixed** in the existing ros-ocp-backend codebase:
- RBAC nil pointer panic, API returning 200 on DB failure
- Kafka type assertion panics, subscribe failure silently ignored
- Missing HTTP timeouts, non-deterministic CSV row order
- Poison message infinite redelivery, GORM error ignored
- Date parse error swallowed, Kafka payload logged at Info level
- SendMessage failure not reconciled

### Phase 1+2+3: Native Go Recommendation Engine
The core engine — **the biggest change**:
- **CSV parsing** (`internal/ingestion/csvparser.go`): Parses operator CSV with float→int64 conversion, NaN/Inf validation
- **Daily digest computation** (`internal/ingestion/digest.go`): Groups rows by container+day, computes percentiles (p50/p60/p95/p98/p99/max/mean)
- **Recommendation engine** (`internal/engine/`):
  - `recommend_all.go` — Orchestrator: single SELECT, compute N terms, batch write
  - `recommend_cpu.go` — Decay-weighted percentile, 25mc floor, dual cost/performance output
  - `recommend_memory.go` — Adaptive margin (15-50% based on P95-P50 spread), separate limit > request
  - `detect_idle.go` — CPU < 10mc threshold
  - `trend.go` — Linear regression slope for trend detection
  - `notifications.go` — 24+ notification codes (idle, OOM, insufficient data, etc.)
  - `term_config.go` — Configurable short/medium/long term windows with decay half-life
- **API layer** (`internal/api/handlers.go`): New native handlers with CSV export, stale detection, fallback to Kruize
- **Test infrastructure** (`internal/testutil/`): testcontainers-go + PostgreSQL 16 + fixtures
- **Migrations 000022-000027**: daily_container_digests, daily_namespace_digests, org_recommendation_terms, notification_code_definitions, recommendation_sets relational columns, recommendation_quality/history tables

**Key design choice:** The native engine uses **averaged P95** (not absolute max like Kruize), resulting in ~10-16% less memory for stable workloads. This is deliberate — tighter recommendations with OOM feedback closing the safety gap. See appendix in `docs/plans/phase-1-2-3-go-engine.md`.

### Phase 4: OOM Feedback and Quality Tracking
- **OOM bump** (`recommend_memory.go`): Post-margin logarithmic bump: `1 + 0.15 × log₂(1 + oom_count)`, capped at 1.60×
- **Operator OOM query**: `kube_pod_container_status_last_terminated_reason{reason="OOMKilled"}` → `oom_count` CSV column
- **Quality writer** (`engine/quality.go`): 4 metrics — oom_events_after_rec, stability_pct, adoption_detected, recommendation_age_hours
- **CSV column alignment**: Renamed native parser columns to match operator/nise output
- **Nise oom_count**: Random 90/10 OOM generation + deterministic YAML support

### Phase 5: History, Boxplots, and Retention
- **Recommendation history** (`engine/history.go`): Snapshots every recommendation for trend analysis
- **Raw usage samples** (`container_usage_samples` table): Stores per-15-minute measurements
- **Boxplot assembly** (`model/boxplot.go`): Exact five-number summaries via PostgreSQL `percentile_cont()` at query time
- **Strongly-typed DetailResponse** (`model/detail_response.go`): Kruize-compatible JSON shape without runtime JSON manipulation
- **Retention sweep** (`engine/retention.go`): Background goroutine drops old monthly partitions (configurable, default 6 months)

### Phase 6: Namespace Recommendations + GPU + Savings + History API
- **Namespace recommendations** (`engine/recommend_namespace.go`): Aggregate container recs to namespace level with boxplots
- **Namespace boxplots**: Memory P60/P98/P99 percentiles, memory trend slope notifications
- **Custom timeframe settings API**: `GET/PUT/DELETE /recommendations/openshift/settings/terms`
- **Replica count**: `pod_count_min/max/avg` from operator's `workload_pod_count` column
- **CPU/Memory savings estimate** (`engine/savings.go`): Queries Koku effective_rates, computes delta × rate × pod_hours
- **Historical Tracking API**: `GET /recommendations/openshift/history` and `/quality` endpoints
- **History/quality retention**: Separate configurable retention (default 90 days)
- **cluster_uuid type migration**: TEXT → UUID for consistency (migration 000041)
- **GPU recommendations engine** (see section 9 for full details)
- **GPU savings estimation**: Uses Koku `gpu_cost_per_month` rate
- **GPU API filters**: `has_gpu`, `gpu_model`, `gpu_classification` (in-memory post-enrichment)
- **API response caching**: Cache-Control headers on recommendation endpoints
- **OpenAPI spec updates**: Comprehensive Swagger spec covering all new endpoints

---

## 4. Plan Documents Index

Read these for detailed design decisions, architecture diagrams, and test plans:

| Document | Path | Content |
|----------|------|---------|
| Phase 0 plan | `docs/plans/phase-0-critical-fixes.md` | 12 bug fixes with TDD approach |
| Phase 1-2-3 plan | `docs/plans/phase-1-2-3-go-engine.md` | Core engine architecture, Kruize comparison |
| Phase 4 plan | `docs/plans/phase-4-oom-feedback.md` | OOM bump design, quality tracking, cross-repo merge order |
| Phase 5 plan | `docs/plans/phase-5-history-and-boxplots.md` | History, boxplots, retention, raw samples |
| Replica count + savings plan | `docs/plans/replica-count-and-cost-impact.md` | Operator changes, cost calculation formula |
| GPU recommendations plan | `docs/plans/gpu-recommendations.md` | DCGM metrics, classification, MIG, savings |
| GPU test plan | `docs/plans/gpu-recommendations-test-plan.md` | Test matrix, E2E playbook |
| Known issues | `docs/known-issues.md` | Missing UI integration, engine gaps |
| Performance analysis | `docs/native-engine-performance.md` | Benchmarks and scale concerns |
| Kruize comparison | `docs/kruize-vs-native-comparison.md` | Detailed memory recommendation differences |
| Namespace boxplots | `docs/phase6-namespace-boxplots-implementation.md` | Implementation notes |
| Phase 4 PR checklist | `docs/plans/phase-4-pr-checklist.md` | Cross-repo merge order |

---

## 5. Feature Implementation Status

| Feature | Status | Key Files |
|---------|--------|-----------|
| Phase 0: 12 robustness bug fixes | ✅ Done | Various (RBAC, handlers, Kafka, utils) |
| Native CPU/memory recommendations | ✅ Done | `engine/recommend_all.go`, `engine/percentile.go` |
| Decay-weighted percentiles | ✅ Done | `engine/decay.go`, `engine/types.go` |
| Idle workload detection | ✅ Done | `engine/detect_idle.go` |
| Notification system (24+ codes) | ✅ Done | `engine/notifications.go`, `notifications/mapping.go` |
| Custom timeframe settings API | ✅ Done | `api/handlers_terms.go` |
| CSV export | ✅ Done | `api/utils.go` |
| Stale recommendation detection | ✅ Done | `model/recommendation_set_native.go` |
| OOM feedback in memory recommendations | ✅ Done | `engine/recommend_all.go` (OOM bump) |
| Recommendation quality tracking | ✅ Done | `engine/quality.go` |
| Recommendation history snapshots | ✅ Done | `engine/history.go` |
| Raw usage samples + boxplots | ✅ Done | `model/boxplot.go`, migration 000031 |
| Retention sweep (background goroutine) | ✅ Done | `engine/retention.go` |
| Strongly-typed DetailResponse | ✅ Done | `model/detail_response.go` |
| Namespace recommendations | ✅ Done | `engine/recommend_namespace.go`, `ingestion/namespace.go` |
| Namespace boxplots + memory percentiles | ✅ Done | `model/boxplot.go`, migration 000033-000034 |
| Replica count (pod_count_min/max/avg) | ✅ Done | `engine/aggregate_pod_counts.go`, migration 000039 |
| CPU/Memory savings estimate | ✅ Done | `engine/savings.go`, `costdata/provider.go` |
| Historical Tracking / Quality API | ✅ Done | `api/handlers_history.go`, `api/handlers_quality.go` |
| History/quality retention policy | ✅ Done | `engine/retention.go` |
| cluster_uuid TEXT→UUID migration | ✅ Done | migration 000041 |
| GPU recommendations engine | ✅ Done | `engine/gpu_recommender.go`, `engine/gpu_metadata.go` |
| GPU digest ingestion pipeline | ✅ Done | `ingestion/pipeline.go` (upsertGPUDigests) |
| GPU API enrichment | ✅ Done | `api/gpu_enrichment.go` |
| GPU API filters | ✅ Done | `api/gpu_enrichment.go` (filterGPUResults) |
| GPU savings estimation | ✅ Done (code) | `engine/gpu_recommender.go` (ApplyGPUSavings) |
| GPU savings E2E verification | ⏳ Pending | Need arm64 image with latest fix deployed |
| Nise GPU data generation | ✅ Done | `nise/generators/ocp/ocp_generator.py` |
| Operator DCGM profiling metrics | ✅ Done | `koku-metrics-operator` branch |
| Operator OOM collection | ✅ Done | `koku-metrics-operator` branch |
| Operator workload_pod_count | ✅ Done | `koku-metrics-operator` branch |
| Koku effective_rates endpoint | ✅ Done | `koku/masu/api/effective_rates.py` |
| OpenAPI spec updates | ✅ Done | `openapi.json` |
| koku-ui GPU display | ❌ Not started | Waiting for Stefan's UX mockups |
| koku-ui replica count display | ❌ Not started | Waiting for Stefan's UX mockups |
| koku-ui savings display | ❌ Not started | Waiting for Stefan's UX mockups |
| Pull requests | ❌ Not created | User explicitly deferred |

---

## 6. Apollo Test Cluster Details

### Cluster Access

| Property | Value |
|----------|-------|
| **Type** | SNO (Single Node OpenShift) |
| **Architecture** | `aarch64` (ARM64) — **all images must use `--platform linux/arm64`** |
| **API URL** | `https://api.sno.karmalabs.corp:6443` |
| **Hypervisor** | `hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com` |
| **kubeadmin password** | `/root/.kcli/clusters/sno/auth/kubeadmin-password` on hypervisor |
| **OpenShift version** | 4.21 (Kubernetes v1.34.6) |
| **Node** | `sno-sno.karmalabs.corp` (192.168.122.55) |

### Network Access

sshuttle is required:
```bash
sshuttle -r root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com 192.168.122.0/24 172.30.0.0/16 10.128.0.0/14
```

### Keycloak (JWT Authentication)

| Property | Value |
|----------|-------|
| **Namespace** | `keycloak` |
| **Admin** | `temp-admin` |
| **Realm** | `kubernetes` |
| **Client** | `cost-management-operator` |
| **JWT org_id** | `org1234567` |

### Image Registry

```
default-route-openshift-image-registry.apps.sno.karmalabs.corp/cost-onprem/ros-ocp-backend:gpu-latest
```

ROS deployments currently use tag `:gpu`.

---

## 7. Current E2E State on Apollo

### Verified Working

- GPU recommendations: 3 containers (v100-legacy/V100, inference-server/T4, training-job/A100) with correct classification
- GPU API filters: `has_gpu`, `gpu_model`, `gpu_classification` all verified
- Koku effective_rates: Returns `gpu_cost_per_month: $2500`
- ROS DB at migration 43, Koku Django at migration 0347

### Not Yet Verified (Latest Code)

The latest commit fixes `ApplyGPUSavings` to return `$0.00` (not nil) for well-utilized GPUs when cost data is available. This commit is NOT yet deployed. Needs arm64 build + push + restart.

### Test Data on Apollo

- **Cluster ID:** `d4e5f6a7-b8c9-0123-defa-444444444444`
- **Provider UUID:** `d665a309-ccbf-4510-bcdb-59db1f7e0da7` ("GPU Test OCP Cluster")
- **GPU digest rows:** 12 (3 containers × 4 days)

---

## 8. Immediate Pending Work

1. **Build arm64 image** with latest commit, push to Apollo, verify GPU savings shows `$0.00` for well-utilized
2. **koku-ui changes** — blocked on Stefan's UX mockups
3. **Pull requests** — user deferred, no PRs created yet
4. **Java/JVM recommendations** — Kruize has these; not planned for native engine yet

---

## 9. GPU Recommendations — Technical Details

### Classification Logic (`engine/gpu_recommender.go`)

Two-tier model:

**Tier 1 (Turing+: T4, A10, A30, A100, L4, L40, L40S, H100, H200, B100, B200):**
- Uses DCGM profiling metrics (PROF_SM_ACTIVE, PROF_PIPE_TENSOR_ACTIVE, PROF_DRAM_ACTIVE)
- Thresholds: `idle` (<5% SM), `underutilized` (<30%), `memory_bound` (DRAM > 2× SM), `well_utilized`

**Tier 2 (Pre-Turing: P40, P100, V100):**
- Only frame buffer usage (no profiling metrics)
- Returns notification code 28 (`NotifGPUNoProfilingData`)

### GPU Metadata (`engine/gpu_metadata.go`)

`GPUModels` map with specs for all NVIDIA GPUs: model matching, architecture tier, MIG profiles, FB capacity.

### GPU Digest Pipeline

1. `ingestion/pipeline.go:upsertGPUDigests()` — Extracts GPU rows from MetricRow, aggregates by day, writes to `gpu_container_digests`
2. `engine/gpu_query.go:QueryGPURecommendations()` — Reads digests, calls `RecommendGPU()` per container
3. `api/gpu_enrichment.go:enrichWithGPU()` — Attaches GPU recs to NativeContainerResult, fetches cost rates, applies savings

### DCGM Metrics (After Operator Changes)

| CSV Column | DCGM Metric | Tier |
|-----------|-------------|------|
| `gpu_model` | Device name | Both |
| `gpu_fb_usage_*` | `DCGM_FI_DEV_FB_USED` | Both |
| `gpu_tensor_pipe_active_*` | `DCGM_FI_PROF_PIPE_TENSOR_ACTIVE` | Tier 1 |
| `gpu_dram_active_*` | `DCGM_FI_PROF_DRAM_ACTIVE` | Tier 1 |
| `gpu_sm_active_*` | `DCGM_FI_PROF_SM_ACTIVE` | Tier 1 |

**Removed:** `DCGM_FI_DEV_GPU_UTIL` and `DCGM_FI_DEV_MEM_COPY_UTIL` (misleading).

---

## 10. Cost Impact / Savings Estimation — Technical Details

### CPU/Memory Savings (`engine/savings.go`)

```
savings = (current_request - recommended_request) × rate × monthly_pod_hours
```

### GPU Savings (`engine/gpu_recommender.go:ApplyGPUSavings`)

- **idle GPU:** Full `gpu_cost_per_month` rate
- **MIG right-sizing:** `(1 - recommended_slices/total_slices) × rate`
- **well_utilized:** `$0.00` (not nil — we know there's no waste)
- **No cost data:** `nil` (unknown)

### Koku effective_rates Endpoint

`GET /api/cost-management/v1/effective_rates/?org_id=1234567&cluster_id=<UUID>&start_date=...&end_date=...`

**CRITICAL:** `org_id` must be **numeric** (no "org" prefix). Code strips it: `strings.TrimPrefix(orgID, "org")`.

---

## 11. Nise GPU Data Generation

File: `nise/nise/generators/ocp/ocp_generator.py`

14 new GPU columns in `OCP_ROS_USAGE_COLUMN`. New functions: `_enrich_ros_data_with_gpus()`, `_gen_ros_gpu_metrics()`.

**YAML GOTCHA:** `node:`, `pod:`, `gpu:` keys are labels with `None` values. Their children (`node_name`, `pod_name`, `gpu_model`) must be **siblings** at the same indentation, NOT nested.

---

## 12. Koku Backend Changes

- `koku/masu/api/effective_rates.py` — Returns cost model rates + namespace aggregates
- Includes all rate types including `gpu_cost_per_month`
- Returns `distribution_type`, `markup_pct`, per-namespace cost aggregates

---

## 13. Operator Changes

- **DCGM metric replacement:** DEV_ → PROF_ profiling metrics (Turing+ GPU requirement)
- **workload_pod_count:** PromQL query using `kube_pod_container_status_ready`
- **oom_count:** PromQL query using `kube_pod_container_status_last_terminated_reason{reason="OOMKilled"}`

---

## 14. Database Schema State

### ROS Database (`costonprem_ros`, user `ros_user`)

**Migration version:** 43

Key tables: `recommendation_sets`, `daily_container_digests`, `gpu_container_digests`, `namespace_recommendation_sets`, `daily_namespace_digests`, `recommendation_history`, `recommendation_quality`, `container_usage_samples`, `namespace_usage_samples`, `clusters` (UUID type), `rh_accounts`, `notification_code_definitions`, `org_recommendation_terms`, `recommendation_profiles`, `workloads`, `workload_metrics`

### Koku Database (`costonprem_koku`, user `koku_user`)

**Django migration:** reporting 0347

---

## 15. Key Bugs Found and Fixed

| Bug | Root Cause | Fix | Commit |
|-----|-----------|-----|--------|
| org_id prefix mismatch with effective_rates | ROS passes `org1234567`, Koku expects `1234567` | `strings.TrimPrefix(orgID, "org")` | `4f3d96a` |
| ApplyGPUSavings returns nil for well_utilized | `savings > 0` check excluded $0 case | Always set savings when cost data available | `c53f539` |
| GPU data parsed but not persisted | Pipeline only wrote CPU/memory digests | Added `upsertGPUDigests()` in pipeline.go | `50d953c` |
| Import cycle (model↔engine) | gpu_enrichment.go in model package | Moved to `internal/api/` | `50d953c` |
| gpu_container_digests ON CONFLICT without unique index | PostgreSQL requires unique index | Migration 000043 | `50d953c` |
| Koku listener "Migrations not done" | Stale Django migrations in DB | Run `migrate_schemas` from inside pod | — |
| Koku listener "Received unexpected OCP report" | cluster_id mismatch | Register provider with correct cluster_id | — |
| Koku listener "No ROS reports" | Tarball `./` prefix mismatch with manifest | Repackage without `./` prefix | — |
| Nise GPU data empty | YAML indentation error | Fixed sibling key indentation | — |
| arm64 image → Exec format error | Built for x86_64, cluster is aarch64 | `--platform linux/arm64` | — |

---

## 16. E2E Testing Playbook

See `docs/plans/gpu-recommendations-test-plan.md` for the complete GPU test plan. Quick reference:

### Generate GPU Data
```bash
cd ~/dev/koku/nise
.venv/bin/nise report ocp --ros-ocp-info --static-report-file /tmp/gpu_static_data.yml \
  --ocp-cluster-id d4e5f6a7-b8c9-0123-defa-444444444444 \
  --insights-upload /tmp/nise_gpu_output --write-monthly
```

### Verify API
```bash
IDENTITY=$(echo -n '{"identity":{"account_number":"7890123","org_id":"org1234567","type":"User","user":{"username":"operator-svc","email":"op@test.com","is_org_admin":true,"access":{}}},"entitlements":{"cost_management":{"is_entitled":true}}}' | base64 -w0)
curl -s -H "x-rh-identity: $IDENTITY" "http://localhost:8899/api/cost-management/v1/recommendations/openshift?has_gpu=true"
```

---

## 17. Known Issues and Gaps

- **gpu_distributed cost type**: May not be implemented on-prem. See `docs/known-issues.md`.
- **No idle GPU in test data**: Current nise data only generates well_utilized and no-profiling containers.
- **MIG recommendations not testable E2E**: Requires actual MIG-enabled workloads.
- **koku-ui not updated**: All new features (GPU, replicas, savings, namespace) lack UI.
- **No PRs created**: User explicitly deferred pull request creation.
- **Java/JVM recommendations**: Kruize has these; not planned for native engine.

See `docs/known-issues.md` for full list of engine-implemented but UI-missing features.

---

## 18. Development Environment Notes

### Unit Tests
```bash
cd ~/dev/koku/ros-ocp-backend
go test ./internal/engine/ -run "GPU|Gpu|Savings|Mig|Recommend|OOM|Quality" -v
go test ./internal/ingestion/ -run "GPU|HasGPU|Digest|CSV" -v
go test ./internal/api/ -run "GPU|Filter|History|Quality|Terms" -v
go test ./... -timeout 300s  # all tests
```

### arm64 Image Build (10+ minutes due to QEMU)
```bash
podman build --platform linux/arm64 -t ros-ocp-backend:gpu-latest .
```

---

## 19. What NOT to Do

1. **Never build x86_64 images for Apollo** — it's aarch64
2. **Never pass `org_id` with "org" prefix to Koku effective_rates**
3. **Never use `./` prefix in tarball filenames** for Koku ingress
4. **Never use `required_permissions: ["all"]` in Cursor agent commands** — causes invisible approval hangs
5. **Never nest YAML keys under `node:`, `pod:`, `gpu:` in nise static data**
6. **Never query `recommendation_sets.cluster_id`** — it's `cluster_uuid` (UUID type)
7. **Never assume GPU savings `nil` means $0** — nil = unknown, $0 = no savings
8. **Never skip `redis-cli FLUSHALL`** after Koku code changes

---

## Appendix: Key File Index

### ros-ocp-backend — Engine
| File | Purpose |
|------|---------|
| `engine/recommend_all.go` | CPU/memory recommendation orchestrator |
| `engine/recommend_namespace.go` | Namespace-level recommendations |
| `engine/gpu_recommender.go` | GPU classification + ApplyGPUSavings |
| `engine/gpu_metadata.go` | GPU model specs + MIG profiles |
| `engine/gpu_query.go` | Query gpu_container_digests |
| `engine/savings.go` | CPU/memory savings calculation |
| `engine/quality.go` | Recommendation quality tracking |
| `engine/history.go` | Recommendation history snapshots |
| `engine/retention.go` | Partition retention sweep |
| `engine/notifications.go` | Notification code evaluation |

### ros-ocp-backend — Ingestion
| File | Purpose |
|------|---------|
| `ingestion/pipeline.go` | CSV → digests + GPU digests + samples |
| `ingestion/csvparser.go` | CSV parsing with GPU columns |
| `ingestion/digest.go` | Daily digest computation |
| `ingestion/namespace.go` | Namespace digest processing |

### ros-ocp-backend — API
| File | Purpose |
|------|---------|
| `api/handlers.go` | Main recommendation handlers |
| `api/handlers_history.go` | History API |
| `api/handlers_quality.go` | Quality API |
| `api/handlers_terms.go` | Settings/terms API |
| `api/gpu_enrichment.go` | GPU enrichment + filters + savings |
| `model/detail_response.go` | Kruize-compatible response struct |
| `model/boxplot.go` | Boxplot assembly |
| `model/recommendation_set_native.go` | Native SQL queries |
| `costdata/provider.go` | Koku effective_rates client |

### Migrations (000022-000043)
22-27: Core schema. 31: container_usage_samples. 32-36: Namespace columns. 37: Container memory percentiles. 38: Variation/limit columns. 39: Pod count. 40: No-cost-data notification. 41: cluster_uuid UUID. 42-43: GPU container digests.
