package ingestion

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseClusterQuotaCSVRows_OperatorColumns(t *testing.T) {
	csv := `interval_start,interval_end,cluster_resource_quota,cpu_request_cluster_sum,cpu_request_cluster_used,memory_request_cluster_sum,memory_request_cluster_used
2026-05-01T00:00:00Z,2026-05-01T01:00:00Z,team-payments,10,4,1073741824,536870912
`
	rows, err := ParseClusterQuotaCSVRows(strings.NewReader(csv))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "team-payments", rows[0].ClusterQuotaName)
	assert.Equal(t, int64(10000), rows[0].CPURequestHardMC)
	assert.Equal(t, int64(4000), rows[0].CPURequestUsedMC)
	assert.Equal(t, int64(1073741824), rows[0].MemoryRequestHardBytes)
	assert.Equal(t, int64(536870912), rows[0].MemoryRequestUsedBytes)
}

func TestParseClusterQuotaCSVRows_NiseColumnNames(t *testing.T) {
	csv := `interval_start,interval_end,cluster_quota_name,cpu_request_hard,cpu_request_used,cpu_limit_hard,cpu_limit_used,memory_request_hard,memory_request_used,memory_limit_hard,memory_limit_used
2026-05-01T00:00:00Z,2026-05-01T01:00:00Z,team-dev,2.5,1,3,1.5,8589934592,4294967296,17179869184,8589934592
`
	rows, err := ParseClusterQuotaCSVRows(strings.NewReader(csv))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(2500), rows[0].CPURequestHardMC)
	assert.Equal(t, int64(1000), rows[0].CPURequestUsedMC)
}

func TestParseClusterQuotaCSVRows_SpecialCharactersInName(t *testing.T) {
	csv := `interval_start,interval_end,cluster_quota_name,cpu_request_hard,cpu_request_used
2026-05-01T00:00:00Z,2026-05-01T01:00:00Z,team.payments-prod_v2,2,1
`
	rows, err := ParseClusterQuotaCSVRows(strings.NewReader(csv))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "team.payments-prod_v2", rows[0].ClusterQuotaName)
}

func TestParseClusterQuotaCSVRows_MissingUsedColumns(t *testing.T) {
	csv := `interval_start,interval_end,cluster_quota_name,cpu_request_hard,memory_request_hard
2026-05-01T00:00:00Z,2026-05-01T01:00:00Z,team-a,1,1048576
`
	rows, err := ParseClusterQuotaCSVRows(strings.NewReader(csv))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, int64(1000), rows[0].CPURequestHardMC)
	assert.Equal(t, int64(0), rows[0].CPURequestUsedMC)
}

func TestParseClusterQuotaCSVRows_MissingRequiredColumns(t *testing.T) {
	csv := `interval_start,cluster_quota_name
2026-05-01T00:00:00Z,team-a
`
	_, err := ParseClusterQuotaCSVRows(strings.NewReader(csv))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing columns")
}

// The nise generator can now emit storage, pod/object counts and namespace
// membership from static YAML (nise #774). These three tests cover the CSV ->
// daily digest hop, which had no coverage for those columns: nothing proved the
// values survive grouping, and a silent drop here would look identical to the
// generator never emitting them.
func TestClusterQuotaDigestKeepsStoragePodsObjectsAndNamespaces(t *testing.T) {
	csvText := `interval_start,interval_end,cluster_quota_name,cpu_request_hard,storage_request_hard,storage_request_used,pods_hard,pods_used,object_count_hard,object_count_used,namespaces
2026-05-01T00:00:00Z,2026-05-01T01:00:00Z,demo-crq,20,1000000,990000,1000,990,2000,1980,demo
2026-05-01T01:00:00Z,2026-05-01T02:00:00Z,demo-crq,20,999000,970000,950,900,1900,1800,
`
	rows, err := ParseClusterQuotaCSVRows(strings.NewReader(csvText))
	require.NoError(t, err)
	require.Len(t, rows, 2)

	groups := make(map[clusterQuotaDigestKey]*clusterQuotaDigestAgg)
	for _, row := range rows {
		addClusterQuotaRowToGroups(groups, row, "1234567", "11111111-1111-1111-1111-111111111111")
	}
	require.Len(t, groups, 1, "same-day intervals must share one digest row")

	for _, agg := range groups {
		// Distinct values per interval: a last-wins or min aggregation fails here,
		// while equal values would pass under either.
		assert.Equal(t, int64(1000000), agg.storageRequestHard)
		assert.Equal(t, int64(990000), agg.storageRequestUsed)
		assert.Equal(t, int64(1000), agg.podsHard)
		assert.Equal(t, int64(990), agg.podsUsed)
		assert.Equal(t, int64(2000), agg.objectCountHard)
		assert.Equal(t, int64(1980), agg.objectCountUsed)
		assert.Equal(t, "demo", agg.namespaces, "a later blank cell must not erase membership")
	}
}

func TestClusterQuotaDigestStoresExplicitZeroAsNull(t *testing.T) {
	// A supplied 0 means "no quota of this kind". It reaches the digest as NULL,
	// which the snapshot reader dereferences back to 0
	// (internal/engine/quota/recommend_cluster_quota.go derefInt64), so the
	// recommendation stays suppressed instead of synthesizing a default.
	csvText := `interval_start,interval_end,cluster_quota_name,cpu_request_hard,object_count_hard,object_count_used
2026-05-01T00:00:00Z,2026-05-01T01:00:00Z,no-objects-crq,20,0,0
`
	rows, err := ParseClusterQuotaCSVRows(strings.NewReader(csvText))
	require.NoError(t, err)
	require.Len(t, rows, 1)

	groups := make(map[clusterQuotaDigestKey]*clusterQuotaDigestAgg)
	addClusterQuotaRowToGroups(groups, rows[0], "1234567", "11111111-1111-1111-1111-111111111111")
	require.Len(t, groups, 1)

	for _, agg := range groups {
		assert.Equal(t, int64(0), agg.objectCountHard)
		assert.Nil(t, nullableInt64Digest(agg.objectCountHard))
		assert.Nil(t, nullableInt64Digest(agg.objectCountUsed))
	}
}

func TestClusterQuotaDigestGroupsPerReportDate(t *testing.T) {
	csvText := `interval_start,interval_end,cluster_quota_name,cpu_request_hard,pods_hard
2026-05-01T22:00:00Z,2026-05-01T23:00:00Z,demo-crq,20,1000
2026-05-02T00:00:00Z,2026-05-02T01:00:00Z,demo-crq,20,900
`
	rows, err := ParseClusterQuotaCSVRows(strings.NewReader(csvText))
	require.NoError(t, err)
	require.Len(t, rows, 2)

	groups := make(map[clusterQuotaDigestKey]*clusterQuotaDigestAgg)
	for _, row := range rows {
		addClusterQuotaRowToGroups(groups, row, "1234567", "11111111-1111-1111-1111-111111111111")
	}
	// The bucket is the interval_end day, so an interval that straddles midnight
	// is counted on the day it closes.
	require.Len(t, groups, 2)
	podsByDate := map[string]int64{}
	for key, agg := range groups {
		podsByDate[key.reportDate.Format("2006-01-02")] = agg.podsHard
	}
	assert.Equal(t, int64(1000), podsByDate["2026-05-01"])
	assert.Equal(t, int64(900), podsByDate["2026-05-02"])
}

func TestForEachClusterQuotaCSVRow_DoesNotMaterializeFullSlice(t *testing.T) {
	csv := `interval_start,interval_end,cluster_quota_name,cpu_request_hard
2026-05-01T00:00:00Z,2026-05-01T01:00:00Z,team-a,1
2026-05-01T01:00:00Z,2026-05-01T02:00:00Z,team-a,2
`
	seen := 0
	count, err := forEachClusterQuotaCSVRow(context.Background(), strings.NewReader(csv), func(row ClusterQuotaMetricRow) error {
		seen++
		assert.Equal(t, "team-a", row.ClusterQuotaName)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, count)
	assert.Equal(t, 2, seen)
}
