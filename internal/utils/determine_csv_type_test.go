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
		{"ros-openshift-cluster-quota-20260501-20260528.csv", types.PayloadTypeClusterQuota},
		{"/tmp/ros-openshift-cluster-quota-20260501.csv", types.PayloadTypeClusterQuota},
		{"ocp_ros_cluster_quota_usage.csv", types.PayloadTypeClusterQuota},
		{"ros-openshift-namespace-20260501.csv", types.PayloadTypeNamespace},
		{"ocp_ros_namespace_usage.csv", types.PayloadTypeNamespace},
		{"ros-openshift-snapshot-20260501.csv", types.PayloadTypeSnapshot},
		{"ocp_snapshot_inventory.csv", types.PayloadTypeSnapshot},
		{"ros-openshift-storage-20260501.csv", types.PayloadTypeStorage},
		{"ocp_storage_usage.csv", types.PayloadTypeStorage},
		{"ocp_ros_usage.csv", types.PayloadTypeContainer},
		{"some/path/with/namespace/in/middle.csv", types.PayloadTypeContainer},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, DetermineCSVType(tc.file))
		})
	}
}
