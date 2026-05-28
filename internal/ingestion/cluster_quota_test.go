package ingestion

import (
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
	assert.Equal(t, int64(1073741824), rows[0].MemoryRequestHard)
	assert.Equal(t, int64(536870912), rows[0].MemoryRequestUsed)
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
