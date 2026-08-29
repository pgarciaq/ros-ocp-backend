package csv

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/gpu"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pvc"
	"github.com/redhatinsights/ros-ocp-backend/librobne/vm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClassifyFilename(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		want Kind
	}{
		{"ros-openshift-container-2026-08-01.csv", KindContainerROS},
		{"./ros-openshift-container-hour.csv", KindContainerROS},
		{"May-2026-uuid-ocp_ros_usage.csv", KindContainerROS},
		{"ocp_ros_usage.csv", KindContainerROS},
		{"ocp_ros_namespace_usage.csv", KindNamespace},
		{"May-2026-uuid-ocp_ros_namespace_usage.csv", KindNamespace},
		{"ros-openshift-namespace-2026-08-01.csv", KindNamespace},
		{"./ros-openshift-namespace-hour.csv", KindNamespace},
		{"cm-openshift-pod-usage.csv", KindCostOnly},
		{"ocp_pod_usage.csv", KindCostOnly},
		{"ros-openshift-storage-20260501.csv", KindStorage},
		{"./ros-openshift-storage-hour.csv", KindStorage},
		{"ocp_storage_usage.csv", KindStorage},
		{"May-2026-uuid-ocp_storage_usage.csv", KindStorage},
		{"cm-openshift-storage-usage-202606.4.csv", KindStorage},
		{"d684644b-40be-49df-8320-5d51457c0d49-cm-openshift-storage-usage-202606.4.csv", KindStorage},
		{"ros-openshift-vm-usage-20260501.csv", KindVM},
		{"./ros-openshift-vm-usage-hour.csv", KindVM},
		{"ocp_ros_vm_usage.csv", KindVM},
		{"May-2026-uuid-ocp_ros_vm_usage.csv", KindVM},
		{"ros-openshift-vm-pvc-20260501.csv", KindVMPVC},
		{"May-2026-uuid-ocp_ros_vm_pvc.csv", KindVMPVC},
		{"ros-openshift-vm-gpu-device-20260501.csv", KindVMGPU},
		{"May-2026-uuid-ocp_ros_vm_gpu_device.csv", KindVMGPU},
		{"ros-openshift-cluster-quota-202011.csv", KindClusterQuota},
		{"./ros-openshift-cluster-quota-hour.csv", KindClusterQuota},
		{"ocp_ros_cluster_quota.csv", KindClusterQuota},
		{"May-2026-uuid-ocp_ros_cluster_quota.csv", KindClusterQuota},
		{"ros-openshift-snapshot-inventory-20260501.csv", KindSnapshot},
		{"./ros-openshift-snapshot-inventory-hour.csv", KindSnapshot},
		{"ocp_snapshot_inventory.csv", KindSnapshot},
		{"May-2026-uuid-ocp_snapshot_inventory.csv", KindSnapshot},
		{"cm-openshift-snapshot-inventory-202606.4.csv", KindSnapshot},
		{"d684644b-40be-49df-8320-5d51457c0d49-cm-openshift-snapshot-inventory-202606.4.csv", KindSnapshot},
		{"ocp_vm_usage.csv", KindUnknown},
		{"readme.txt", KindUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, ClassifyFilename(tc.name))
		})
	}
}

func TestParseRows_HeaderNameBased(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"namespace,workload,workload_type,container_name,pod,interval_start,interval_end,cpu_request_container_avg,cpu_usage_container_avg,memory_request_container_avg,memory_usage_container_avg",
		"app,api,deployment,api,api-0,2026-08-01 00:00:00 +0000 UTC,2026-08-01 01:00:00 +0000 UTC,0.1,0.05,104857600,52428800",
	}, "\n")
	rows, skipped, err := ParseRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Len(t, rows, 1)
	assert.Equal(t, "app", rows[0].Namespace)
	assert.Equal(t, int64(100), rows[0].CPURequestMC)
	assert.Equal(t, int64(50), rows[0].CPUUsageMC)
	assert.Equal(t, int64(102400), rows[0].MemRequestKiB)
	assert.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), rows[0].IntervalStart)
}

func TestForEachRow_skipsBadNumericAndInvokesCallback(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"namespace,workload,workload_type,container_name,pod,interval_start,interval_end,cpu_request_container_avg,cpu_usage_container_avg,memory_request_container_avg,memory_usage_container_avg",
		"app,api,deployment,api,api-0,2026-08-01 00:00:00 +0000 UTC,2026-08-01 01:00:00 +0000 UTC,0.1,0.05,104857600,52428800",
		"app,api,deployment,api,api-1,2026-08-01 01:00:00 +0000 UTC,2026-08-01 02:00:00 +0000 UTC,NaN,0.05,104857600,52428800",
		"app,api,deployment,api,api-2,2026-08-01 02:00:00 +0000 UTC,2026-08-01 03:00:00 +0000 UTC,0.2,0.1,104857600,52428800",
	}, "\n")
	var got []Row
	skipped, err := ForEachRow(context.Background(), strings.NewReader(csvBody), func(row Row) error {
		got = append(got, row)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 1, skipped)
	require.Len(t, got, 2)
	assert.Equal(t, "api-0", got[0].Pod)
	assert.Equal(t, "api-2", got[1].Pod)
}

func TestParseRows_MissingRequiredColumns(t *testing.T) {
	t.Parallel()
	_, _, err := ParseRows(strings.NewReader("interval_start,namespace\n2026-08-01 00:00:00,app\n"))
	var miss *MissingROSColumnsError
	require.ErrorAs(t, err, &miss)
	assert.Contains(t, miss.Error(), "not a ROS container CSV")
	assert.Contains(t, miss.Columns, "workload")
}

func TestParseRows_SkipsBadNumeric(t *testing.T) {
	t.Parallel()
	csvBody := niseHeader() + "\n" +
		niseRow("app", "api", "2026-08-01 00:00:00 +0000 UTC", "2026-08-01 01:00:00 +0000 UTC", "not-a-number", "0.05") + "\n" +
		niseRow("app", "api", "2026-08-01 01:00:00 +0000 UTC", "2026-08-01 02:00:00 +0000 UTC", "0.2", "0.1") + "\n"
	rows, skipped, err := ParseRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	assert.Equal(t, 1, skipped)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(200), rows[0].CPURequestMC)
}

func TestParseRows_AllRowsUnparseable(t *testing.T) {
	t.Parallel()
	csvBody := niseHeader() + "\n" +
		niseRow("app", "api", "2026-08-01 00:00:00 +0000 UTC", "2026-08-01 01:00:00 +0000 UTC", "not-a-number", "0.05") + "\n"
	rows, skipped, err := ParseRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	assert.Equal(t, 1, skipped)
	assert.Empty(t, rows)
}

func TestParseNamespaceRows_ValidRows(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"interval_start,interval_end,namespace,cpu_request_namespace_sum,cpu_limit_namespace_sum,cpu_usage_namespace_avg,cpu_usage_namespace_max,cpu_usage_namespace_min,cpu_throttle_namespace_avg,cpu_throttle_namespace_max,memory_request_namespace_sum,memory_limit_namespace_sum,memory_usage_namespace_avg,memory_usage_namespace_max,memory_usage_namespace_min,memory_rss_usage_namespace_avg,memory_rss_usage_namespace_max",
		"2026-03-20 00:00:00 +0000 UTC,2026-03-20 01:00:00 +0000 UTC,kube-system,0.500,1.000,0.250,0.400,0.100,0.010,0.020,1073741824,2147483648,536870912,805306368,268435456,268435456,536870912",
		"2026-03-20 01:00:00 +0000 UTC,2026-03-20 02:00:00 +0000 UTC,kube-system,0.600,1.200,0.300,0.500,0.150,0.020,0.040,1073741824,2147483648,536870912,805306368,268435456,268435456,536870912",
	}, "\n")
	rows, skipped, err := ParseNamespaceRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Len(t, rows, 2)
	r := rows[0]
	assert.Equal(t, "kube-system", r.Namespace)
	assert.Equal(t, int64(500), r.CPURequestMC)
	assert.Equal(t, int64(1000), r.CPULimitMC)
	assert.Equal(t, int64(250), r.CPUUsageMC)
	assert.Equal(t, int64(400), r.CPUUsageMaxMC)
	assert.Equal(t, int64(1048576), r.MemRequestKiB)
	assert.Equal(t, int64(524288), r.MemUsageKiB)
	assert.Equal(t, time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC), r.IntervalStart)
}

func TestParseNamespaceRows_MissingRequiredColumn(t *testing.T) {
	t.Parallel()
	_, _, err := ParseNamespaceRows(strings.NewReader("interval_start,interval_end,namespace,cpu_request_namespace_sum\n"))
	var miss *MissingNamespaceColumnsError
	require.ErrorAs(t, err, &miss)
	assert.Contains(t, miss.Error(), "not a ROS namespace CSV")
	assert.Contains(t, miss.Columns, "cpu_usage_namespace_avg")
}

func TestParseNamespaceRows_EmptyCSV(t *testing.T) {
	t.Parallel()
	rows, skipped, err := ParseNamespaceRows(strings.NewReader(""))
	require.NoError(t, err)
	assert.Zero(t, skipped)
	assert.Nil(t, rows)
}

func TestParseNamespaceRows_SkipsBadTimestamp(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"interval_start,interval_end,namespace,cpu_request_namespace_sum,cpu_usage_namespace_avg,memory_request_namespace_sum,memory_usage_namespace_avg",
		"bad-date,2026-03-20 01:00:00 +0000 UTC,ns1,0.500,0.250,1073741824,536870912",
		"2026-03-20 01:00:00 +0000 UTC,2026-03-20 02:00:00 +0000 UTC,ns1,0.600,0.300,1073741824,536870912",
	}, "\n")
	rows, skipped, err := ParseNamespaceRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	assert.Equal(t, 1, skipped)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(600), rows[0].CPURequestMC)
}

func TestParseNamespaceRows_OptionalColumnsAbsent(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"interval_start,interval_end,namespace,cpu_request_namespace_sum,cpu_usage_namespace_avg,memory_request_namespace_sum,memory_usage_namespace_avg",
		"2026-03-20 00:00:00 +0000 UTC,2026-03-20 01:00:00 +0000 UTC,ns-minimal,0.500,0.250,1073741824,536870912",
	}, "\n")
	rows, skipped, err := ParseNamespaceRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(0), rows[0].CPULimitMC)
	assert.Equal(t, int64(0), rows[0].CPUUsageMaxMC)
	assert.Equal(t, int64(0), rows[0].MemRSSKiB)
	assert.Empty(t, rows[0].QuotaName)
	assert.Zero(t, rows[0].CPURequestUsedMC)
	assert.Zero(t, rows[0].StorageRequestHardBytes)
	assert.Zero(t, rows[0].PodsHard)
}

func TestParseNamespaceRows_OptionalQuotaColumns(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"interval_start,interval_end,namespace,quota_name,cpu_request_namespace_sum,cpu_limit_namespace_sum,cpu_usage_namespace_avg,memory_request_namespace_sum,memory_limit_namespace_sum,memory_usage_namespace_avg,cpu_request_namespace_used,cpu_limit_namespace_used,memory_request_namespace_used,memory_limit_namespace_used,storage_request_namespace_hard,storage_request_namespace_used,pods_namespace_hard,pods_namespace_used,object_count_namespace_hard,object_count_namespace_used",
		"2026-03-20 00:00:00 +0000 UTC,2026-03-20 01:00:00 +0000 UTC,app,compute-resources,2.000,4.000,0.250,2147483648,4294967296,536870912,1.000,2.000,1073741824,2147483648,10737418240,5368709120,20,8,50,12",
	}, "\n")
	rows, skipped, err := ParseNamespaceRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Len(t, rows, 1)
	r := rows[0]
	assert.Equal(t, "app", r.Namespace)
	assert.Equal(t, "compute-resources", r.QuotaName)
	assert.Equal(t, int64(2000), r.CPURequestMC)
	assert.Equal(t, int64(4000), r.CPULimitMC)
	assert.Equal(t, int64(1000), r.CPURequestUsedMC)
	assert.Equal(t, int64(2000), r.CPULimitUsedMC)
	assert.Equal(t, int64(1073741824), r.MemoryRequestUsedBytes)
	assert.Equal(t, int64(2147483648), r.MemoryLimitUsedBytes)
	assert.Equal(t, int64(10737418240), r.StorageRequestHardBytes)
	assert.Equal(t, int64(5368709120), r.StorageRequestUsedBytes)
	assert.Equal(t, int64(20), r.PodsHard)
	assert.Equal(t, int64(8), r.PodsUsed)
	assert.Equal(t, int64(50), r.ObjectCountHard)
	assert.Equal(t, int64(12), r.ObjectCountUsed)
}

func TestParseNamespaceRows_QuotaNameAlias(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"interval_start,interval_end,namespace,resource_quota_name,cpu_request_namespace_sum,cpu_usage_namespace_avg,memory_request_namespace_sum,memory_usage_namespace_avg",
		"2026-03-20 00:00:00 +0000 UTC,2026-03-20 01:00:00 +0000 UTC,app,team-quota,0.500,0.250,1073741824,536870912",
	}, "\n")
	rows, skipped, err := ParseNamespaceRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Len(t, rows, 1)
	assert.Equal(t, "team-quota", rows[0].QuotaName)
}

func TestLatestNamespaceQuotaSnapshots_MaxPerDayThenLatestDay(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"interval_start,interval_end,namespace,quota_name,cpu_request_namespace_sum,cpu_usage_namespace_avg,memory_request_namespace_sum,memory_usage_namespace_avg,cpu_request_namespace_used,pods_namespace_hard",
		"2026-03-20 00:00:00 +0000 UTC,2026-03-20 01:00:00 +0000 UTC,app,compute,1.000,0.100,1073741824,536870912,0.400,10",
		"2026-03-20 01:00:00 +0000 UTC,2026-03-20 02:00:00 +0000 UTC,app,compute,2.000,0.200,1073741824,536870912,0.800,20",
		"2026-03-21 00:00:00 +0000 UTC,2026-03-21 01:00:00 +0000 UTC,app,compute,3.000,0.300,1073741824,536870912,1.500,30",
	}, "\n")
	rows, skipped, err := ParseNamespaceRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	snaps := LatestNamespaceQuotaSnapshots(rows)
	require.Len(t, snaps, 1)
	assert.Equal(t, "app", snaps[0].Namespace)
	assert.Equal(t, "compute", snaps[0].QuotaName)
	assert.Equal(t, int64(3000), snaps[0].CPURequestHardMC)
	assert.Equal(t, int64(1500), snaps[0].CPURequestUsedMC)
	assert.Equal(t, int64(30), snaps[0].PodsHard)
	assert.Equal(t, time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC), snaps[0].LastObservedAt)
	daily := DailyNamespaceQuotaDigests(rows)
	require.Len(t, daily, 2)
	assert.Equal(t, time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC), daily[0].LastObservedAt)
	assert.Equal(t, int64(2000), daily[0].CPURequestHardMC)
	assert.Equal(t, time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC), daily[1].LastObservedAt)
	assert.Equal(t, int64(3000), daily[1].CPURequestHardMC)
}

func TestLatestNamespaceQuotaSnapshots_SkipsEmptyQuotaName(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"interval_start,interval_end,namespace,cpu_request_namespace_sum,cpu_usage_namespace_avg,memory_request_namespace_sum,memory_usage_namespace_avg",
		"2026-03-20 00:00:00 +0000 UTC,2026-03-20 01:00:00 +0000 UTC,app,2.000,0.250,1073741824,536870912",
	}, "\n")
	rows, skipped, err := ParseNamespaceRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	assert.Empty(t, LatestNamespaceQuotaSnapshots(rows))
}

func TestLatestNamespaceQuotaSnapshots_PerQuotaName(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"interval_start,interval_end,namespace,quota_name,cpu_request_namespace_sum,cpu_usage_namespace_avg,memory_request_namespace_sum,memory_usage_namespace_avg",
		"2026-03-20 00:00:00 +0000 UTC,2026-03-20 01:00:00 +0000 UTC,app,cpu-quota,1.000,0.100,1073741824,536870912",
		"2026-03-20 00:00:00 +0000 UTC,2026-03-20 01:00:00 +0000 UTC,app,mem-quota,4.000,0.100,4294967296,536870912",
	}, "\n")
	rows, skipped, err := ParseNamespaceRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	snaps := LatestNamespaceQuotaSnapshots(rows)
	require.Len(t, snaps, 2)
	assert.Equal(t, "cpu-quota", snaps[0].QuotaName)
	assert.Equal(t, int64(1000), snaps[0].CPURequestHardMC)
	assert.Equal(t, "mem-quota", snaps[1].QuotaName)
	assert.Equal(t, int64(4000), snaps[1].CPURequestHardMC)
}

func TestDailyDigests_GroupsByDayAndSorts(t *testing.T) {
	t.Parallel()
	csvBody := niseHeader() + "\n" +
		niseRow("app", "api", "2026-08-01 00:00:00 +0000 UTC", "2026-08-01 01:00:00 +0000 UTC", "0.1", "0.05") + "\n" +
		niseRow("app", "api", "2026-08-01 01:00:00 +0000 UTC", "2026-08-01 02:00:00 +0000 UTC", "0.3", "0.15") + "\n" +
		niseRow("ns2", "web", "2026-08-01 00:00:00 +0000 UTC", "2026-08-01 01:00:00 +0000 UTC", "0.2", "0.1") + "\n"
	rows, skipped, err := ParseRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	digests, ds, err := DailyDigests(rows)
	require.NoError(t, err)
	require.Len(t, digests, 2)
	assert.Equal(t, "api", digests[0].Key.Workload)
	assert.Equal(t, "web", digests[1].Key.Workload)
	assert.Equal(t, int64(2), digests[0].Row.SampleCount)
	assert.Equal(t, time.Date(2026, 8, 1, 2, 0, 0, 0, time.UTC), ds.MaxEnd)
}

func TestDailyNamespaceDigests_GroupsByNamespaceAndDay(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"interval_start,interval_end,namespace,cpu_request_namespace_sum,cpu_usage_namespace_avg,cpu_usage_namespace_max,memory_request_namespace_sum,memory_usage_namespace_avg,memory_usage_namespace_max",
		"2026-03-20 00:00:00 +0000 UTC,2026-03-20 01:00:00 +0000 UTC,kube-system,0.500,0.250,0.400,1073741824,536870912,805306368",
		"2026-03-20 01:00:00 +0000 UTC,2026-03-20 02:00:00 +0000 UTC,kube-system,0.600,0.300,0.500,1073741824,536870912,805306368",
		"2026-03-20 00:00:00 +0000 UTC,2026-03-20 01:00:00 +0000 UTC,app,0.200,0.100,0.150,1073741824,536870912,805306368",
		"2026-03-21 00:00:00 +0000 UTC,2026-03-21 01:00:00 +0000 UTC,kube-system,0.700,0.350,0.450,1073741824,536870912,805306368",
	}, "\n")
	rows, skipped, err := ParseNamespaceRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	grouped, ds, err := DailyNamespaceDigests(rows)
	require.NoError(t, err)
	require.Len(t, grouped, 2)
	sys := grouped["kube-system"]
	require.Len(t, sys, 2)
	assert.Equal(t, time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC), sys[0].BucketDate)
	assert.Equal(t, time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC), sys[1].BucketDate)
	assert.Equal(t, int64(2), sys[0].SampleCount)
	assert.Equal(t, int64(500), sys[0].CPUUsageMaxMC, "max of per-interval max column")
	assert.Equal(t, int64(1), grouped["app"][0].SampleCount)
	assert.Equal(t, time.Date(2026, 3, 21, 1, 0, 0, 0, time.UTC), ds.MaxEnd)
}

func TestDailyNamespaceDigests_MaxFallbackToAvg(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"interval_start,interval_end,namespace,cpu_request_namespace_sum,cpu_usage_namespace_avg,memory_request_namespace_sum,memory_usage_namespace_avg",
		"2026-03-20 00:00:00 +0000 UTC,2026-03-20 01:00:00 +0000 UTC,app,0.100,0.050,1073741824,536870912",
		"2026-03-20 01:00:00 +0000 UTC,2026-03-20 02:00:00 +0000 UTC,app,0.200,0.080,1073741824,1073741824",
	}, "\n")
	rows, skipped, err := ParseNamespaceRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	grouped, _, err := DailyNamespaceDigests(rows)
	require.NoError(t, err)
	day := grouped["app"][0]
	assert.Equal(t, int64(80), day.CPUUsageMaxMC)
	assert.Equal(t, int64(1048576), day.MemUsageMaxKiB)
}

func TestParseRows_OptionalNodeGPUColumns(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"interval_start,interval_end,namespace,workload,workload_type,container_name,pod,node,node_capacity_cpu_cores,node_capacity_memory_bytes,node_allocatable_cpu_cores,node_allocatable_memory_bytes,node_allocatable_gpu_count,node_capacity_pods,machineset_name,instance_type,accelerator_model_name,accelerator_profile_name,accelerator_frame_buffer_usage_min,accelerator_frame_buffer_usage_max,accelerator_frame_buffer_usage_avg,sm_active_avg,gpu_uuid,cpu_request_container_avg,cpu_usage_container_avg,memory_request_container_avg,memory_usage_container_avg",
		"2026-08-01 00:00:00 +0000 UTC,2026-08-01 01:00:00 +0000 UTC,app,api,deployment,api,api-0,worker-1,4,8589934592,3.72,8053063680,2,110,workers,m5.xlarge,NVIDIA A100-SXM4-80GB,1g.10gb,1024.5,2048,1536,0.25,GPU-aaa,0.1,0.05,104857600,52428800",
	}, "\n")
	rows, skipped, err := ParseRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Len(t, rows, 1)
	r := rows[0]
	assert.Equal(t, "worker-1", r.Node)
	assert.Equal(t, int64(4000), r.NodeCapacityCPUMC)
	assert.Equal(t, int64(8388608), r.NodeCapacityMemKiB)
	assert.Equal(t, int64(3720), r.NodeAllocatableCPUMC)
	assert.Equal(t, int64(7864320), r.NodeAllocatableMemKiB)
	assert.Equal(t, int64(2), r.NodeAllocatableGPUCount)
	assert.Equal(t, int64(110), r.NodePodCapacity)
	assert.Equal(t, "workers", r.MachineSetName)
	assert.Equal(t, "m5.xlarge", r.InstanceType)
	assert.Equal(t, "NVIDIA A100-SXM4-80GB", r.GPUModel)
	assert.Equal(t, "1g.10gb", r.GPUProfile)
	assert.InDelta(t, 1024.5, r.FBUsageMinMiB, 1e-9)
	assert.InDelta(t, 0.25, r.SMActiveAvg, 1e-9)
	assert.Equal(t, "GPU-aaa", r.GPUUUID)
	assert.True(t, r.HasGPU())
}

func TestParseRows_OptionalColumnsMissingStayZero(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		niseHeader(),
		niseRow("app", "api", "2026-08-01 00:00:00 +0000 UTC", "2026-08-01 01:00:00 +0000 UTC", "0.1", "0.05"),
	}, "\n")
	rows, skipped, err := ParseRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Len(t, rows, 1)
	assert.Zero(t, rows[0].NodeAllocatableCPUMC)
	assert.False(t, rows[0].HasGPU())
	assert.Empty(t, rows[0].GPUUUID)
}

func TestDailyNodeDigests_SumsHourAndSkipsEmptyNode(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	rows := []Row{
		{IntervalStart: day, Node: "worker-1", Pod: "a", CPURequestMC: 100, CPUUsageMC: 50, MemRequestKiB: 1024, MemUsageKiB: 512, NodeCapacityCPUMC: 4000},
		{IntervalStart: day, Node: "worker-1", Pod: "b", CPURequestMC: 200, CPUUsageMC: 80, MemRequestKiB: 2048, MemUsageKiB: 256, NodeCapacityCPUMC: 4000},
		{IntervalStart: day.Add(time.Hour), Node: "worker-1", Pod: "a", CPURequestMC: 100, CPUUsageMC: 40, MemRequestKiB: 1024, MemUsageKiB: 400, NodeCapacityCPUMC: 4000},
		{IntervalStart: day, Node: "", Pod: "orphan", CPURequestMC: 999, CPUUsageMC: 999},
	}
	got := DailyNodeDigests(rows, 0.93)
	require.Len(t, got, 1)
	assert.Equal(t, "worker-1", got[0].Node)
	assert.Equal(t, day, got[0].BucketDate)
	assert.Equal(t, int64(300), got[0].MaxCPURequestsMC)
	assert.Equal(t, int64(2), got[0].MaxPodCount)
	assert.Equal(t, int64(2), got[0].SampleCount)
	require.NotNil(t, got[0].MaxCPUAllocMC)
	assert.Equal(t, int64(3720), *got[0].MaxCPUAllocMC)
	assert.Equal(t, int64(40), got[0].CPUUsageP50MC) // hour0=130, hour1=40 → sorted 40,130; p50 idx 0
	assert.Equal(t, int64(130), got[0].CPUUsageMaxMC)
}

func TestDailyNodeDigests_PrefersObservedAllocatable(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	got := DailyNodeDigests([]Row{{
		IntervalStart: day, Node: "n1", CPUUsageMC: 10, NodeCapacityCPUMC: 4000, NodeAllocatableCPUMC: 3500,
	}}, 0.93)
	require.Len(t, got, 1)
	require.NotNil(t, got[0].MaxCPUAllocMC)
	assert.Equal(t, int64(3500), *got[0].MaxCPUAllocMC)
}

func TestDailyNodeDigestsWeighted_DropsZeroAndScalesUsage(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	rows := []Row{
		{IntervalStart: day, Node: "worker-1", Pod: "a", CPUUsageMC: 100, CPURequestMC: 200, MemUsageKiB: 400, MemRequestKiB: 800, NodeCapacityCPUMC: 4000},
		{IntervalStart: day.Add(time.Hour), Node: "worker-1", Pod: "a", CPUUsageMC: 80, CPURequestMC: 200, MemUsageKiB: 300, MemRequestKiB: 800, NodeCapacityCPUMC: 4000},
	}
	dropped := DailyNodeDigestsWeighted(rows, 0.93, func(time.Time) float64 { return 0 })
	require.Empty(t, dropped)

	half := DailyNodeDigestsWeighted(rows, 0.93, func(time.Time) float64 { return 0.5 })
	require.Len(t, half, 1)
	assert.Equal(t, int64(2), half[0].SampleCount)
	assert.Equal(t, int64(40), half[0].CPUUsageP50MC) // hour0=50, hour1=40
	assert.Equal(t, int64(100), half[0].MaxCPURequestsMC)
	require.NotNil(t, half[0].MaxCPUAllocMC)
	assert.Equal(t, int64(3720), *half[0].MaxCPUAllocMC, "capacity fallback must stay unscaled")
}

func TestDailyGPUDigests_SkipsNoModelAndCountsUUIDs(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	rows := []Row{
		{IntervalStart: day, Namespace: "app", WorkloadName: "api", ContainerName: "api", Node: "gpu-1", GPUModel: "NVIDIA A100-SXM4-80GB", GPUUUID: "GPU-a", FBUsageAvgMiB: 100, SMActiveAvg: 0.10},
		{IntervalStart: day.Add(time.Hour), Namespace: "app", WorkloadName: "api", ContainerName: "api", Node: "gpu-1", GPUModel: "NVIDIA A100-SXM4-80GB", GPUUUID: "GPU-b", FBUsageAvgMiB: 200, SMActiveAvg: 0.30},
		{IntervalStart: day, Namespace: "app", WorkloadName: "cpu", ContainerName: "cpu", Node: "cpu-1"},
	}
	ds := DailyGPUDigests(rows)
	ck := gpu.GPUContainerKey{Namespace: "app", Workload: "api", ContainerName: "api"}
	require.Len(t, ds.Grouped, 1)
	require.Len(t, ds.Grouped[ck], 1)
	d := ds.Grouped[ck][0]
	assert.Equal(t, day, d.IntervalStart)
	assert.Equal(t, "gpu-1", d.NodeName)
	assert.Equal(t, 2, d.GPUCount)
	assert.Equal(t, int32(150), d.FBUsageAvgMiB)
	assert.Equal(t, "gpu-1", ds.NodeMap[ck])
}

func TestDailyGPUDigests_MissingUUIDCountsOne(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	ds := DailyGPUDigests([]Row{{
		IntervalStart: day, Namespace: "app", WorkloadName: "api", ContainerName: "api", GPUModel: "A100",
	}})
	ck := gpu.GPUContainerKey{Namespace: "app", Workload: "api", ContainerName: "api"}
	require.Len(t, ds.Grouped[ck], 1)
	assert.Equal(t, 1, ds.Grouped[ck][0].GPUCount)
}

func TestDailyGPUDigestsWeighted_DropsNonPositiveWeight(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	rows := []Row{
		{IntervalStart: day, Namespace: "app", WorkloadName: "api", ContainerName: "api", GPUModel: "A100", FBUsageAvgMiB: 100, SMActiveAvg: 0.10},
		{IntervalStart: day.Add(time.Hour), Namespace: "app", WorkloadName: "api", ContainerName: "api", GPUModel: "A100", FBUsageAvgMiB: 900, SMActiveAvg: 0.90},
	}
	dropped := DailyGPUDigestsWeighted(rows, func(r Row) float64 {
		if r.IntervalStart.Hour() == 1 {
			return 0
		}
		return 1
	})
	ck := gpu.GPUContainerKey{Namespace: "app", Workload: "api", ContainerName: "api"}
	require.Len(t, dropped.Grouped[ck], 1)
	assert.Equal(t, int32(100), dropped.Grouped[ck][0].FBUsageAvgMiB)

	none := DailyGPUDigestsWeighted(rows, func(Row) float64 { return 0 })
	assert.Empty(t, none.Grouped)
}

func TestDailyGPUDigestsWeighted_FractionalWeightIsFullVote(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	rows := []Row{
		{IntervalStart: day, Namespace: "app", WorkloadName: "api", ContainerName: "api", GPUModel: "A100", FBUsageMaxMiB: 100, FBUsageAvgMiB: 100},
		{IntervalStart: day.Add(time.Hour), Namespace: "app", WorkloadName: "api", ContainerName: "api", GPUModel: "A100", FBUsageMaxMiB: 900, FBUsageAvgMiB: 900},
	}
	got := DailyGPUDigestsWeighted(rows, func(r Row) float64 {
		if r.IntervalStart.Hour() == 1 {
			return 0.25
		}
		return 1
	})
	ck := gpu.GPUContainerKey{Namespace: "app", Workload: "api", ContainerName: "api"}
	require.Len(t, got.Grouped[ck], 1)
	assert.Equal(t, int32(900), got.Grouped[ck][0].FBUsageMaxMiB, "0.25 is drop-or-full, not scaled")
	assert.Equal(t, int32(500), got.Grouped[ck][0].FBUsageAvgMiB, "mean of unscaled 100 and 900")
}

func TestUniqueClusterIDs(t *testing.T) {
	t.Parallel()
	ids := UniqueClusterIDs([]Row{
		{ClusterID: "b"},
		{ClusterID: ""},
		{ClusterID: "a"},
		{ClusterID: "b"},
	})
	assert.Equal(t, []string{"a", "b"}, ids)
}

func TestParsePVCRows_BasicCSV(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"report_period_start,interval_start,interval_end,namespace,pod,persistentvolumeclaim,persistentvolume,storageclass,persistentvolumeclaim_capacity_bytes,volume_request_storage_byte_seconds,persistentvolumeclaim_usage_byte_seconds",
		"2026-05-01 00:00:00+00:00,2026-05-01 00:00:00+00:00,2026-05-01 01:00:00+00:00,production,app-pod-1,data-pvc,pv-data,gp3,10737418240,36000000000000,18000000000000",
	}, "\n")
	rows, skipped, err := ParsePVCRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Len(t, rows, 1)
	assert.Equal(t, "production", rows[0].Namespace)
	assert.Equal(t, "data-pvc", rows[0].PersistentVolumeClaim)
	assert.Equal(t, "app-pod-1", rows[0].Pod)
	assert.Equal(t, "pv-data", rows[0].PersistentVolume)
	assert.Equal(t, "gp3", rows[0].StorageClass)
	assert.Equal(t, int64(10737418240), rows[0].CapacityBytes)
	assert.Equal(t, int64(18000000000000), rows[0].UsageByteSeconds)
	assert.Equal(t, time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), rows[0].IntervalStart)
}

func TestParsePVCRows_MissingRequiredColumns(t *testing.T) {
	t.Parallel()
	_, _, err := ParsePVCRows(strings.NewReader("some_column,another_column\nval1,val2\n"))
	var miss *MissingStorageColumnsError
	require.ErrorAs(t, err, &miss)
	assert.Contains(t, miss.Error(), "not a storage CSV")
	assert.Contains(t, miss.Columns, "persistentvolumeclaim")
}

func TestParsePVCRows_EmptyPVCNameSkipped(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"interval_start,namespace,persistentvolumeclaim",
		"2026-05-01 00:00:00+00:00,ns1,",
	}, "\n")
	rows, skipped, err := ParsePVCRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	assert.Zero(t, skipped)
	assert.Empty(t, rows)
}

func TestParsePVCRows_OptionalVMNameAbsent(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"interval_start,interval_end,namespace,pod,persistentvolumeclaim",
		"2026-05-01 00:00:00+00:00,2026-05-01 01:00:00+00:00,kubevirt,virt-launcher-fedora-vm-x9y8z,vm-disk",
	}, "\n")
	rows, skipped, err := ParsePVCRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Len(t, rows, 1)
	assert.Empty(t, rows[0].VMName)
	assert.Equal(t, "virt-launcher-fedora-vm-x9y8z", rows[0].Pod)
}

func TestParsePVCRows_SkipsBadTimestamp(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"interval_start,namespace,persistentvolumeclaim",
		"bad-date,ns1,pvc-1",
		"2026-05-01 00:00:00+00:00,ns1,pvc-1",
	}, "\n")
	rows, skipped, err := ParsePVCRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	assert.Equal(t, 1, skipped)
	require.Len(t, rows, 1)
	assert.Equal(t, "pvc-1", rows[0].PersistentVolumeClaim)
}

func TestDailyPVCDigests_BasicAggregation(t *testing.T) {
	t.Parallel()
	rows := []PVCRow{
		{
			IntervalStart:         time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
			IntervalEnd:           time.Date(2026, 5, 1, 1, 0, 0, 0, time.UTC),
			Namespace:             "prod",
			PersistentVolumeClaim: "data-pvc",
			PersistentVolume:      "pv-1",
			StorageClass:          "gp3",
			CapacityBytes:         10 << 30,
			UsageByteSeconds:      3600 * 5e9,
		},
		{
			IntervalStart:         time.Date(2026, 5, 1, 1, 0, 0, 0, time.UTC),
			IntervalEnd:           time.Date(2026, 5, 1, 2, 0, 0, 0, time.UTC),
			Namespace:             "prod",
			Pod:                   "virt-launcher-my-vm-abc12",
			PersistentVolumeClaim: "data-pvc",
			PersistentVolume:      "pv-1",
			StorageClass:          "gp3",
			CapacityBytes:         10 << 30,
			UsageByteSeconds:      3600 * 7e9,
		},
	}
	grouped, ds := DailyPVCDigests(rows)
	require.False(t, ds.MaxEnd.IsZero())
	key := pvc.PVCKey{Namespace: "prod", PVC: "data-pvc"}
	require.Len(t, grouped[key], 1)
	d := grouped[key][0]
	assert.Equal(t, "virt-launcher-my-vm-abc12", d.LastSeenPod)
	assert.Equal(t, 2, d.SampleCount)
	assert.Equal(t, int64(5e9), d.UsageBytesMin)
	assert.Equal(t, int64(7e9), d.UsageBytesMax)
	assert.Equal(t, int64(10<<30), d.CapacityBytes)
}

func TestDailyPVCDigests_MultipleDays(t *testing.T) {
	t.Parallel()
	rows := []PVCRow{
		{
			IntervalStart:         time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC),
			IntervalEnd:           time.Date(2026, 5, 1, 11, 0, 0, 0, time.UTC),
			Namespace:             "prod",
			PersistentVolumeClaim: "pvc-a",
			CapacityBytes:         10 << 30,
			UsageByteSeconds:      3600 * 1e9,
		},
		{
			IntervalStart:         time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
			IntervalEnd:           time.Date(2026, 5, 2, 11, 0, 0, 0, time.UTC),
			Namespace:             "prod",
			PersistentVolumeClaim: "pvc-a",
			CapacityBytes:         10 << 30,
			UsageByteSeconds:      3600 * 2e9,
		},
	}
	grouped, _ := DailyPVCDigests(rows)
	key := pvc.PVCKey{Namespace: "prod", PVC: "pvc-a"}
	require.Len(t, grouped[key], 2)
	assert.True(t, grouped[key][0].BucketDate.Before(grouped[key][1].BucketDate))
}

func TestDailyPVCDigests_Empty(t *testing.T) {
	t.Parallel()
	grouped, ds := DailyPVCDigests(nil)
	assert.Empty(t, grouped)
	assert.True(t, ds.MaxEnd.IsZero())
}

func TestParseVMRows_ValidRequiredColumns(t *testing.T) {
	t.Parallel()
	csvBody := vmUsageHeader() + "\n" +
		"2026-05-01T12:00:00Z,2026-05-01T12:15:00Z,web-vm,production,worker-1,linux,1500,2000,4000,1048576,2097152,1572864,107374182400,53687091200,107374182400,120,80,1048576,524288\n"
	rows, skipped, err := ParseVMRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Len(t, rows, 1)
	r := rows[0]
	assert.Equal(t, "web-vm", r.VMName)
	assert.Equal(t, "production", r.Namespace)
	assert.Equal(t, "worker-1", r.NodeName)
	assert.Equal(t, "linux", r.GuestOS)
	assert.InDelta(t, 1500, r.CPUUsageMC, 0.001)
	assert.InDelta(t, 2000, r.CPURequestMC, 0.001)
	require.NotNil(t, r.MemoryAvailableKiB)
	assert.InDelta(t, 1572864, *r.MemoryAvailableKiB, 0.001)
	assert.Equal(t, time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC), r.IntervalStart)
	assert.Equal(t, time.Date(2026, 5, 1, 12, 15, 0, 0, time.UTC), r.IntervalEnd)
}

func TestParseVMRows_MissingRequiredColumn(t *testing.T) {
	t.Parallel()
	_, _, err := ParseVMRows(strings.NewReader("interval_start,vm_name,namespace\n"))
	var miss *MissingVMColumnsError
	require.ErrorAs(t, err, &miss)
	assert.Contains(t, miss.Error(), "not a VM usage CSV")
	assert.Contains(t, miss.Columns, "interval_end")
	assert.Contains(t, miss.Columns, "cpu_usage_mc")
}

func TestParseVMRows_SkipsEmptyNameAndBadTimestamp(t *testing.T) {
	t.Parallel()
	csvBody := vmUsageHeader() + "\n" +
		"not-a-timestamp,2026-05-01T12:15:00Z,bad-vm,ns,node,linux,100,200,300,1024,2048,,1000,,,,,,\n" +
		"2026-05-01T12:00:00Z,2026-05-01T12:15:00Z,,ns,node,linux,100,200,300,1024,2048,,1000,,,,,,\n" +
		"2026-05-01T12:00:00Z,2026-05-01T12:15:00Z,good-vm,ns,node,linux,100,200,300,1024,2048,,1000,,,,,,\n"
	rows, skipped, err := ParseVMRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	assert.Equal(t, 2, skipped)
	require.Len(t, rows, 1)
	assert.Equal(t, "good-vm", rows[0].VMName)
}

func TestParseVMPVCRows_ValidAndMissingColumns(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"interval_start,vm_name,namespace,pvc_name,disk_capacity_bytes,volume_mode",
		"2026-05-01T12:00:00Z,web-vm,production,data-pvc,10737418240,Filesystem",
	}, "\n")
	rows, skipped, err := ParseVMPVCRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Len(t, rows, 1)
	assert.Equal(t, "data-pvc", rows[0].PVCName)
	assert.Equal(t, int64(10737418240), rows[0].DiskCapacityBytes)

	_, _, err = ParseVMPVCRows(strings.NewReader("interval_start,vm_name\n"))
	var miss *MissingVMPVCColumnsError
	require.ErrorAs(t, err, &miss)
	assert.Contains(t, miss.Columns, "pvc_name")
}

func TestParseVMGPURows_ValidAndMissingColumns(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"interval_start,namespace,vm_name,gpu_uuid,gpu_model,utilization_avg,utilization_max,fb_used_avg_mib,fb_used_max_mib,sm_active_avg,tensor_active_avg,dram_active_avg,mig_profile,max_slices",
		"2026-05-01T12:00:00Z,production,web-vm,GPU-1,A100,0.4,0.8,1000,2000,0.3,0.2,0.1,1g.5gb,7",
	}, "\n")
	rows, skipped, err := ParseVMGPURows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Len(t, rows, 1)
	assert.Equal(t, "GPU-1", rows[0].GPUUUID)
	assert.Equal(t, "A100", rows[0].GPUModel)
	assert.Equal(t, int32(7), rows[0].MaxSlices)

	_, _, err = ParseVMGPURows(strings.NewReader("interval_start,vm_name\n"))
	var miss *MissingVMGPUColumnsError
	require.ErrorAs(t, err, &miss)
	assert.Contains(t, miss.Columns, "gpu_uuid")
}

func TestDailyVMDigests_GroupsByVMAndDay(t *testing.T) {
	t.Parallel()
	csvBody := vmUsageHeader() + "\n" +
		"2026-05-01T00:00:00Z,2026-05-01T01:00:00Z,web-vm,production,worker-1,linux,1000,2000,4000,1048576,2097152,,10737418240,,,,,,\n" +
		"2026-05-01T01:00:00Z,2026-05-01T02:00:00Z,web-vm,production,worker-1,linux,1500,2000,4000,1048576,2097152,,10737418240,,,,,,\n" +
		"2026-05-02T00:00:00Z,2026-05-02T01:00:00Z,web-vm,production,worker-1,linux,2000,2000,4000,1048576,2097152,,10737418240,,,,,,\n" +
		"2026-05-01T00:00:00Z,2026-05-01T01:00:00Z,other-vm,apps,worker-2,linux,500,1000,2000,524288,1048576,,1073741824,,,,,,\n"
	rows, skipped, err := ParseVMRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	digests, ds := DailyVMDigests(rows, nil, nil)
	require.False(t, ds.MaxEnd.IsZero())
	require.Len(t, digests, 3)
	web := filterVMDigests(digests, "production", "web-vm")
	require.Len(t, web, 2)
	assert.True(t, web[0].BucketDate.Before(web[1].BucketDate))
	assert.Equal(t, int32(2), web[0].SampleCount)
	assert.Equal(t, int64(2000), web[0].CPURequestMC)
	assert.Equal(t, "linux", web[0].GuestOS)
}

func TestDailyVMDigests_AttachesCompanions(t *testing.T) {
	t.Parallel()
	usage := []VMRow{{
		IntervalStart:      time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		IntervalEnd:        time.Date(2026, 5, 1, 1, 0, 0, 0, time.UTC),
		VMName:             "web-vm",
		Namespace:          "production",
		CPUUsageMC:         1000,
		CPURequestMC:       2000,
		CPULimitMC:         4000,
		MemoryUsageKiB:     1048576,
		MemoryRequestKiB:   2097152,
		DiskAllocatedBytes: 10737418240,
	}}
	pvcRows := []VMPVCRow{{
		IntervalStart:     time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		VMName:            "web-vm",
		Namespace:         "production",
		PVCName:           "data-pvc",
		DiskCapacityBytes: 10 << 30,
		VolumeMode:        "Filesystem",
	}}
	gpuRows := []VMGPURow{{
		IntervalStart:  time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		Namespace:      "production",
		VMName:         "web-vm",
		GPUUUID:        "GPU-1",
		GPUModel:       "A100",
		UtilizationAvg: 0.4,
		UtilizationMax: 0.8,
		MaxSlices:      7,
	}}
	digests, _ := DailyVMDigests(usage, pvcRows, gpuRows)
	require.Len(t, digests, 1)
	require.Len(t, digests[0].PVCs, 1)
	assert.Equal(t, "data-pvc", digests[0].PVCs[0].PVCName)
	require.Len(t, digests[0].Devices, 1)
	assert.Equal(t, "GPU-1", digests[0].Devices[0].UUID)
}

func TestDailyVMDigestsWeighted_FractionalWeightIsFullVote(t *testing.T) {
	t.Parallel()
	day := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	rows := []VMRow{
		{IntervalStart: day, VMName: "web-vm", Namespace: "ns", CPUUsageMC: 1000},
		{IntervalStart: day.Add(2 * time.Hour), VMName: "web-vm", Namespace: "ns", CPUUsageMC: 8000},
	}
	full, _ := DailyVMDigestsWeighted(rows, nil, nil, func(t time.Time) float64 {
		if t.Hour() == 2 {
			return 0.25
		}
		return 1
	})
	require.Len(t, full, 1)
	assert.Equal(t, int64(8000), full[0].CPUUsageMaxMC, "0.25 is drop-or-full, not scaled to 2000")
}

func vmUsageHeader() string {
	return "interval_start,interval_end,vm_name,namespace,node_name,guest_os,cpu_usage_mc,cpu_request_mc,cpu_limit_mc,memory_usage_kib,memory_request_kib,memory_available_kib,disk_allocated_bytes,filesystem_used_bytes,filesystem_capacity_bytes,disk_read_iops,disk_write_iops,disk_read_bytes_per_sec,disk_write_bytes_per_sec"
}

func filterVMDigests(digests []vm.DailyVMDigest, ns, name string) []vm.DailyVMDigest {
	var out []vm.DailyVMDigest
	for _, d := range digests {
		if d.Namespace == ns && d.VMName == name {
			out = append(out, d)
		}
	}
	return out
}

func niseHeader() string {
	return "interval_start,interval_end,namespace,workload,workload_type,container_name,pod,cpu_request_container_avg,cpu_usage_container_avg,memory_request_container_avg,memory_usage_container_avg"
}

func niseRow(ns, wl, start, end, cpuReq, cpuUse string) string {
	var b bytes.Buffer
	b.WriteString(start)
	b.WriteByte(',')
	b.WriteString(end)
	b.WriteByte(',')
	b.WriteString(ns)
	b.WriteByte(',')
	b.WriteString(wl)
	b.WriteString(",deployment,")
	b.WriteString(wl)
	b.WriteByte(',')
	b.WriteString(wl)
	b.WriteString("-0,")
	b.WriteString(cpuReq)
	b.WriteByte(',')
	b.WriteString(cpuUse)
	b.WriteString(",104857600,52428800")
	return b.String()
}

func TestParseClusterQuotaRows_OperatorHeader(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"report_period_start,report_period_end,interval_start,interval_end,cluster_quota_name,cpu_request_hard,cpu_request_used,cpu_limit_hard,cpu_limit_used,memory_request_hard,memory_request_used,memory_limit_hard,memory_limit_used,storage_request_hard,storage_request_used,pods_hard,pods_used,object_count_hard,object_count_used,namespaces",
		"2020-11-01 00:00:00 +0000 UTC,2020-12-01 00:00:00 +0000 UTC,2026-08-01 18:00:00 +0000 UTC,2026-08-01 18:59:59 +0000 UTC,team-a,10.000000,3.000000,20.000000,5.000000,1073741824.000000,536870912.000000,2147483648.000000,1073741824.000000,,,,,,,",
	}, "\n")
	rows, skipped, err := ParseClusterQuotaRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Len(t, rows, 1)
	assert.Equal(t, "team-a", rows[0].ClusterQuotaName)
	assert.Equal(t, int64(10000), rows[0].CPURequestHardMC)
	assert.Equal(t, int64(3000), rows[0].CPURequestUsedMC)
	assert.Equal(t, int64(1073741824), rows[0].MemoryRequestHardBytes)
	assert.Equal(t, int64(536870912), rows[0].MemoryRequestUsedBytes)
	assert.Empty(t, rows[0].Namespaces)
}

func TestParseClusterQuotaRows_NISEAliasesAndQuotedNamespaces(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"interval_start,interval_end,cluster_resource_quota,cpu_request_cluster_sum,cpu_request_cluster_used,memory_request_cluster_sum,memory_request_cluster_used,namespaces",
		`2026-08-01 00:00:00 +0000 UTC,2026-08-01 01:00:00 +0000 UTC,team-b,2.000,0.500,2147483648,1073741824,"app,other"`,
	}, "\n")
	rows, skipped, err := ParseClusterQuotaRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Len(t, rows, 1)
	assert.Equal(t, "team-b", rows[0].ClusterQuotaName)
	assert.Equal(t, int64(2000), rows[0].CPURequestHardMC)
	assert.Equal(t, int64(500), rows[0].CPURequestUsedMC)
	assert.Equal(t, int64(2147483648), rows[0].MemoryRequestHardBytes)
	assert.Equal(t, "app,other", rows[0].Namespaces)
}

func TestParseClusterQuotaRows_SkipsEmptyNameAndBadNumeric(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"interval_start,interval_end,cluster_quota_name,cpu_request_hard,memory_request_hard",
		"2026-08-01 00:00:00 +0000 UTC,2026-08-01 01:00:00 +0000 UTC,,1.000,1073741824",
		"2026-08-01 00:00:00 +0000 UTC,2026-08-01 01:00:00 +0000 UTC,ok,not-a-number,1073741824",
		"2026-08-01 00:00:00 +0000 UTC,2026-08-01 01:00:00 +0000 UTC,kept,1.000,1073741824",
	}, "\n")
	rows, skipped, err := ParseClusterQuotaRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	assert.Equal(t, 1, skipped)
	require.Len(t, rows, 1)
	assert.Equal(t, "kept", rows[0].ClusterQuotaName)
}

func TestLatestClusterQuotaSnapshots_MaxPerDayThenLatestHardDay(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"interval_start,interval_end,cluster_quota_name,cpu_request_hard,cpu_request_used,namespaces",
		"2026-08-01 00:00:00 +0000 UTC,2026-08-01 01:00:00 +0000 UTC,team-a,1.000,0.200,app",
		"2026-08-01 01:00:00 +0000 UTC,2026-08-01 02:00:00 +0000 UTC,team-a,2.000,0.400,",
		"2026-08-02 00:00:00 +0000 UTC,2026-08-02 01:00:00 +0000 UTC,team-a,0.000,0.000,",
		"2026-08-01 00:00:00 +0000 UTC,2026-08-01 01:00:00 +0000 UTC,team-b,4.000,1.000,other",
	}, "\n")
	rows, skipped, err := ParseClusterQuotaRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	snaps := LatestClusterQuotaSnapshots(rows)
	require.Len(t, snaps, 2)
	assert.Equal(t, "team-a", snaps[0].ClusterQuotaName)
	assert.Equal(t, int64(2000), snaps[0].CPURequestHardMC)
	assert.Equal(t, int64(400), snaps[0].CPURequestUsedMC)
	assert.Equal(t, "app", snaps[0].Namespaces)
	assert.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), snaps[0].LastObservedAt)
	assert.Equal(t, "team-b", snaps[1].ClusterQuotaName)
	assert.Equal(t, int64(4000), snaps[1].CPURequestHardMC)
	daily := DailyClusterQuotaDigests(rows)
	require.Len(t, daily, 3)
	assert.Equal(t, "team-a", daily[0].ClusterQuotaName)
	assert.Equal(t, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), daily[0].LastObservedAt)
	assert.Equal(t, int64(2000), daily[0].CPURequestHardMC)
	assert.Equal(t, "team-a", daily[1].ClusterQuotaName)
	assert.Equal(t, time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC), daily[1].LastObservedAt)
	assert.Equal(t, int64(0), daily[1].CPURequestHardMC)
	assert.False(t, daily[1].HasHardLimits())
	assert.Equal(t, "team-b", daily[2].ClusterQuotaName)
}

func TestParseSnapshotRows_RequiredColumns(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"namespace,snapshot_name,creation_timestamp,source_pvc_name,restore_size_bytes,source_pvc_exists,restored_pvc_count,interval_start,interval_end",
		"app,snap-a,2026-07-01T00:00:00Z,data-pvc,1073741824,true,0,2026-08-01T00:00:00Z,2026-08-01T01:00:00Z",
	}, "\n")
	rows, skipped, err := ParseSnapshotRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	require.Len(t, rows, 1)
	assert.Equal(t, "app", rows[0].Namespace)
	assert.Equal(t, "snap-a", rows[0].SnapshotName)
	assert.Equal(t, "data-pvc", rows[0].SourcePVCName)
	assert.True(t, rows[0].SourcePVCExists)
	assert.Equal(t, int64(1073741824), rows[0].RestoreSizeBytes)
}

func TestParseSnapshotRows_MissingRequiredColumns(t *testing.T) {
	t.Parallel()
	_, _, err := ParseSnapshotRows(strings.NewReader("namespace,snapshot_name\napp,snap-a\n"))
	var miss *MissingSnapshotColumnsError
	require.ErrorAs(t, err, &miss)
	assert.Contains(t, miss.Columns, "creation_timestamp")
}

func TestLatestSnapshotInventory_KeepsLatestHour(t *testing.T) {
	t.Parallel()
	csvBody := strings.Join([]string{
		"namespace,snapshot_name,creation_timestamp,source_pvc_name,restore_size_bytes,source_pvc_exists,interval_start,interval_end",
		"app,snap-a,2026-07-01T00:00:00Z,data-pvc,100,true,2026-08-01T00:00:00Z,2026-08-01T01:00:00Z",
		"app,snap-a,2026-07-01T00:00:00Z,data-pvc,200,false,2026-08-01T01:00:00Z,2026-08-01T02:00:00Z",
		"app,snap-b,2026-07-02T00:00:00Z,other-pvc,50,true,2026-08-01T00:00:00Z,2026-08-01T01:00:00Z",
	}, "\n")
	rows, skipped, err := ParseSnapshotRows(strings.NewReader(csvBody))
	require.NoError(t, err)
	require.Zero(t, skipped)
	inv := LatestSnapshotInventory(rows)
	require.Len(t, inv, 2)
	assert.Equal(t, "snap-a", inv[0].SnapshotName)
	assert.Equal(t, int64(200), inv[0].RestoreSizeBytes)
	assert.False(t, inv[0].SourcePVCExists)
	assert.Equal(t, "snap-b", inv[1].SnapshotName)
}
