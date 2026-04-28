# Agent Memory Dump — ROS OCP Native Engine Development

**Date:** 2026-04-29  
**Purpose:** Complete context for a new AI agent to resume work on the ros-ocp-backend native recommendation engine and related ecosystem changes.  
**Read this entire file before doing anything.**

---

## Table of Contents

1. [Project Goal](#1-project-goal)
2. [Repository Map and Branch State](#2-repository-map-and-branch-state)
3. [Architecture Overview](#3-architecture-overview)
4. [Feature Implementation Status](#4-feature-implementation-status)
5. [Apollo Test Cluster Details](#5-apollo-test-cluster-details)
6. [Current E2E State on Apollo](#6-current-e2e-state-on-apollo)
7. [Immediate Pending Work](#7-immediate-pending-work)
8. [GPU Recommendations — Complete Technical Details](#8-gpu-recommendations--complete-technical-details)
9. [Cost Impact / Savings Estimation — Complete Technical Details](#9-cost-impact--savings-estimation--complete-technical-details)
10. [Historical Tracking / Quality API](#10-historical-tracking--quality-api)
11. [Replica Count Feature](#11-replica-count-feature)
12. [Namespace Recommendations](#12-namespace-recommendations)
13. [Nise GPU Data Generation](#13-nise-gpu-data-generation)
14. [Koku Backend Changes](#14-koku-backend-changes)
15. [Operator Changes](#15-operator-changes)
16. [Database Schema State](#16-database-schema-state)
17. [Key Bugs Found and Fixed](#17-key-bugs-found-and-fixed)
18. [E2E Testing Playbook](#18-e2e-testing-playbook)
19. [Known Issues and Gaps](#19-known-issues-and-gaps)
20. [Development Environment Notes](#20-development-environment-notes)
21. [What NOT to Do](#21-what-not-to-do)

---

## 1. Project Goal

Replace the Kruize (Java) recommendation engine in ros-ocp-backend with a **native Go engine** that:

- Computes CPU, memory, and GPU recommendations directly from daily digest data
- Provides cost impact / savings estimates by querying Koku's cost model rates
- Supports replica count awareness (pod_count_min/max/avg)
- Offers historical tracking via recommendation_history and recommendation_quality tables
- Includes namespace-level recommendations with boxplot visualizations
- Works both on-prem (PostgreSQL-only) and SaaS (with Trino)
- Is at least as good as competitors (KubeCost, Utilyze) for GPU recommendations

The native engine is being developed on the `pgarciaq-rosocp-superpowers-phase6` branch and has **86 commits ahead of main**.

---

## 2. Repository Map and Branch State

### ros-ocp-backend (PRIMARY — most changes here)
- **Path:** `/home/pgarciaq/dev/koku/ros-ocp-backend/`
- **Branch:** `pgarciaq-rosocp-superpowers-phase6` (86 commits ahead of `main`)
- **Remote:** `pgarciaq` → `git@github.com:pgarciaq/ros-ocp-backend.git`
- **Origin:** `https://github.com/RedHatInsights/ros-ocp-backend.git`
- **Phase branches exist for:** phase0 through phase6 (current)
- **Latest commit:** `c53f539 Fix ApplyGPUSavings to return $0 instead of nil for well-utilized GPUs`

### koku (backend — effective_rates endpoint + AGENTS.md)
- **Path:** `/home/pgarciaq/dev/koku/koku/`
- **Branch:** `pgarciaq-rosocp-superpowers`
- **Remote:** `pgarciaq` → `git@github.com:pgarciaq/koku.git`
- **Our commits (2 new):**
  - `6fe59f939` Include all distributed cost types in effective-rates endpoint
  - `d10909686` Add effective-rates masu endpoint for ROS savings estimates
- **Note:** This branch also includes upstream commits that were already merged to main by other developers (price lists, GPU finalization, MIG, etc.)

### koku-metrics-operator (operator — DCGM metric changes)
- **Path:** `/home/pgarciaq/dev/koku/koku-metrics-operator/`
- **Branch:** `pgarciaq-rosocp-superpowers-phase4`
- **Remote:** `pgarciaq` → `git@github.com:pgarciaq/koku-metrics-operator.git`
- **Our commits (4 new):**
  - `924de0f4` Replace misleading DCGM DEV_ metrics with PROF_ profiling metrics
  - `1001a815` Add workload_pod_count PromQL query and CSV column for ROS containers
  - `603b37b0` COST-5691: Add unit tests for OOM count in ROS container CSV
  - `3383cf6c` Add OOM count PromQL query and CSV column for ROS containers

### nise (test data generator — GPU profiling metrics)
- **Path:** `/home/pgarciaq/dev/koku/nise/`
- **Branch:** `pgarciaq-rosocp-superpowers-phase4`
- **Remote:** `pgarciaq` → `https://github.com/pgarciaq/nise.git`
- **Our commits (7 new, 3 GPU-relevant):**
  - `1057bbe` Add GPU profiling metrics to ROS container CSV generation
  - `d67a6e1` Add workload_pod_count column to ROS container CSV generation
  - `c92f444` Add oom_count column to ROS container CSV generation

### koku-ui (frontend — NO changes yet)
- **Path:** `/home/pgarciaq/dev/koku/koku-ui/`
- **Branch:** `main`
- **Status:** Waiting for UX designer (Stefan) to provide mockups before implementing GPU UI

---

## 3. Architecture Overview

### Data Flow for GPU Recommendations

```
1. Operator queries Prometheus for DCGM_FI_PROF_* metrics (every hour)
   ↓
2. Operator writes CSV rows with GPU columns to tar.gz
   ↓
3. tar.gz uploaded to Koku ingress (MinIO/S3)
   ↓
4. Koku listener picks up the payload, sends ROS-tagged files to Kafka (hccm.ros.events)
   ↓
5. ROS processor (ros-ocp-backend) consumes Kafka messages
   ↓
6. CSV parsed by internal/ingestion/csvparser.go → MetricRow structs
   ↓
7. MetricRow.HasGPU() identifies rows with GPU data
   ↓
8. internal/ingestion/pipeline.go:
   a. Groups rows → daily_container_digests (CPU/memory) via upsertDigests
   b. Groups GPU rows → gpu_container_digests via upsertGPUDigests
   ↓
9. API request arrives at internal/api/handlers.go
   ↓
10. Fetches CPU/memory recommendations from recommendation_sets
    ↓
11. internal/api/gpu_enrichment.go:enrichWithGPU() called:
    a. Queries gpu_container_digests via engine.QueryGPURecommendations()
    b. engine.RecommendGPU() classifies each GPU workload
    c. Fetches cost rates from Koku effective_rates endpoint
    d. engine.ApplyGPUSavings() computes savings estimate
    e. Attaches GPURecommendation to NativeContainerResult
    ↓
12. GPU filters applied post-enrichment (has_gpu, gpu_model, gpu_classification)
    ↓
13. JSON response returned to client
```

### Key Packages in ros-ocp-backend

| Package | Purpose |
|---------|---------|
| `internal/ingestion/` | CSV parsing, digest computation, GPU digest upsert |
| `internal/engine/` | Recommendation algorithms, GPU classification, savings, quality, retention |
| `internal/api/` | Echo HTTP handlers, GPU enrichment, filters, history/quality endpoints |
| `internal/model/` | GORM models, native query functions, detail response shaping |
| `internal/costdata/` | Interface for fetching Koku effective rates |
| `internal/config/` | Viper-based config with env var mapping |
| `internal/testutil/` | Test fixtures (SeedDigest, SeedGPUDigest, SeedRecommendationSet, etc.) |
| `internal/services/` | Report processor (Kafka consumer → pipeline) |

---

## 4. Feature Implementation Status

| Feature | Status | Key Files |
|---------|--------|-----------|
| Native CPU/memory recommendations | ✅ Done | `engine/recommend_all.go`, `engine/percentile.go` |
| OOM feedback in recommendations | ✅ Done | `engine/recommend_all.go` (OOM bump logic) |
| Replica count (pod_count_min/max/avg) | ✅ Done | `engine/aggregate_pod_counts.go`, migration 000039 |
| Cost impact / savings estimate (CPU/mem) | ✅ Done | `engine/savings.go`, `costdata/provider.go` |
| Custom timeframe settings API | ✅ Done | `api/handlers_terms.go`, `model/recommendation_set_native.go` |
| Historical tracking / quality API | ✅ Done | `api/handlers_history.go`, `api/handlers_quality.go` |
| History/quality retention policy | ✅ Done | `engine/retention.go` |
| Namespace recommendations | ✅ Done | `engine/recommend_namespace.go`, `ingestion/namespace.go` |
| Namespace boxplots | ✅ Done | `model/boxplot.go` |
| GPU recommendations engine | ✅ Done | `engine/gpu_recommender.go`, `engine/gpu_metadata.go` |
| GPU digest ingestion pipeline | ✅ Done | `ingestion/pipeline.go` (upsertGPUDigests) |
| GPU API enrichment | ✅ Done | `api/gpu_enrichment.go` |
| GPU API filters | ✅ Done | `api/gpu_enrichment.go` (filterGPUResults) |
| GPU savings estimation | ✅ Done (code) | `engine/gpu_recommender.go` (ApplyGPUSavings) |
| GPU savings E2E verification | ⏳ Pending | Need to rebuild arm64 image with latest fix |
| Nise GPU data generation | ✅ Done | `nise/generators/ocp/ocp_generator.py` |
| Operator DCGM profiling metrics | ✅ Done | `koku-metrics-operator` branch |
| Koku effective_rates endpoint | ✅ Done | `koku/masu/api/effective_rates.py` |
| koku-ui GPU display | ❌ Not started | Waiting for Stefan's UX mockups |

---

## 5. Apollo Test Cluster Details

### Cluster Access

| Property | Value |
|----------|-------|
| **Type** | SNO (Single Node OpenShift) |
| **Architecture** | `aarch64` (ARM64) — **CRITICAL: all images must be built with `--platform linux/arm64`** |
| **API URL** | `https://api.sno.karmalabs.corp:6443` |
| **Hypervisor** | `hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com` |
| **kubeadmin password location** | `/root/.kcli/clusters/sno/auth/kubeadmin-password` on hypervisor |
| **OpenShift version** | 4.21 (Kubernetes v1.34.6) |
| **OS** | RHCOS 9.6 |
| **Node** | `sno-sno.karmalabs.corp` (192.168.122.55) |

### Network Access

**sshuttle is required** to access the cluster from the developer workstation. The tunnel routes traffic to the cluster network only (not the hypervisor).

```bash
# sshuttle tunnel (must be running in a separate terminal)
sshuttle -r root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com 192.168.122.0/24 172.30.0.0/16 10.128.0.0/14
```

To get kubeadmin password:
```bash
ssh root@hpe-apollo-cn99xx-16.khw.eng.rdu2.dc.redhat.com cat /root/.kcli/clusters/sno/auth/kubeadmin-password
```

To login:
```bash
oc login -s https://api.sno.karmalabs.corp:6443 -u kubeadmin --password=<PASSWORD>
```

### Namespace: `cost-onprem`

All cost management services run in this namespace. Key deployments:
- `cost-onprem-ros-api` — ROS API (our main service)
- `cost-onprem-ros-processor` — ROS Kafka consumer
- `cost-onprem-ros-housekeeper` — ROS cleanup
- `cost-onprem-ros-rec-poller` — ROS recommendation polling
- `cost-onprem-koku-api` — Koku API
- `cost-onprem-koku-masu` — Koku Masu (internal API with effective_rates)
- `cost-onprem-koku-listener` — Koku Kafka listener
- `cost-onprem-database-0` — PostgreSQL 16 (shared: `costonprem_koku` + `costonprem_ros`)
- `cost-onprem-ingress-*` — Insights ingress (file upload endpoint)
- `cost-onprem-kruize-*` — Kruize (legacy, still deployed but not used by native engine)

### Keycloak (JWT Authentication)

| Property | Value |
|----------|-------|
| **Namespace** | `keycloak` |
| **Admin username** | `temp-admin` |
| **Realm** | `kubernetes` |
| **Client ID** | `cost-management-operator` |
| **Route** | `keycloak-keycloak.apps.sno.karmalabs.corp` |

To get a JWT token:
```bash
# Get client secret
CLIENT_SECRET=$(oc get secret -n keycloak cost-management-client-secret -o jsonpath='{.data.client-secret}' | base64 -d)

# Get token
TOKEN=$(curl -sk "https://keycloak-keycloak.apps.sno.karmalabs.corp/realms/kubernetes/protocol/openid-connect/token" \
  -d "grant_type=client_credentials&client_id=cost-management-operator&client_secret=$CLIENT_SECRET" | python3 -c "import json,sys; print(json.load(sys.stdin)['access_token'])")
```

**IMPORTANT:** The JWT maps to `org_id: org1234567`. The ROS API reads `org_id` from the `x-rh-identity` header, NOT from the JWT directly. For API testing, use the `x-rh-identity` header:

```bash
IDENTITY=$(echo -n '{"identity":{"account_number":"7890123","org_id":"org1234567","type":"User","user":{"username":"operator-svc","email":"op@test.com","is_org_admin":true,"access":{}}},"entitlements":{"cost_management":{"is_entitled":true}}}' | base64 -w0)

curl -s -H "x-rh-identity: $IDENTITY" "http://localhost:<PORT>/api/cost-management/v1/recommendations/openshift"
```

### Image Registry

The cluster uses the internal OpenShift image registry:
```
default-route-openshift-image-registry.apps.sno.karmalabs.corp/cost-onprem/ros-ocp-backend:gpu-latest
```

To push images:
```bash
podman login default-route-openshift-image-registry.apps.sno.karmalabs.corp -u kubeadmin -p $(oc whoami -t)
podman build --platform linux/arm64 -t ros-ocp-backend:gpu-latest .
podman tag ros-ocp-backend:gpu-latest default-route-openshift-image-registry.apps.sno.karmalabs.corp/cost-onprem/ros-ocp-backend:gpu-latest
podman push default-route-openshift-image-registry.apps.sno.karmalabs.corp/cost-onprem/ros-ocp-backend:gpu-latest
```

The ROS deployments currently use the tag `:gpu` (not `:gpu-latest`). After pushing, update image references if needed:
```bash
oc set image deployment/cost-onprem-ros-api -n cost-onprem ros-api=image-registry.openshift-image-registry.svc:5000/cost-onprem/ros-ocp-backend:gpu-latest
```

---

## 6. Current E2E State on Apollo

### What's Deployed and Verified

| Component | Status | Details |
|-----------|--------|---------|
| ROS API with GPU enrichment | ✅ Running | Image tag `:gpu`, commit `4f3d96a` (missing latest savings fix) |
| GPU digest data | ✅ 12 rows | 3 containers × 4 days: v100-legacy (V100), inference-server (T4), training-job (A100) |
| GPU recommendations in API | ✅ Working | Model, classification, confidence all correct |
| GPU API filters | ✅ Verified | `has_gpu=true`, `gpu_model=T4`, `gpu_classification=well_utilized` all work |
| `KOKU_MASU_URL` env var | ✅ Set | `http://cost-onprem-koku-masu:8000` |
| Koku effective_rates endpoint | ✅ Working | Returns `gpu_cost_per_month: 2500.0` for cluster `d4e5f6a7-...` |
| GPU savings in API | ❌ Shows `null` | Two issues: (1) `org_id` prefix bug (fixed in `4f3d96a`), (2) `$0` vs `null` bug (fixed in `c53f539`) |
| ROS DB migration version | ✅ 43 | All migrations applied |
| Koku Django migration version | ✅ 0347 | All migrations applied |

### What Needs Verification

The latest commit `c53f539` fixes both remaining issues but is **NOT yet deployed** to Apollo. The arm64 image build is very slow (~10+ min) due to QEMU emulation. After building and deploying, verify:

1. `well_utilized` containers show `savings_usd: 0.0` (not `null`)
2. `idle` containers show `savings_usd: 2500.0` (full GPU rate)
3. Containers with no cost data available show `savings_usd: null`

### Test Data on Apollo

The GPU test data was generated with nise and uploaded as a tar.gz to the ingress endpoint. Key details:

- **Cluster ID:** `d4e5f6a7-b8c9-0123-defa-444444444444`
- **Provider UUID:** `d665a309-ccbf-4510-bcdb-59db1f7e0da7` (registered as "GPU Test OCP Cluster")
- **Org ID:** `org1234567`
- **Containers with GPU data:**
  - `inference-server` — Tesla T4 (Tier 1, profiling data → `well_utilized`)
  - `training-job` — A100 (Tier 1, profiling data → `well_utilized`)
  - `v100-legacy` — V100 (Tier 2, no profiling data → classification empty, notification 28)

---

## 7. Immediate Pending Work

### Priority 1: Finish GPU Savings E2E Verification

1. Build arm64 image with commit `c53f539` (the `$0` vs `null` fix)
2. Push to Apollo registry
3. Restart ROS API deployment
4. Verify savings values in API responses:
   - `well_utilized` → `estimated_monthly_gpu_savings_usd: 0.0`
   - V100 legacy (no classification) → `estimated_monthly_gpu_savings_usd: 0.0`
   - If we add idle GPU test data → `estimated_monthly_gpu_savings_usd: 2500.0`

**The build must use `--platform linux/arm64` because Apollo is aarch64!**

### Priority 2: Items Still Pending per User's Last Request

| Item | Status | Notes |
|------|--------|-------|
| 1. Commit and push everything | ✅ Done | All committed and pushed |
| 2. Add unit tests | ✅ Done | Tests for all GPU pipeline functions |
| 3. Integration tests with GPU data | ✅ Done | `TestGetNativeRecommendationSetList_GPUEnrichment` |
| 4. Wire API filters | ✅ Done | `has_gpu`, `gpu_model`, `gpu_classification` |
| 5. Replace savings placeholder | ✅ Done (code) | Uses Koku effective_rates; needs E2E verify |
| 6. Update plans | ✅ Done | Both plan docs updated |

### Priority 3: Future Features (Not Started)

1. **koku-ui changes for GPU display** — Waiting for Stefan's UX mockups
2. **Java/JVM recommendations** — Kruize has these; not yet planned for native engine
3. **Pull request creation** — User explicitly said "we are not going to create pull requests just yet"

---

## 8. GPU Recommendations — Complete Technical Details

### Design Documents

- **Plan:** `docs/plans/gpu-recommendations.md`
- **Test plan:** `docs/plans/gpu-recommendations-test-plan.md`

### GPU Classification Logic (`engine/gpu_recommender.go`)

The engine uses a two-tier model:

**Tier 1 (Turing+ GPUs: T4, A10, A30, A100, L4, L40, L40S, H100, H200, B100, B200):**
- Have DCGM profiling metrics (PROF_SM_ACTIVE, PROF_PIPE_TENSOR_ACTIVE, PROF_DRAM_ACTIVE)
- Classification based on SM activity, tensor pipe activity, DRAM activity
- Thresholds: `idle` (<5% SM), `underutilized` (<30% SM), `memory_bound` (DRAM > 2× SM), `compute_bound_underutil`, `well_utilized`

**Tier 2 (Pre-Turing: P40, P100, V100):**
- Only frame buffer usage available (no profiling metrics)
- Cannot classify utilization, only detect idle (FB usage < threshold)
- Returns notification code 28 (`NotifGPUNoProfilingData`)

### GPU Metadata (`engine/gpu_metadata.go`)

Contains `GPUModels` map with specs for all supported NVIDIA GPUs:
- Model name matching (case-insensitive, substring)
- Architecture tier (1 or 2)
- MIG profiles (for A100, A30, H100, H200, B100, B200)
- Frame buffer capacity in MiB

### GPU Digest Table (`gpu_container_digests`)

Created by migration 000042, unique constraint by migration 000043:
```sql
CREATE TABLE gpu_container_digests (
    id BIGSERIAL,
    cluster_uuid UUID NOT NULL,
    org_id TEXT NOT NULL,
    namespace TEXT NOT NULL,
    workload TEXT NOT NULL,
    container_name TEXT NOT NULL,
    interval_start TIMESTAMPTZ NOT NULL,
    gpu_model_name TEXT NOT NULL,
    gpu_profile_name TEXT NOT NULL DEFAULT '',
    fb_usage_min_mib REAL, fb_usage_max_mib REAL, fb_usage_avg_mib REAL,
    tensor_pipe_active_min REAL, tensor_pipe_active_max REAL, tensor_pipe_active_avg REAL,
    dram_active_min REAL, dram_active_max REAL, dram_active_avg REAL,
    sm_active_min REAL, sm_active_max REAL, sm_active_avg REAL,
    PRIMARY KEY (id, interval_start)
) PARTITION BY RANGE (interval_start);
```

### Notification Codes for GPU

| Code | Constant | Meaning |
|------|----------|---------|
| 26 | `NotifGPUIdle` | GPU is idle (<5% SM active) |
| 27 | `NotifGPUMemBound` | Memory-bound workload |
| 28 | `NotifGPUNoProfilingData` | Pre-Turing GPU, no profiling metrics |
| 29 | `NotifGPUUnderutilized` | GPU underutilized (5-30% SM) |

### DCGM Metrics (Operator → CSV)

After our operator changes, the CSV columns for GPU data are:

| Column | DCGM Metric | Tier |
|--------|-------------|------|
| `gpu_model` | Device name | Both |
| `gpu_uuid` | Device UUID | Both |
| `gpu_mig_profile` | MIG profile name | MIG only |
| `gpu_fb_usage_min/max/avg` | `DCGM_FI_DEV_FB_USED` | Both |
| `gpu_tensor_pipe_active_min/max/avg` | `DCGM_FI_PROF_PIPE_TENSOR_ACTIVE` | Tier 1 only |
| `gpu_dram_active_min/max/avg` | `DCGM_FI_PROF_DRAM_ACTIVE` | Tier 1 only |
| `gpu_sm_active_min/max/avg` | `DCGM_FI_PROF_SM_ACTIVE` | Tier 1 only |

**Removed metrics (previously collected but misleading):**
- `DCGM_FI_DEV_GPU_UTIL` — Kernel time %, not compute utilization
- `DCGM_FI_DEV_MEM_COPY_UTIL` — Memory controller busy %, not capacity

---

## 9. Cost Impact / Savings Estimation — Complete Technical Details

### CPU/Memory Savings (`engine/savings.go`)

Computes `estimated_monthly_savings_usd` for each container:
1. Fetches effective rates from Koku via `costdata.CostDataProvider`
2. Calculates current CPU/memory cost based on current usage × rate
3. Calculates recommended CPU/memory cost based on recommended values × rate
4. Savings = current - recommended (clamped to ≥ 0)

### GPU Savings (`engine/gpu_recommender.go:ApplyGPUSavings`)

- **idle GPU:** Savings = full `gpu_cost_per_month` rate (could remove the GPU)
- **MIG right-sizing:** Savings = `(1 - recommended_slices/total_slices) × rate`
- **well_utilized / no savings:** Savings = `$0.00` (not nil — we know the cost)
- **No cost data available:** Savings = `nil` (unknown)

### Koku effective_rates Endpoint

**URL:** `GET /api/cost-management/v1/effective_rates/`  
**Parameters:** `org_id` (numeric, e.g., `1234567`), `cluster_id` (UUID), `start_date`, `end_date`  
**Returns:**
```json
{
    "cluster_id": "d4e5f6a7-...",
    "provider_uuid": "d665a309-...",
    "distribution_type": "cpu",
    "markup_pct": 15.0,
    "configured_rates": {
        "cpu_core_usage_per_hour": {"infrastructure": 0.0, "supplementary": 0.015},
        "gpu_cost_per_month": {"infrastructure": 2500.0, "supplementary": 0.0},
        ...
    },
    "namespace_aggregates": {}
}
```

**CRITICAL:** The `org_id` parameter must be **numeric** (e.g., `1234567`), NOT prefixed with "org". The Koku endpoint internally prepends "org" to form the schema name. The ros-ocp-backend code strips the prefix in `gpu_enrichment.go:enrichWithGPU()`:
```go
kokuOrgID := strings.TrimPrefix(orgID, "org")
```

**Configuration:** Set `KOKU_MASU_URL` env var on the ROS API deployment (e.g., `http://cost-onprem-koku-masu:8000`).

---

## 10. Historical Tracking / Quality API

### Endpoints

| Endpoint | Method | Purpose |
|----------|--------|---------|
| `/api/cost-management/v1/recommendations/openshift/history` | GET | Historical recommendation snapshots |
| `/api/cost-management/v1/recommendations/openshift/quality` | GET | Recommendation quality metrics (accuracy, stability) |

### Tables (both partitioned by `recorded_at`)

- `recommendation_history` — Snapshots of recommendations over time
- `recommendation_quality` — Quality metrics (MAE, stability score, drift)

### Retention

Separate retention policy in `engine/retention.go`. Configurable via `HISTORY_RETENTION_DAYS` (default: 90) and `QUALITY_RETENTION_DAYS` (default: 90). Independent from the general Koku data retention period.

---

## 11. Replica Count Feature

### What it Does

Adds `pod_count_min`, `pod_count_max`, `pod_count_avg` to recommendations. Computed from the `workload_pod_count` column in the daily digest, which comes from the operator's `kube_pod_container_status_ready` PromQL query.

### Key Files
- `engine/aggregate_pod_counts.go` — Aggregation logic
- `migration 000039` — Adds columns to recommendation_sets and namespace_recommendation_sets
- Operator: `koku-metrics-operator` commit `1001a815`
- Nise: `nise` commit `d67a6e1`

---

## 12. Namespace Recommendations

### What it Does

Aggregates container-level recommendations to the namespace level. Includes:
- CPU/memory request/limit recommendations summed across containers
- Boxplot visualizations (P25, P50, P75 + whiskers)
- Memory trend slope notifications

### Key Files
- `engine/recommend_namespace.go` — Recommendation logic
- `ingestion/namespace.go` — Namespace digest processing
- `model/namespace_recommendation_set_native.go` — Native query
- `model/boxplot.go` — Boxplot computation
- Migrations 000032-000036

---

## 13. Nise GPU Data Generation

### Changes Made

File: `nise/nise/generators/ocp/ocp_generator.py`

Added 14 GPU-related columns to `OCP_ROS_USAGE_COLUMN`:
```
gpu_model, gpu_uuid, gpu_mig_profile,
gpu_fb_usage_min, gpu_fb_usage_max, gpu_fb_usage_avg,
gpu_tensor_pipe_active_min, gpu_tensor_pipe_active_max, gpu_tensor_pipe_active_avg,
gpu_dram_active_min, gpu_dram_active_max, gpu_dram_active_avg,
gpu_sm_active_min, gpu_sm_active_max, gpu_sm_active_avg
```

New functions:
- `_enrich_ros_data_with_gpus()` — Populates GPU metrics for pods that have `gpu:` in static YAML
- `_gen_ros_gpu_metrics()` — Generates realistic metrics per GPU architecture tier

### Static YAML Format for GPU Data

```yaml
generators:
  - OCPGenerator:
      start_date: 2026-04-01
      end_date: 2026-04-30
      nodes:
        - node:
          node_name: gpu-node-1
          cpu_cores: 64
          memory_gig: 256
          namespaces:
            ml-training:
              pods:
                - pod:
                  pod_name: training-job
                  cpu_request: 8
                  mem_request_gig: 32
                  gpu:
                  gpu_model: A100
                  gpu_count: 1
```

**CRITICAL YAML GOTCHA:** The `node:`, `pod:`, and `gpu:` keys act as labels with `None` values. Their child keys (`node_name`, `pod_name`, `gpu_model`) must be **siblings at the same indentation level**, NOT nested under them. Getting this wrong produces empty GPU data silently.

### Testing Nise GPU Generation

```bash
cd ~/dev/koku/nise
.venv/bin/python -m pytest tests/test_ocp_generator.py -k "gpu" -v
```

---

## 14. Koku Backend Changes

### effective_rates Endpoint

**File:** `koku/masu/api/effective_rates.py` (new file)  
**Registered in:** `koku/masu/api/urls.py`

Returns cost model rates and namespace-level cost aggregates for a given org_id + cluster_id + date range. Used by ros-ocp-backend to compute savings estimates.

Includes all rate types:
- `cpu_core_usage_per_hour`, `cpu_core_request_per_hour`
- `memory_gb_usage_per_hour`, `memory_gb_request_per_hour`
- `storage_gb_usage_per_month`, `storage_gb_request_per_month`
- `node_cost_per_month`, `cluster_cost_per_month`
- `gpu_cost_per_month`

Also returns `distribution_type`, `markup_pct`, and per-namespace cost aggregates.

### AGENTS.md Updates

Updated `koku/AGENTS.md` with SNO deployment troubleshooting, on-prem mode documentation, and cost-onprem Helm chart details.

---

## 15. Operator Changes

### DCGM Metric Replacement

**Commit:** `924de0f4 Replace misleading DCGM DEV_ metrics with PROF_ profiling metrics`

Replaced:
- `DCGM_FI_DEV_GPU_UTIL` → `DCGM_FI_PROF_SM_ACTIVE`
- `DCGM_FI_DEV_MEM_COPY_UTIL` → `DCGM_FI_PROF_DRAM_ACTIVE`
- Added: `DCGM_FI_PROF_PIPE_TENSOR_ACTIVE`

**Minimum DCGM Exporter version:** 3.1.x+ (for PROF_ metrics)  
**GPU architecture requirement:** Turing+ (compute capability 7.5+) for profiling metrics

### workload_pod_count

**Commit:** `1001a815`  
PromQL: `kube_pod_container_status_ready` aggregated by workload

### oom_count

**Commit:** `3383cf6c`  
PromQL: `kube_pod_container_status_last_terminated_reason{reason="OOMKilled"}` delta

---

## 16. Database Schema State

### ROS Database (`costonprem_ros`)

**User:** `ros_user`  
**Migration version:** 43 (latest: `000043_add_gpu_digests_unique_constraint`)

Key tables:
- `recommendation_sets` — Container recommendations (cluster_uuid is UUID type since migration 41)
- `daily_container_digests` — Daily CPU/memory aggregates (partitioned monthly)
- `gpu_container_digests` — Daily GPU metric aggregates (partitioned monthly)
- `namespace_recommendation_sets` — Namespace-level recommendations
- `daily_namespace_digests` — Namespace-level daily aggregates
- `recommendation_history` — Historical snapshots (partitioned monthly)
- `recommendation_quality` — Quality metrics (partitioned monthly)
- `container_usage_samples` — Raw usage samples for boxplots
- `namespace_usage_samples` — Namespace raw samples
- `clusters` — Cluster registry (cluster_uuid is UUID type)
- `rh_accounts` — Org/account registry
- `notification_code_definitions` — Notification code descriptions
- `org_recommendation_terms` — Custom timeframe settings per org
- `recommendation_profiles` — Recommendation profile configs

### Koku Database (`costonprem_koku`)

**User:** `koku_user`  
**Django migration version:** `reporting: 0347`

Key tables for our work:
- `"org1234567".cost_model` — Has "OCP Cost Model (Savings Demo)" with `gpu_cost_per_month: $2500`
- `api_provider` — "GPU Test OCP Cluster" with cluster_id `d4e5f6a7-b8c9-0123-defa-444444444444`

---

## 17. Key Bugs Found and Fixed

### Bug: org_id prefix mismatch with Koku effective_rates
- **Symptom:** effective_rates returned empty `configured_rates`
- **Root cause:** ROS passes `org_id=org1234567` but Koku expects `org_id=1234567` (it prepends "org" internally)
- **Fix:** `strings.TrimPrefix(orgID, "org")` in `gpu_enrichment.go`
- **Commit:** `4f3d96a`

### Bug: ApplyGPUSavings returns nil for well_utilized GPUs
- **Symptom:** `savings_usd: null` for all GPUs including well-utilized ones
- **Root cause:** Code only set savings when `savings > 0`, but $0 is meaningful ("no waste")
- **Fix:** Always set savings when cost data is available; nil only means "unknown"
- **Commit:** `c53f539`

### Bug: GPU data parsed but not persisted
- **Symptom:** `gpu_container_digests` table empty despite successful ingestion
- **Root cause:** `ProcessCSVToDigests` only wrote CPU/memory digests, not GPU digests
- **Fix:** Added `upsertGPUDigests()` call in pipeline.go
- **Commit:** `50d953c`

### Bug: Import cycle when GPU enrichment was in model package
- **Root cause:** `model` → `engine` → `model` circular import
- **Fix:** Moved `gpu_enrichment.go` from `internal/model/` to `internal/api/`
- **Commit:** `50d953c`

### Bug: gpu_container_digests ON CONFLICT fails without unique constraint
- **Root cause:** PostgreSQL requires a unique index for ON CONFLICT DO UPDATE
- **Fix:** Migration 000043 adds unique index on (cluster_uuid, namespace, workload, container_name, interval_start)
- **Commit:** `50d953c`

### Bug: Koku listener "Migrations not done" after image rebuild
- **Root cause:** Koku image had newer Django migrations than what was in the DB
- **Fix:** Run `python manage.py migrate_schemas --schema=public` and `--schema=org1234567` from inside the API pod

### Bug: Koku listener "Received unexpected OCP report"
- **Root cause:** Nise data cluster_id didn't match any registered provider
- **Fix:** Registered a new OCP provider via API with matching cluster_id

### Bug: Koku listener "No ROS reports to handle"
- **Root cause:** Tarball file paths had `./` prefix but manifest.json didn't
- **Fix:** Repackage tarball with explicit filenames (no `./` prefix)

### Bug: Nise GPU data empty in CSV
- **Root cause:** YAML indentation error — `gpu_model:` nested under `gpu:` instead of being a sibling
- **Fix:** Corrected YAML structure (see section 13)

### Bug: arm64 image build produces x86_64 binary
- **Symptom:** `Exec format error` on aarch64 cluster
- **Fix:** Always use `podman build --platform linux/arm64`

---

## 18. E2E Testing Playbook

### Prerequisites
1. sshuttle tunnel running
2. `oc login` to Apollo cluster
3. ROS API image built for arm64 and pushed

### Generate GPU Test Data with Nise

```bash
cd ~/dev/koku/nise

cat > /tmp/gpu_static_data.yml << 'EOF'
generators:
  - OCPGenerator:
      start_date: 2026-04-01
      end_date: 2026-04-28
      nodes:
        - node:
          node_name: gpu-node-1
          cpu_cores: 64
          memory_gig: 256
          namespaces:
            ml-training:
              pods:
                - pod:
                  pod_name: training-job
                  cpu_request: 8
                  mem_request_gig: 32
                  gpu:
                  gpu_model: A100
                  gpu_count: 1
                - pod:
                  pod_name: inference-server
                  cpu_request: 4
                  mem_request_gig: 16
                  gpu:
                  gpu_model: Tesla T4
                  gpu_count: 1
        - node:
          node_name: gpu-node-2
          cpu_cores: 32
          memory_gig: 128
          namespaces:
            legacy-workloads:
              pods:
                - pod:
                  pod_name: v100-legacy
                  cpu_request: 4
                  mem_request_gig: 16
                  gpu:
                  gpu_model: V100
                  gpu_count: 1
EOF

.venv/bin/nise report ocp \
  --ros-ocp-info \
  --static-report-file /tmp/gpu_static_data.yml \
  --ocp-cluster-id d4e5f6a7-b8c9-0123-defa-444444444444 \
  --insights-upload /tmp/nise_gpu_output \
  --write-monthly
```

### Package and Upload

```bash
cd /tmp/nise_gpu_output/<cluster_id>/<date_range>/

# Create manifest.json (CRITICAL: files list must match actual filenames WITHOUT ./ prefix)
# Then create tarball:
tar czf /tmp/gpu-e2e.tar.gz <files without ./ prefix>

# Get JWT token and upload
TOKEN=$(curl -sk "https://keycloak-keycloak.apps.sno.karmalabs.corp/realms/kubernetes/protocol/openid-connect/token" \
  -d "grant_type=client_credentials&client_id=cost-management-operator&client_secret=$CLIENT_SECRET" | python3 -c "import json,sys; print(json.load(sys.stdin)['access_token'])")

INGRESS_ROUTE=$(oc get route -n cost-onprem cost-onprem-ingress -o jsonpath='{.spec.host}')
curl -sk -X POST "https://$INGRESS_ROUTE/api/ingress/v1/upload" \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@/tmp/gpu-e2e.tar.gz;type=application/vnd.redhat.hccm.tar+tgz"
```

### Verify API Response

```bash
# Port-forward to ROS API
oc port-forward -n cost-onprem svc/cost-onprem-ros-api 8899:8000 &

IDENTITY=$(echo -n '{"identity":{"account_number":"7890123","org_id":"org1234567","type":"User","user":{"username":"operator-svc","email":"op@test.com","is_org_admin":true,"access":{}}},"entitlements":{"cost_management":{"is_entitled":true}}}' | base64 -w0)

# All recommendations
curl -s -H "x-rh-identity: $IDENTITY" "http://localhost:8899/api/cost-management/v1/recommendations/openshift" | python3 -m json.tool

# GPU-only
curl -s -H "x-rh-identity: $IDENTITY" "http://localhost:8899/api/cost-management/v1/recommendations/openshift?has_gpu=true" | python3 -m json.tool

# Filter by model
curl -s -H "x-rh-identity: $IDENTITY" "http://localhost:8899/api/cost-management/v1/recommendations/openshift?gpu_model=T4" | python3 -m json.tool

# Filter by classification
curl -s -H "x-rh-identity: $IDENTITY" "http://localhost:8899/api/cost-management/v1/recommendations/openshift?gpu_classification=well_utilized" | python3 -m json.tool
```

---

## 19. Known Issues and Gaps

### gpu_distributed Cost Type Gap

Koku has a `gpu_distributed` cost type that distributes GPU overhead costs to namespaces proportionally. Currently, `gpu_distributed` is **only implemented in the SaaS (Trino) path**. The on-prem (PostgreSQL-only) path may or may not have it — this needs verification against the latest main branch. If it's missing on-prem, GPU-distributed costs would be $0 in the savings calculation.

**Documented in:** `docs/known-issues.md`

### No Idle GPU in Test Data

The current nise-generated test data produces `well_utilized` (T4, A100) and no-profiling (V100) containers. There's no `idle` GPU container in the test data to verify idle savings calculation. To test this, generate a pod with very low GPU utilization or manually insert a test row.

### MIG Profile Recommendations Not Testable

MIG right-sizing requires A100/A30/H100 with MIG-enabled workloads. The test data doesn't include MIG profiles. The unit tests cover this logic but E2E verification requires actual MIG data.

### koku-ui Not Updated

No frontend changes for GPU recommendations. Waiting for Stefan's UX mockups.

---

## 20. Development Environment Notes

### Running Unit Tests (ros-ocp-backend)

```bash
cd ~/dev/koku/ros-ocp-backend

# All GPU-related unit tests
go test ./internal/engine/ -run "GPU|Gpu|Savings|Mig" -v
go test ./internal/ingestion/ -run "GPU|HasGPU" -v
go test ./internal/api/ -run "GPU|Filter" -v

# Integration tests (require Docker/Podman for testcontainers-go)
go test ./internal/api/ -run "Integration" -v -timeout 120s
go test ./internal/engine/ -run "Integration" -v -timeout 120s

# All tests
go test ./... -timeout 300s
```

### Running Nise Tests

```bash
cd ~/dev/koku/nise
.venv/bin/python -m pytest tests/test_ocp_generator.py -k "gpu" -v
```

### Running Koku Tests

```bash
cd ~/dev/koku/koku/koku
pipenv run python manage.py test masu.test.api.test_effective_rates --no-input -v 2
```

### Building arm64 Image

**This takes 10+ minutes** due to QEMU emulation on x86_64:
```bash
cd ~/dev/koku/ros-ocp-backend
podman build --platform linux/arm64 -t ros-ocp-backend:gpu-latest .
```

---

## 21. What NOT to Do

1. **Never build x86_64 images for Apollo** — it's aarch64. Always `--platform linux/arm64`.
2. **Never pass `org_id` with "org" prefix to Koku's effective_rates** — it prepends "org" internally.
3. **Never use `./` prefix in tarball filenames** — the manifest.json filenames must match exactly.
4. **Never run MinIO and the frontend simultaneously** — both use port 9000.
5. **Never use `required_permissions: ["all"]` in Cursor agent commands** — causes invisible approval prompts that hang the IDE.
6. **Never nest YAML keys under `node:`, `pod:`, `gpu:` in nise static data** — they must be siblings.
7. **Never query recommendation_sets.cluster_id** — it's `cluster_uuid` (UUID type since migration 41).
8. **Never forget to create GPU digest partitions** — `EnsureGPUDigestPartitions` is called in the pipeline but manual testing may need partition creation.
9. **Never assume GPU savings of `nil` means $0** — `nil` means "no cost data available", `$0` means "we know there's no savings opportunity".
10. **Never skip `docker exec koku_valkey redis-cli FLUSHALL`** after Koku code changes — the API cache will serve stale data.

---

## Appendix: File Index for GPU Feature

### ros-ocp-backend

| File | Purpose |
|------|---------|
| `internal/engine/gpu_recommender.go` | Core GPU classification + ApplyGPUSavings |
| `internal/engine/gpu_metadata.go` | GPU model specs, MIG profiles, MatchGPUModel() |
| `internal/engine/gpu_query.go` | QueryGPURecommendations() — reads gpu_container_digests |
| `internal/engine/gpu_recommender_test.go` | Unit tests for classification |
| `internal/engine/gpu_metadata_test.go` | Unit tests for model matching |
| `internal/engine/gpu_savings_test.go` | Unit tests for ApplyGPUSavings |
| `internal/ingestion/pipeline.go` | upsertGPUDigests(), EnsureGPUDigestPartitions() |
| `internal/ingestion/gpu_digest_test.go` | Unit tests for HasGPU, min/max/meanFloat |
| `internal/ingestion/models.go` | MetricRow with GPU fields + HasGPU() |
| `internal/ingestion/csvparser.go` | CSV column parsing including GPU columns |
| `internal/api/gpu_enrichment.go` | enrichWithGPU(), filterGPUResults(), toGPURecommendation() |
| `internal/api/gpu_enrichment_test.go` | Unit tests for toGPURecommendation |
| `internal/api/gpu_filter_test.go` | Unit tests for filterGPUResults, matchesAny |
| `internal/api/handlers.go` | API handlers calling enrichWithGPU + parseGPUFilters |
| `internal/api/handlers_integration_test.go` | GPU enrichment integration test |
| `internal/costdata/provider.go` | CostDataProvider interface + HTTP implementation |
| `internal/model/detail_response.go` | GPURecommendation struct in API response |
| `internal/testutil/fixtures.go` | SeedGPUDigest() for integration tests |
| `migrations/000042_create_gpu_container_digests.up.sql` | GPU digest table |
| `migrations/000043_add_gpu_digests_unique_constraint.up.sql` | Unique index for upsert |
| `docs/plans/gpu-recommendations.md` | Design document |
| `docs/plans/gpu-recommendations-test-plan.md` | Test plan |

### koku

| File | Purpose |
|------|---------|
| `koku/masu/api/effective_rates.py` | effective_rates endpoint |
| `koku/masu/api/urls.py` | URL registration |

### koku-metrics-operator

| File | Purpose |
|------|---------|
| `internal/collector/queries.go` | PromQL queries including DCGM PROF_ metrics |

### nise

| File | Purpose |
|------|---------|
| `nise/generators/ocp/ocp_generator.py` | GPU column generation |
| `tests/test_ocp_generator.py` | GPU generation tests |
