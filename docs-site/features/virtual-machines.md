# Virtual machine recommendations

OpenShift Virtualization workloads receive right-sizing recommendations from daily metrics aggregated from 15-minute ROS samples.

## Confidence levels

Confidence reflects **how stable the QEMU guest agent is on the latest day**, not whether the agent was ever installed.

| Level | What it means |
|-------|----------------|
| **High** | At least one full day of metrics (20+ samples) and ≥ 80% of today's samples include guest-agent memory data. Sizing uses in-guest working set (memory available). Disk growth may use guest filesystem trends when two or more days of filesystem data exist. |
| **Moderate** | Either no guest agent in the lookback window, or agent data is missing or unstable on the latest day. Sizing uses hypervisor memory usage. |
| **Low** | Less than one full day of metrics on the latest day. Treat recommendations as preliminary; disk expansion is not projected. |

New VMs with the guest agent enabled from first boot can reach **high** confidence as soon as the first complete day of data is available—there is no multi-day waiting period.

### Notifications

- **Guest agent not installed (38):** Hypervisor-only metrics for the entire window.
- **Guest agent interrupted (44):** Agent data appeared earlier but the latest day is below the 80% stability threshold (removed, flapping, or partial-day install).
- **Insufficient data (45):** Fewer than 20 samples on the latest day.
