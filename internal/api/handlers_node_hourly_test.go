package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func nodeHourlyApp() *echo.Echo {
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/node/:id/hourly-utilization", api.GetNodeHourlyUtilization)
	return app
}

func seedNodeHourlyData(t *testing.T, ctx context.Context) {
	t.Helper()
	pool := database.GetPool()

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'node-hourly-cluster', 'src-nh', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	reportDate := time.Now().UTC().AddDate(0, 0, -1).Truncate(24 * time.Hour)
	testutil.EnsureMonthlyPartition(t, pool, "hourly_node_digests", reportDate)

	for hour := 0; hour < 3; hour++ {
		_, err = pool.Exec(ctx, `
			INSERT INTO hourly_node_digests (org_id, cluster_uuid, node_name, report_date, hour,
				cpu_usage_p95_mc, mem_usage_p95_kib, sample_count, max_pod_count)
			VALUES ($1, $2::uuid, 'worker-01', $3, $4, $5, $6, $7, $8)`,
			testutil.TestOrgID, testutil.TestClusterUUID, reportDate, hour,
			100*(hour+1), 2048*(hour+1), 4, 10+hour,
		)
		require.NoError(t, err)
	}
}

func TestGetNodeHourlyUtilization_HappyPath(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	seedNodeHourlyData(t, ctx)

	app := nodeHourlyApp()
	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	url := fmt.Sprintf(
		"/api/cost-management/v1/recommendations/openshift/node/worker-01/hourly-utilization?cluster_uuid=%s",
		testutil.TestClusterUUID,
	)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp api.NodeHourlyUtilizationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.Equal(t, 3, resp.Meta.Count)
	assert.Equal(t, 14, resp.Meta.Days)
	require.Len(t, resp.Data, 3)

	assert.Equal(t, 0, resp.Data[0].Hour)
	assert.Equal(t, 100, resp.Data[0].CPUUsageP95MC)
	assert.Equal(t, 2048, resp.Data[0].MemUsageP95KiB)
	assert.Equal(t, 4, resp.Data[0].SampleCount)
	assert.Equal(t, 10, resp.Data[0].MaxPodCount)
	assert.NotEmpty(t, resp.Data[0].ReportDate)

	assert.Equal(t, 1, resp.Data[1].Hour)
	assert.Equal(t, 200, resp.Data[1].CPUUsageP95MC)

	assert.Equal(t, 2, resp.Data[2].Hour)
	assert.Equal(t, 300, resp.Data[2].CPUUsageP95MC)
}

func TestGetNodeHourlyUtilization_EmptyData(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'empty-nh-cluster', 'src-empty-nh', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := nodeHourlyApp()
	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	url := fmt.Sprintf(
		"/api/cost-management/v1/recommendations/openshift/node/nonexistent-node/hourly-utilization?cluster_uuid=%s",
		testutil.TestClusterUUID,
	)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp api.NodeHourlyUtilizationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.Equal(t, 0, resp.Meta.Count)
	assert.Empty(t, resp.Data)
}

func TestGetNodeHourlyUtilization_MissingClusterUUID(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	app := nodeHourlyApp()
	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/node/worker-01/hourly-utilization", nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "error", body["status"])
	assert.Contains(t, body["message"], "cluster_uuid is required")
}

func TestGetNodeHourlyUtilization_CustomDays(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	seedNodeHourlyData(t, ctx)

	app := nodeHourlyApp()
	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	url := fmt.Sprintf(
		"/api/cost-management/v1/recommendations/openshift/node/worker-01/hourly-utilization?cluster_uuid=%s&days=7",
		testutil.TestClusterUUID,
	)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp api.NodeHourlyUtilizationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.Equal(t, 7, resp.Meta.Days)
	assert.Equal(t, 3, resp.Meta.Count)
}

func TestGetNodeHourlyUtilization_DaysCappedAtMax(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	seedNodeHourlyData(t, ctx)

	app := nodeHourlyApp()
	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	url := fmt.Sprintf(
		"/api/cost-management/v1/recommendations/openshift/node/worker-01/hourly-utilization?cluster_uuid=%s&days=999",
		testutil.TestClusterUUID,
	)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp api.NodeHourlyUtilizationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.Equal(t, 90, resp.Meta.Days, "days should be capped at maxHourlyDays (90)")
}

func TestGetNodeHourlyUtilization_UnknownClusterReturnsEmpty(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'known-cluster', 'src-known', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := nodeHourlyApp()
	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	url := "/api/cost-management/v1/recommendations/openshift/node/worker-01/hourly-utilization?cluster_uuid=99999999-9999-9999-9999-999999999999"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp api.NodeHourlyUtilizationResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.Equal(t, 0, resp.Meta.Count, "unknown cluster should return empty data (RBAC filter)")
	assert.Empty(t, resp.Data)
}
