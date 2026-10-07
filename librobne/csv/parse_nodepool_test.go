package csv

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseNodepoolRows_Basic(t *testing.T) {
	csv := "hc_cluster_id,pool_name,window_start,window_end,spec_replicas,status_replicas,autoscaling,collected_at,source\n" +
		"hc-1,pool-a,2026-10-06 10:00:00 +0000 UTC,2026-10-06 11:00:00 +0000 UTC,2,2,,2026-10-06 11:00:30 +0000 UTC,hypershift\n" +
		"hc-1,pool-b,2026-10-06 10:00:00 +0000 UTC,2026-10-06 11:00:00 +0000 UTC,0,0,,2026-10-06 11:00:30 +0000 UTC,hypershift\n" +
		"hc-1,pool-c,2026-10-06 10:00:00 +0000 UTC,2026-10-06 11:00:00 +0000 UTC,1,1,0-3,2026-10-06 11:00:30 +0000 UTC,hypershift\n"
	rows, skipped, err := ParseNodepoolRows(strings.NewReader(csv))
	require.NoError(t, err)
	assert.Equal(t, 0, skipped)
	require.Len(t, rows, 3)
	assert.Equal(t, "hc-1", rows[0].HCClusterID)
	assert.Equal(t, int64(2), rows[0].SpecReplicas)
	assert.Equal(t, "", rows[0].Autoscaling)
	assert.Equal(t, int64(0), rows[1].SpecReplicas, "zero-zero lands verbatim (rule interprets)")
	assert.Equal(t, "0-3", rows[2].Autoscaling)
	assert.Equal(t, "hypershift", rows[2].Source)
}

func TestParseNodepoolRows_SkipsMalformed(t *testing.T) {
	csv := "hc_cluster_id,pool_name,window_start,window_end,spec_replicas,status_replicas,autoscaling,collected_at,source\n" +
		"hc-1,pool-neg,2026-10-06 10:00:00 +0000 UTC,2026-10-06 11:00:00 +0000 UTC,-1,2,,2026-10-06 11:00:30 +0000 UTC,hypershift\n" +
		"hc-1,pool-bad,2026-10-06 10:00:00 +0000 UTC,2026-10-06 11:00:00 +0000 UTC,2,2,lots,2026-10-06 11:00:30 +0000 UTC,hypershift\n" +
		",pool-noname,2026-10-06 10:00:00 +0000 UTC,2026-10-06 11:00:00 +0000 UTC,2,2,,2026-10-06 11:00:30 +0000 UTC,hypershift\n" +
		"hc-1,pool-ok,2026-10-06 10:00:00 +0000 UTC,2026-10-06 11:00:00 +0000 UTC,2,2,,2026-10-06 11:00:30 +0000 UTC,hypershift\n"
	rows, skipped, err := ParseNodepoolRows(strings.NewReader(csv))
	require.NoError(t, err)
	assert.Equal(t, 3, skipped, "negative count + bad autoscaling + empty pool skip with counters")
	require.Len(t, rows, 1)
	assert.Equal(t, "pool-ok", rows[0].PoolName)
}

func TestParseNodepoolRows_MissingColumns(t *testing.T) {
	csv := "hc_cluster_id,pool_name,spec_replicas\n" +
		"hc-1,pool-a,2\n"
	_, _, err := ParseNodepoolRows(strings.NewReader(csv))
	require.Error(t, err)
	var missing *MissingNodepoolColumnsError
	require.ErrorAs(t, err, &missing)
	assert.NotEmpty(t, missing.Columns)
}

func TestClassifyFilename_Nodepool(t *testing.T) {
	assert.Equal(t, KindNodepool, ClassifyFilename("ros-openshift-nodepool-202610.csv"))
	assert.Equal(t, KindNodepool, ClassifyFilename("d684644b-1111-2222-3333-444455556666-ros-openshift-nodepool-202610.csv"))
}
