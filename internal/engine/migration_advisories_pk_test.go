package engine

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// TestMigration_000218_AdvisoriesMgmtPK pins #763: the advisories key
// includes management_cluster_uuid on a fully-migrated database, and the
// down migration collapses multi-farm rows back to one per old key — the
// greatest (expires_at, management_cluster_uuid) survives — so restoring
// the narrower PK cannot fail on duplicates.
func TestMigration_000218_AdvisoriesMgmtPK(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// Up state: the 4-column key.
	assert.Equal(t,
		[]string{"org_id", "hc_cluster_id", "window_start", "management_cluster_uuid"},
		queryPrimaryKeyColumns(t, pool, "hcp_correlation_advisories"),
		"the #763 key must include management_cluster_uuid")

	org := testutil.TestOrgID + "-mig-763"
	ws := time.Now().UTC().Truncate(time.Hour).Add(-3 * time.Hour)
	we := ws.Add(time.Hour)
	farmA := "11111111-1111-4111-8111-111111111111"
	farmB := "22222222-2222-4222-8222-222222222222"
	seed := func(mgmt, hc string, expires time.Time) {
		_, err := pool.Exec(ctx, `
			INSERT INTO hcp_correlation_advisories (
				org_id, hc_cluster_id, management_cluster_uuid, window_start, window_end,
				verdict, confidence, h_p99_s, h_threshold_s, c_ratio, n_signals, expires_at
			) VALUES ($1, $2, $3, $4, $5, 'tune_noisy_webhook', 'high', 3, 0.3, NULL, '{}', $6)`,
			org, hc, mgmt, ws, we, expires)
		require.NoError(t, err)
	}
	// Two farms' shared-plane rows in one org/window (only insertable under
	// the 4-column key), distinct expiry so the down's dedupe has a
	// deterministic winner, plus an unrelated per-HC row.
	seed(farmA, "", ws.Add(time.Hour))
	seed(farmB, "", ws.Add(2*time.Hour))
	seed(farmA, "hc-mig-763", ws.Add(time.Hour))

	// Down: must run cleanly with multi-farm rows present.
	downSQL, err := os.ReadFile("../../migrations/000218_hcp_advisories_mgmt_pk.down.sql")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(downSQL))
	require.NoError(t, err, "the down migration must not fail on multi-farm rows")

	assert.Equal(t, []string{"org_id", "hc_cluster_id", "window_start"},
		queryPrimaryKeyColumns(t, pool, "hcp_correlation_advisories"),
		"the down must restore the 3-column key")
	var survivor string
	var farmRows, hcRows int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT management_cluster_uuid::text FROM hcp_correlation_advisories
		 WHERE org_id = $1 AND hc_cluster_id = ''`, org).Scan(&survivor))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM hcp_correlation_advisories
		 WHERE org_id = $1 AND hc_cluster_id = ''`, org).Scan(&farmRows))
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM hcp_correlation_advisories
		 WHERE org_id = $1 AND hc_cluster_id <> ''`, org).Scan(&hcRows))
	assert.Equal(t, 1, farmRows, "multi-farm rows must collapse to one per old key")
	assert.Equal(t, farmB, survivor, "the greatest-expires_at farm row must survive the collapse")
	assert.Equal(t, 1, hcRows, "per-HC rows are not farm-collapsed")

	// Round trip: the up restores the 4-column key — and purges the
	// surviving shared-plane row (pre-fix rows may be undetectably mixed;
	// farms with live evidence re-fire within the hour).
	upSQL, err := os.ReadFile("../../migrations/000218_hcp_advisories_mgmt_pk.up.sql")
	require.NoError(t, err)
	_, err = pool.Exec(ctx, string(upSQL))
	require.NoError(t, err)
	assert.Equal(t,
		[]string{"org_id", "hc_cluster_id", "window_start", "management_cluster_uuid"},
		queryPrimaryKeyColumns(t, pool, "hcp_correlation_advisories"))
	var sharedAfterUp int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM hcp_correlation_advisories
		 WHERE org_id = $1 AND hc_cluster_id = ''`, org).Scan(&sharedAfterUp))
	assert.Equal(t, 0, sharedAfterUp,
		"the up must purge pre-existing shared-plane rows (undetectably mixed pre-fix)")
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM hcp_correlation_advisories
		 WHERE org_id = $1 AND hc_cluster_id <> ''`, org).Scan(&hcRows))
	assert.Equal(t, 1, hcRows, "per-HC rows survive the up's purge")

	// After the up re-applies, two farms coexist in one org/window again.
	seed(farmA, "", ws.Add(3*time.Hour))
	seed(farmB, "", ws.Add(3*time.Hour))
	var farmRowsAfter int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM hcp_correlation_advisories
		 WHERE org_id = $1 AND hc_cluster_id = ''`, org).Scan(&farmRowsAfter))
	assert.Equal(t, 2, farmRowsAfter,
		"after the up re-applies, two farms must coexist in one org/window again")
}
