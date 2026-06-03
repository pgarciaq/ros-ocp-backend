package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestContainerHistoryFailurePreservesRecommendations(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	batch := []ContainerRec{
		{
			OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
			Namespace: testutil.TestNamespace, Workload: testutil.TestWorkload,
			WorkloadType: testutil.TestWorkloadType, ContainerName: testutil.TestContainer,
			Term: "short", Engine: "cost",
			RecCPURequestMC: 100, RecCPULimitMC: 200,
			RecMemRequestKiB: 1024, RecMemLimitKiB: 2048,
		},
	}

	require.NoError(t, WriteRecommendations(ctx, pool, batch))

	SetAnalyticsWriteHooksForTest(&AnalyticsWriteHooks{
		ContainerHistory: func(context.Context, *pgxpool.Pool, []ContainerRec, string) error {
			return fmt.Errorf("simulated history write failure")
		},
	})
	t.Cleanup(func() { SetAnalyticsWriteHooksForTest(nil) })

	histErr := WriteContainerHistory(ctx, pool, batch, "")
	require.Error(t, histErr)

	var recCount int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`,
		testutil.TestOrgID, testutil.TestClusterUUID,
	).Scan(&recCount)
	require.NoError(t, err)
	assert.Greater(t, recCount, 0, "recommendations must remain after history failure")
}

func TestContainerQualityFailurePreservesRecommendations(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	batch := []ContainerRec{
		{
			OrgID: testutil.TestOrgID, ClusterUUID: testutil.TestClusterUUID,
			Namespace: testutil.TestNamespace, Workload: testutil.TestWorkload,
			WorkloadType: testutil.TestWorkloadType, ContainerName: testutil.TestContainer,
			Term: "short", Engine: "cost",
			RecCPURequestMC: 200, RecCPULimitMC: 220,
			RecMemRequestKiB: 2048, RecMemLimitKiB: 4096,
		},
	}
	require.NoError(t, WriteRecommendations(ctx, pool, batch))

	key := containerKey{
		Namespace: testutil.TestNamespace, Workload: testutil.TestWorkload,
		WorkloadType: testutil.TestWorkloadType, ContainerName: testutil.TestContainer,
	}
	oldRecs := map[string]map[containerKey]OldRecommendation{
		"cost":        {key: {RecCPURequestMC: 100, RecMemRequestKiB: 1024}},
		"performance": {},
	}

	SetAnalyticsWriteHooksForTest(&AnalyticsWriteHooks{
		ContainerQuality: func(
			context.Context, *pgxpool.Pool, []ContainerRec,
			map[string]map[containerKey]OldRecommendation, map[containerKey]int64,
		) error {
			return errors.New("simulated quality write failure")
		},
	})
	t.Cleanup(func() { SetAnalyticsWriteHooksForTest(nil) })

	qualErr := WriteContainerQuality(ctx, pool, batch, oldRecs, OOMCountsByContainer(batch))
	require.Error(t, qualErr)

	var recCount int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM recommendation_sets WHERE org_id = $1 AND cluster_uuid = $2`,
		testutil.TestOrgID, testutil.TestClusterUUID,
	).Scan(&recCount)
	require.NoError(t, err)
	assert.Greater(t, recCount, 0, "recommendations must remain after quality failure")
}

func TestNamespaceHistoryPermanentFailurePreservesRecommendations(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	testutil.SeedNamespaceDigestSeries(t, pool, "ns-analytics-degraded", 7, 200, 10, 524288, 1024)
	end := testutil.BaseDate.AddDate(0, 0, 6)

	results, err := RecommendAllNamespaces(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	require.NoError(t, WriteNamespaceRecommendations(ctx, pool, results))

	SetAnalyticsWriteHooksForTest(&AnalyticsWriteHooks{
		NamespaceHistory: func(context.Context, *pgxpool.Pool, []NamespaceRec) error {
			return &pgconn.PgError{Code: "23505", Message: "simulated constraint violation"}
		},
	})
	t.Cleanup(func() { SetAnalyticsWriteHooksForTest(nil) })

	degraded, retryErr := WriteNamespaceRecommendationHistories(ctx, pool, results, nil, func(error) bool { return false })
	require.NoError(t, retryErr, "permanent history errors must not trigger message retry")
	require.True(t, degraded)

	var recCount int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM namespace_recommendation_sets
		 WHERE org_id = $1 AND namespace_name = $2 AND term IS NOT NULL`,
		testutil.TestOrgID, "ns-analytics-degraded",
	).Scan(&recCount)
	require.NoError(t, err)
	assert.Greater(t, recCount, 0, "namespace recommendations must remain after history failure")
}

func TestNamespaceHistoryTransientFailureReturnsRetryError(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	testutil.SeedNamespaceDigestSeries(t, pool, "ns-analytics-retry", 7, 200, 10, 524288, 1024)
	end := testutil.BaseDate.AddDate(0, 0, 6)

	results, err := RecommendAllNamespaces(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	require.NotEmpty(t, results)

	require.NoError(t, WriteNamespaceRecommendations(ctx, pool, results))

	SetAnalyticsWriteHooksForTest(&AnalyticsWriteHooks{
		NamespaceHistory: func(context.Context, *pgxpool.Pool, []NamespaceRec) error {
			return &pgconn.PgError{Code: "08006", Message: "connection failure"}
		},
	})
	t.Cleanup(func() { SetAnalyticsWriteHooksForTest(nil) })

	degraded, retryErr := WriteNamespaceRecommendationHistories(ctx, pool, results, nil, func(err error) bool {
		var pgErr *pgconn.PgError
		return errors.As(err, &pgErr) && pgErr.Code == "08006"
	})
	require.Error(t, retryErr, "transient history errors must be retryable")
	require.False(t, degraded)
}
