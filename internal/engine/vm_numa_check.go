package engine

import "fmt"

// CheckNUMAFit returns a notification when VM memory exceeds a single NUMA node's capacity.
//
// Per-host NUMA topology is not in the VM digest today; nodeNUMANodeMemGiB comes from
// ROS_VM_NUMA_NODE_MEMORY_GIB (default 64) until the operator exposes per-node NUMA metrics.
func CheckNUMAFit(vmMemoryGiB float64, nodeNUMANodeMemGiB float64) *VMNotification {
	if nodeNUMANodeMemGiB <= 0 || vmMemoryGiB <= nodeNUMANodeMemGiB {
		return nil
	}
	return &VMNotification{
		Code: NotifVMNUMAOversized,
		Type: vmNotifTypeWarning,
		Message: fmt.Sprintf(
			"VM memory request (%.0f GiB) exceeds single NUMA node capacity (%.0f GiB) — NUMA pinning not possible. "+
				"Set ROS_VM_NUMA_NODE_MEMORY_GIB to match host topology when known.",
			vmMemoryGiB, nodeNUMANodeMemGiB,
		),
	}
}
