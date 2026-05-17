# GPU Recommendations -- Test Plan

This test plan covers all phases of the GPU recommendations feature.
See [gpu-recommendations.md](gpu-recommendations.md) for the design.

## Test Infrastructure

### Nise GPU Data Generation (Implemented)

Nise now generates GPU profiling columns in ROS CSVs (`--ros-ocp-info`). When a pod
in the static YAML has a `gpus:` spec, the ROS container CSV includes 14 GPU columns:
`accelerator_model_name`, `accelerator_profile_name`, `accelerator_frame_buffer_usage_{min,max,avg}`,
`tensor_pipe_active_{min,max,avg}`, `dram_active_{min,max,avg}`, `sm_active_{min,max,avg}`.

Tier 1 GPUs (T4, A10, A30, A100, H100, L40S) get all profiling metrics.
Tier 2 GPUs (V100) get only frame buffer usage with empty PROF_ columns.
Non-GPU pods get empty GPU columns.

### Synthetic GPU CSV Test Data

In addition to nise, we use synthetic test CSVs as Go string constants for unit tests.
Each CSV fixture represents a specific GPU scenario.

Required fixtures:

| Fixture | GPU | MIG | PROF_ present | Scenario |
|---|---|---|---|---|
| `csv_a100_idle` | A100-SXM4-80GB | No | Yes | Idle: sm_active < 0.02 |
| `csv_a100_underutilized` | A100-SXM4-80GB | No | Yes | Underutilized: tensor < 0.15, sm < 0.25 |
| `csv_a100_memory_bound` | A100-SXM4-80GB | No | Yes | Memory-bound: dram > 0.60, tensor < 0.15 |
| `csv_a100_well_utilized` | A100-SXM4-80GB | No | Yes | Well-utilized: tensor >= 0.25 |
| `csv_a100_mig_3g40gb` | A100-SXM4-80GB | 3g.40gb | Yes | Already on MIG profile |
| `csv_h100_bursty` | H100-SXM5-80GB | No | Yes | Bursty: high sm_max/sm_avg ratio |
| `csv_t4_no_mig` | T4 | No | Yes | Non-MIG GPU, underutilized |
| `csv_v100_no_prof` | V100-SXM2-32GB | No | **No** | Tier 2: PROF_ columns blank |
| `csv_no_gpu` | -- | -- | -- | Container without GPU (all GPU columns empty) |
| `csv_mixed` | Various | Mixed | Mixed | Multiple containers with different GPUs |

Each fixture row has standard CPU/memory columns plus the GPU columns appended.
PROF_ values are 0.0-1.0 ratios, fb_usage is MiB.

---

## Phase 0: Operator Tests (koku-metrics-operator)

### 0-T1. Removed columns do not appear in CSV output

**Type:** Unit test (Go)
**File:** `internal/collector/*_test.go`
**Steps:**
1. Run the collector with mock Prometheus responses (existing pattern)
2. Assert the ROS CSV header does NOT contain `accelerator_core_usage_percentage_*`
3. Assert the ROS CSV header does NOT contain `accelerator_memory_copy_percentage_*`

### 0-T2. New PROF_ columns appear in CSV output

**Type:** Unit test (Go)
**File:** `internal/collector/*_test.go`
**Steps:**
1. Mock Prometheus responses for `DCGM_FI_PROF_PIPE_TENSOR_ACTIVE`,
   `DCGM_FI_PROF_DRAM_ACTIVE`, `DCGM_FI_PROF_SM_ACTIVE`
2. Assert ROS CSV header contains `tensor_pipe_active_min`, `tensor_pipe_active_max`,
   `tensor_pipe_active_avg`, and the same for `dram_active` and `sm_active`
3. Assert values are correctly placed in columns

### 0-T3. PROF_ columns are blank when Prometheus returns no data

**Type:** Unit test (Go)
**File:** `internal/collector/*_test.go`
**Steps:**
1. Mock Prometheus returning empty results for all PROF_ queries
2. Assert the CSV rows have empty values for the 9 PROF_ columns
3. Assert `accelerator_frame_buffer_usage_*` and `accelerator_model_name` are still present

### 0-T4. Expected CSV test files match new schema

**Type:** Verification
**File:** `internal/collector/test_files/expected_reports/`
**Steps:**
1. Update all expected CSV files to remove old DEV_ columns and add PROF_ columns
2. Run existing tests to confirm they pass with the new column layout

### 0-T5. Cost CSV path is unaffected

**Type:** Unit test (Go)
**Steps:**
1. Assert cost CSV still contains `DCGM_FI_PROF_GR_ENGINE_ACTIVE` and
   `DCGM_FI_DEV_MIG_MAX_SLICES` queries
2. No columns removed or added in the cost CSV path

---

## Phase A: Ingestion Tests (ros-ocp-backend)

### A-T1. CSV parser reads GPU columns correctly

**Type:** Unit test (Go)
**File:** `internal/ingestion/csvparser_test.go`
**Fixture:** `csv_a100_underutilized`
**Steps:**
1. Parse a CSV row with all GPU columns populated
2. Assert `MetricRow.AcceleratorModelName` == "NVIDIA A100-SXM4-80GB"
3. Assert `MetricRow.AcceleratorProfileName` == "" (no MIG)
4. Assert `MetricRow.AcceleratorFBUsageMax` is within expected range
5. Assert `MetricRow.TensorPipeActiveAvg` is within expected range (0.0-1.0)
6. Assert `MetricRow.DRAMActiveAvg` is within expected range
7. Assert `MetricRow.SMActiveAvg` is within expected range

### A-T2. CSV parser handles missing GPU columns gracefully

**Type:** Unit test (Go)
**File:** `internal/ingestion/csvparser_test.go`
**Fixture:** `csv_no_gpu`
**Steps:**
1. Parse a CSV row where all GPU columns are empty
2. Assert all GPU fields on MetricRow are zero-valued
3. Assert CPU/memory fields are still parsed correctly

### A-T3. CSV parser handles Tier 2 GPU (PROF_ blank, FB present)

**Type:** Unit test (Go)
**File:** `internal/ingestion/csvparser_test.go`
**Fixture:** `csv_v100_no_prof`
**Steps:**
1. Parse a CSV row where `accelerator_model_name` and `fb_usage` are populated
   but all PROF_ columns are blank
2. Assert `AcceleratorModelName` == "Tesla V100-SXM2-32GB"
3. Assert `AcceleratorFBUsageAvg` > 0
4. Assert `TensorPipeActiveAvg` == 0 (zero value)
5. Assert `DRAMActiveAvg` == 0
6. Assert `SMActiveAvg` == 0

### A-T4. CSV parser handles MIG profile name

**Type:** Unit test (Go)
**File:** `internal/ingestion/csvparser_test.go`
**Fixture:** `csv_a100_mig_3g40gb`
**Steps:**
1. Parse a CSV row with `accelerator_profile_name` == "3g.40gb"
2. Assert `AcceleratorProfileName` == "3g.40gb"

### A-T5. gpu_container_digests migration roundtrip

**Type:** Integration test (Go, testcontainers-go)
**File:** `internal/engine/migration_roundtrip_test.go`
**Steps:**
1. Apply all migrations up to the new `gpu_container_digests` migration
2. Assert the table exists with expected columns
3. Roll back the migration
4. Assert the table no longer exists

### A-T6. Daily GPU digest aggregation

**Type:** Unit test (Go)
**File:** `internal/ingestion/digest_test.go` (or new `gpu_digest_test.go`)
**Steps:**
1. Provide 24 hourly GPU metric rows for one container
2. Run the daily aggregation function
3. Assert the resulting digest has:
   - `fb_usage_min_mib` == min of all hourly mins
   - `fb_usage_max_mib` == max of all hourly maxes
   - `tensor_pipe_active_avg` == weighted average of hourly avgs
   - Same for dram_active and sm_active

### A-T7. Daily aggregation skips non-GPU containers

**Type:** Unit test (Go)
**Steps:**
1. Provide 24 hourly rows with no GPU data
2. Assert no `gpu_container_digests` row is produced

---

## Phase B: Recommendation Engine Tests

### B-T1. GPU model name matching

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_metadata_test.go`
**Steps:**
1. Input: "NVIDIA A100-SXM4-80GB" -> match `A100_80GB`
2. Input: "NVIDIA A100-PCIE-40GB" -> match `A100_40GB`
3. Input: "NVIDIA H100 80GB HBM3" -> match `H100_80GB`
4. Input: "Tesla V100-SXM2-32GB" -> match `V100_32GB`
5. Input: "NVIDIA T4" -> match `T4`
6. Input: "unknown-gpu" -> return nil (unrecognized model)

### B-T2. Workload classification: idle

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_recommender_test.go`
**Input:** 7 daily digests where `sm_active_avg < 0.02` for all days
**Assert:**
- Classification == "idle"
- NotifGPUIdle (26) is emitted

### B-T3. Workload classification: underutilized

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_recommender_test.go`
**Input:** 7 daily digests where `tensor_pipe_active_avg < 0.15` AND
`sm_active_avg < 0.25`
**Assert:**
- Classification == "underutilized"
- NotifGPUUnderutilized (10) is emitted

### B-T4. Workload classification: memory-bound

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_recommender_test.go`
**Input:** 7 daily digests where `dram_active_avg > 0.60` AND
`tensor_pipe_active_avg < 0.15`
**Assert:**
- Classification == "memory_bound"
- `memory_bound_detected` == true
- NotifGPUMemBound (12) is emitted

### B-T5. Workload classification: well-utilized

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_recommender_test.go`
**Input:** 7 daily digests where `tensor_pipe_active_avg >= 0.25`
**Assert:**
- Classification == "well_utilized"
- No GPU notification emitted (no action needed)

### B-T6. Workload classification: no profiling data (Tier 2)

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_recommender_test.go`
**Input:** 7 daily digests where all PROF_ columns are zero/NULL but
`fb_usage_max_mib > 0`
**Assert:**
- Classification is not set (nil or empty)
- No idle/underutilized/memory-bound notification emitted
- A "profiling metrics unavailable" notification IS emitted
- MIG recommendation based on FB usage alone still works (if MIG-capable)

### B-T7. MIG profile selection: A100-80GB, low FB usage

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_recommender_test.go`
**Input:** A100-80GB, `fb_usage_max_mib` P98 = 4500 MiB, tensor_pipe_active = 0.05
**Assert:**
- Recommended profile: "1g.10gb" (smallest A100-80GB profile with >= 5400 MiB after
  20% headroom, i.e., 4500 * 1.2 = 5400)

### B-T8. MIG profile selection: A100-80GB, high FB usage

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_recommender_test.go`
**Input:** A100-80GB, `fb_usage_max_mib` P98 = 60000 MiB
**Assert:**
- Recommended profile: "full_gpu" (no MIG profile fits -- needs more than 7g.80gb)

### B-T9. MIG profile selection: H100-80GB

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_recommender_test.go`
**Input:** H100-80GB, `fb_usage_max_mib` P98 = 15000 MiB
**Assert:**
- Recommended profile: "2g.20gb" or "3g.40gb" depending on headroom calc

### B-T10. MIG profile: already on recommended profile

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_recommender_test.go`
**Input:** A100-80GB, current profile = "3g.40gb", FB P98 = 30000 MiB
**Assert:**
- Recommended profile == "3g.40gb" (no change)
- No underutilized notification

### B-T11. MIG profile: non-MIG GPU

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_recommender_test.go`
**Input:** T4 (MIGSupported = false), underutilized
**Assert:**
- `recommended_gpu_profile` is null (not applicable)
- Classification still reported (underutilized)
- NotifGPUUnderutilized emitted

### B-T12. Confidence: insufficient observation days

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_recommender_test.go`
**Input:** 2 daily digests
**Assert:**
- `gpu_confidence` == 0.3

### B-T13. Confidence: 7 observation days

**Type:** Unit test (Go)
**Input:** 7 daily digests with stable utilization
**Assert:**
- `gpu_confidence` >= 0.8

### B-T14. Confidence: bursty workload

**Type:** Unit test (Go)
**Input:** 14 daily digests where `sm_active_max / sm_active_avg > 5`
**Assert:**
- Base confidence = 1.0 (14+ days), reduced by bursty penalty (0.7)
- `gpu_confidence` == 0.7

### B-T15. Threshold configuration via environment variables

**Type:** Unit test (Go)
**Steps:**
1. Set `ROS_GPU_IDLE_THRESHOLD` to 0.05 (instead of default 0.02)
2. Provide digests where `sm_active_avg` = 0.03
3. Assert classified as idle with the higher threshold
4. Unset the env var, re-run: assert NOT idle with default threshold

---

## Phase C: Notifications and Savings Tests

### C-T1. NotifGPUIdle (code 26) emitted for idle GPU

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_recommender_test.go`
**Steps:**
1. Run recommender with idle fixture
2. Assert notification code 26 is in the result

### C-T2. NotifGPUMemBound (code 27) emitted for memory-bound GPU

**Type:** Unit test (Go)
**Steps:**
1. Run recommender with memory-bound fixture
2. Assert notification code 27 is in the result
3. Assert `memory_bound_detected` == true

### C-T3. GPU savings: idle GPU with cost data

**Type:** Unit test (Go)
**File:** `internal/engine/savings_test.go` or `gpu_recommender_test.go`
**Input:** Idle A100, Koku reports gpu_distributed cost = $500/month
**Assert:**
- `estimated_monthly_gpu_savings_usd` == 500.00

### C-T4. GPU savings: MIG right-sizing

**Type:** Unit test (Go)
**Input:** Underutilized A100 (full GPU), recommended profile = "1g.10gb" (1 slice of 7)
**Assert:**
- `estimated_monthly_gpu_savings_usd` == `(1 - 1/7) * full_gpu_cost`
- Approximately 85.7% savings

### C-T5. GPU savings: no cost data from Koku

**Type:** Unit test (Go)
**Input:** Underutilized T4, Koku returns no GPU cost data
**Assert:**
- `estimated_monthly_gpu_savings_usd` is null
- NotifNoCostData notification is emitted

### C-T6. GPU savings: already on optimal MIG profile

**Type:** Unit test (Go)
**Input:** A100 on "1g.10gb", recommended = "1g.10gb" (same)
**Assert:**
- `estimated_monthly_gpu_savings_usd` == 0 (or null -- no savings)

---

## Phase D: API Response Tests

### D-T1. DetailResponse includes gpu block for GPU containers

**Type:** Integration test (Go, httptest + testcontainers-go)
**File:** `internal/api/handlers_integration_test.go`
**Steps:**
1. Seed database with daily_container_digests + gpu_container_digests for an A100
2. Run the recommendation engine
3. GET `/recommendations/openshift/containers?container=<name>`
4. Assert response JSON contains `gpu` block with:
   - `current_gpu_model` == "NVIDIA A100-SXM4-80GB"
   - `gpu_classification` is a valid string
   - `gpu_confidence` > 0
   - `tensor_pipe_active_avg` > 0

### D-T2. DetailResponse has null gpu block for non-GPU containers

**Type:** Integration test (Go)
**Steps:**
1. Seed database with CPU/memory-only container digests (no GPU)
2. Run the recommendation engine
3. GET the recommendation
4. Assert `gpu` field is null in the JSON response

### D-T3. Filter: has_gpu=true

**Type:** Integration test (Go)
**Steps:**
1. Seed database with 2 containers: one with GPU, one without
2. GET `/recommendations/openshift/containers?has_gpu=true`
3. Assert only the GPU container is returned

### D-T4. Filter: gpu_model=A100

**Type:** Integration test (Go)
**Steps:**
1. Seed database with 2 GPU containers: one A100, one T4
2. GET `/recommendations/openshift/containers?gpu_model=A100`
3. Assert only the A100 container is returned

### D-T5. Filter: gpu_classification=idle,underutilized

**Type:** Integration test (Go)
**Steps:**
1. Seed database with 3 GPU containers: idle, underutilized, well-utilized
2. GET `?gpu_classification=idle,underutilized`
3. Assert 2 results returned (not the well-utilized one)

### D-T6. OpenAPI spec validation

**Type:** Verification (manual or `oasdiff`)
**Steps:**
1. Validate `openapi.json` parses without errors
2. Verify new `gpu` schema definition exists
3. Verify `has_gpu`, `gpu_model`, `gpu_classification` query parameters are defined
4. Verify response schema references the `gpu` block

---

## End-to-End Tests

### E2E-T0. Nise-simulated full pipeline (no GPU hardware required)

This is the primary E2E test. Uses nise to generate synthetic GPU data that flows
through the full pipeline without requiring physical NVIDIA hardware.

**Prereqs:** Cost Management on-prem deployment (Apollo SNO or docker compose),
nise installed, sshuttle tunnel if remote cluster.

**Steps:**

1. **Create a static YAML** with GPU-equipped pods. Use the existing
   `tests/ocp_gpu_static_report.yml` in nise as a starting point:

   ```yaml
   generators:
     - OCPGenerator:
         nodes:
           - node:
             node_name: gpu-node-1
             cpu_cores: 32
             memory_gig: 256
             namespaces:
               gpu-namespace:
                 pods:
                   - pod:
                     pod_name: ml-training-pod
                     cpu_request: 8
                     mem_request_gig: 64
                     cpu_limit: 16
                     mem_limit_gig: 128
                     gpus:
                       - gpu:
                         gpu_model: "A100"
                         gpu_memory_capacity_mib: 40960
                   - pod:
                     pod_name: inference-pod
                     cpu_request: 4
                     mem_request_gig: 32
                     cpu_limit: 8
                     mem_limit_gig: 64
                     gpus:
                       - gpu:
                         gpu_model: "Tesla T4"
                         gpu_memory_capacity_mib: 15360
                   - pod:
                     pod_name: v100-legacy-pod
                     cpu_request: 4
                     mem_request_gig: 32
                     cpu_limit: 8
                     mem_limit_gig: 64
                     gpus:
                       - gpu:
                         gpu_model: "V100"
                         gpu_memory_capacity_mib: 32768
   ```

2. **Generate data with nise:**

   ```bash
   cd ~/dev/koku/nise
   .venv/bin/nise report ocp \
     --ros-ocp-info \
     --static-report-file /tmp/gpu_static_data.yml \
     --ocp-cluster-id my-ocp-cluster-3 \
     --insights-upload /tmp/nise_gpu_output \
     --daily-reports
   ```

3. **Verify GPU columns in generated CSV:**

   ```bash
   head -1 /tmp/nise_gpu_output/my-ocp-cluster-3/*/ros-openshift-container-*.csv | tr ',' '\n' | grep -n "accelerator\|tensor\|dram\|sm_active"
   ```

   Expected: columns for `accelerator_model_name`, `accelerator_profile_name`,
   `accelerator_frame_buffer_usage_{min,max,avg}`, `tensor_pipe_active_{min,max,avg}`,
   `dram_active_{min,max,avg}`, `sm_active_{min,max,avg}`.

4. **Package and upload to MinIO** (if on-prem cluster):

   ```bash
   PAYLOAD=$(python3 -c "import uuid; print(uuid.uuid4().hex)")
   cd /tmp/nise_gpu_output/my-ocp-cluster-3/<date-range>
   tar czf /tmp/${PAYLOAD}.2026_04.tar.gz .
   # Upload to MinIO without .tar.gz extension
   ```

5. **Trigger ingestion** via Masu API:

   ```bash
   curl -s "http://localhost:5042/api/cost-management/v1/ingest_ocp_payload/?org_id=1234567&payload_name=${PAYLOAD}.2026_04"
   ```

6. **Verify GPU data in database:**

   ```sql
   -- Check ros-ocp-backend processed the GPU columns
   SELECT container_name, pod,
          accelerator_model_name, accelerator_profile_name,
          tensor_pipe_active_avg, dram_active_avg, sm_active_avg,
          accelerator_frame_buffer_usage_avg
   FROM daily_container_digests
   WHERE accelerator_model_name IS NOT NULL AND accelerator_model_name != ''
   LIMIT 10;
   ```

7. **Verify API returns GPU recommendations:**

   ```bash
   IDENTITY=$(echo -n '{"identity":{"account_number":"10001","org_id":"1234567","type":"User","user":{"username":"user_dev","email":"user_dev@foo.com","is_org_admin":true,"access":{}}},"entitlements":{"cost_management":{"is_entitled":true}}}' | base64 -w0)

   curl -s -H "x-rh-identity: $IDENTITY" \
     "http://localhost:8000/api/cost-management/v1/recommendations/openshift?limit=10" \
     | python3 -c "
   import json, sys
   d = json.load(sys.stdin)
   for rec in d.get('data', []):
       gpu = rec.get('recommendations', {}).get('gpu')
       if gpu:
           print(f'{rec[\"container\"]}: model={gpu[\"current_gpu_model\"]}, '
                 f'class={gpu.get(\"gpu_classification\",\"N/A\")}, '
                 f'confidence={gpu.get(\"gpu_confidence\",\"N/A\")}, '
                 f'tensor={gpu.get(\"tensor_pipe_active_avg\",\"N/A\")}, '
                 f'profile={gpu.get(\"recommended_gpu_profile\",\"N/A\")}')
   "
   ```

**Expected results:**
- A100 pod: classification present (idle/underutilized/etc.), profiling metrics non-zero
- T4 pod: classification present, no MIG recommendation (T4 doesn't support MIG)
- V100 pod: no classification (Tier 2), notification 28 (no profiling data),
  FB usage present

### E2E-T1. Full pipeline with real GPU hardware

**Prereqs:** Apollo SNO with NVIDIA GPU, operator deployed, ros-ocp-backend deployed
**Steps:**
1. Deploy a GPU workload (e.g., `nvidia-smi` in a loop, or a small inference job)
2. Wait for operator to collect 2+ hours of data
3. Trigger data upload and ingestion
4. Check `gpu_container_digests` table has rows
5. Trigger recommendation engine
6. Verify API returns GPU recommendations with classification

### E2E-T2. Tier 2 graceful degradation on V100

**Prereqs:** Cluster with V100 GPU (or simulate with nise using `gpu_model: "V100"`)
**Steps:**
1. Generate nise data with V100 GPU or deploy a V100 GPU workload
2. Ingest the data
3. Verify PROF_ columns are blank in the CSV (or zeros for nise data)
4. Verify ros-ocp-backend produces FB-only recommendation
5. Verify notification 28 (profiling metrics unavailable) in API response

### E2E-T3. MIG workload recommendation

**Prereqs:** A100 or H100 with MIG enabled, or nise data with `mig_instances`
**Steps:**
1. Generate nise data with MIG profiles or deploy workloads on MIG partitions
2. Ingest the data
3. Verify `accelerator_profile_name` is populated in CSV
4. Verify ros-ocp-backend correctly identifies current MIG profile
5. Verify recommended profile is reasonable given observed usage

---

## Nise Tests (Python)

### N-T1. ROS CSV column definition includes GPU columns

**Type:** Unit test (Python)
**File:** `tests/test_ocp_generator.py::test_ros_usage_column_includes_gpu_columns`
**Assert:** All 14 GPU columns present in `OCP_ROS_USAGE_COLUMN`

### N-T2. GPU pod has profiling metrics (Tier 1)

**Type:** Unit test (Python)
**File:** `tests/test_ocp_generator.py::test_ros_data_gpu_pod_has_gpu_metrics`
**Assert:** A100 pod has float values for tensor_pipe_active, dram_active, sm_active, FB usage

### N-T3. Non-GPU pod has empty GPU columns

**Type:** Unit test (Python)
**File:** `tests/test_ocp_generator.py::test_ros_data_non_gpu_pod_has_empty_gpu_metrics`
**Assert:** All 14 GPU columns are empty strings for non-GPU pods

### N-T4. Tier 2 GPU has FB only, no PROF_ metrics

**Type:** Unit test (Python)
**File:** `tests/test_ocp_generator.py::test_ros_data_tier2_gpu_no_profiling_metrics`
**Assert:** V100 pod has FB usage but empty tensor/dram/sm columns

### N-T5. MIG GPU has profile name

**Type:** Unit test (Python)
**File:** `tests/test_ocp_generator.py::test_ros_data_mig_gpu_has_profile_name`
**Assert:** MIG-enabled pod has `accelerator_profile_name` populated

---

## Test Matrix

| Test ID | Phase | Type | DB Required | GPU Required |
|---|---|---|---|---|
| 0-T1 to 0-T5 | 0 | Unit (Go) | No | No |
| A-T1 to A-T4 | A | Unit (Go) | No | No |
| A-T5 | A | Integration (testcontainers) | Yes | No |
| A-T6, A-T7 | A | Unit (Go) | No | No |
| B-T1 to B-T15 | B | Unit (Go) | No | No |
| C-T1 to C-T6 | C | Unit (Go) | No | No |
| D-T1 to D-T5 | D | Integration (testcontainers) | Yes | No |
| D-T6 | D | Verification | No | No |
| N-T1 to N-T5 | Nise | Unit (Python) | No | No |
| E2E-T0 | All | Manual (nise data) | Yes | No |
| E2E-T1 to E2E-T3 | All | Manual (Apollo) | Yes | **Yes** |

### How to Run

```bash
# All ros-ocp-backend tests
cd ros-ocp-backend && go test ./...

# Only GPU-related unit tests (all pass)
go test ./internal/engine/ -run "TestMatchGPUModel|TestGPUModel|TestClassifyGPU|TestSelectMIG|TestGPUConfidence|TestRecommendGPU|TestApplyGPUSavings|TestGpuMonthlyRate|TestMigTotalSlices|TestMigProfileSlices" -v
go test ./internal/ingestion/ -run "TestMinFloat|TestMaxFloat|TestMeanFloat|TestHasGPU" -v
go test ./internal/api/ -run "TestToGPURecommendation|TestFilterGPU|TestMatchesAny" -v

# GPU integration tests (requires Docker for testcontainers)
go test ./internal/api/ -run "TestGetNativeRecommendationSetList_GPUEnrichment" -v -timeout 120s

# Operator tests
cd koku-metrics-operator && make test

# Nise GPU tests (Python)
cd nise && .venv/bin/python -m pytest tests/test_ocp_generator.py -k "gpu" -v
```

### Implementation Status per Test ID

| Test ID | Status | Notes |
|---|---|---|
| 0-T1 to 0-T5 | **Done** | Operator tests updated with new column layout |
| A-T1 to A-T4 | **Done** | CSV parser tests for GPU columns |
| A-T5 | **Done** | Migration roundtrip (version 43) |
| A-T6, A-T7 | **Partial** | Covered by `gpu_digest_test.go` (minFloat/maxFloat/meanFloat/HasGPU) and integration test |
| B-T1 | **Done** | `gpu_metadata_test.go` |
| B-T2 to B-T6 | **Done** | `gpu_recommender_test.go` |
| B-T7 to B-T11 | **Done** | `gpu_recommender_test.go` (MIG selection tests) |
| B-T12 to B-T14 | **Done** | `gpu_recommender_test.go` (confidence tests) |
| B-T15 | **Done** | `TestGpuThreshold_Default`, `_EnvOverride`, `_InvalidEnvFallsBack`, `_EmptyEnvFallsBack`, `TestClassifyGPUWorkload_IdleThresholdOverride`, `_MemBoundThresholdOverride` |
| C-T1 to C-T2 | **Done** | Covered in `gpu_recommender_test.go` |
| C-T3 to C-T6 | **Done** | `gpu_savings_test.go` (ApplyGPUSavings with idle, MIG, no cost data, well-utilized) |
| D-T1 | **Done** | `TestGetNativeRecommendationSetList_GPUEnrichment/response_includes_GPU_block` |
| D-T2 | **Done** | Implicitly covered by `has_gpu=false` filter subtest |
| D-T3 | **Done** | `TestGetNativeRecommendationSetList_GPUEnrichment/has_gpu=true_filter` + `has_gpu=false_filter` |
| D-T4 | **Done** | `TestGetNativeRecommendationSetList_GPUEnrichment/gpu_model_filter` |
| D-T5 | **Done** | `TestGetNativeRecommendationSetList_GPUEnrichment/gpu_classification_filter` |
| N-T1 to N-T5 | **Done** | 6 GPU-specific nise tests |
| E2E-T0 | **Done** | Verified on Apollo SNO cluster with nise-generated data |

---

## Regression Tests

After GPU recommendations are implemented, these existing tests MUST continue
to pass without modification:

- `internal/engine/recommend_cpu_test.go` -- CPU recommendations unaffected
- `internal/engine/recommend_memory_test.go` -- Memory recommendations unaffected
- `internal/engine/savings_test.go` -- Existing savings calculations unchanged
- `internal/api/handlers_integration_test.go` -- Existing API endpoints unchanged
- `internal/ingestion/csvparser_test.go` -- Existing CSV columns still parsed
- `internal/engine/migration_roundtrip_test.go` -- All migrations still reversible

Any failure in these tests after GPU changes indicates a regression.

### Nise regression tests

Nise GPU changes must not break existing OCP data generation:

```bash
cd nise && .venv/bin/python -m pytest tests/test_ocp_generator.py -v
```

All 84 tests (78 existing + 6 GPU-specific) must pass.

---

## Phase E: Time-Slicing Tests

### E-T1. Node name parsed from CSV

**Type:** Unit test (Go)
**File:** `internal/ingestion/csvparser_test.go`
**Steps:**
1. Parse a CSV row with a `node` column populated
2. Assert `MetricRow.Node` == expected node name
3. Parse a row with empty `node` column, assert `MetricRow.Node` == ""

### E-T2. gpu_container_digests stores node_name

**Type:** Integration test (Go)
**File:** `internal/ingestion/pipeline_test.go` or `gpu_digest_test.go`
**Steps:**
1. Ingest CSV data with `node` column populated
2. Query `gpu_container_digests` table
3. Assert `node_name` column matches expected value
4. Ingest data where `node` is empty, assert `node_name` == ""

### E-T3. ComputeNodeTimeslicingRecs: basic 3-candidate scenario

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_timeslicing_test.go`
**Input:** 4 GPU recs for node "gpu-t4-worker-1":
  - 3 underutilized T4s (SM ~12%)
  - 1 well-utilized T4 (SM ~67%)
**Assert:**
- Recommendation generated (>=50% candidates)
- `RecommendedReplicas` == 4 (floor(1/0.12) clamped to 8, floor gives 8... clamp max is 8)
- `CandidateContainers` has 3 entries
- `ImpactedContainers` has 1 entry
- Notification 29 emitted

### E-T4. ComputeNodeTimeslicingRecs: all containers underutilized

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_timeslicing_test.go`
**Input:** 2 GPU recs for node "all-underutil-node":
  - 2 underutilized T4s (SM ~20%)
**Assert:**
- Recommendation generated (100% candidates, no impacted)
- `ImpactedContainers` is empty
- Confidence has no impacted penalty (only 0.7 base)

### E-T5. ComputeNodeTimeslicingRecs: majority not met

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_timeslicing_test.go`
**Input:** 4 GPU recs for a node:
  - 1 underutilized T4
  - 3 well-utilized T4s
**Assert:**
- No recommendation generated (<50% candidates)

### E-T6. ComputeNodeTimeslicingRecs: skips MIG-capable GPUs with MIG recs

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_timeslicing_test.go`
**Input:** 2 underutilized A100s on same node, both with MIG recommendations
**Assert:**
- No time-slicing recommendation (MIG takes precedence)

### E-T7. ComputeNodeTimeslicingRecs: skips idle GPUs

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_timeslicing_test.go`
**Input:** Node with 2 idle T4s (SM < 0.02) and 1 underutilized T4
**Assert:**
- Idle GPUs not counted as candidates
- If remaining candidates < 50%, no recommendation

### E-T8. ComputeNodeTimeslicingRecs: skips memory-bound GPUs

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_timeslicing_test.go`
**Input:** Node with 2 memory-bound T4s and 1 underutilized T4
**Assert:**
- Memory-bound GPUs not counted as candidates

### E-T9. Replicas clamping

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_timeslicing_test.go`
**Input cases:**
- Very low utilization (SM 0.01) -> floor(1/0.01) = 100 -> clamped to 8
- High utilization (SM 0.40) -> floor(1/0.40) = 2 -> stays at 2
- Just below threshold (SM 0.51) -> floor(1/0.51) = 1 -> below 2, no recommendation
**Assert:** Correct clamping in each case

### E-T10. Savings calculation with cost data

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_timeslicing_test.go`
**Input:** 3 candidate T4s, gpu_monthly_rate = $300, recommended_replicas = 4
**Assert:**
- `savings_per_gpu` = $300 * (1 - 1/4) = $225
- `total_node_savings` = $225 * 3 = $675

### E-T11. Savings nil when no cost data

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_timeslicing_test.go`
**Input:** Candidates present but no Koku cost data
**Assert:**
- `savings_per_gpu_usd` is nil
- `total_node_savings_usd` is nil
- Recommendation still generated (savings optional)

### E-T12. Confidence calculation with impacted containers

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_timeslicing_test.go`
**Input:** 3 candidates (avg confidence 0.8) + 1 impacted, total 4 GPU containers
**Assert:**
- base = 0.8 * 0.7 = 0.56
- impacted penalty = 1.0 - 0.3 * (1/4) = 0.925
- final = 0.56 * 0.925 = 0.518

### E-T13. Mixed GPU models on same node

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_timeslicing_test.go`
**Input:** Node with 2 T4s (underutilized) and 1 L4 (underutilized)
**Assert:**
- Separate recommendations per GPU model
- T4 recommendation has 2 candidates
- L4 recommendation has 1 candidate (or skipped if < majority)

### E-T14. API endpoint: GET /recommendations/openshift/gpu/timeslicing

**Type:** Integration test (Go, httptest + testcontainers)
**File:** `internal/api/handlers_node_recs_test.go`
**Steps:**
1. Seed database with gpu_container_digests for 2 nodes (different GPU models)
2. GET `/recommendations/openshift/gpu/timeslicing`
3. Assert response contains node-level recommendations
4. Assert `meta.count` matches number of nodes with recommendations

### E-T15. API filter: node_name

**Type:** Integration test (Go)
**Steps:**
1. GET `/recommendations/openshift/gpu/timeslicing?node_name=gpu-t4-worker-1`
2. Assert only recommendations for that node are returned

### E-T16. API filter: gpu_model

**Type:** Integration test (Go)
**Steps:**
1. GET `/recommendations/openshift/gpu/timeslicing?gpu_model=T4`
2. Assert only T4 recommendations returned

### E-T17. Container-level cross-reference

**Type:** Integration test (Go)
**Steps:**
1. Seed data for a node with time-slicing candidates
2. GET container-level recommendations for one of the candidate containers
3. Assert notification code 29 appears in the container's GPU recommendation
4. Assert time-slicing context is surfaced (node reference, recommended replicas)

### E-T18. Notification code 29 emitted correctly

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_timeslicing_test.go`
**Steps:**
1. Run `ComputeNodeTimeslicingRecs` with valid candidates
2. Assert notification 29 in the TimeslicingRec result
3. Assert notification 29 added to each candidate container's GPURec

---

### Phase E Test Matrix

| Test ID | Type | DB Required | Notes |
|---|---|---|---|
| E-T1 | Unit | No | CSV parser |
| E-T2 | Integration | Yes | Pipeline persistence |
| E-T3 to E-T13 | Unit | No | Engine logic |
| E-T14 to E-T17 | Integration | Yes | API + testcontainers |
| E-T18 | Unit | No | Notification wiring |

### Phase E Implementation Status

| Test ID | Status | Notes |
|---|---|---|
| E-T1 | **Done** | `TestParseCSVRows_NodeColumn` in `csvparser_test.go` |
| E-T2 | **Done** | `TestUpsertGPUDigests_StoresNodeName`, `_NodeNameEmptyWhenNotProvided` |
| E-T3 | **Done** | `TestComputeNodeTimeslicingRec_HappyPath` |
| E-T4 | **Done** | `TestComputeNodeTimeslicingRec_AllUnderutilized` |
| E-T5 | **Done** | `TestComputeNodeTimeslicingRec_SkipBelowMajority` |
| E-T6 | **Done** | `TestComputeNodeTimeslicingRec_SkipAllMIG` |
| E-T7 | **Done** | `TestComputeNodeTimeslicingRec_SkipAllIdle` |
| E-T8 | **Done** | `TestPartitionContainers/memory_bound_excluded` |
| E-T9 | **Done** | `TestComputeReplicas` (8 sub-tests for clamping) |
| E-T10 | **Done** | `TestComputeTimeslicingSavings/4_replicas_3_candidates` |
| E-T11 | **Done** | `TestComputeTimeslicingSavings/no_cost_data` |
| E-T12 | **Done** | `TestComputeTimeslicingConfidence` (6 sub-tests) |
| E-T13 | **Done** | `TestComputeNodeTimeslicingRecs_MultipleGPUModels` |
| E-T14 | **Done** | `TestGetNodeRecommendations_WithData` + pagination integration tests |
| E-T15 | **Done** | `TestGetNodeRecommendations_FilterByNodeName` |
| E-T16 | **Done** | `TestGetNodeRecommendations_FilterByGPUModel` |
| E-T17 | **Done** | `TestComputeNodeTimeslicingRec_SetsContainerCrossRef`, `TestToGPURecommendation_WithTimeslicingCrossRef`, `TestToGPURecommendation_NoTimeslicingCrossRef` |
| E-T18 | **Done** | `TestComputeNodeTimeslicingRec_SetsNotifOnCandidates` |

Additional tests beyond the original plan:

| Test | Status | Notes |
|---|---|---|
| RBAC cluster filtering | **Done** | `TestGetNodeRecommendations_RBAC_FiltersByCluster` + 3 more integration tests |
| RBAC node filtering | **Done** | `TestFilterNodeRecsByRBAC_*` (6 unit tests) + integration tests |
| Pagination sort/offset/limit | **Done** | `TestSortNodeRecs_*` (6), `TestApplyNodePagination_*` (2), `TestBuildNodeLinks_*` (4) |
| Pagination integration | **Done** | `TestGetNodeRecommendations_PaginationMeta`, `_OrderByConfidence`, `_InvalidOrderBy`, `_OffsetBeyondResults` |
| 7-day freshness check | **Done** | `TestIsNodeFresh` (5 sub-tests), `TestComputeNodeTimeslicingRec_StaleNode/FreshNode/ZeroLastSeen` |
| Node last seen tracking | **Done** | `TestQueryGPURecommendations_NodeLastSeenTracksMax` |
| Org isolation | **Done** | `TestGetNodeRecommendations_OrgIsolation` |
| ResourceNode RBAC type | **Done** | `TestAddRBACFilter_NodeResourceTypeAccepted` + 3 more in `rbac/` |
| gpu_distributed verification (Koku) | **Done** | `test_gpu_distributed_rows_included_in_namespace_aggregates`, `test_gpu_rows_do_not_inflate_cpu_memory_hours`, `test_mixed_pod_and_gpu_distributed_costs_sum_correctly` in `koku/masu/test/api/test_effective_rates.py` |
