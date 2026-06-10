package services

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	promtest "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/internal/types"
)

func seedAnalyticsRecommendationFixture(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID string) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (99001, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at, analytics_incomplete)
		VALUES (99001, $1::uuid, 'analytics-test', 'src-analytics', NOW(), false)
		ON CONFLICT DO NOTHING`, clusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	for i := 0; i < 7; i++ {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:       start.AddDate(0, 0, i),
			OrgID:            orgID,
			ClusterUUID:      clusterUUID,
			Namespace:        testutil.TestNamespace,
			Workload:         testutil.TestWorkload,
			WorkloadType:     testutil.TestWorkloadType,
			ContainerName:    testutil.TestContainer,
			CPURequestP50MC:  80,
			CPURequestP95MC:  100,
			CPUUsageP50MC:    70,
			CPUUsageP95MC:    90,
			CPUUsageP98MC:    95,
			CPUUsageP99MC:    98,
			CPUUsageMaxMC:    105,
			CPUThrottleP95MC: 5,
			CPUThrottleMaxMC: 10,
			MemRequestP50KiB: 102400,
			MemRequestP95KiB: 112640,
			MemUsageP50KiB:   102400,
			MemUsageP95KiB:   112640,
			MemUsageMaxKiB:   120832,
			MemRSSP95KiB:     110000,
			MemRSSMaxKiB:     115000,
			SampleCount:      96,
		})
	}
}

func TestRunContainerRecommendations_DegradedMode_PersistsRecsOnHistoryFailure(t *testing.T) {
	t.Setenv("ROS_INGEST_STRICT_ANALYTICS", "false")
	config.ResetForTest()
	_ = config.GetConfig()

	pool := testutil.SetupTestDB(t)
	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })

	orgID := "org-analytics-degraded"
	clusterUUID := "33333333-4444-5555-6666-777777777777"
	seedAnalyticsRecommendationFixture(t, pool, orgID, clusterUUID)

	engine.SetAnalyticsWriteHooksForTest(&engine.AnalyticsWriteHooks{
		ContainerHistory: engine.StubContainerHistoryHook(engine.ErrAnalyticsStub()),
	})
	t.Cleanup(func() { engine.SetAnalyticsWriteHooksForTest(nil) })

	before := promtest.ToFloat64(metrics.AnalyticsIncompleteTotal.WithLabelValues(orgID, clusterUUID, "history"))

	kafkaMsg := types.KafkaMsg{}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID

	err := runContainerRecommendations(kafkaMsg)
	require.NoError(t, err, "degraded mode should not fail ingestion")

	ctx := context.Background()
	var recCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`,
		orgID, clusterUUID).Scan(&recCount))
	assert.Greater(t, recCount, 0, "recommendations should be persisted in degraded mode")

	var incomplete bool
	var incompleteAt *time.Time
	require.NoError(t, pool.QueryRow(ctx, `
		SELECT c.analytics_incomplete, c.analytics_incomplete_at
		FROM clusters c
		JOIN rh_accounts ra ON ra.id = c.tenant_id
		WHERE ra.org_id = $1 AND c.cluster_uuid = $2::uuid`,
		orgID, clusterUUID).Scan(&incomplete, &incompleteAt))
	assert.True(t, incomplete)
	require.NotNil(t, incompleteAt)

	after := promtest.ToFloat64(metrics.AnalyticsIncompleteTotal.WithLabelValues(orgID, clusterUUID, "history"))
	assert.Equal(t, before+1, after)
}

func TestRunContainerRecommendations_StrictMode_BlocksOnHistoryFailure(t *testing.T) {
	t.Setenv("ROS_INGEST_STRICT_ANALYTICS", "true")
	config.ResetForTest()
	_ = config.GetConfig()

	pool := testutil.SetupTestDB(t)
	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })

	orgID := "org-analytics-strict"
	clusterUUID := "44444444-5555-6666-7777-888888888888"
	seedAnalyticsRecommendationFixture(t, pool, orgID, clusterUUID)

	engine.SetAnalyticsWriteHooksForTest(&engine.AnalyticsWriteHooks{
		ContainerHistory: engine.StubContainerHistoryHook(engine.ErrAnalyticsStub()),
	})
	t.Cleanup(func() { engine.SetAnalyticsWriteHooksForTest(nil) })

	kafkaMsg := types.KafkaMsg{}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID

	err := runContainerRecommendations(kafkaMsg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing recommendation history")

	ctx := context.Background()
	var recCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`,
		orgID, clusterUUID).Scan(&recCount))
	assert.Equal(t, 0, recCount, "strict mode should not persist recommendations when history fails")
}

func TestRunContainerRecommendations_DegradedMode_IncrementsQualityMetric(t *testing.T) {
	t.Setenv("ROS_INGEST_STRICT_ANALYTICS", "false")
	config.ResetForTest()
	_ = config.GetConfig()

	pool := testutil.SetupTestDB(t)
	origPool := db.Pool
	db.Pool = pool
	t.Cleanup(func() { db.Pool = origPool })

	orgID := "org-analytics-quality"
	clusterUUID := "55555555-6666-7777-8888-999999999999"
	seedAnalyticsRecommendationFixture(t, pool, orgID, clusterUUID)

	engine.SetAnalyticsWriteHooksForTest(&engine.AnalyticsWriteHooks{
		ContainerQuality: engine.StubContainerQualityHook(engine.ErrAnalyticsStub()),
	})
	t.Cleanup(func() { engine.SetAnalyticsWriteHooksForTest(nil) })

	before := promtest.ToFloat64(metrics.AnalyticsIncompleteTotal.WithLabelValues(orgID, clusterUUID, "quality"))

	kafkaMsg := types.KafkaMsg{}
	kafkaMsg.Metadata.Org_id = orgID
	kafkaMsg.Metadata.Cluster_uuid = clusterUUID

	err := runContainerRecommendations(kafkaMsg)
	require.NoError(t, err)

	ctx := context.Background()
	var recCount int
	require.NoError(t, pool.QueryRow(ctx,
		`SELECT count(*) FROM recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`,
		orgID, clusterUUID).Scan(&recCount))
	assert.Greater(t, recCount, 0)

	after := promtest.ToFloat64(metrics.AnalyticsIncompleteTotal.WithLabelValues(orgID, clusterUUID, "quality"))
	assert.Equal(t, before+1, after)
}
