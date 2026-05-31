# Virtual Machine Recommendations

OpenShift Virtualization (KubeVirt) VMs can be right-sized like containers, with recommendations for vCPU, memory, disk growth, and instance type.

## Idle vs abandoned

| Status | What it means | What we recommend |
|--------|---------------|-------------------|
| **Idle** | CPU and memory usage stay **low** but not necessarily zero | Downsize to a small floor (for example 1 vCPU and 1 GiB on Linux) |
| **Abandoned** | **Zero** CPU and memory usage on **every** day in the observation window | **0** vCPU and **0** GiB — treat the VM as unused; consider deleting or powering off |

Abandoned is stronger than idle:

- Detection uses daily **max** usage (not percentiles): all days must show 0 CPU and 0 memory.
- You get a **critical** notification (code **43**), not the idle warning (code **18**).
- A VM is never marked both idle and abandoned; abandoned wins.

Default rule: at least **3** days of all-zero usage in the term window (about **72 hours** with daily digests).

## Configure the abandoned threshold

**Settings API** (per organization):

```http
GET /api/cost-management/v1/recommendations/openshift/settings/vm
```

Look for `thresholds.abandoned_min_days` (default `3`). Update with PUT on the same path (partial body allowed), for example:

```json
{
  "thresholds": {
    "abandoned_min_days": 5
  }
}
```

**Deployment environment** (locks the field for all tenants when set):

| Variable | Default | Meaning |
|----------|---------|---------|
| `ROS_VM_ABANDONED_MIN_DAYS` | `3` | Minimum number of daily digests with zero max CPU and memory |

## Find abandoned VMs in the API

```http
GET /api/cost-management/v1/recommendations/openshift/vm?filter[is_abandoned]=true
```

Response `metadata` includes `is_abandoned`. Sort with `order_by=is_abandoned`.

Notifications include a structured object, for example:

```json
{
  "code": 43,
  "type": "critical",
  "message": "VM appears abandoned: zero CPU and memory usage for 5 days. Consider deleting or powering off to recover resources."
}
```

## Further reading

- [VM Recommendations Design](../../docs/design/vm-recommendations.md) — algorithms, env vars, and API reference
- [UI Integration Guide](../ui-integration-guide.md) — full REST patterns for the Cost Management UI
