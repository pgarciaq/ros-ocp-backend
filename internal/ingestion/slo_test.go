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

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
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
	// The DB-backed round trip lives in TestProcessSLOCSV_DBStoreRoundTrip.
	// The guard runs before any pool use, so a nil pool is safe here.
	now := time.Now().UTC()
	err := UpsertWorkerPressureDerived(context.Background(), nil, "o", "c", "hc",
		now, now.Add(time.Hour), true, 101, nil, now)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
}

func TestProcessSLOCSV_DBStoreRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-slo-store-" + t.Name()
	clusterUUID := testutil.TestClusterUUID

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM hosted_api_bucket_rollups WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM hosted_worker_pressure WHERE org_id = $1`, orgID)
	})

	data, err := os.ReadFile(filepath.Join("testdata", "slo_golden.csv"))
	require.NoError(t, err)
	// Pre-create partitions for the golden CSV's month (September 2026).
	require.NoError(t, EnsureSLOPartitionsForMonth(ctx, pool, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)))
	require.NoError(t, ProcessSLOCSV(ctx, pool, strings.NewReader(string(data)), orgID, clusterUUID))
	require.NoError(t, ProcessSLOCSV(ctx, pool, strings.NewReader(string(data)), orgID, clusterUUID))

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM hosted_api_bucket_rollups WHERE org_id = $1`, orgID).Scan(&count))
	assert.Equal(t, 6, count, "golden rows land once; re-ingest is idempotent")

	var total int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT bucket_count FROM hosted_api_bucket_rollups WHERE org_id = $1 AND source = 'kubernetes' AND verb_group = 'read' AND le = '+Infinity'::float8`,
		orgID).Scan(&total))
	assert.Equal(t, int64(310), total)

	now := time.Now().UTC()
	ws := now.Truncate(time.Hour)
	require.NoError(t, UpsertWorkerPressureDerived(ctx, pool, orgID, clusterUUID,
		"d5d31999-1111-4444-8888-aaaaaaaaaaaa", ws, ws.Add(time.Hour), false, 100.0, []string{"12"}, now))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM hosted_worker_pressure WHERE org_id = $1`, orgID).Scan(&count))
	assert.Equal(t, 1, count)
}
