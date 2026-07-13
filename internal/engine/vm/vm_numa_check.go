package vm

import "fmt"

// CheckNUMAFit returns a notification when VM memory exceeds a single NUMA node's capacity.
//
// nodeNUMANodeMemGiB is typically from resolveNUMANodeMemoryGiB (node allocatable memory /
// ROS_VM_NUMA_ASSUMED_SOCKETS) with fallback to ROS_VM_NUMA_NODE_MEMORY_GIB (default 64).
func CheckNUMAFit(vmMemoryGiB float64, nodeNUMANodeMemGiB float64) *VMNotification {
	if nodeNUMANodeMemGiB <= 0 || vmMemoryGiB <= nodeNUMANodeMemGiB {
		return nil
	}
	return &VMNotification{
		Code: NotifVMNUMAOversized,
		Type: vmNotifTypeWarning,
		Message: fmt.Sprintf(
			"VM memory request (%.0f GiB) exceeds single NUMA node capacity (%.0f GiB) — NUMA pinning not possible. "+
				"Tune ROS_VM_NUMA_NODE_MEMORY_GIB or ensure daily_node_digests reflect host memory.",
			vmMemoryGiB, nodeNUMANodeMemGiB,
		),
	}
}
