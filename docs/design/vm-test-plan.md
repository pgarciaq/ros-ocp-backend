# VM Recommendations — Test Plan

**Status:** Implemented (phase11) — unit, E2E, and IQE coverage in place; gaps noted below  
**Related:** [VM Recommendations Design](vm-recommendations.md)

---

## Summary

| Layer | Originally planned | Implemented | Notes |
|-------|-------------------|-------------|-------|
| **Unit** (ros-ocp-backend) | ~25–30 | **~58** test functions | Ingestion, engine, catalog, notifications, API, settings |
| **E2E** (cost-onprem-chart) | ~12–15 | **13** | ROS + settings + extended flow |
| **IQE** | ~10–12 | **26** | Full API contract in `test_ros_vm_recommendations.py` |
| **Nise** | 4 scenarios | ✅ | `ocp_report_vm.yml` (chart + IQE) |

Legend: ✅ implemented · ⬜ not implemented / pending

---

## Layer A — ros-ocp-backend unit tests

### Plugin (`internal/plugins/vm/`)

| Test (planned) | Status | Notes |
|----------------|--------|-------|
| Trait assertions (`CSVIngestor`, routes, retention) | ⬜ | No `plugin_test.go`; behavior covered indirectly via integration path in `plugin.go` |
| Metadata name/phase/priority | ⬜ | |
| Enabled / 404 when disabled | ✅ | [`disabled_plugin_route_guards_test.go`](../../internal/api/disabled_plugin_route_guards_test.go) |

### Ingestion (`internal/ingestion/vm_csv_test.go`, `vm_digest_builder_test.go`)

| Test | Status |
|------|--------|
| Valid all columns / 15-min rows | ✅ `TestVMParseCSVRows_ValidAllColumns` |
| Guest agent null vs populated | ✅ `TestVMParseCSVRows_MissingGuestAgentColumns`, digest builder guest-agent tests |
| Malformed row / wrong header | ✅ |
| Single/multi VM, multi-day digests | ✅ `vm_digest_builder_test.go` (6 tests) |

### Engine (`internal/engine/vm_recommender_test.go`, `vm_instance_catalog_test.go`, `vm_config_test.go`, `vm_settings_test.go`)

| Test | Status |
|------|--------|
| CPU/memory right-sizing, floors | ✅ |
| Downsize hysteresis ratio and min delta | ✅ |
| Idle Linux / Windows | ✅ |
| Oversized → notification 19 | ✅ |
| Guest agent high / moderate confidence | ✅ |
| Disk projection guest vs hypervisor | ✅ `TestVMRecommend_DiskProjectionGrowth`, `DiskProjectionNoFilesystem`, `DiskGrowingHypervisorNotification` |
| High I/O → notification 39 | ✅ |
| Notifications 37–42, multiple combined | ✅ |
| Instance type smallest-fit | ✅ `vm_instance_catalog_test.go` (7 tests) |
| Series classification (cx/m/u) | ✅ `TestVMClassifySeries_*` |
| Config defaults and env overrides | ✅ `vm_config_test.go` |
| Settings validation | ✅ `vm_settings_test.go` |

### API (`internal/api/handlers_vm_recs_test.go`)

| Test | Status |
|------|--------|
| List empty / filters / invalid limit | ✅ |
| Detail missing params | ✅ |
| Settings GET | ✅ `TestVMSettings_GET_ReturnsConfig` |
| Notification JSON parse (structured + legacy int array) | ✅ |
| order_by allowlist | ✅ |

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

### Nise template ✅

`tests/data/nise_templates/ocp_report_vm.yml` — idle Linux/Windows, guest-agent VM, legacy profiles.

---

## Layer C — IQE (`test_ros_vm_recommendations.py`) — 26 tests ✅

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

---

## Layer D — Nise

| Scenario | Status |
|----------|--------|
| Oversized Linux VM | ✅ `web-server-linux-01` / similar in templates |
| Idle Linux / Windows | ✅ `idle-vm-linux-01`, `idle-windows-legacy-01` |
| Windows + guest agent | ✅ `db-server-windows-01` |
| High I/O / upsize | ⬜ | Partial via legacy-app profiles |

Data file: `iqe_cost_management/data/openshift/ocp_report_ros_vm.yml` (IQE), `cost-onprem-chart/tests/data/nise_templates/ocp_report_vm.yml`.

---

## Suggested follow-up tests (not in original plan)

| Test | Priority |
|------|----------|
| Plugin unit tests (`plugin_test.go`) | Low |
| DB lifecycle integration test | Medium |
| E2E assert notification codes 37–42 on seeded VMs | Medium |
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
```

---

## References

- [VM Recommendations Design](vm-recommendations.md)
- [Plugin Architecture](../architecture/plugin-architecture.md)
- Chart: [`test_vm_recommendations.py`](../../../cost-onprem-chart/tests/suites/ros/test_vm_recommendations.py)
- IQE: [`test_ros_vm_recommendations.py`](../../../iqe-cost-management-plugin/iqe_cost_management/tests/rest_api/v1/test_ros_vm_recommendations.py)
