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
		{"ocp_ros_namespace_usage.csv", KindOther},
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
