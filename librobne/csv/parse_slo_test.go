package csv

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseSLORows_Basic(t *testing.T) {
	csv := "hc_cluster_id,window_start,window_end,verb_group,le,bucket_count,collected_at,source\n" +
		"d5d31999-1111-4444-8888-aaaaaaaaaaaa,2026-09-29 10:00:00 +0000 UTC,2026-09-29 11:00:00 +0000 UTC,mutating,+Inf,102,2026-09-29 11:00:30 +0000 UTC,kubernetes\n" +
		"d5d31999-1111-4444-8888-aaaaaaaaaaaa,2026-09-29 10:00:00 +0000 UTC,2026-09-29 11:00:00 +0000 UTC,read,0.5,87,2026-09-29 11:00:30 +0000 UTC,metrics-server\n"
	rows, skipped, err := ParseSLORows(strings.NewReader(csv))
	require.NoError(t, err)
	assert.Equal(t, 0, skipped)
	require.Len(t, rows, 2)
	assert.Equal(t, "mutating", rows[0].VerbGroup)
	assert.Positive(t, rows[0].Le) // +Inf
	assert.Equal(t, "kubernetes", rows[0].Source)
	assert.InDelta(t, 0.5, rows[1].Le, 1e-9)
	assert.Equal(t, "metrics-server", rows[1].Source)
}

func TestParseSLORows_SkipsBadVerbAndCounts(t *testing.T) {
	csv := "hc_cluster_id,window_start,window_end,verb_group,le,bucket_count,collected_at,source\n" +
		"hc1,2026-09-29 10:00:00 +0000 UTC,2026-09-29 11:00:00 +0000 UTC,WATCH,+Inf,5,2026-09-29 11:00:30 +0000 UTC,kubernetes\n" +
		"hc1,2026-09-29 10:00:00 +0000 UTC,2026-09-29 11:00:00 +0000 UTC,read,+Inf,7,2026-09-29 11:00:30 +0000 UTC,kubernetes\n"
	rows, skipped, err := ParseSLORows(strings.NewReader(csv))
	require.NoError(t, err)
	assert.Equal(t, 1, skipped) // WATCH excluded at source; backend skips unknown groups
	require.Len(t, rows, 1)
	assert.Equal(t, int64(7), rows[0].BucketCount)
}

func TestClassifyFilename_SLO(t *testing.T) {
	assert.Equal(t, KindSLO, ClassifyFilename("ros-openshift-slo-202609.csv"))
	assert.Equal(t, KindSLO, ClassifyFilename("d684644b-1111-2222-3333-444455556666-ros-openshift-slo-202609.csv"))
}
