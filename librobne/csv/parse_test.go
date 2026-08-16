package csv

import (
	"bytes"
	"strings"
	"testing"
	"time"

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
