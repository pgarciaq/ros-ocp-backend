package engine_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins/container"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// TestTerms_AffectRecommendationOutput_Integration verifies that configuring
// different term windows produces different recommendation outputs.
//
// Scenario: 4 days of container digest data are seeded. Custom terms are configured:
//   - short:  window_days=2,  min_data_days=1
//   - medium: window_days=5,  min_data_days=3
//   - long:   window_days=10, min_data_days=5
//
// Expected: short and medium terms produce recommendations (have enough data),
// but long term does NOT (4 days < min_data_days=5).
func TestTerms_AffectRecommendationOutput_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testutil.TestOrgID
	clusterUUID := testutil.TestClusterUUID

	_, err := pool.Exec(ctx,
		`INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		 VALUES (1, $1, 'term-test-cluster', 'src-term', now()) ON CONFLICT DO NOTHING`, clusterUUID)
	require.NoError(t, err)

	// Seed exactly 4 days of data ending today.
	now := time.Now().UTC().Truncate(24 * time.Hour)
	seedDays := 4
	for i := 0; i < seedDays; i++ {
		day := now.AddDate(0, 0, -(seedDays - 1 - i))
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:       day,
			OrgID:            orgID,
			ClusterUUID:      clusterUUID,
			Namespace:        "term-test-ns",
			Workload:         "term-test-deploy",
			WorkloadType:     "Deployment",
			ContainerName:    "term-test-container",
			CPURequestP50MC:  100 + int64(i)*10,
			CPURequestP95MC:  200 + int64(i)*10,
			CPUUsageP50MC:    80 + int64(i)*5,
			CPUUsageP95MC:    150 + int64(i)*10,
			CPUUsageP98MC:    160 + int64(i)*10,
			CPUUsageP99MC:    170 + int64(i)*10,
			CPUUsageMaxMC:    200 + int64(i)*10,
			CPUThrottleP95MC: 5,
			CPUThrottleMaxMC: 10,
			MemRequestP50KiB: 1000 + int64(i)*100,
			MemRequestP60KiB: 1050 + int64(i)*100,
			MemRequestP95KiB: 1200 + int64(i)*100,
			MemRequestP98KiB: 1250 + int64(i)*100,
			MemRequestP99KiB: 1280 + int64(i)*100,
			MemUsageP50KiB:   900 + int64(i)*80,
			MemUsageP60KiB:   950 + int64(i)*80,
			MemUsageP95KiB:   1100 + int64(i)*100,
			MemUsageP98KiB:   1150 + int64(i)*100,
			MemUsageP99KiB:   1180 + int64(i)*100,
			MemUsageMaxKiB:   1300 + int64(i)*100,
			MemRSSP95KiB:     850 + int64(i)*50,
			MemRSSMaxKiB:     1000 + int64(i)*60,
			OOMCountSum:      0,
			CPUUsageMeanMC:   120 + int64(i)*8,
			MemUsageMeanKiB:  950 + int64(i)*90,
			SampleCount:      96,
		})
	}

	// Insert custom terms into the DB: short=2d, medium=5d, long=10d.
	insertTerms(t, pool, orgID, "container", []termRow{
		{ord: 1, windowDays: 2, minDataDays: 1, decayHL: nil},
		{ord: 2, windowDays: 5, minDataDays: 3, decayHL: nil},
		{ord: 3, windowDays: 10, minDataDays: 5, decayHL: nil},
	})

	// Run the recommendation engine.
	start := now.AddDate(0, 0, -10)
	end := now
	var recs []engine.ContainerRec
	err = engine.RecommendWorkloadsStreaming(ctx, pool, orgID, clusterUUID, start, end,
		engine.OOMConfig{}, func(batch []engine.ContainerRec) error {
			recs = append(recs, batch...)
			return nil
		})
	require.NoError(t, err)

	// Group recs by term.
	termRecs := map[string][]engine.ContainerRec{}
	for _, r := range recs {
		termRecs[r.Term] = append(termRecs[r.Term], r)
	}

	// short (window=2, min_data=1): should have recs (we have 2+ days in window).
	assert.NotEmpty(t, termRecs["short"], "short term should produce recommendations with 4 days of data")

	// medium (window=5, min_data=3): should have recs (we have 4 days in 5-day window).
	assert.NotEmpty(t, termRecs["medium"], "medium term should produce recommendations with 4 days of data")

	// long (window=10, min_data=5): should NOT have recs (we only have 4 days < min_data_days=5).
	assert.Empty(t, termRecs["long"], "long term should NOT produce recommendations with only 4 days of data (needs 5)")

	// Verify that short-term and medium-term recommendations use different data windows
	// (short uses only last 2 days, medium uses all 4).
	if len(termRecs["short"]) > 0 && len(termRecs["medium"]) > 0 {
		shortRec := termRecs["short"][0]
		medRec := termRecs["medium"][0]
		assert.Equal(t, 2, shortRec.DataDays, "short term should use 2 days of data")
		assert.Equal(t, 4, medRec.DataDays, "medium term should use 4 days of data")
	}
}

// TestTerms_EnvVarLock_OverridesDB_Integration verifies that when an admin env
// var locks a term, the DB value is ignored and the env var value is used.
func TestTerms_EnvVarLock_OverridesDB_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testutil.TestOrgID

	// Set env var to lock short term at window_days=3 for container.
	t.Setenv("ROS_TERMS_CONTAINER_SHORT_WINDOW_DAYS", "3")

	// Insert a DB term with window_days=7 for short (should be overridden).
	insertTerms(t, pool, orgID, "container", []termRow{
		{ord: 1, windowDays: 7, minDataDays: 3, decayHL: nil},
	})

	// Load terms — the env var should win.
	terms, err := engine.LoadTermConfig(ctx, pool, orgID, "container")
	require.NoError(t, err)
	require.Len(t, terms, 3)

	assert.Equal(t, 3, terms[0].WindowDays, "env var should override DB value for short term")
	assert.True(t, engine.IsTermLocked("container", "short"), "short term should be locked")
	assert.False(t, engine.IsTermLocked("container", "medium"), "medium term should not be locked")
}

type termRow struct {
	ord         int
	windowDays  int
	minDataDays int
	decayHL     *float64
}

func insertTerms(t *testing.T, pool *pgxpool.Pool, orgID, recType string, rows []termRow) {
	t.Helper()
	ctx := context.Background()
	for _, r := range rows {
		_, err := pool.Exec(ctx,
			`INSERT INTO org_recommendation_terms (org_id, recommendation_type, term_ord, window_days, min_data_days, decay_halflife_hours)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT (org_id, recommendation_type, term_ord) DO UPDATE SET
			   window_days = $4, min_data_days = $5, decay_halflife_hours = $6`,
			orgID, recType, r.ord, r.windowDays, r.minDataDays, r.decayHL)
		require.NoError(t, err)
	}
	// Invalidate cache so LoadTermConfigCached reads from DB.
	engine.InvalidateTermCache(orgID, recType)
}
