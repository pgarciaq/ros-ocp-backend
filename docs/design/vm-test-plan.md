# VM Recommendations — Test Plan

**Status:** Implemented (phase11) — unit, E2E, and IQE coverage in place; gaps noted below  
**Related:** [VM Recommendations Design](vm-recommendations.md)

---

## Summary

| Layer | Originally planned | Implemented | Notes |
|-------|-------------------|-------------|-------|
| **Unit** (ros-ocp-backend) | ~25–30 | **~70** test functions | Ingestion, engine, catalog, notifications, API, settings, plugin |
| **E2E** (cost-onprem-chart) | ~12–15 | **~44** | ROS + settings + extended + enhancements + GPU + MVP promotions |
| **IQE** | ~10–12 | **~59** | Full API contract in `test_ros_vm_recommendations.py` |
| **Nise** | 4 scenarios | ✅ | `ocp_report_vm.yml` (chart + IQE) |

Legend: ✅ implemented · ⬜ not implemented / pending

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

### API (`internal/api/handlers_vm_recs_test.go`)

| Test | Status |
|------|--------|
| List empty / filters / invalid limit | ✅ |
| Detail missing params | ✅ |
| Settings GET | ✅ `TestVMSettings_GET_ReturnsConfig` |
| Settings PUT (valid, invalid JSON, out of range, partial) | ✅ `handlers_vm_settings_test.go` |
| Notification JSON parse (structured + legacy int array) | ✅ |
| order_by allowlist | ✅ |
| `filter[is_abandoned]` | ✅ `TestVMRecommendations_ListFilterAbandoned` |
| `filter[has_gpu]`, `filter[gpu_classification]` | ✅ | Unit `handlers_vm_recs_integration_test.go` + IQE + E2E |
| Detail `gpu_devices` array | ✅ | `TestVMDetail_Success_WithGPUDevices` |

### Integration (`vm_lifecycle_integration_test.go`)

| Test | Status |
|------|--------|
| CSV → digests → recommendations | ⬜ | No dedicated DB integration test file; covered by E2E flow |

---

## Layer B — cost-onprem-chart E2E

### ROS (`tests/suites/ros/test_vm_recommendations.py`) — 5 tests ✅

| Test | Status |
|------|--------|
| `test_vm_list_envelope` | ✅ |
| `test_vm_list_required_fields` | ✅ |
| `test_vm_list_current_and_recommended` | ✅ |
| `test_vm_filter_by_vm_name` | ✅ |
| `test_vm_filter_by_namespace` | ✅ |
| Filter cluster / status / confidence | ⬜ | Covered in IQE, not chart ROS suite |
| Skip if plugin disabled | ✅ (via `skip_if_vm_plugin_disabled`) |

### Settings (`tests/suites/ros/test_vm_settings.py`) — 2 tests ✅

| Test | Status |
|------|--------|
| `test_vm_settings_get_defaults` | ✅ |
| `test_vm_settings_put_optional` | ✅ |
| DELETE reset / locked fields 403 | ⬜ | IQE partial coverage |

### Extended E2E (`tests/suites/e2e/test_vm_recommendations_flow.py`) — 6 tests ✅

| Test | Status |
|------|--------|
| `test_vm_data_ingestion` | ✅ |
| `test_vm_recommendations_generated` | ✅ |
| `test_vm_recommendation_detail` | ✅ |
| `test_vm_idle_detection` | ✅ |
| `test_vm_settings_endpoint` | ✅ |
| `test_vm_guest_agent_confidence` | ✅ |
| Explicit notification 18/19 subtests | ⬜ | Idle/oversized asserted via metadata; not code-level assert |

### Extended E2E — MVP enhancements (`test_vm_enhancements_flow.py`) — 9 tests ✅

| Test | Status |
|------|--------|
| `test_windows_kernel_reserve_reflected` | ✅ |
| `test_crash_loop_notification_present` | ✅ |
| `test_unknown_os_notification` | ✅ |
| `test_instance_type_catalog_gn1_recognized` | ✅ |
| `test_settings_api_kernel_reserve` | ✅ |
| `test_settings_api_downsize_stability_days` | ✅ |
| `test_windows_update_spike_notification` | ✅ |
| `test_downsize_held_notification` | ✅ |
| `test_non_selectable_instance_types_not_recommended` | ✅ |

Template: `tests/data/nise_templates/ocp_report_vm_enhancements.yml`. Run:
`NAMESPACE=cost-onprem ./scripts/run-pytest.sh --extended -k vm_enhancements_flow`.

### Nise template ✅

`tests/data/nise_templates/ocp_report_vm.yml` — idle Linux/Windows, guest-agent VM, legacy profiles.

### Extended E2E — GPU (`test_vm_gpu_flow.py`) — 10 tests ✅

| Test | Status |
|------|--------|
| `test_vm_gpu_csv_ingestion` | ✅ |
| `test_vm_gpu_idle_classification_and_notification` | ✅ |
| `test_vm_gpu_filter_has_gpu` | ✅ |
| `test_vm_gpu_filter_classification_idle` | ✅ |
| `test_vm_gpu_detail_response_shape` | ✅ |
| `test_vm_gpu_underutil_notification_51` | ✅ |
| `test_vm_gpu_memory_saturated_notification_52` | ✅ |
| `test_vm_gpu_compute_saturated_notification_53` | ✅ |
| `test_vm_gpu_instance_type_gn1_match` | ✅ |
| `test_vm_no_gpu_excludes_gn1` | ✅ |

Template: `tests/data/nise_templates/ocp_report_vm_gpu.yml`. Run:
`NAMESPACE=cost-onprem ./scripts/run-pytest.sh --extended -k vm_gpu_flow`.

### Extended E2E — MVP promotions (`test_vm_mvp_promotions_flow.py`) — 7 tests ✅

| Test | Status |
|------|--------|
| `test_01_adaptive_margin_applied` | ✅ |
| `test_02_current_instance_type_populated` | ✅ |
| `test_03_time_slicing_recommendation` | ✅ |
| `test_04_multi_gpu_partial_idle` (notification 54) | ✅ |
| `test_05_mig_optimal_profile` | ✅ |
| `test_06_disk_projection_30_days` | ✅ |
| `test_07_recommendation_history_api` | ✅ |

Template: `tests/data/nise_templates/ocp_report_vm_mvp_promotions.yml`. Run:
`NAMESPACE=cost-onprem ./scripts/run-pytest.sh --extended -k vm_mvp_promotions_flow`.

### GPU device CSV (unit + routing)

| Test | Status |
|------|--------|
| `vm_gpu_device_csv_test.go` (ros-ocp-backend) | ✅ |
| Koku `ROS_EXTRA_PATTERNS` / `is_ros_vm_filename` for `ros-openshift-vm-gpu-device`, `ocp_ros_vm_gpu_device` | ✅ `test_kafka_msg_handler.py`, `test_common.py` |

---

## Layer C — IQE (`test_ros_vm_recommendations.py`) — ~59 tests ✅

Paths (constants):

```python
RECOMMENDATIONS_VM = "/recommendations/openshift/vm"
RECOMMENDATIONS_VM_DETAIL = "/recommendations/openshift/vm/detail"
RECOMMENDATIONS_SETTINGS_VM = "/recommendations/openshift/settings/vm"
RECOMMENDATIONS_SETTINGS_VM_TERMS = "/recommendations/openshift/settings/vm/terms"
```

| Area | Tests | Status |
|------|-------|--------|
| List envelope, pagination, filters (namespace, cluster, idle, oversized, term, engine, confidence) | 11 | ✅ |
| Ordering | 1 | ✅ |
| Detail 200, digests, 404, guest agent confidence | 4 | ✅ |
| Settings GET/PUT/partial, terms GET/PUT | 5 | ✅ |
| Current/recommended allocation, notifications, IO, disk projection | 5 | ✅ |
| Auth 401 / forbidden | 2 | ✅ |
| Known nise VM names (optional) | 1 | ✅ |

**New vs original plan:** disk projection, notifications structure, settings terms, `memory_floors` / `min_growth_mib_per_day` validated in settings tests.

### IQE — notification codes 46–49

| Test | Status |
|------|--------|
| `test_vm_notification_code_46_unknown_os` | ✅ |
| `test_vm_notification_code_47_windows_spike` | ✅ |
| `test_vm_notification_code_48_crash_loop` | ✅ |
| `test_vm_notification_code_49_downsize_held` | ✅ |
| `test_vm_filter_by_restart_count` | ✅ |
| `test_vm_windows_kernel_reserve_vs_linux` | ✅ |
| `test_vm_settings_kernel_reserve` / `test_vm_settings_downsize_stability_days` | ✅ |
| `test_vm_instance_type_non_selectable_not_recommended` | ✅ |

Data: `iqe_cost_management/data/openshift/ocp_report_ros_vm.yml` (namespace `vm-enhancements`).

### IQE — GPU notifications and filters (codes 50–53)

| Test | Status |
|------|--------|
| `test_vm_gpu_list_filter_has_gpu` | ✅ |
| `test_vm_gpu_list_filter_classification` | ✅ |
| `test_vm_gpu_detail_response_shape` | ✅ |
| `test_vm_gpu_idle_notification_code_50` | ✅ |
| `test_vm_gpu_underutil_notification_code_51` | ✅ |
| `test_vm_gpu_memory_saturated_code_52` | ✅ |
| `test_vm_gpu_compute_saturated_code_53` | ✅ |
| `test_vm_gpu_instance_type_gn1_match` | ✅ |
| `test_vm_no_gpu_excludes_gn1` | ✅ |

GPU VMs are defined in `ocp_report_ros_vm.yml` (`ml-training`, `inference` namespaces).

### IQE — MVP promotion scenarios

| Area | Status |
|------|--------|
| Adaptive margin, `current_instance_type`, time-slicing, multi-GPU notification 54 | ✅ Covered in IQE VM/GPU tests + chart E2E `test_vm_mvp_promotions_flow.py` |
| Recommendation history API | ✅ IQE + E2E `test_07_recommendation_history_api` |

---

## Layer D — Nise

| Scenario | Status |
|----------|--------|
| Oversized Linux VM | ✅ `web-server-linux-01` / similar in templates |
| Idle Linux / Windows | ✅ `idle-vm-linux-01`, `idle-windows-legacy-01` |
| Windows + guest agent | ✅ `db-server-windows-01` |
| High I/O / upsize | ⬜ | Partial via legacy-app profiles |
| GPU idle / underutil / saturated | ✅ | `gpu-idle-vm`, `gpu-underutil-mig-vm`, `gpu-memory-saturated-vm`, `gpu-compute-saturated-vm` in nise templates |

Data file: `iqe_cost_management/data/openshift/ocp_report_ros_vm.yml` (IQE), `cost-onprem-chart/tests/data/nise_templates/ocp_report_vm.yml`, `ocp_report_vm_gpu.yml` (GPU E2E).

---

## Suggested follow-up tests (not in original plan)

| Test | Priority |
|------|----------|
| Koku routing regression for new ROS filename patterns | Low — covered in Masu unit tests |
| DB lifecycle integration test | Medium |
| E2E assert notification codes 37–42 on seeded VMs | ✅ `test_vm_notifications_matrix.py` |
| OpenAPI contract entries for VM paths | Medium |
| IQE: `memory_floors` and `disk.min_growth_mib_per_day` in PUT body | Low |

---

## Running tests

```bash
# Unit
cd ros-ocp-backend
go test ./internal/engine/... ./internal/ingestion/... ./internal/api/... -run 'VM|Vm|vm' -count=1

# E2E (cluster required)
cd cost-onprem-chart
NAMESPACE=cost-onprem ./scripts/run-pytest.sh --ros -k vm
NAMESPACE=cost-onprem ./scripts/run-pytest.sh --extended -k vm_recommendations_flow
NAMESPACE=cost-onprem ./scripts/run-pytest.sh --extended -k vm_gpu_flow
NAMESPACE=cost-onprem ./scripts/run-pytest.sh --extended -k vm_mvp_promotions_flow
```

---

## References

- [VM Recommendations Design](vm-recommendations.md)
- [Plugin Architecture](../architecture/plugin-architecture.md)
- Chart: [`test_vm_recommendations.py`](../../../cost-onprem-chart/tests/suites/ros/test_vm_recommendations.py)
- IQE: [`test_ros_vm_recommendations.py`](../../../iqe-cost-management-plugin/iqe_cost_management/tests/rest_api/v1/test_ros_vm_recommendations.py)
