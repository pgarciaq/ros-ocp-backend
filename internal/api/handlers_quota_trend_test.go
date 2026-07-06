package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

const quotaTrendTestNamespace = "quota-trend-ns"

func setupQuotaTrendTestApp(t *testing.T) *echo.Echo {
	t.Helper()
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/quota/:quota-id/trend", api.GetQuotaTrend)
	return app
}

func seedQuotaRecSet(t *testing.T, ctx context.Context) string {
	t.Helper()
	pool := database.GetPool()
	require.NotNil(t, pool)

	quotaID := model.NativeQuotaID(testutil.TestClusterUUID, quotaTrendTestNamespace, "")
	_, err := pool.Exec(ctx, `
		INSERT INTO quota_recommendation_sets (
			org_id, cluster_uuid, namespace, quota_name,
			quota_id, recommendation_type, risk_level, headroom_basis_points,
			last_observed_at
		) VALUES ($1, $2, $3, '', $4, 'tighten', 'low', 2000, now())
		ON CONFLICT (org_id, cluster_uuid, namespace, quota_name) DO UPDATE SET
			quota_id = EXCLUDED.quota_id`,
		testutil.TestOrgID, testutil.TestClusterUUID, quotaTrendTestNamespace, quotaID,
	)
	require.NoError(t, err)

	return quotaID
}

func TestGetQuotaTrend_HappyPath(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'quota-trend-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	quotaID := seedQuotaRecSet(t, ctx)

	baseDate := testutil.BaseDate
	cpuHard := int64(4000)
	memHard := int64(8589934592) // 8 GiB
	for i := range 5 {
		cpuUsed := int64(1000 + i*200)
		memUsed := int64(2147483648 + int64(i)*536870912) // 2 GiB + i*512 MiB
		testutil.SeedNamespaceQuotaDigest(t, pool, testutil.NamespaceQuotaDigestRow{
			ReportDate:        baseDate.AddDate(0, 0, i),
			OrgID:             testutil.TestOrgID,
			ClusterUUID:       testutil.TestClusterUUID,
			Namespace:         quotaTrendTestNamespace,
			CPURequestHard:    &cpuHard,
			CPURequestUsed:    &cpuUsed,
			MemoryRequestHard: &memHard,
			MemoryRequestUsed: &memUsed,
		})
	}

	app := setupQuotaTrendTestApp(t)

	startStr := baseDate.Format("2006-01-02")
	endStr := baseDate.AddDate(0, 0, 4).Format("2006-01-02")

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/quota/"+quotaID+"/trend?start_date="+startStr+"&end_date="+endStr,
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var resp model.QuotaTrendResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))

	assert.Equal(t, 5, resp.Meta.Count)
	assert.Len(t, resp.Data, 5)
	assert.Equal(t, quotaTrendTestNamespace, resp.Meta.Namespace)
	assert.Equal(t, testutil.TestClusterUUID, resp.Meta.ClusterUUID)
	assert.Equal(t, startStr, resp.Meta.StartDate)
	assert.Equal(t, endStr, resp.Meta.EndDate)

	assert.Equal(t, baseDate.Format("2006-01-02"), resp.Data[0].Date)
	assert.NotNil(t, resp.Data[0].CPURequestHardMillicores)
	assert.Equal(t, int64(4000), *resp.Data[0].CPURequestHardMillicores)
	assert.NotNil(t, resp.Data[0].CPURequestUsedMillicores)
	assert.Equal(t, int64(1000), *resp.Data[0].CPURequestUsedMillicores)
}

func TestGetQuotaTrend_EmptyResult(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'quota-trend-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	quotaID := seedQuotaRecSet(t, ctx)

	app := setupQuotaTrendTestApp(t)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/quota/"+quotaID+"/trend",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var resp model.QuotaTrendResponse
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))

	assert.Equal(t, 0, resp.Meta.Count)
	assert.Empty(t, resp.Data)
}

func TestGetQuotaTrend_NotFound(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)

	app := setupQuotaTrendTestApp(t)

	fakeID := "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/quota/"+fakeID+"/trend",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, req)
	assert.Equal(t, http.StatusNotFound, recorder.Code)
}

func TestGetQuotaTrend_InvalidDateRange(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'quota-trend-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	quotaID := seedQuotaRecSet(t, ctx)
	app := setupQuotaTrendTestApp(t)

	tests := []struct {
		name  string
		query string
	}{
		{"start after end", "start_date=2026-06-15&end_date=2026-06-01"},
		{"invalid start format", "start_date=not-a-date"},
		{"invalid end format", "end_date=2026/06/01"},
		{"date range exceeds 90 days", "start_date=2025-01-01&end_date=2025-12-31"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(
				http.MethodGet,
				"/api/cost-management/v1/recommendations/openshift/quota/"+quotaID+"/trend?"+tt.query,
				nil,
			)
			req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
			recorder := httptest.NewRecorder()
			app.ServeHTTP(recorder, req)
			assert.Equal(t, http.StatusBadRequest, recorder.Code)
		})
	}
}

func TestGetQuotaTrend_BadUUID(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	app := setupQuotaTrendTestApp(t)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/quota/not-a-uuid/trend",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, req)
	assert.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestGetQuotaTrend_NoIdentity(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	app := setupQuotaTrendTestApp(t)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/quota/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/trend",
		nil,
	)
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, req)
	assert.Equal(t, http.StatusUnauthorized, recorder.Code)
}
