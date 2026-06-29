package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func setupSnapshotAgeDistributionEcho() *echo.Echo {
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/snapshots/age-distribution", api.GetSnapshotAgeDistribution)
	return app
}

func seedSnapshotForAge(t *testing.T, orgID, clusterUUID string, ageDays int, idx int) {
	t.Helper()
	ctx := context.Background()
	pool := database.GetPool()
	snapName := "snap-age-" + uuid.New().String()[:8]
	_, err := pool.Exec(ctx, `
		INSERT INTO snapshot_recommendation_sets (
			org_id, cluster_uuid, namespace, snapshot_name,
			recommendation_type, age_days, restore_size_bytes,
			creation_timestamp, updated_at
		) VALUES ($1, $2, 'age-test-ns', $3, 'stale', $4, 0,
			NOW() - ($4::int * INTERVAL '1 day'), NOW())`,
		orgID, clusterUUID, snapName, ageDays,
	)
	require.NoError(t, err)
}

func TestGetSnapshotAgeDistribution_DefaultBoundaries(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	orgID := "org-age-dist-" + uuid.New().String()[:8]
	clusterUUID := testutil.TestClusterUUID
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'age-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, clusterUUID)
	require.NoError(t, err)

	// Seed snapshots across buckets: <7, 7-30, 30-90, 90+
	for i := range 3 {
		seedSnapshotForAge(t, orgID, clusterUUID, 2+i, i)
	}
	for i := range 2 {
		seedSnapshotForAge(t, orgID, clusterUUID, 15+i, 10+i)
	}
	seedSnapshotForAge(t, orgID, clusterUUID, 60, 20)
	for i := range 2 {
		seedSnapshotForAge(t, orgID, clusterUUID, 100+i, 30+i)
	}

	app := setupSnapshotAgeDistributionEcho()

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots/age-distribution", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.SnapshotAgeDistributionResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.Equal(t, 8, resp.Total)
	require.Len(t, resp.Buckets, 4)

	assert.Equal(t, "<7 days", resp.Buckets[0].Label)
	assert.Equal(t, 0, resp.Buckets[0].MinDays)
	assert.Equal(t, intPtr(6), resp.Buckets[0].MaxDays)
	assert.Equal(t, 3, resp.Buckets[0].Count)

	assert.Equal(t, "7-30 days", resp.Buckets[1].Label)
	assert.Equal(t, 7, resp.Buckets[1].MinDays)
	assert.Equal(t, intPtr(29), resp.Buckets[1].MaxDays)
	assert.Equal(t, 2, resp.Buckets[1].Count)

	assert.Equal(t, "30-90 days", resp.Buckets[2].Label)
	assert.Equal(t, 30, resp.Buckets[2].MinDays)
	assert.Equal(t, intPtr(89), resp.Buckets[2].MaxDays)
	assert.Equal(t, 1, resp.Buckets[2].Count)

	assert.Equal(t, "90+ days", resp.Buckets[3].Label)
	assert.Equal(t, 90, resp.Buckets[3].MinDays)
	assert.Nil(t, resp.Buckets[3].MaxDays)
	assert.Equal(t, 2, resp.Buckets[3].Count)
}

func TestGetSnapshotAgeDistribution_CustomBoundaries(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	orgID := "org-age-custom-" + uuid.New().String()[:8]
	clusterUUID := testutil.TestClusterUUID
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'age-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, clusterUUID)
	require.NoError(t, err)

	seedSnapshotForAge(t, orgID, clusterUUID, 3, 0)
	seedSnapshotForAge(t, orgID, clusterUUID, 20, 1)

	app := setupSnapshotAgeDistributionEcho()

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots/age-distribution?bucket_boundaries=14,60", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.SnapshotAgeDistributionResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.Equal(t, 2, resp.Total)
	require.Len(t, resp.Buckets, 3)
	assert.Equal(t, "<14 days", resp.Buckets[0].Label)
	assert.Equal(t, 1, resp.Buckets[0].Count)
	assert.Equal(t, "14-60 days", resp.Buckets[1].Label)
	assert.Equal(t, 1, resp.Buckets[1].Count)
	assert.Equal(t, "60+ days", resp.Buckets[2].Label)
	assert.Equal(t, 0, resp.Buckets[2].Count)
}

func TestGetSnapshotAgeDistribution_Empty(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	orgID := "org-age-empty-" + uuid.New().String()[:8]
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)

	app := setupSnapshotAgeDistributionEcho()

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots/age-distribution", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp api.SnapshotAgeDistributionResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.Equal(t, 0, resp.Total)
	require.Len(t, resp.Buckets, 4)
	for _, b := range resp.Buckets {
		assert.Equal(t, 0, b.Count)
	}
}

func TestGetSnapshotAgeDistribution_InvalidBoundaries(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	orgID := "org-age-invalid-" + uuid.New().String()[:8]
	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)

	app := setupSnapshotAgeDistributionEcho()

	tests := []struct {
		name  string
		query string
	}{
		{"non-numeric", "bucket_boundaries=abc"},
		{"negative values", "bucket_boundaries=-1,10"},
		{"zero value", "bucket_boundaries=0,10"},
		{"not ascending", "bucket_boundaries=30,7,90"},
		{"duplicates", "bucket_boundaries=7,7,30"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet,
				"/api/cost-management/v1/recommendations/openshift/snapshots/age-distribution?"+tt.query, nil)
			req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusBadRequest, rec.Code)
		})
	}
}

func TestGetSnapshotAgeDistribution_NoIdentity(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	app := setupSnapshotAgeDistributionEcho()

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/snapshots/age-distribution", nil)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func intPtr(v int) *int { return &v }
