package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

const oomTestNamespace = "oom-test-ns"
const oomTestWorkload = "oom-test-deploy"
const oomTestContainer = "oom-main"

func seedOOMRecommendationSet(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()

	containerID := model.NativeContainerID(
		testutil.TestClusterUUID, oomTestNamespace, oomTestWorkload, testutil.TestWorkloadType, oomTestContainer,
	)

	_, err := pool.Exec(ctx, `
		INSERT INTO recommendation_sets (
			org_id, cluster_uuid, namespace, workload, workload_type,
			container_name, container_id, term, engine, stale, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, 'short_term', 'cost', false, now())
		ON CONFLICT (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine) DO NOTHING`,
		testutil.TestOrgID, testutil.TestClusterUUID, oomTestNamespace,
		oomTestWorkload, testutil.TestWorkloadType, oomTestContainer, containerID,
	)
	require.NoError(t, err)

	return containerID
}

func setupOOMTestApp(t *testing.T) *echo.Echo {
	t.Helper()
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/containers/:recommendation-id/oom-timeline", api.GetOOMTimeline)
	return app
}

func TestGetOOMTimeline_HappyPath(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'oom-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	containerID := seedOOMRecommendationSet(t, pool)

	baseDate := testutil.BaseDate
	for i := range 5 {
		oomCount := int64(0)
		if i == 1 || i == 3 {
			oomCount = int64(i + 1)
		}
		testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
			BucketDate:    baseDate.AddDate(0, 0, i),
			OrgID:         testutil.TestOrgID,
			ClusterUUID:   testutil.TestClusterUUID,
			Namespace:     oomTestNamespace,
			Workload:      oomTestWorkload,
			WorkloadType:  testutil.TestWorkloadType,
			ContainerName: oomTestContainer,
			CPUUsageP95MC: 100,
			MemUsageP95KiB: 1024,
			OOMCountSum:   oomCount,
			SampleCount:   24,
		})
	}

	app := setupOOMTestApp(t)

	startStr := baseDate.Format("2006-01-02")
	endStr := baseDate.AddDate(0, 0, 4).Format("2006-01-02")

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/containers/"+containerID+"/oom-timeline?start_date="+startStr+"&end_date="+endStr,
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var resp model.OOMTimelineResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))

	assert.Equal(t, int64(6), resp.Meta.Count, "total OOM events: 2+4=6")
	assert.Len(t, resp.Data, 2, "only days with OOM > 0")
	assert.Equal(t, containerID, resp.Meta.ContainerID)
	assert.Equal(t, startStr, resp.Meta.StartDate)
	assert.Equal(t, endStr, resp.Meta.EndDate)

	assert.Equal(t, baseDate.AddDate(0, 0, 1).Format("2006-01-02"), resp.Data[0].Date)
	assert.Equal(t, int64(2), resp.Data[0].OOMKillCount)
	assert.Equal(t, baseDate.AddDate(0, 0, 3).Format("2006-01-02"), resp.Data[1].Date)
	assert.Equal(t, int64(4), resp.Data[1].OOMKillCount)
}

func TestGetOOMTimeline_EmptyResult(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'oom-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	containerID := seedOOMRecommendationSet(t, pool)

	testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
		BucketDate:    testutil.BaseDate,
		OrgID:         testutil.TestOrgID,
		ClusterUUID:   testutil.TestClusterUUID,
		Namespace:     oomTestNamespace,
		Workload:      oomTestWorkload,
		WorkloadType:  testutil.TestWorkloadType,
		ContainerName: oomTestContainer,
		CPUUsageP95MC: 100,
		MemUsageP95KiB: 1024,
		OOMCountSum:   0,
		SampleCount:   24,
	})

	app := setupOOMTestApp(t)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/containers/"+containerID+"/oom-timeline",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var resp model.OOMTimelineResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))

	assert.Equal(t, int64(0), resp.Meta.Count)
	assert.Empty(t, resp.Data)
	assert.Equal(t, containerID, resp.Meta.ContainerID)
}

func TestGetOOMTimeline_NotFound(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)

	app := setupOOMTestApp(t)

	fakeID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/containers/"+fakeID+"/oom-timeline",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, req)
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestGetOOMTimeline_InvalidDateRange(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'oom-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	containerID := seedOOMRecommendationSet(t, pool)
	app := setupOOMTestApp(t)

	tests := []struct {
		name  string
		query string
	}{
		{"start after end", "start_date=2026-06-15&end_date=2026-06-01"},
		{"invalid start format", "start_date=not-a-date"},
		{"invalid end format", "end_date=2026/06/01"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(
				http.MethodGet,
				"/api/cost-management/v1/recommendations/openshift/containers/"+containerID+"/oom-timeline?"+tt.query,
				nil,
			)
			req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
			recorder := httptest.NewRecorder()
			app.ServeHTTP(recorder, req)
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
		})
	}
}

func TestGetOOMTimeline_BadUUID(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	app := setupOOMTestApp(t)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/containers/not-a-uuid/oom-timeline",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, req)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestGetOOMTimeline_DefaultDateRange(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'oom-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	containerID := seedOOMRecommendationSet(t, pool)

	yesterday := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, -1)
	testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
		BucketDate:    yesterday,
		OrgID:         testutil.TestOrgID,
		ClusterUUID:   testutil.TestClusterUUID,
		Namespace:     oomTestNamespace,
		Workload:      oomTestWorkload,
		WorkloadType:  testutil.TestWorkloadType,
		ContainerName: oomTestContainer,
		CPUUsageP95MC: 100,
		MemUsageP95KiB: 1024,
		OOMCountSum:   7,
		SampleCount:   24,
	})

	app := setupOOMTestApp(t)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/containers/"+containerID+"/oom-timeline",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var resp model.OOMTimelineResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))

	assert.Equal(t, int64(7), resp.Meta.Count)
	assert.Len(t, resp.Data, 1)
	assert.Equal(t, yesterday.Format("2006-01-02"), resp.Data[0].Date)
}

func TestGetOOMTimeline_NoIdentity(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	app := setupOOMTestApp(t)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/containers/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/oom-timeline",
		nil,
	)
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, req)
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}
