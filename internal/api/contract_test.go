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
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func setupContractTestApp(t *testing.T) (*echo.Echo, string, context.Context) {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	connStr := pool.Config().ConnString()
	gormDB, err := gorm.Open(postgres.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	database.DB = gormDB
	database.Pool = pool
	t.Cleanup(func() {
		database.DB = nil
		database.Pool = nil
	})

	_, err = pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'contract-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	start := testutil.RecentStart()
	testutil.SeedDigestSeriesFrom(t, pool, start, 7, 200, 10, 524288, 1024)
	end := start.AddDate(0, 0, 6)
	recs, err := engine.RecommendAllWorkloads(ctx, pool, testutil.TestOrgID, testutil.TestClusterUUID, start, end, engine.OOMConfig{})
	require.NoError(t, err)
	require.NotEmpty(t, recs)
	require.NoError(t, engine.WriteRecommendations(ctx, pool, recs))

	engine.EnsureHistoryPartitions(ctx, pool)
	require.NoError(t, engine.WriteRecommendationHistory(ctx, pool, recs, ""))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift", api.GetNativeRecommendationSetList)
	v1.GET("/recommendations/openshift/savings-summary", api.GetFleetSavingsSummary)
	v1.GET("/recommendations/openshift/history", api.GetRecommendationHistory)

	return app, makeIdentityHeader(testutil.TestOrgID), ctx
}

func TestContractResponseShape_ContainerList(t *testing.T) {
	app, identityHeader, _ := setupContractTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift?limit=5&offset=0", nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	meta, ok := body["meta"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, float64(5), meta["limit"])
	if offset, ok := meta["offset"]; ok {
		assert.Equal(t, float64(0), offset)
	}
	count, ok := meta["count"].(float64)
	require.True(t, ok)
	assert.Greater(t, count, float64(0))

	data, ok := body["data"].([]interface{})
	require.True(t, ok)
	require.NotEmpty(t, data)

	item, ok := data[0].(map[string]interface{})
	require.True(t, ok)

	assert.NotEmpty(t, item["cluster_uuid"])
	assert.NotEmpty(t, item["cluster_alias"])
	assert.NotEmpty(t, item["project"])
	assert.NotEmpty(t, item["workload"])
	assert.NotEmpty(t, item["workload_type"])
	assert.NotEmpty(t, item["container"])

	lastReported, ok := item["last_reported"].(string)
	require.True(t, ok)
	assert.Regexp(t, `^\d{4}-\d{2}-\d{2}T`, lastReported)

	recs, ok := item["recommendations"].(map[string]interface{})
	require.True(t, ok)
	_, hasCurrent := recs["current"]
	assert.True(t, hasCurrent)

	terms, ok := recs["recommendation_terms"].(map[string]interface{})
	require.True(t, ok)
	for _, termKey := range []string{"short_term", "medium_term", "long_term"} {
		term, ok := terms[termKey].(map[string]interface{})
		if !ok {
			continue
		}
		engines, ok := term["recommendation_engines"].(map[string]interface{})
		require.True(t, ok)
		cost, ok := engines["cost"].(map[string]interface{})
		if ok {
			_, hasConfig := cost["config"]
			assert.True(t, hasConfig)
		}
	}
}

func TestContractResponseShape_Savings(t *testing.T) {
	app, identityHeader, ctx := setupContractTestApp(t)

	_, err := database.Pool.Exec(ctx, `
		INSERT INTO recommendation_sets (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, stale, notification_codes, estimated_monthly_savings_usd, updated_at)
		VALUES ($1, $2, 'ns1', 'w1', 'Deployment', 'c1', 'medium', 'cost', false, '{}', 123456, now())
		ON CONFLICT DO NOTHING`, testutil.TestOrgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/savings-summary", nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	savings, ok := body["estimated_monthly_savings"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "USD", savings["units"])
	value, ok := savings["value"].(string)
	require.True(t, ok)
	assert.Regexp(t, `^-?\d+\.\d{6}$`, value)
}

func TestContractResponseShape_History(t *testing.T) {
	app, identityHeader, _ := setupContractTestApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/history?limit=1", nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))

	data, ok := body["data"].([]interface{})
	require.True(t, ok)
	require.NotEmpty(t, data)

	row, ok := data[0].(map[string]interface{})
	require.True(t, ok)

	savings, ok := row["estimated_monthly_savings"].(map[string]interface{})
	if ok {
		assert.Equal(t, "USD", savings["units"])
		value, ok := savings["value"].(string)
		require.True(t, ok)
		assert.Regexp(t, `^-?\d+\.\d{6}$`, value)
	}
}

func TestContractQueryParams_FlatSyntax(t *testing.T) {
	app, identityHeader, _ := setupContractTestApp(t)

	url := "/api/cost-management/v1/recommendations/openshift?" +
		"project=" + testutil.TestNamespace +
		"&cluster=contract-cluster" +
		"&order_by=last_reported&order_how=desc&limit=10"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	data, ok := body["data"].([]interface{})
	require.True(t, ok)
	for _, raw := range data {
		item := raw.(map[string]interface{})
		assert.Equal(t, testutil.TestNamespace, item["project"])
	}
}

func TestContractQueryParams_BracketSyntax(t *testing.T) {
	app, identityHeader, _ := setupContractTestApp(t)

	url := "/api/cost-management/v1/recommendations/openshift?" +
		"filter%5Bproject%5D=" + testutil.TestNamespace +
		"&filter%5Bcluster%5D=contract-cluster" +
		"&order_by%5Blast_reported%5D=desc&limit=10"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var body map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	data, ok := body["data"].([]interface{})
	require.True(t, ok)
	for _, raw := range data {
		item := raw.(map[string]interface{})
		assert.Equal(t, testutil.TestNamespace, item["project"])
	}
}
