# GPU Recommendations -- Test Plan

This test plan covers all phases of the GPU recommendations feature.
See [gpu-recommendations.md](gpu-recommendations.md) for the design.

## Test Infrastructure

### Synthetic GPU CSV Test Data

Nise does not generate `accelerator_*` or PROF_ columns in ROS CSVs. We create
synthetic test CSVs as Go string constants or embedded files. Each CSV fixture
represents a specific GPU scenario.

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
- NotifGPUIdle (11) is emitted

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

### C-T1. NotifGPUIdle (code 11) emitted for idle GPU

**Type:** Unit test (Go)
**File:** `internal/engine/gpu_recommender_test.go`
**Steps:**
1. Run recommender with idle fixture
2. Assert notification code 11 is in the result

### C-T2. NotifGPUMemBound (code 12) emitted for memory-bound GPU

**Type:** Unit test (Go)
**Steps:**
1. Run recommender with memory-bound fixture
2. Assert notification code 12 is in the result
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

## End-to-End Tests (Manual / Apollo Cluster)

These require a cluster with NVIDIA GPUs. They validate the full pipeline from
operator collection through Koku ingestion to ros-ocp-backend display.

### E2E-T1. Full pipeline: operator -> ingestion -> recommendation

**Prereqs:** Apollo SNO with NVIDIA GPU, operator deployed, ros-ocp-backend deployed
**Steps:**
1. Deploy a GPU workload (e.g., `nvidia-smi` in a loop, or a small inference job)
2. Wait for operator to collect 2+ hours of data
3. Trigger data upload and ingestion
4. Check `gpu_container_digests` table has rows
5. Trigger recommendation engine
6. Verify API returns GPU recommendations with classification

### E2E-T2. Tier 2 graceful degradation on V100

**Prereqs:** Cluster with V100 GPU (or simulate by deploying DCGM Exporter
without PROF_ metrics enabled)
**Steps:**
1. Deploy a GPU workload
2. Wait for operator data collection
3. Verify CSV has blank PROF_ columns but populated FB columns
4. Verify ros-ocp-backend produces FB-only recommendation
5. Verify "profiling metrics unavailable" notification in API response

### E2E-T3. MIG workload recommendation

**Prereqs:** A100 or H100 with MIG enabled, at least 2 MIG instances
**Steps:**
1. Deploy workloads on different MIG partitions
2. Wait for operator data collection
3. Verify `accelerator_profile_name` is populated in CSV
4. Verify ros-ocp-backend correctly identifies current MIG profile
5. Verify recommended profile is reasonable given observed usage

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
| E2E-T1 to E2E-T3 | All | Manual (Apollo) | Yes | **Yes** |

### How to Run

```bash
# All ros-ocp-backend tests
cd ros-ocp-backend && go test ./...

# Only GPU-related unit tests
go test ./internal/engine/ -run "GPU|Gpu|gpu"
go test ./internal/ingestion/ -run "GPU|Gpu|gpu"

# Integration tests (requires Docker for testcontainers)
go test ./internal/api/ -run "Integration" -tags=integration
go test ./internal/engine/ -run "Integration" -tags=integration

# Operator tests
cd koku-metrics-operator && make test
```

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
