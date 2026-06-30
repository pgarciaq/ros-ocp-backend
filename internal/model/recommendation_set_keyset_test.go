package model_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/tags"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestGetNativeRecommendations_KeysetPagination(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
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
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	queryParams := map[string]interface{}{"rs.stale = ?": false}
	opts := listoptions.ListOptions{
		Limit:    1,
		OrderBy:  listoptions.DefaultContainerRecsDBColumn,
		OrderHow: listoptions.OrderDesc,
	}
	page1, err := model.GetNativeRecommendations(testutil.TestOrgID, opts, queryParams, map[string][]string{"*": {}})
	require.NoError(t, err)
	require.Len(t, page1.Results, 1)
	assert.GreaterOrEqual(t, page1.Count, 2)
	assert.True(t, page1.HasNext)

	opts.HasCursor = true
	opts.AfterNamespace = page1.LastAnchor.Namespace
	opts.AfterWorkload = page1.LastAnchor.Workload
	opts.AfterWorkloadType = page1.LastAnchor.WorkloadType
	opts.AfterContainer = page1.LastAnchor.ContainerName
	opts.AfterContainerClusterUUID = page1.LastAnchor.ClusterUUID
	opts.AfterContainerSortPresent = true
	opts.AfterContainerSortValue = page1.LastAnchor.SortValue
	page2, err := model.GetNativeRecommendations(testutil.TestOrgID, opts, queryParams, map[string][]string{"*": {}})
	require.NoError(t, err)
	require.Len(t, page2.Results, 1)
	assert.NotEqual(t, page1.Results[0].ID, page2.Results[0].ID)
}

func TestGetNativeRecommendations_OffsetPaginationCountConsistent(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	setupNativeListGormDB(t, pool)
	seedNativeListCluster(t, pool, testutil.TestOrgID, testutil.TestClusterUUID, "test-cluster", 1)

	const pageNamespace = "offset-page-ns"
	start := testutil.RecentStart()
	for i, name := range []string{"container-a", "container-b", "container-c"} {
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:       start,
			OrgID:            testutil.TestOrgID,
			ClusterUUID:      testutil.TestClusterUUID,
			Namespace:        pageNamespace,
			Workload:         "workload-page",
			WorkloadType:     testutil.TestWorkloadType,
			ContainerName:    name,
			CPURequestP50MC:  int64(180 + i*10),
			CPURequestP95MC:  int64(210 + i*10),
			CPUUsageP50MC:    int64(170 + i*10),
			CPUUsageP95MC:    int64(200 + i*10),
			MemRequestP50KiB: 512000,
			MemRequestP95KiB: 524288,
			MemUsageP50KiB:   500000,
			MemUsageP95KiB:   520000,
		})
	}
	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	queryParams := map[string]interface{}{
		"rs.stale = ?":     false,
		"rs.namespace = ?": pageNamespace,
	}
	page1, err := model.GetNativeRecommendations(testutil.TestOrgID, listoptions.ListOptions{
		Limit:  2,
		Offset: 0,
	}, queryParams, map[string][]string{"*": {}})
	require.NoError(t, err)
	require.Len(t, page1.Results, 2)
	assert.Equal(t, 3, page1.Count)
	assert.True(t, page1.HasNext)

	page2, err := model.GetNativeRecommendations(testutil.TestOrgID, listoptions.ListOptions{
		Limit:  2,
		Offset: 2,
	}, queryParams, map[string][]string{"*": {}})
	require.NoError(t, err)
	require.Len(t, page2.Results, 1)
	assert.Equal(t, page1.Count, page2.Count)
	assert.False(t, page2.HasNext)

	seen := map[string]struct{}{}
	for _, r := range append(page1.Results, page2.Results...) {
		seen[r.Container] = struct{}{}
	}
	assert.Len(t, seen, 3)
}

func setupNativeListGormDB(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })
}

func seedNativeListCluster(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID, alias string, tenantID int) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, tenantID, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES ($1, $2, $3, 'src-1', now()) ON CONFLICT DO NOTHING`, tenantID, clusterUUID, alias)
	require.NoError(t, err)
}

func TestGetNativeRecommendations_UsesOrgContainerKeys(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	setupNativeListGormDB(t, pool)
	seedNativeListCluster(t, pool, testutil.TestOrgID, testutil.TestClusterUUID, "test-cluster", 1)

	start := testutil.RecentStart()
	testutil.SeedDigestSeriesFrom(t, pool, start, 7, 200, 10, 524288, 1024)
	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	var keyCount int
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM org_container_keys WHERE org_id = $1`, testutil.TestOrgID).Scan(&keyCount)
	require.NoError(t, err)
	require.Equal(t, 1, keyCount)

	queryParams := map[string]interface{}{"rs.stale = ?": false}
	page, err := model.GetNativeRecommendations(testutil.TestOrgID, listoptions.ListOptions{Limit: 10}, queryParams, map[string][]string{"*": {}})
	require.NoError(t, err)
	require.Len(t, page.Results, 1)
	assert.Equal(t, testutil.TestContainer, page.Results[0].Container)
}

func TestGetNativeRecommendations_RBACClusterFilter(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	setupNativeListGormDB(t, pool)

	cfg := config.GetConfig()
	origRBAC := cfg.RBACEnabled
	cfg.RBACEnabled = true
	t.Cleanup(func() { cfg.RBACEnabled = origRBAC })

	clusterAllowed := "11111111-1111-1111-1111-111111111111"
	clusterDenied := "22222222-2222-2222-2222-222222222222"
	seedNativeListCluster(t, pool, testutil.TestOrgID, clusterAllowed, "allowed", 1)
	seedNativeListCluster(t, pool, testutil.TestOrgID, clusterDenied, "denied", 1)

	start := testutil.RecentStart()
	end := start.AddDate(0, 0, 6)
	for i := 0; i < 7; i++ {
		for _, spec := range []struct {
			clusterUUID, namespace string
		}{
			{clusterAllowed, "allowed-namespace"},
			{clusterDenied, "denied-namespace"},
		} {
			testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
				BucketDate:       start.AddDate(0, 0, i),
				OrgID:            testutil.TestOrgID,
				ClusterUUID:      spec.clusterUUID,
				Namespace:        spec.namespace,
				Workload:         testutil.TestWorkload,
				WorkloadType:     testutil.TestWorkloadType,
				ContainerName:    testutil.TestContainer,
				CPURequestP50MC:  180,
				CPURequestP95MC:  210,
				CPUUsageP50MC:    170,
				CPUUsageP95MC:    200,
				MemRequestP50KiB: 512000,
				MemRequestP95KiB: 524288,
				MemUsageP50KiB:   500000,
				MemUsageP95KiB:   520000,
			})
		}
	}
	for _, spec := range []struct {
		clusterUUID, namespace string
	}{
		{clusterAllowed, "allowed-namespace"},
		{clusterDenied, "denied-namespace"},
	} {
		recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, spec.clusterUUID, start, end, engine.OOMConfig{})
		require.NoError(t, err)
		require.NotEmpty(t, recs)
		require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))
	}

	queryParams := map[string]interface{}{"rs.stale = ?": false}
	perms := map[string][]string{"openshift.cluster": {clusterAllowed}}
	page, err := model.GetNativeRecommendations(testutil.TestOrgID, listoptions.ListOptions{Limit: 10}, queryParams, perms)
	require.NoError(t, err)
	require.Len(t, page.Results, 1)
	assert.Equal(t, clusterAllowed, page.Results[0].ClusterUUID)
}

func TestGetNativeRecommendations_WorkloadTypeFilter(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	setupNativeListGormDB(t, pool)
	seedNativeListCluster(t, pool, testutil.TestOrgID, testutil.TestClusterUUID, "test-cluster", 1)

	// Use different workloads so that org_container_keys (which collapses
	// workload_type out of the PK) creates distinct key rows.
	for _, spec := range []struct {
		workload, workloadType, container string
	}{
		{"api-deploy", "deployment", "main"},
		{"api-sts", "statefulset", "main"},
	} {
		_, err := pool.Exec(ctx, `
			INSERT INTO recommendation_sets (
				org_id, cluster_uuid, namespace, workload, workload_type, container_name,
				term, engine, stale, notification_codes, estimated_savings_cents, updated_at
			) VALUES ($1, $2, 'shared-ns', $3, $4, $5, 'short', 'cost', false, '{}', 0, now())`,
			testutil.TestOrgID, testutil.TestClusterUUID,
			spec.workload, spec.workloadType, spec.container,
		)
		require.NoError(t, err)
	}

	require.NoError(t, model.RefreshOrgContainerKeys(ctx, pool, testutil.TestOrgID))

	queryParams := map[string]interface{}{
		"rs.stale = ?":                false,
		"LOWER(rs.workload_type) = ?": []string{"deployment"},
	}
	page, err := model.GetNativeRecommendations(
		testutil.TestOrgID,
		listoptions.ListOptions{Limit: 10},
		queryParams,
		map[string][]string{"*": {}},
	)
	require.NoError(t, err)
	require.Len(t, page.Results, 1)
	assert.Equal(t, "deployment", page.Results[0].WorkloadType)
}

func TestGetNativeRecommendations_NamespaceFilter(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	setupNativeListGormDB(t, pool)
	seedNativeListCluster(t, pool, testutil.TestOrgID, testutil.TestClusterUUID, "test-cluster", 1)

	start := testutil.RecentStart()
	testutil.SeedDigestSeriesFrom(t, pool, start, 7, 200, 10, 524288, 1024)
	testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
		BucketDate:      start,
		OrgID:           testutil.TestOrgID,
		ClusterUUID:     testutil.TestClusterUUID,
		Namespace:       "other-namespace",
		Workload:        "workload-b",
		WorkloadType:    testutil.TestWorkloadType,
		ContainerName:   "container-b",
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
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	queryParams := map[string]interface{}{
		"rs.stale = ?":  false,
		"rs.namespace = ?": testutil.TestNamespace,
	}
	page, err := model.GetNativeRecommendations(testutil.TestOrgID, listoptions.ListOptions{Limit: 10}, queryParams, map[string][]string{"*": {}})
	require.NoError(t, err)
	require.Len(t, page.Results, 1)
	assert.Equal(t, testutil.TestNamespace, page.Results[0].Project)
}

func TestRefreshOrgRecommendationStats(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'test-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	testutil.SeedDigestSeriesFrom(t, pool, start, 7, 200, 10, 524288, 1024)
	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	count, ok, err := model.GetOrgContainerCount(testutil.TestOrgID)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, int64(1), count)
}

func TestGetNativeRecommendations_TagFilter(t *testing.T) {
	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "true")
	t.Setenv("ROS_TAGS_SOURCE", "api")
	require.True(t, config.TagsFeatureEnabled())

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	setupNativeListGormDB(t, pool)
	seedNativeListCluster(t, pool, testutil.TestOrgID, testutil.TestClusterUUID, "test-cluster", 1)

	start := testutil.RecentStart()
	testutil.SeedDigestSeriesFrom(t, pool, start, 7, 200, 10, 524288, 1024)
	testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
		BucketDate:       start,
		OrgID:            testutil.TestOrgID,
		ClusterUUID:      testutil.TestClusterUUID,
		Namespace:        "other-namespace",
		Workload:         "workload-b",
		WorkloadType:     testutil.TestWorkloadType,
		ContainerName:    "container-b",
		CPURequestP50MC:  180,
		CPURequestP95MC:  210,
		CPUUsageP50MC:    170,
		CPUUsageP95MC:    200,
		MemRequestP50KiB: 512000,
		MemRequestP95KiB: 524288,
		MemUsageP50KiB:   500000,
		MemUsageP95KiB:   520000,
	})
	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	svc := tags.NewSyncService(pool)
	updated, err := svc.SyncOrgTags(ctx, tags.SyncRequest{
		OrgID: testutil.TestOrgID,
		NamespaceTags: []tags.NamespaceTags{
			{
				ClusterUUID: testutil.TestClusterUUID,
				Namespace:   testutil.TestNamespace,
				Tags:        map[string]string{"environment": "production"},
			},
			{
				ClusterUUID: testutil.TestClusterUUID,
				Namespace:   "other-namespace",
				Tags:        map[string]string{"environment": "staging"},
			},
		},
	})
	require.NoError(t, err)
	require.Equal(t, 2, updated)

	queryParams := map[string]interface{}{
		"rs.stale = ?":           false,
		model.TagFiltersQueryKey: []model.TagFilter{{Key: "environment", Values: []string{"production"}}},
	}
	page, err := model.GetNativeRecommendations(testutil.TestOrgID, listoptions.ListOptions{Limit: 10}, queryParams, map[string][]string{"*": {}})
	require.NoError(t, err)
	require.Len(t, page.Results, 1)
	assert.Equal(t, testutil.TestNamespace, page.Results[0].Project)
}

func TestGetNativeRecommendations_TagFilterIgnoredWhenDisabled(t *testing.T) {
	config.ResetTagsForTest()
	t.Setenv("ROS_TAGS_ENABLED", "false")
	require.False(t, config.TagsFeatureEnabled())

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	setupNativeListGormDB(t, pool)
	seedNativeListCluster(t, pool, testutil.TestOrgID, testutil.TestClusterUUID, "test-cluster", 1)

	start := testutil.RecentStart()
	testutil.SeedDigestSeriesFrom(t, pool, start, 7, 200, 10, 524288, 1024)
	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	_, err = pool.Exec(ctx, `
		UPDATE org_container_keys SET resolved_tags = '{"environment":"production"}'::jsonb
		WHERE org_id = $1`, testutil.TestOrgID)
	require.NoError(t, err)

	queryParams := map[string]interface{}{
		"rs.stale = ?":           false,
		model.TagFiltersQueryKey: []model.TagFilter{{Key: "environment", Values: []string{"production"}}},
	}
	page, err := model.GetNativeRecommendations(testutil.TestOrgID, listoptions.ListOptions{Limit: 10}, queryParams, map[string][]string{"*": {}})
	require.NoError(t, err)
	require.Len(t, page.Results, 1)
}

func TestGetNativeRecommendations_StaleFilter_DefaultExcludesStale(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	setupNativeListGormDB(t, pool)
	seedNativeListCluster(t, pool, testutil.TestOrgID, testutil.TestClusterUUID, "test-cluster", 1)

	now := time.Now().UTC()
	insertRecommendationSetRow(t, ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID,
		"ns-a", "deploy-a", "Deployment", "ctr-fresh", "short", "cost", false, now)
	insertRecommendationSetRow(t, ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID,
		"ns-a", "deploy-b", "Deployment", "ctr-stale", "short", "cost", true, now)
	require.NoError(t, model.RefreshOrgContainerKeys(ctx, pool, testutil.TestOrgID))

	queryParams := map[string]interface{}{"rs.stale = ?": false}
	page, err := model.GetNativeRecommendations(
		testutil.TestOrgID, listoptions.ListOptions{Limit: 10},
		queryParams, map[string][]string{"*": {}},
	)
	require.NoError(t, err)
	require.Len(t, page.Results, 1)
	assert.Equal(t, "ctr-fresh", page.Results[0].Container)
	assert.Equal(t, 1, page.Count)
}

func TestGetNativeRecommendations_StaleFilter_Only(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	setupNativeListGormDB(t, pool)
	seedNativeListCluster(t, pool, testutil.TestOrgID, testutil.TestClusterUUID, "test-cluster", 1)

	now := time.Now().UTC()
	insertRecommendationSetRow(t, ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID,
		"ns-a", "deploy-a", "Deployment", "ctr-fresh", "short", "cost", false, now)
	insertRecommendationSetRow(t, ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID,
		"ns-a", "deploy-b", "Deployment", "ctr-stale", "short", "cost", true, now)
	require.NoError(t, model.RefreshOrgContainerKeys(ctx, pool, testutil.TestOrgID))

	queryParams := map[string]interface{}{"rs.stale = ?": true}
	page, err := model.GetNativeRecommendations(
		testutil.TestOrgID, listoptions.ListOptions{Limit: 10},
		queryParams, map[string][]string{"*": {}},
	)
	require.NoError(t, err)
	require.Len(t, page.Results, 1)
	assert.Equal(t, "ctr-stale", page.Results[0].Container)
	assert.Equal(t, 1, page.Count)
}

func TestGetNativeRecommendations_StaleFilter_Both(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	setupNativeListGormDB(t, pool)
	seedNativeListCluster(t, pool, testutil.TestOrgID, testutil.TestClusterUUID, "test-cluster", 1)

	now := time.Now().UTC()
	insertRecommendationSetRow(t, ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID,
		"ns-a", "deploy-a", "Deployment", "ctr-fresh", "short", "cost", false, now)
	insertRecommendationSetRow(t, ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID,
		"ns-a", "deploy-b", "Deployment", "ctr-stale", "short", "cost", true, now)
	require.NoError(t, model.RefreshOrgContainerKeys(ctx, pool, testutil.TestOrgID))

	// filter[stale]=true means no stale key in queryParams (show both).
	queryParams := map[string]interface{}{}
	page, err := model.GetNativeRecommendations(
		testutil.TestOrgID, listoptions.ListOptions{Limit: 10},
		queryParams, map[string][]string{"*": {}},
	)
	require.NoError(t, err)
	require.Len(t, page.Results, 2)
	assert.Equal(t, 2, page.Count)
}

func TestGetNativeRecommendations_StaleFilter_CountConsistency(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	setupNativeListGormDB(t, pool)
	seedNativeListCluster(t, pool, testutil.TestOrgID, testutil.TestClusterUUID, "test-cluster", 1)

	now := time.Now().UTC()
	for i, stale := range []bool{false, false, false, true, true} {
		ctr := "ctr-" + string(rune('a'+i))
		wl := "deploy-" + string(rune('a'+i))
		insertRecommendationSetRow(t, ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID,
			"ns-a", wl, "Deployment", ctr, "short", "cost", stale, now)
	}
	require.NoError(t, model.RefreshOrgContainerKeys(ctx, pool, testutil.TestOrgID))

	tests := []struct {
		name        string
		queryParams map[string]interface{}
		wantCount   int
	}{
		{"default_excludes_stale", map[string]interface{}{"rs.stale = ?": false}, 3},
		{"only_stale", map[string]interface{}{"rs.stale = ?": true}, 2},
		{"both", map[string]interface{}{}, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			page, err := model.GetNativeRecommendations(
				testutil.TestOrgID, listoptions.ListOptions{Limit: 100},
				tt.queryParams, map[string][]string{"*": {}},
			)
			require.NoError(t, err)
			assert.Len(t, page.Results, tt.wantCount)
			assert.Equal(t, tt.wantCount, page.Count)
		})
	}
}
