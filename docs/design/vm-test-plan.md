# VM Recommendations — Test Plan

**Status:** Planned (phase11)  
**Related:** [VM Recommendations Design](vm-recommendations.md)

---

## Overview

This document specifies all planned tests for the OpenShift Virtualization
recommendations feature across four repositories: ros-ocp-backend (unit),
cost-onprem-chart (E2E), iqe-cost-management-plugin (IQE), and nise (data generation).

---

## Layer A — ros-ocp-backend Unit Tests (~25-30 tests)

### Plugin (`internal/plugins/vm/plugin_test.go`)

| Test | Validates |
|------|-----------|
| `TestVMPlugin_TraitAssertions` | Implements `CSVIngestor`, `APIProvider`, `RetentionProvider` |
| `TestVMPlugin_Metadata` | Name=`vm`, Phase=1, Priority=40 |
| `TestVMPlugin_SupportedCSVTypes` | Returns `PayloadTypeVM` |
| `TestVMPlugin_RetentionTables` | Returns `["daily_vm_digests", "vm_recommendations"]` |
| `TestVMPlugin_EnabledByDefault` | No-ops silently without data; enabled=true |
| `TestVMPlugin_RegisterRoutes_404WhenDisabled` | Route guard returns 404 |
| `TestVMPlugin_RegisterRoutes_RegistersEndpoints` | List + detail routes |

### Ingestion (`internal/ingestion/vm_test.go`)

| Test | Validates |
|------|-----------|
| `TestParseVMCSVRows_OperatorColumns` | 15-min intervals, 14-column format |
| `TestParseVMCSVRows_NiseColumnNames` | Alternate column name aliases |
| `TestParseVMCSVRows_GuestAgentNull` | Null guest-agent columns handled gracefully |
| `TestParseVMCSVRows_GuestAgentPopulated` | `memory_available`, `filesystem_used/capacity` parsed |
| `TestParseVMCSVRows_MalformedRow` | Rejects bad data without crash |
| `TestParseVMCSVRows_14ColumnContract` | CSV contract validation (column count/names) |

### Engine (`internal/engine/recommend_vm_test.go`)

| Test | Validates |
|------|-----------|
| `TestRecommendVM_CPURightSizing` | p95 → ceil to whole vCPUs, min 1 |
| `TestRecommendVM_MemoryRightSizing` | p95 + margin → ceil to whole GiB, min 1 |
| `TestRecommendVM_WindowsMemoryFloor` | >= 2 GiB when OS = Windows |
| `TestRecommendVM_LinuxMemoryFloor` | >= 512 MiB (0.5 GiB rounded to 1 GiB) |
| `TestRecommendVM_DownsizeHysteresis_Ratio` | No rec if rec/current >= 0.60 |
| `TestRecommendVM_DownsizeHysteresis_MinDelta` | No rec if delta < 2 vCPU or < 2 GiB |
| `TestRecommendVM_UpsizeAlwaysEmitted` | Upsize when usage exceeds current |
| `TestRecommendVM_IdleLinux` | CPU p95 < 50mc AND mem < 512 MiB → NotifVMIdle (18) |
| `TestRecommendVM_IdleWindows` | CPU p95 < 200mc AND mem < 3072 MiB → NotifVMIdle (18) |
| `TestRecommendVM_OversizedNotification` | Code 19 on significant downsize |
| `TestRecommendVM_MinDataDaysNotMet` | No rec when < 3/14/30 data days per term |
| `TestRecommendVM_GuestAgent_HighConfidence` | Working set used; confidence = "high" |
| `TestRecommendVM_NoGuestAgent_ModerateConfidence` | Raw usage; confidence = "moderate" |
| `TestRecommendVM_DiskTrending` | Linear projection + 25% headroom, round to 10 GiB |
| `TestRecommendVM_HighIOPSHint` | read+write p95 > 3000 → informational notification |
| `TestRecommendVM_InstanceTypeMatching` | Smallest type satisfying vCPU + GiB |
| `TestRecommendVM_SeriesClassification_CPUHeavy` | High CPU:memory ratio → cx1 |
| `TestRecommendVM_SeriesClassification_Balanced` | Moderate both → m1 |
| `TestRecommendVM_SeriesClassification_Idle` | Very low → u1 |

### API (`internal/api/handlers_vm_recs_test.go`)

| Test | Validates |
|------|-----------|
| `TestVMRecommendations_List_200` | Correct envelope with meta, data, links |
| `TestVMRecommendations_Detail_200` | Single VM response |
| `TestVMRecommendations_Detail_404` | Unknown ID |
| `TestVMRecommendations_Filter_VMName` | Filter by vm_name |
| `TestVMRecommendations_Filter_Namespace` | Filter by namespace |
| `TestVMRecommendations_Filter_Cluster` | Filter by cluster UUID |
| `TestVMRecommendations_Filter_Status` | Filter by recommendation_status |
| `TestVMRecommendations_ResponseShape` | Contains guest_agent_detected, confidence, io_profile |
| `TestVMRecommendations_Pagination` | Cursor-based pagination |
| `TestVMRecommendations_DisabledPlugin_404` | Route returns 404 when ROS_ENABLE_VM_RECS=false |

### Settings (`internal/api/handlers_vm_settings_test.go`)

| Test | Validates |
|------|-----------|
| `TestVMSettings_Thresholds_GET_Defaults` | Returns compiled defaults for recommendation_type=vm |
| `TestVMSettings_Thresholds_PUT_Persists` | Tenant overrides saved |
| `TestVMSettings_Thresholds_DELETE_Resets` | Reverts to defaults |
| `TestVMSettings_Thresholds_EnvLocked_403` | Locked fields reject PUT |
| `TestVMSettings_Terms_GET_Defaults` | Returns 7/30/90 day windows |
| `TestVMSettings_Terms_PUT_Persists` | Custom windows saved |
| `TestVMSettings_Terms_DELETE_Resets` | Reverts to defaults |

### Integration (`internal/plugins/vm/vm_lifecycle_integration_test.go`)

| Test | Validates |
|------|-----------|
| `TestVMLifecycle_CSVToDigests` | CSV ingestion populates daily_vm_digests |
| `TestVMLifecycle_FullPipeline` | CSV → digests → recommendVM() → vm_recommendations row |

---

## Layer B — cost-onprem-chart E2E Tests (~12-15 tests)

### ROS Integration (`tests/suites/ros/test_vm_recommendations.py`)

| Test | Validates |
|------|-----------|
| `test_vm_recommendations_list_envelope` | meta, data, links structure |
| `test_vm_recommendations_required_fields` | vm_name, namespace, cluster_id, confidence |
| `test_vm_recommendations_current_recommended` | vcpu, memory_gib, optional instance_type |
| `test_vm_recommendations_notifications` | Array structure, valid codes |
| `test_vm_recommendations_filter_vm_name` | Filter works |
| `test_vm_recommendations_filter_namespace` | Filter works |
| `test_vm_recommendations_skip_if_disabled` | 404 → pytest.skip |

### Settings (`tests/suites/ros/test_vm_settings.py`)

| Test | Validates |
|------|-----------|
| `test_vm_settings_get_defaults` | recommendation_type=vm returns thresholds |
| `test_vm_settings_put_persists` | Custom values saved |
| `test_vm_settings_delete_resets` | Revert to defaults |
| `test_vm_settings_locked_fields_403` | Env-locked fields rejected |

### Extended E2E (`tests/suites/e2e/test_vm_recommendations_flow.py`)

| Test | Validates |
|------|-----------|
| `test_vm_e2e_upload_and_api` | Full pipeline: register source → nise → upload → poll DB → API |
| Subtests | Oversized VM → notification 19; Idle VM → notification 18 |

### Nise Template

File: `tests/data/nise_templates/ocp_report_vm.yml`

Scenarios:
- Oversized Linux VM: 8 vCPU allocated, p95 usage ~1.5 cores
- Idle Linux VM: 4 vCPU, near-zero usage for 7+ days
- Windows VM with guest agent: memory_available populated, filesystem columns

---

## Layer C — IQE Tests (~10-12 tests)

### `test_ros_vm_recommendations.py`

| Test | Validates |
|------|-----------|
| `test_vm_list_envelope` | meta.count, data array, links |
| `test_vm_required_fields` | vm_name, namespace, cluster_id, guest_agent_detected, confidence |
| `test_vm_current_recommended_present` | vcpu, memory_gib in response |
| `test_vm_optional_fields` | instance_type, disk_gib, io_profile nullable |
| `test_vm_notifications_valid` | Array with code, type, message |
| `test_vm_filter_namespace` | Filter works |
| `test_vm_filter_vm_name` | Filter works |
| `test_vm_filter_recommendation_status` | active, idle, oversized |
| `test_vm_settings_get` | Thresholds for recommendation_type=vm |
| `test_vm_settings_put` | Persists (skip if env-locked) |
| `test_vm_settings_delete` | Resets to defaults |

### Constants

```python
RECOMMENDATIONS_VIRTUAL_MACHINES = "/recommendations/openshift/virtual-machines"
RECOMMENDATIONS_SETTINGS_VM = "/recommendations/openshift/settings/virtual-machines"
```

Requirement tag: `cost_ros_ocp_vm`  
Markers: `@pytest.mark.cost_ocp_on_prem`, `@pytest.mark.cost_required`

---

## Layer D — Nise (test data generation)

### New ROS VM CSV generator

- Report type: `ros_vm_usage`
- 15-min interval rows (96 per VM per day)
- 14 columns matching `ros-openshift-vm-usage-*.csv` spec
- Static YAML: `virtual_machines:` block under namespace
- Guest agent columns: populated for some VMs, null for others

### Scenarios to generate

| VM | Profile | Guest agent | Expected recommendation |
|----|---------|-------------|------------------------|
| `erp-backend` | 8 vCPU, 32 GiB; p95 usage 1.5 cores, 8 GiB | No | Downsize to 4 vCPU, 16 GiB (m1.large) |
| `legacy-app-02` | 4 vCPU, 8 GiB; near-zero usage 45 days | No | Idle (notification 18) |
| `win-dc-01` | 8 vCPU, 16 GiB; baseline ~150mc, ~2 GiB | Yes (Windows) | Right-sized (not idle despite low usage) |
| `db-server` | 4 vCPU, 16 GiB; 60% CPU, 80% memory | Yes (Linux) | Upsize to 8 vCPU, 32 GiB |

---

## Dependency Order

```
1. ros-ocp-backend unit tests     ← immediate (inline CSV fixtures, no cluster)
2. nise ROS VM generator          ← needed for E2E data
3. cost-onprem-chart E2E          ← needs deployed plugin + nise data
4. IQE tests                      ← needs deployed + ingested data
```

Unit tests have zero external dependencies and are written alongside implementation.
E2E/IQE require operator VM-A and nise support for real end-to-end validation.

---

## References

- [VM Recommendations Design](vm-recommendations.md)
- [Plugin Architecture](../architecture/plugin-architecture.md)
- [Test Plan §12.2](../architecture/test-plan.md)
- [Configurable Thresholds](../../docs-site/features/configurable-thresholds.md)
