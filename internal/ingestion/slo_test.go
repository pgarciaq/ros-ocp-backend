package ingestion

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseSLORows_Golden parses the shared golden fixture (#644 canonical
// contract). The operator side must byte-match this file.
func TestParseSLORows_Golden(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "slo_golden.csv"))
	require.NoError(t, err)
	rows, err := ParseSLORows(strings.NewReader(string(data)))
	require.NoError(t, err)
	require.Len(t, rows, 6)

	r := rows[0]
	assert.Equal(t, "d5d31999-1111-4444-8888-aaaaaaaaaaaa", r.HCClusterID)
	assert.Equal(t, "mutating", r.VerbGroup)
	assert.InDelta(t, 0.1, r.Le, 1e-9)
	assert.Equal(t, int64(12), r.BucketCount)
	assert.Equal(t, "kubernetes", r.Source)

	// +Inf always present per contract.
	foundInf := map[string]bool{}
	for _, row := range rows {
		if row.Le > 1e300 {
			foundInf[row.VerbGroup] = true
		}
	}
	assert.True(t, foundInf["mutating"], "mutating +Inf row present")
	assert.True(t, foundInf["read"], "read +Inf row present")
	assert.True(t, foundInf["other"], "other +Inf row present")
}

func TestParseSLORows_SkipMalformed(t *testing.T) {
	csv := `hc_cluster_id,window_start,window_end,verb_group,le,bucket_count,collected_at,source
d5d31999-1111-4444-8888-aaaaaaaaaaaa,2026-09-29 10:00:00 +0000 UTC,2026-09-29 11:00:00 +0000 UTC,mutating,+Inf,102,2026-09-29 11:00:30 +0000 UTC,kubernetes
,2026-09-29 10:00:00 +0000 UTC,2026-09-29 11:00:00 +0000 UTC,read,+Inf,310,2026-09-29 11:00:30 +0000 UTC,kubernetes
d5d31999-1111-4444-8888-aaaaaaaaaaaa,2026-09-29 10:00:00 +0000 UTC,2026-09-29 11:00:00 +0000 UTC,WATCH,+Inf,5,2026-09-29 11:00:30 +0000 UTC,kubernetes
d5d31999-1111-4444-8888-aaaaaaaaaaaa,2026-09-29 10:00:00 +0000 UTC,2026-09-29 11:00:00 +0000 UTC,read,notale,310,2026-09-29 11:00:30 +0000 UTC,kubernetes
d5d31999-1111-4444-8888-aaaaaaaaaaaa,2026-09-29 10:00:00 +0000 UTC,2026-09-29 11:00:00 +0000 UTC,read,+Inf,-3,2026-09-29 11:00:30 +0000 UTC,kubernetes
d5d31999-1111-4444-8888-aaaaaaaaaaaa,2026-09-29 11:00:00 +0000 UTC,2026-09-29 10:00:00 +0000 UTC,read,+Inf,9,2026-09-29 11:00:30 +0000 UTC,kubernetes
d5d31999-1111-4444-8888-aaaaaaaaaaaa,2026-09-29 10:00:00 +0000 UTC,2026-09-29 11:00:00 +0000 UTC,read,+Inf,9,2026-09-29 11:00:30 +0000 UTC,
`
	rows, err := ParseSLORows(strings.NewReader(csv))
	require.NoError(t, err)
	// Only the first row survives: empty hc id, WATCH verb, bad le,
	// negative count, inverted window, and empty source are all skipped
	// with counters.
	assert.Len(t, rows, 1)
	assert.Equal(t, int64(102), rows[0].BucketCount)
}

func TestParseSLORows_MissingColumns(t *testing.T) {
	csv := "hc_cluster_id,window_start\nabc,2026-09-29 10:00:00 +0000 UTC\n"
	_, err := ParseSLORows(strings.NewReader(csv))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing columns")
}

func TestParseSLORows_MissingSourceColumn(t *testing.T) {
	// Pre-amendment files without source are rejected contract-strict (rows
	// skip with counters at ingest; the file still marks Done).
	csv := "hc_cluster_id,window_start,window_end,verb_group,le,bucket_count,collected_at\nhc1,2026-09-29 10:00:00 +0000 UTC,2026-09-29 11:00:00 +0000 UTC,read,+Inf,7,2026-09-29 11:00:30 +0000 UTC\n"
	_, err := ParseSLORows(strings.NewReader(csv))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source")
}

func TestUpsertWorkerPressureDerived_RangeGuard(t *testing.T) {
	// Range guard is pure (no DB): coverage outside [0,100] must fail fast.
	// DB-backed upsert is covered by testcontainers integration. The guard
	// runs before any pool use, so a nil pool is safe here.
	now := time.Now().UTC()
	err := UpsertWorkerPressureDerived(context.Background(), nil, "o", "c", "hc",
		now, now.Add(time.Hour), true, 101, nil, now)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}
