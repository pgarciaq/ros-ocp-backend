package snapshot

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func defaultSnapshotTestSettings() SnapshotSettings {
	return SnapshotSettings{
		OrphanAgeDays:      7,
		NeverRestoredDays:  30,
		StaleDays:          90,
		RedundantThreshold: 3,
		CostPerGiBMonth:    0.05,
	}
}

func setupSnapshotRecommendTest(t *testing.T) (*pgxpool.Pool, SnapshotFixtureNames) {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	names := SeedSnapshotTestInventory(t, pool, testutil.TestOrgID, testutil.TestClusterUUID)
	return pool, names
}

func TestSnapshotRecommendations_OrphanedDetected(t *testing.T) {
	pool, names := setupSnapshotRecommendTest(t)
	recs := classifySnapshotTestInventory(t, pool, defaultSnapshotTestSettings())

	rec, ok := snapshotRecByName(recs, names.NamespaceA, names.Orphaned)
	require.True(t, ok)
	assert.Equal(t, "orphaned", rec.RecommendationType)
	assert.Contains(t, rec.NotificationCodes, engine.NotifSnapshotOrphaned)
}

func TestSnapshotRecommendations_StaleDetected(t *testing.T) {
	pool, names := setupSnapshotRecommendTest(t)
	recs := classifySnapshotTestInventory(t, pool, defaultSnapshotTestSettings())

	rec, ok := snapshotRecByName(recs, names.NamespaceA, names.Stale)
	require.True(t, ok)
	assert.Equal(t, "stale", rec.RecommendationType)
	assert.Contains(t, rec.NotificationCodes, engine.NotifSnapshotStale)
}

func TestSnapshotRecommendations_NeverRestoredDetected(t *testing.T) {
	pool, names := setupSnapshotRecommendTest(t)
	recs := classifySnapshotTestInventory(t, pool, defaultSnapshotTestSettings())

	rec, ok := snapshotRecByName(recs, names.NamespaceB, names.NeverRestored)
	require.True(t, ok)
	assert.Equal(t, "never_restored", rec.RecommendationType)
	assert.Contains(t, rec.NotificationCodes, engine.NotifSnapshotNeverUsed)
}

func TestSnapshotRecommendations_RedundantDetected(t *testing.T) {
	pool, names := setupSnapshotRecommendTest(t)
	recs := classifySnapshotTestInventory(t, pool, defaultSnapshotTestSettings())

	rec, ok := snapshotRecByName(recs, names.NamespaceB, names.Redundant)
	require.True(t, ok)
	assert.Equal(t, "redundant", rec.RecommendationType)
	assert.Contains(t, rec.NotificationCodes, engine.NotifSnapshotRedundant)
}

func TestSnapshotRecommendations_HealthyNotFlagged(t *testing.T) {
	pool, names := setupSnapshotRecommendTest(t)
	recs := classifySnapshotTestInventory(t, pool, defaultSnapshotTestSettings())

	rec, ok := snapshotRecByName(recs, names.NamespaceB, names.Healthy)
	require.True(t, ok)
	assert.Equal(t, "managed", rec.RecommendationType)
	assert.Contains(t, rec.NotificationCodes, engine.NotifSnapshotManaged)
	assert.NotEqual(t, "orphaned", rec.RecommendationType)
	assert.NotEqual(t, "stale", rec.RecommendationType)
	assert.NotEqual(t, "redundant", rec.RecommendationType)
}

func TestSnapshotRecommendations_CustomThresholds(t *testing.T) {
	unsetSnapshotThresholdEnvForTest(t)
	config.ResetForTest()

	pool, names := setupSnapshotRecommendTest(t)
	ctx := context.Background()

	// Raise stale threshold so the 120-day snapshot is no longer stale.
	_, err := pool.Exec(ctx, `
		INSERT INTO snapshot_settings (
			org_id, orphan_age_days, never_restored_days, stale_days,
			redundant_threshold, cost_per_gib_month_usd, inventory_fresh_hours, updated_at
		) VALUES ($1, 7, 30, 180, 3, 0.05, 6, NOW())
		ON CONFLICT (org_id) DO UPDATE SET stale_days = EXCLUDED.stale_days`,
		testutil.TestOrgID,
	)
	require.NoError(t, err)

	settings, err := ResolveSnapshotSettings(ctx, pool, testutil.TestOrgID, nil)
	require.NoError(t, err)
	require.Equal(t, 180, settings.StaleDays)

	recs := classifySnapshotTestInventory(t, pool, settings)

	staleRec, ok := snapshotRecByName(recs, names.NamespaceA, names.Stale)
	require.True(t, ok)
	assert.Equal(t, "never_restored", staleRec.RecommendationType,
		"120-day snapshot should be never_restored when stale_days=180")
	assert.NotEqual(t, "stale", staleRec.RecommendationType)
}

// unsetSnapshotThresholdEnvForTest removes locked snapshot threshold env vars so
// ResolveSnapshotSettings reads per-org DB values in integration tests.
func unsetSnapshotThresholdEnvForTest(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"ROS_SNAPSHOT_ORPHAN_AGE_DAYS",
		"ROS_SNAPSHOT_NEVER_RESTORED_DAYS",
		"ROS_SNAPSHOT_STALE_DAYS",
		"ROS_SNAPSHOT_REDUNDANT_THRESHOLD",
		"ROS_SNAPSHOT_INVENTORY_FRESH_HOURS",
	} {
		prev, ok := os.LookupEnv(key)
		if !ok {
			continue
		}
		require.NoError(t, os.Unsetenv(key))
		t.Cleanup(func() { _ = os.Setenv(key, prev) })
	}
}
