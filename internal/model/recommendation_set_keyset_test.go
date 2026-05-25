package model_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestGetNativeRecommendations_KeysetPagination(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	connStr := pool.Config().ConnString()
	gormDB, err := gorm.Open(postgres.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	database.DB = gormDB
	t.Cleanup(func() { database.DB = nil })

	_, err = pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	testutil.SeedDigestSeriesFrom(t, pool, start, 7, 200, 10, 524288, 1024)
	testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
		BucketDate:    start,
		OrgID:         testutil.TestOrgID,
		ClusterUUID:   testutil.TestClusterUUID,
		Namespace:     testutil.TestNamespace,
		Workload:      "workload-b",
		WorkloadType:  testutil.TestWorkloadType,
		ContainerName: "container-b",
		CPURequestP50MC: 180,
		CPURequestP95MC: 210,
		CPUUsageP50MC:   170,
		CPUUsageP95MC:   200,
		MemRequestP50KiB: 512000,
		MemRequestP95KiB: 524288,
		MemUsageP50KiB:   500000,
		MemUsageP95KiB:   520000,
	})
	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	require.NoError(t, engine.WriteRecommendations(ctx, pool, recs))

	opts := listoptions.ListOptions{Limit: 1}
	page1, err := model.GetNativeRecommendations(testutil.TestOrgID, opts, nil, map[string][]string{"*": {}})
	require.NoError(t, err)
	require.Len(t, page1.Results, 1)
	assert.GreaterOrEqual(t, page1.Count, 2)
	assert.True(t, page1.HasNext)

	opts.HasCursor = true
	opts.AfterNamespace = page1.Results[0].Project
	opts.AfterWorkload = page1.Results[0].Workload
	opts.AfterContainer = page1.Results[0].Container
	page2, err := model.GetNativeRecommendations(testutil.TestOrgID, opts, nil, map[string][]string{"*": {}})
	require.NoError(t, err)
	require.Len(t, page2.Results, 1)
	assert.NotEqual(t, page1.Results[0].ID, page2.Results[0].ID)
}

func TestRefreshOrgRecommendationStats(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	connStr := pool.Config().ConnString()
	gormDB, err := gorm.Open(postgres.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	database.DB = gormDB
	t.Cleanup(func() { database.DB = nil })

	_, err = pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	testutil.SeedDigestSeriesFrom(t, pool, start, 7, 200, 10, 524288, 1024)
	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NoError(t, engine.WriteRecommendations(ctx, pool, recs))

	count, ok, err := model.GetOrgContainerCount(testutil.TestOrgID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, int64(1), count)
}
