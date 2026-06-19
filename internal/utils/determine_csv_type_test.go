package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

func TestDetermineCSVType_PrefixOrder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		file string
		want types.PayloadType
	}{
		// Operator-generated filenames (prefix match)
		{"ros-openshift-cluster-quota-20260501-20260528.csv", types.PayloadTypeClusterQuota},
		{"/tmp/ros-openshift-cluster-quota-20260501.csv", types.PayloadTypeClusterQuota},
		{"ocp_ros_cluster_quota_usage.csv", types.PayloadTypeClusterQuota},
		{"ros-openshift-namespace-20260501.csv", types.PayloadTypeNamespace},
		{"ocp_ros_namespace_usage.csv", types.PayloadTypeNamespace},
		{"ros-openshift-snapshot-20260501.csv", types.PayloadTypeSnapshot},
		{"ocp_snapshot_inventory.csv", types.PayloadTypeSnapshot},
		{"ros-openshift-storage-20260501.csv", types.PayloadTypeStorage},
		{"ros-openshift-vm-gpu-device-20260501.csv", types.PayloadTypeVMGPU},
		{"ros-openshift-vm-usage-20260501.csv", types.PayloadTypeVM},
		{"ocp_ros_vm_usage.csv", types.PayloadTypeVM},
		{"ocp_storage_usage.csv", types.PayloadTypeStorage},
		{"ocp_ros_usage.csv", types.PayloadTypeContainer},
		{"some/path/with/namespace/in/middle.csv", types.PayloadTypeContainer},

		// Cost management CSV files (cm-openshift-*) must be classified as unknown.
		// Both bare and UUID-prefixed filenames must be handled.
		{"cm-openshift-pod-usage-202606.3.csv", types.PayloadTypeUnknown},
		{"cm-openshift-pod-usage-202605.1.csv", types.PayloadTypeUnknown},
		{"cm-openshift-node-capacity-202606.3.csv", types.PayloadTypeUnknown},
		{"/tmp/cm-openshift-pod-usage-202606.3.csv", types.PayloadTypeUnknown},
		// UUID-prefixed operator-generated CM files (the actual format the operator produces)
		{"d684644b-40be-49df-8320-5d51457c0d49-cm-openshift-pod-usage-202606.3.csv", types.PayloadTypeUnknown},
		{"d684644b-40be-49df-8320-5d51457c0d49-cm-openshift-storage-usage-202606.4.csv", types.PayloadTypeUnknown},
		{"d684644b-40be-49df-8320-5d51457c0d49-cm-openshift-namespace-usage-202606.0.csv", types.PayloadTypeUnknown},
		{"/tmp/extract/d684644b-cm-openshift-vm-usage-202606.5.csv", types.PayloadTypeUnknown},

		// Nise-generated filenames with date/UUID prefix (contains fallback)
		{"May-2026-02059694-68ab-4d58-8809-de1e91f1d0e5-ocp_ros_cluster_quota.csv", types.PayloadTypeClusterQuota},
		{"May-2026-02059694-68ab-4d58-8809-de1e91f1d0e5-ocp_ros_namespace_usage.csv", types.PayloadTypeNamespace},
		{"May-2026-02059694-68ab-4d58-8809-de1e91f1d0e5-ocp_snapshot_inventory.csv", types.PayloadTypeSnapshot},
		{"May-2026-02059694-68ab-4d58-8809-de1e91f1d0e5-ocp_storage_usage.csv", types.PayloadTypeStorage},
		{"May-2026-02059694-68ab-4d58-8809-de1e91f1d0e5-ocp_ros_vm_usage.csv", types.PayloadTypeVM},
		{"May-2026-02059694-68ab-4d58-8809-de1e91f1d0e5-ocp_ros_usage.csv", types.PayloadTypeContainer},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, DetermineCSVType(tc.file))
		})
	}
}
