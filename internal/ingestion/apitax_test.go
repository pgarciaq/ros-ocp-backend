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

// TestParseAPITaxRows_Golden parses the shared golden fixture (thin W5
// contract: webhook|window|le|bucket|total|rejected|collected|source).
// The operator side must byte-match this file.
func TestParseAPITaxRows_Golden(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "apitax_golden.csv"))
	require.NoError(t, err)
	rows, err := ParseAPITaxRows(strings.NewReader(string(data)))
	require.NoError(t, err)
	require.Len(t, rows, 3)
	assert.Equal(t, "pod.network-node-identity.openshift.io", rows[0].WebhookName)
	assert.InDelta(t, 0.5, rows[0].Le, 1e-9)
	assert.True(t, rows[0].HasTotal && rows[0].HasRejected)
	assert.Equal(t, "kubernetes", rows[2].Source)
}

// TestParseAPITaxRows_SkipMalformed and MissingColumns live in librobne
// (parser unit); ingest trusts the parser's skip counters here.

// TestProcessAPITaxCSV_DBStoreRoundTrip proves golden rows land once and
// re-ingest is idempotent, with totals joined onto bucket rows.
func TestProcessAPITaxCSV_DBStoreRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-apitax-store-" + t.Name()
	clusterUUID := testutil.TestClusterUUID

	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM hosted_api_tax_rollups WHERE org_id = $1`, orgID)
	})

	data, err := os.ReadFile(filepath.Join("testdata", "apitax_golden.csv"))
	require.NoError(t, err)
	require.NoError(t, ProcessAPITaxCSV(ctx, pool, strings.NewReader(string(data)), orgID, clusterUUID))
	require.NoError(t, ProcessAPITaxCSV(ctx, pool, strings.NewReader(string(data)), orgID, clusterUUID))

	var count int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM hosted_api_tax_rollups WHERE org_id = $1`, orgID).Scan(&count))
	assert.Equal(t, 3, count, "golden rows land once; re-ingest is idempotent")

	var total, rejected int64
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT total_count, rejected_count FROM hosted_api_tax_rollups
		 WHERE org_id = $1 AND webhook_name LIKE 'pod.network%' AND le = '+Infinity'::float8`,
		orgID).Scan(&total, &rejected))
	assert.Equal(t, int64(102), total)
	assert.Equal(t, int64(2), rejected)
}
