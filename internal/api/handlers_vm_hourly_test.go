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

func vmHourlyApp() *echo.Echo {
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/vm/hourly-activity", api.GetVMHourlyActivity)
	return app
}

func seedVMHourlyData(t *testing.T, ctx context.Context) {
	t.Helper()
	pool := database.GetPool()

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'vm-hourly-cluster', 'src-vh', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	reportDate := time.Now().UTC().AddDate(0, 0, -1).Truncate(24 * time.Hour)
	testutil.EnsureMonthlyPartition(t, pool, "hourly_vm_digests", reportDate)

	for hour := 0; hour < 3; hour++ {
		_, err = pool.Exec(ctx, `
			INSERT INTO hourly_vm_digests (org_id, cluster_uuid, namespace, vm_name, report_date, hour,
				cpu_usage_p95_mc, mem_usage_p95_kib, sample_count, disk_read_iops_p95, disk_write_iops_p95)
			VALUES ($1, $2::uuid, $3, 'test-vm', $4, $5, $6, $7, $8, $9, $10)`,
			testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestNamespace, reportDate, hour,
			150*(hour+1), 4096*(hour+1), 4, 50*(hour+1), 30*(hour+1),
		)
		require.NoError(t, err)
	}
}

func TestGetVMHourlyActivity_HappyPath(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	seedVMHourlyData(t, ctx)

	app := vmHourlyApp()
	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	url := fmt.Sprintf(
		"/api/cost-management/v1/recommendations/openshift/vm/hourly-activity?cluster_uuid=%s&vm_name=test-vm&namespace=%s",
		testutil.TestClusterUUID, testutil.TestNamespace,
	)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp api.VMHourlyActivityResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.Equal(t, 3, resp.Meta.Count)
	assert.Equal(t, 14, resp.Meta.Days)
	require.Len(t, resp.Data, 3)

	assert.Equal(t, 0, resp.Data[0].Hour)
	assert.Equal(t, 150, resp.Data[0].CPUUsageP95MC)
	assert.Equal(t, 4096, resp.Data[0].MemUsageP95KiB)
	assert.Equal(t, 4, resp.Data[0].SampleCount)
	assert.Equal(t, 50, resp.Data[0].DiskReadIOPSP95)
	assert.Equal(t, 30, resp.Data[0].DiskWriteIOPSP95)
	assert.NotEmpty(t, resp.Data[0].ReportDate)

	assert.Equal(t, 1, resp.Data[1].Hour)
	assert.Equal(t, 300, resp.Data[1].CPUUsageP95MC)

	assert.Equal(t, 2, resp.Data[2].Hour)
	assert.Equal(t, 450, resp.Data[2].CPUUsageP95MC)
}

func TestGetVMHourlyActivity_EmptyData(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'empty-vh-cluster', 'src-empty-vh', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := vmHourlyApp()
	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	url := fmt.Sprintf(
		"/api/cost-management/v1/recommendations/openshift/vm/hourly-activity?cluster_uuid=%s&vm_name=nonexistent&namespace=gone",
		testutil.TestClusterUUID,
	)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp api.VMHourlyActivityResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.Equal(t, 0, resp.Meta.Count)
	assert.Empty(t, resp.Data)
}

func TestGetVMHourlyActivity_MissingClusterUUID(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	app := vmHourlyApp()
	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/vm/hourly-activity?vm_name=test-vm&namespace=ns", nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "error", body["status"])
	assert.Contains(t, body["message"], "cluster_uuid is required")
}

func TestGetVMHourlyActivity_MissingVMNameAndNamespace(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	app := vmHourlyApp()
	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	url := fmt.Sprintf(
		"/api/cost-management/v1/recommendations/openshift/vm/hourly-activity?cluster_uuid=%s",
		testutil.TestClusterUUID,
	)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "error", body["status"])
	assert.Contains(t, body["message"], "vm_name and namespace are required")
}

func TestGetVMHourlyActivity_CustomDays(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	seedVMHourlyData(t, ctx)

	app := vmHourlyApp()
	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	url := fmt.Sprintf(
		"/api/cost-management/v1/recommendations/openshift/vm/hourly-activity?cluster_uuid=%s&vm_name=test-vm&namespace=%s&days=7",
		testutil.TestClusterUUID, testutil.TestNamespace,
	)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp api.VMHourlyActivityResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.Equal(t, 7, resp.Meta.Days)
	assert.Equal(t, 3, resp.Meta.Count)
}

func TestGetVMHourlyActivity_DaysCappedAtMax(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	seedVMHourlyData(t, ctx)

	app := vmHourlyApp()
	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	url := fmt.Sprintf(
		"/api/cost-management/v1/recommendations/openshift/vm/hourly-activity?cluster_uuid=%s&vm_name=test-vm&namespace=%s&days=999",
		testutil.TestClusterUUID, testutil.TestNamespace,
	)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp api.VMHourlyActivityResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.Equal(t, 90, resp.Meta.Days, "days should be capped at maxHourlyDays (90)")
}

func TestGetVMHourlyActivity_UnknownClusterReturnsEmpty(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'known-vh-cluster', 'src-known-vh', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	app := vmHourlyApp()
	identityHeader := makeIdentityHeader(testutil.TestOrgID)

	url := "/api/cost-management/v1/recommendations/openshift/vm/hourly-activity?cluster_uuid=99999999-9999-9999-9999-999999999999&vm_name=test-vm&namespace=ns"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp api.VMHourlyActivityResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	assert.Equal(t, 0, resp.Meta.Count, "unknown cluster should return empty data (RBAC filter)")
	assert.Empty(t, resp.Data)
}
