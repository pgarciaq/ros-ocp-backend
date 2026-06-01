package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

// TestDetermineCSVType_NiseMonthlyOutput exercises realistic nise --write-monthly filenames
// and manifest resource_optimization_files entries (hand-crafted upload tarballs).
func TestDetermineCSVType_NiseMonthlyOutput(t *testing.T) {
	t.Parallel()

	clusterUUID := "02059694-68ab-4d58-8809-de1e91f1d0e5"
	secondaryUUID := "a1b2c3d4-e5f6-7890-abcd-ef1234567890"

	type fileCase struct {
		file string
		want types.PayloadType
	}

	// Representative nise monthly directory layout (Month-Year-UUID-<type>.csv).
	monthlyFiles := []fileCase{
		{"May-2026-" + clusterUUID + "-ocp_ros_usage.csv", types.PayloadTypeContainer},
		{"June-2026-" + clusterUUID + "-ocp_ros_namespace_usage.csv", types.PayloadTypeNamespace},
		{"April-2025-" + clusterUUID + "-ocp_ros_cluster_quota.csv", types.PayloadTypeClusterQuota},
		{"January-2026-" + clusterUUID + "-ocp_storage_usage.csv", types.PayloadTypeStorage},
		{"Feb-2026-" + clusterUUID + "-ocp_snapshot_inventory.csv", types.PayloadTypeSnapshot},
		// Nested path as in extracted tarballs (./ prefix stripped at upload, path retained locally).
		{"20260501-20260601/May-2026-" + clusterUUID + "-ocp_ros_usage.csv", types.PayloadTypeContainer},
		// Multiple UUID-like segments before the type token.
		{"May-2026-" + clusterUUID + "-" + secondaryUUID + "-ocp_ros_namespace_usage.csv", types.PayloadTypeNamespace},
		// Operator-style names in the same directory (prefix match, no date prefix).
		{"ros-openshift-cluster-quota-20260501-20260528.csv", types.PayloadTypeClusterQuota},
		{"ros-openshift-namespace-20260501.csv", types.PayloadTypeNamespace},
		{"ros-openshift-storage-20260501.csv", types.PayloadTypeStorage},
		{"ros-openshift-snapshot-inventory-20260501.csv", types.PayloadTypeSnapshot},
		{"ros-openshift-vm-gpu-device-20260501.csv", types.PayloadTypeVMGPU},
		{"ros-openshift-vm-usage-20260501.csv", types.PayloadTypeVM},
		{"ocp_ros_usage.csv", types.PayloadTypeContainer},
		{"ocp_ros_namespace_usage.csv", types.PayloadTypeNamespace},
		{"ocp_ros_cluster_quota.csv", types.PayloadTypeClusterQuota},
		{"ocp_storage_usage.csv", types.PayloadTypeStorage},
		{"ocp_snapshot_inventory.csv", types.PayloadTypeSnapshot},
		{"May-2026-" + clusterUUID + "-ocp_ros_vm_usage.csv", types.PayloadTypeVM},
		{"ocp_ros_vm_usage.csv", types.PayloadTypeVM},
	}

	for _, tc := range monthlyFiles {
		tc := tc
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, DetermineCSVType(tc.file))
		})
	}

	// Manifest resource_optimization_files from a realistic hand-crafted manifest.json.
	manifestFiles := []string{
		"May-2026-" + clusterUUID + "-ocp_ros_usage.csv",
		"May-2026-" + clusterUUID + "-ocp_ros_namespace_usage.csv",
		"May-2026-" + clusterUUID + "-ocp_ros_cluster_quota.csv",
		"May-2026-" + clusterUUID + "-ocp_storage_usage.csv",
		"May-2026-" + clusterUUID + "-ocp_snapshot_inventory.csv",
	}
	expected := map[types.PayloadType]int{
		types.PayloadTypeContainer:    1,
		types.PayloadTypeNamespace:    1,
		types.PayloadTypeClusterQuota: 1,
		types.PayloadTypeStorage:      1,
		types.PayloadTypeSnapshot:     1,
	}
	got := map[types.PayloadType]int{}
	for _, name := range manifestFiles {
		got[DetermineCSVType(name)]++
	}
	assert.Equal(t, expected, got, "each manifest ROS file should map to a distinct payload type")
}
