# Virtual Machine Recommendations

OpenShift Virtualization workloads can be right-sized using ROS VM recommendations. The service analyzes 15-minute usage samples from the metrics operator and suggests vCPU, memory, disk expansion, and **instance type** matches.

## Instance type preferences

Cluster admins can steer recommendations per VM using **VirtualMachineClusterPreference** resources and the VM’s `spec.preference.name` field. The metrics operator exports preferences and VM mappings in `cluster_instance_types.json`; ROS respects the preference **class** when choosing an instance type series.

**Precedence:** administrator preference class overrides automatic CPU:memory ratio classification. If no preference is set, or the class label is unknown, ROS uses the same ratio logic as before.

**Typical class labels:**

| Preference class | Recommended series |
|------------------|-------------------|
| `general-purpose` | General-purpose (`u1.*`) |
| `compute-intensive` | Compute-optimized (`cx1.*`) |
| `memory-intensive` | Memory-optimized (`m1.*`) |

Example: a VM with high CPU versus memory usage might normally map to compute-optimized, but if it references a `database` preference labeled `memory-intensive`, ROS recommends a memory-optimized instance type instead.

## API

- `GET /api/cost-management/v1/recommendations/openshift/vm` — list recommendations
- `GET /api/cost-management/v1/recommendations/openshift/vm/detail` — detail with `metadata.preference_name` / `metadata.preference_class` when configured
- `GET /api/cost-management/v1/recommendations/openshift/instance-types` — cluster instance type catalog and `preferences.configured` summary

See [VM recommendations design](../../docs/design/vm-recommendations.md) for algorithms, settings, and notification codes.
