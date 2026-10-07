package ingestion

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// TestParseNodepoolRows_Golden parses the shared golden fixture (Child A
// contract: hc|pool|window|spec|status|auto|collected|source).
func TestParseNodepoolRows_Golden(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "nodepool_golden.csv"))
	require.NoError(t, err)
	rows, err := ParseNodepoolRows(strings.NewReader(string(data)))
	require.NoError(t, err)
	require.Len(t, rows, 2)
	assert.Equal(t, "d5d31999-89ed-4c13-b5e8-c9193f62e630", rows[0].HCClusterID)
	assert.Equal(t, int64(2), rows[0].SpecReplicas)
	assert.Equal(t, int64(0), rows[1].SpecReplicas, "zero-zero lands verbatim")
}

// TestProcessNodepoolCSV_DBStoreRoundTrip proves golden rows land once and
// re-ingest is idempotent.
func TestProcessNodepoolCSV_DBStoreRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-nodepool-store-" + t.Name()
	clusterUUID := testutil.TestClusterUUID

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM hosted_nodepool_rollups WHERE org_id = $1`, orgID)
	})

	data, err := os.ReadFile(filepath.Join("testdata", "nodepool_golden.csv"))
	require.NoError(t, err)
	require.NoError(t, ProcessNodepoolCSV(ctx, pool, strings.NewReader(string(data)), orgID, clusterUUID))
	require.NoError(t, ProcessNodepoolCSV(ctx, pool, strings.NewReader(string(data)), orgID, clusterUUID))

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM hosted_nodepool_rollups WHERE org_id = $1`, orgID).Scan(&count))
	assert.Equal(t, 2, count, "golden rows land once; re-ingest is idempotent")

	var spec, status int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT spec_replicas, status_replicas FROM hosted_nodepool_rollups
		 WHERE org_id = $1 AND pool_name = 'hc01-parked'`, orgID).Scan(&spec, &status))
	assert.Equal(t, int64(0), spec)
	assert.Equal(t, int64(0), status)
}
