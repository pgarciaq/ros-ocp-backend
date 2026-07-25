# ADR-0324: VM PVC companion CSV for shared storage detection

## Status

Accepted

## Context

Notification code 62 (`VM_SHARED_STORAGE`) identifies VMs that share PersistentVolumeClaims (PVCs) so operators can avoid disruption when rightsizing. Prior to this change, shared-storage detection used a **proxy heuristic**: VMs in the same namespace with identical placement profiles (vCPU, memory, disk capacity) were assumed to share storage. This produced false positives (VMs with the same resource profile but different PVCs) and false negatives (VMs sharing a PVC but differing in resource allocation).

The existing VM usage CSV (`ros-openshift-vm-usage-YYYYMM.csv`) has one row per VM per 15-minute interval. Adding per-PVC columns to this file would change its cardinality — a VM with three PVCs would need three rows per interval — which would break the 1:1 relationship between rows and VM usage samples that the ingestion pipeline, digest aggregation, and downstream recommendation engine all assume.

The GPU companion CSV (`ros-openshift-vm-gpu-device-YYYYMM.csv`) established a precedent for separating per-device data into a companion file with its own cardinality and child table, connected by a foreign key to the parent daily digest.

## Decision

### Separate companion CSV

Introduce `ros-openshift-vm-pvc-YYYYMM.csv` with columns:

| Column | Type | Description |
|--------|------|-------------|
| `interval_start` | timestamp | 15-minute interval start |
| `interval_end` | timestamp | 15-minute interval end |
| `vm_name` | string | KubeVirt VM name |
| `namespace` | string | Kubernetes namespace |
| `node_name` | string | Worker node |
| `pvc_name` | string | PersistentVolumeClaim name |
| `disk_capacity_bytes` | int64 | Allocated disk size in bytes |
| `volume_mode` | string | `Filesystem` or `Block` |

The file is placed in the tarball's `resource_optimization_files` section, which triggers automatic routing through the koku listener to ros-ocp-backend without any koku code changes.

### Child table, not column addition

A new `vm_pvc_digests` table stores per-PVC data as children of `daily_vm_digests`:

```sql
CREATE TABLE vm_pvc_digests (
    id BIGSERIAL PRIMARY KEY,
    vm_digest_id BIGINT NOT NULL REFERENCES daily_vm_digests(id) ON DELETE CASCADE,
    pvc_name TEXT NOT NULL,
    disk_capacity_bytes BIGINT NOT NULL DEFAULT 0,
    volume_mode TEXT NOT NULL DEFAULT 'Filesystem'
);
```

This mirrors the `vm_gpu_device_digests` pattern and avoids denormalizing PVC data into the digest row.

### Detection by name with fallback

When PVC data is available, `DetectSharedPVCs` checks for actual PVC name overlap: two VMs in the same namespace with a matching `pvc_name` in their `vm_pvc_digests`. When PVC data is absent (legacy operator payloads), the function falls back to the existing placement-profile proxy with a message indicating that per-PVC correlation requires the companion CSV.

### Three-repo change

| Repository | Change |
|------------|--------|
| `koku-metrics-operator` | New Prometheus query (`ros:vm_pvc_disk_bytes`), CSV writer, manifest inclusion |
| `ros-ocp-backend` | New payload type, migration, ingestion pipeline, engine update |
| `nise` | New report type constant, generator method, YAML schema extension |

## Consequences

**Positive:**
- Accurate per-PVC shared storage detection eliminates false positives from profile matching
- Graceful fallback preserves functionality for operators that haven't upgraded
- No breaking changes to existing CSV formats or API responses
- `volume_mode` data enables future storage-tiering recommendations

**Negative:**
- Three-repo coordination required for full rollout
- Older operator versions will continue to get proxy-based detection until upgraded
- Additional storage in `vm_pvc_digests` table (bounded by VM × PVC count × retention days)

**Neutral:**
- Notification message text changes from "Correlated workload group" to "VMs sharing PVC `{name}`" but the notification code (62) and JSON structure are unchanged
