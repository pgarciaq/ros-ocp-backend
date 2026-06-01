# VM Recommendations — Test Plan

**Status:** Production-ready (phase11) — unit, default CI, extended E2E, and IQE coverage  
**Related:** [VM Recommendations Design](vm-recommendations.md) · [Public VM feature doc](../docs-site/features/virtual-machines.md)

---

## Summary

| Layer | Originally planned | Implemented | Notes |
|-------|-------------------|-------------|-------|
| **Unit** (ros-ocp-backend) | ~25–30 | **252** test functions | Ingestion, engine, catalog, notifications, API, settings, plugin (`vm_*` test files) |
| **E2E default CI** (cost-onprem-chart) | ~12–15 | **20** | `tests/suites/ros/test_vm_*.py` (no `@extended`) |
| **E2E extended** (cost-onprem-chart) | — | **67** | Flow, GPU, network, GPU time-slicing, enhancements, notifications matrix (37–57), MVP promotions |
| **IQE** | ~10–12 | **89** | `test_ros_vm_recommendations.py` (network + GPU time-slicing) |
| **Nise** | 4 scenarios | ✅ | VM profiles + notification matrix templates |

Legend: ✅ implemented · ⬜ not implemented / deferred

---

## Layer A — ros-ocp-backend unit tests

### Plugin (`internal/plugins/vm/plugin_test.go`)

| Test | Status |
|------|--------|
| `TestVMPlugin_Implements_CSVIngestor` | ✅ |
| `TestVMPlugin_Implements_RetentionProvider` | ✅ |
| `TestVMPlugin_Priority_Is40` | ✅ |
| `TestVMPlugin_Name` | ✅ |
| `TestVMPlugin_RetentionTables_IncludesAllTables` | ✅ |
| `TestVMPlugin_RegisterRoutes_WhenDisabled_NoRoutes` | ✅ |
| Enabled / 404 when disabled (HTTP guards) | ✅ | [`disabled_plugin_route_guards_test.go`](../../internal/api/disabled_plugin_route_guards_test.go) |

### Ingestion (`internal/ingestion/vm_csv_test.go`, `vm_digest_builder_test.go`, `vm_gpu_device_csv_test.go`)

| Test | Status |
|------|--------|
| Valid all columns / 15-min rows | ✅ `TestVMParseCSVRows_ValidAllColumns` |
| Guest agent null vs populated | ✅ `TestVMParseCSVRows_MissingGuestAgentColumns`, digest builder guest-agent tests |
| Malformed row / wrong header | ✅ |
| Single/multi VM, multi-day digests | ✅ `vm_digest_builder_test.go` |
| GPU device CSV parse (valid, empty, malformed, header-only, missing columns) | ✅ `vm_gpu_device_csv_test.go` |
| Digest multi-GPU grouping and per-device averaging | ✅ `TestBuildDailyVMDigests_WithGPUDevices`, `GPUDeviceAveraging`, `NoGPU_EmptyDevices` |

### Engine (`internal/engine/vm_recommender_test.go`, `vm_instance_catalog_test.go`, `vm_config_test.go`, `vm_settings_test.go`)

| Test | Status |
|------|--------|
| CPU/memory right-sizing, floors | ✅ |
| Downsize hysteresis ratio and min delta | ✅ |
| Idle Linux / Windows | ✅ |
| Abandoned zero-usage (codes 43, supersedes idle) | ✅ `TestVMAbandoned_*`, `vm_detect_abandoned_test.go` |
| Oversized → notification 19 | ✅ |
| Guest agent high / moderate confidence | ✅ |
| Disk projection guest vs hypervisor | ✅ `TestVMRecommend_DiskProjectionGrowth`, `DiskProjectionNoFilesystem`, `DiskGrowingHypervisorNotification` |
| High I/O → notification 39 | ✅ |
| Notifications 37–42, multiple combined | ✅ |
| Instance type smallest-fit | ✅ `vm_instance_catalog_test.go` (7 tests) |
| Series classification (cx/m/u) | ✅ `TestVMClassifySeries_*` |
| Config defaults and env overrides | ✅ `vm_config_test.go` |
| Settings validation | ✅ `vm_settings_test.go` |
| History retention prune | ✅ `vm_history_retention_test.go` |

### Phase 11 MVP enhancements (`vm_recommender_test.go`, `vm_instance_catalog_test.go`)

| Enhancement | Unit tests | Status |
|-------------|------------|--------|
| Windows kernel reserve | `TestWindows_KernelReserveSubtracted`, `TestWindows_KernelReserveDoesNotGoBelowFloor`, `TestWindows_KernelReserveConfigurable` | ✅ |
| Windows update spike (code 47) | `TestWindowsUpdateSpike_NotificationTriggered`, `TestWindowsUpdateSpike_NoNotificationWhenSmallSpread`, `TestWindowsUpdateSpike_OnlyForWindows` | ✅ |
| Crash loop (code 48) | `TestCrashLoop_NotificationTriggered`, `TestCrashLoop_BelowThreshold`, `TestCrashLoop_NilRestartCount` | ✅ |
| Instance catalog n1/gn1 | `TestCatalog_ContainsNSeries`, `TestCatalog_ContainsGPUSeries`, `TestMatchInstanceType_NeverRecommendsNonSelectable`, `TestMatchInstanceType_RecognizesGPUType` | ✅ |
| VM GPU classification (codes 50–53) | `TestVMGPU_*` in `vm_gpu_recommender_test.go`, `TestInstanceType_GPU*` | ✅ |
| Unknown OS (code 46) | `TestUnknownOS_NotificationAdded`, `TestUnknownOS_UsesLinuxDefaults` | ✅ |
| Downsize stability (code 49) | `TestDownsizeStability_AllDaysBelow_RecommendsDownsize`, `TestDownsizeStability_OneDayAbove_HoldsAtCurrent`, `TestDownsizeStability_OnlyPerformanceEngine` | ✅ |
| Network classification (n1, code 55) | `vm_network_classification_test.go` (`TestVMClassifySeriesNetwork_*`, `TestVMClassifySeries_NetworkOptimizedWhenBalanced`) | ✅ |
| GPU time-slicing (codes 56–57) | `vm_gpu_timeslicing_test.go` (`TestRecommendVMTimeSlicing_*`, `TestVMGPU_HighFB_TimeSliceUnsafeNotification`) | ✅ |

### API (`internal/api/handlers_vm_recs_test.go`)

| Test | Status |
|------|--------|
| List empty / filters / invalid limit | ✅ |
| Detail missing params | ✅ |
| Settings GET | ✅ `TestVMSettings_GET_ReturnsConfig` |
| Settings PUT (valid, invalid JSON, out of range, partial) | ✅ `handlers_vm_settings_test.go` |
| Settings DELETE reset | ✅ `handlers_vm_settings_test.go` |
| Notification JSON parse (structured + legacy int array) | ✅ |
| order_by allowlist | ✅ |
| `filter[is_abandoned]` | ✅ `TestVMRecommendations_ListFilterAbandoned` |
| `filter[has_gpu]`, `filter[gpu_classification]` | ✅ | Unit `handlers_vm_recs_integration_test.go` + IQE + E2E |
| `filter[is_network_bound]`, `filter[guest_os]` | ✅ | `handlers_vm_recs_integration_test.go`, `vm_api_db_test.go` + IQE |
| Detail `gpu_devices` array | ✅ | `TestVMDetail_Success_WithGPUDevices` |

### Integration (`vm_lifecycle_integration_test.go`, `vm_integration_test.go`)

| Test | Status |
|------|--------|
| CSV → digests → recommendations (notification matrix VMs) | ✅ | `vm_integration_test.go` |
| History retention prune (DB) | ✅ | `vm_history_retention_test.go` |

### OpenAPI contract

| Test | Status |
|------|--------|
| VM paths and schemas match live handlers | ✅ | `internal/api/openapi_contract_test.go` |

---

## Layer B — cost-onprem-chart E2E

### Default CI — ROS (`tests/suites/ros/`) ✅

| File | Tests | Status |
|------|-------|--------|
| [`test_vm_recommendations.py`](../../../cost-onprem-chart/tests/suites/ros/test_vm_recommendations.py) | List envelope, fields, filters, `test_vm_recommendations_exist` | ✅ |
| [`test_vm_settings.py`](../../../cost-onprem-chart/tests/suites/ros/test_vm_settings.py) | GET defaults, PUT, DELETE settings/terms, adaptive margin, kernel reserve, history retention | ✅ |

These run with `NAMESPACE=cost-onprem ./scripts/run-pytest.sh --ros -k vm` (no `--extended`).

### Extended E2E (`tests/suites/e2e/`) ✅

| Test file | Status |
|-----------|--------|
| `test_vm_recommendations_flow.py` | ✅ |
| `test_vm_enhancements_flow.py` | ✅ |
| `test_vm_gpu_flow.py` | ✅ |
| `test_vm_instance_types_flow.py` | ✅ |
| `test_vm_notifications_matrix.py` | ✅ codes 37–57 (37–42 direct; 43–57 cross-ref) |
| `test_vm_mvp_promotions_flow.py` | ✅ |
| `test_vm_preference_flow.py` | ✅ preference metadata ingest (extended) |
| `test_vm_network_flow.py` | ✅ n1 network-bound, notification **55**, `network` settings API |
| `test_vm_gpu_timeslicing_flow.py` | ✅ production time-slicing, notifications **56**–**57**, `gpu` settings API |

### Nise templates ✅

| Template | Purpose |
|----------|---------|
| `ocp_report_vm.yml` | Default VM E2E + downsize-unstable seed |
| `ocp_report_vm_notifications.yml` | Notification matrix direct scenarios (37–42) |
| `ocp_report_vm_enhancements.yml` | Codes 46–49, kernel reserve |
| `ocp_report_vm_gpu.yml` | GPU 50–53 |
| `ocp_report_vm_mvp_promotions.yml` | Adaptive margin, history, MIG |
| `ocp_report_vm_network.yml` | `network-heavy-vm-01` → n1, notification **55** |
| `ocp_report_vm_gpu_timeslicing.yml` | Underutilized + FB-saturated GPUs → **56**–**57** |

---

## Layer C — IQE (`test_ros_vm_recommendations.py`) — 89 tests ✅

Paths (constants):

```python
RECOMMENDATIONS_VM = "/recommendations/openshift/vm"
RECOMMENDATIONS_VM_DETAIL = "/recommendations/openshift/vm/detail"
RECOMMENDATIONS_SETTINGS_VM = "/recommendations/openshift/settings/vm"
RECOMMENDATIONS_VM_HISTORY = "/recommendations/openshift/vm/{vm_name}/history"
```

| Area | Status |
|------|--------|
| List, detail, settings, terms, filters, auth | ✅ |
| Notifications 18–19, 37–57, 50–54 | ✅ |
| Notification **41** instance type rec | ✅ `test_vm_notification_code_41_instance_type_rec` |
| Network-bound / n1 (code **55**) | ✅ `test_vm_network_bound_*`, `test_vm_notification_code_55_network_saturated`, `test_vm_filter_is_network_bound` |
| GPU time-slicing / vGPU (codes **56**–**57**) | ✅ `test_vm_gpu_timeslice_*`, `test_vm_notification_code_56_vgpu_profile`, `test_vm_notification_code_57_fb_unsafe` |
| `filter[guest_os]` | ✅ IQE guest OS filter tests |
| Preference metadata in detail | ✅ `test_vm_preference_metadata_in_detail` |
| History API + `history_retention_days` in settings | ✅ |
| Adaptive margin in settings GET | ✅ `test_vm_settings_cpu_adaptive_margin_enabled` |
| `current.instance_type` + notification 41 | ✅ `test_vm_current_instance_type_populated` |

Data: `iqe_cost_management/data/openshift/ocp_report_ros_vm.yml` (includes `downsize-unstable-vm-01`, `instance-type-rec-01`, `high-io-vm-01`).

---

## Layer D — Nise ✅

| Scenario | Status |
|----------|--------|
| Oversized / idle / Windows / guest agent | ✅ |
| High I/O (2× default IOPS threshold) | ✅ `high_io` → 6000 read/write IOPS |
| Instance type rec profile | ✅ `oversized_for_instance_type` |
| Downsize unstable | ✅ `downsize_unstable` / `downsize-unstable-vm-01` |
| GPU idle / MIG / saturated | ✅ |
| Preference VM name (with `cluster_instance_types.json`) | ✅ `preference-server-vm` documented |

---

## Running tests

```bash
# Unit
cd ros-ocp-backend
go test ./internal/engine/... ./internal/ingestion/... ./internal/api/... -run 'VM|Vm|vm' -count=1

# E2E default CI (cluster required)
cd cost-onprem-chart
NAMESPACE=cost-onprem ./scripts/run-pytest.sh --ros -k vm

# E2E extended
NAMESPACE=cost-onprem ./scripts/run-pytest.sh --extended -k vm

# IQE (on-prem)
ENV_FOR_DYNACONF=local iqe tests plugin cost_management -k test_ros_vm_recommendations
```

---

## References

- [VM Recommendations Design](vm-recommendations.md)
- [Plugin Architecture](../architecture/plugin-architecture.md)
- Chart: [`test_vm_recommendations.py`](../../../cost-onprem-chart/tests/suites/ros/test_vm_recommendations.py)
- IQE: [`test_ros_vm_recommendations.py`](../../../iqe-cost-management-plugin/iqe_cost_management/tests/rest_api/v1/test_ros_vm_recommendations.py)
