# VM (OpenShift Virtualization)

## Overview

| Trait | Value |
|-------|-------|
| Plugin ID | `vm` |
| Scope | Virtual machine (KubeVirt) |
| Recommendation types | Downsize, idle/abandoned, power-off, GPU reduction |
| Fleet inclusion | Yes (`by_plugin.vm`) |

## Endpoint

`GET /recommendations/openshift/vms`

Returns VM-level recommendations including right-sizing (CPU/memory), idle detection, and power-off suggestions for OpenShift Virtualization workloads.

## Savings

Each VM recommendation includes `estimated_monthly_savings` computed from:

- **Downsize:** Delta between current and recommended vCPU/memory × rates
- **Idle/Abandoned:** Full allocation cost (100% recoverable)
- **Power-off:** Full VM cost
- **GPU reduction:** GPU count delta × GPU rate

When no cost data is available, savings returns `null` (unlike container/node/PVC which return $0 with notification code 25).

VM savings are included in fleet `savings-summary` totals under `by_plugin.vm`.

## Kill-Switch Behavior

When `ROS_SAVINGS_ESTIMATES_ENABLED=false`, VM savings return `null` (not computed).

## Settings

VM thresholds are configurable via the Settings API (`PUT /settings/vm`).

## Related

- [Savings Estimations](../features/savings-estimations.md) — fleet rollup, accuracy notes
- [Cost Integration](../architecture/cost-integration.md) — rate resolution chain
