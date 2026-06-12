package api_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func setupContractTestApp(t *testing.T) (*echo.Echo, string, context.Context) {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() {
		database.DB = nil
		database.Pool = nil
	})

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, testutil.TestOrgID)
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
	require.NoError(t, engine.WriteRecommendationsAndRefreshOrg(ctx, pool, recs))

	engine.EnsureHistoryPartitions(ctx, pool)
	require.NoError(t, engine.WriteRecommendationHistory(ctx, pool, recs, ""))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	api.RegisterV1RoutesForTest(v1, nil)

	return app, makeIdentityHeader(testutil.TestOrgID), ctx
}

func seedContractIdleStates(t *testing.T, ctx context.Context) {
	t.Helper()
	pool := database.Pool
	require.NotNil(t, pool)

	_, err := pool.Exec(ctx, `
		UPDATE recommendation_sets SET
			idle_state = 'zombie',
			idle_since = now() - interval '15 days',
			idle_duration_days = 15,
			estimated_waste_cents = 500000,
			peak_cpu_millicores = 1,
			peak_memory_bytes = 1048576
		WHERE org_id = $1 AND cluster_uuid = $2 AND container_name = $3`,
		testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestContainer)
	require.NoError(t, err)

	tag, err := pool.Exec(ctx, `
		INSERT INTO recommendation_sets (
			org_id, cluster_uuid, namespace, workload, workload_type, container_name,
			term, engine, stale, notification_codes, estimated_savings_cents,
			idle_state, idle_since, idle_duration_days, estimated_waste_cents, updated_at,
			rec_cpu_request_millicores, rec_cpu_limit_millicores,
			rec_memory_request_kib, rec_memory_limit_kib,
			current_cpu_request_millicores, current_cpu_limit_millicores,
			current_memory_request_kib, current_memory_limit_kib
		)
		SELECT org_id, cluster_uuid, namespace, workload, workload_type, 'idle-sidecar',
			term, engine, stale, notification_codes, estimated_savings_cents,
			'idle', now() - interval '10 days', 10, 250000, now(),
			rec_cpu_request_millicores, rec_cpu_limit_millicores,
			rec_memory_request_kib, rec_memory_limit_kib,
			current_cpu_request_millicores, current_cpu_limit_millicores,
			current_memory_request_kib, current_memory_limit_kib
		FROM recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND container_name = $3
			AND term = 'medium' AND engine = 'cost'
		ON CONFLICT DO NOTHING`,
		testutil.TestOrgID, testutil.TestClusterUUID, testutil.TestContainer)
	require.NoError(t, err)
	require.Equal(t, int64(1), tag.RowsAffected(), "expected idle-sidecar recommendation row")

	require.NoError(t, model.RefreshOrgContainerKeys(ctx, pool, testutil.TestOrgID))

	var keyCount int64
	err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM org_container_keys WHERE org_id = $1`, testutil.TestOrgID).Scan(&keyCount)
	require.NoError(t, err)
	require.Equal(t, int64(2), keyCount, "expected main and idle-sidecar in org_container_keys")
}

func contractGET(t *testing.T, app *echo.Echo, identityHeader, path string) (int, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	var body map[string]interface{}
	if rec.Code == http.StatusOK && rec.Header().Get(echo.HeaderContentType) != "text/csv" {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	}
	return rec.Code, body
}

func contractGETRaw(t *testing.T, app *echo.Echo, identityHeader, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec
}

func contractGETWithQuery(t *testing.T, app *echo.Echo, identityHeader, path string, query map[string]string) (int, map[string]interface{}) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	q := req.URL.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	req.URL.RawQuery = q.Encode()
	req.Header.Set("X-Rh-Identity", identityHeader)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	var body map[string]interface{}
	if rec.Code == http.StatusOK && rec.Header().Get(echo.HeaderContentType) != "text/csv" {
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	}
	return rec.Code, body
}

func assertMoneyAmountShape(t *testing.T, raw interface{}) {
	t.Helper()
	obj, ok := raw.(map[string]interface{})
	require.True(t, ok, "expected savings object map")
	assert.Equal(t, "USD", obj["units"])
	value, ok := obj["value"].(string)
	require.True(t, ok)
	assert.Regexp(t, `^\d+\.\d{2}$`, value)
}

func assertIdleRecommendationShape(t *testing.T, raw interface{}) {
	t.Helper()
	rec, ok := raw.(map[string]interface{})
	require.True(t, ok)
	for _, key := range []string{"action", "confidence", "reason"} {
		assert.NotEmpty(t, rec[key], "idle_recommendation.%s", key)
	}
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
	assert.Equal(t, "USD", meta["currency"])
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
		INSERT INTO recommendation_sets (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, stale, notification_codes, estimated_savings_cents, updated_at)
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
	assert.Regexp(t, `^-?\d+\.\d{2}$`, value)
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
		assert.Regexp(t, `^-?\d+\.\d{2}$`, value)
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

func TestContractIdleDetection_ContainerListIdleStateDefault(t *testing.T) {
	app, identityHeader, _ := setupContractTestApp(t)

	code, body := contractGET(t, app, identityHeader, "/api/cost-management/v1/recommendations/openshift?limit=5")
	require.Equal(t, http.StatusOK, code)

	data, ok := body["data"].([]interface{})
	require.True(t, ok)
	require.NotEmpty(t, data)
	for _, raw := range data {
		item := raw.(map[string]interface{})
		idleState, ok := item["idle_state"].(string)
		require.True(t, ok, "idle_state must be present")
		assert.Equal(t, "active", idleState)
	}
}

func TestContractIdleDetection_NonActiveIncludesIdleFields(t *testing.T) {
	app, identityHeader, ctx := setupContractTestApp(t)
	seedContractIdleStates(t, ctx)

	code, body := contractGET(t, app, identityHeader,
		"/api/cost-management/v1/recommendations/openshift?filter%5Bidle_state%5D=zombie&limit=10")
	require.Equal(t, http.StatusOK, code)

	data, ok := body["data"].([]interface{})
	require.True(t, ok)
	require.NotEmpty(t, data)

	item := data[0].(map[string]interface{})
	assert.Equal(t, "zombie", item["idle_state"])
	assert.NotEmpty(t, item["idle_since"])
	assert.NotNil(t, item["idle_duration_days"])
	assertIdleRecommendationShape(t, item["idle_recommendation"])
	assertMoneyAmountShape(t, item["estimated_monthly_waste"])
}

func TestContractIdleDetection_ActiveOmitsWasteAndRecommendation(t *testing.T) {
	app, identityHeader, _ := setupContractTestApp(t)

	code, body := contractGET(t, app, identityHeader, "/api/cost-management/v1/recommendations/openshift?limit=20")
	require.Equal(t, http.StatusOK, code)

	data, ok := body["data"].([]interface{})
	require.True(t, ok)
	var activeItem map[string]interface{}
	for _, raw := range data {
		item := raw.(map[string]interface{})
		if item["idle_state"] == "active" {
			activeItem = item
			break
		}
	}
	require.NotNil(t, activeItem)
	_, hasWaste := activeItem["estimated_monthly_waste"]
	_, hasRec := activeItem["idle_recommendation"]
	assert.False(t, hasWaste)
	assert.False(t, hasRec)
}

func TestContractIdleDetection_IdleRecommendationShape(t *testing.T) {
	app, identityHeader, ctx := setupContractTestApp(t)
	seedContractIdleStates(t, ctx)

	code, body := contractGET(t, app, identityHeader,
		"/api/cost-management/v1/recommendations/openshift?filter%5Bidle_state%5D=idle&limit=5")
	require.Equal(t, http.StatusOK, code)

	data, ok := body["data"].([]interface{})
	require.True(t, ok)
	require.NotEmpty(t, data)
	assertIdleRecommendationShape(t, data[0].(map[string]interface{})["idle_recommendation"])
}

func TestContractIdleDetection_FilterZombieOnly(t *testing.T) {
	app, identityHeader, ctx := setupContractTestApp(t)
	seedContractIdleStates(t, ctx)

	code, body := contractGET(t, app, identityHeader,
		"/api/cost-management/v1/recommendations/openshift?filter%5Bidle_state%5D=zombie&limit=50")
	require.Equal(t, http.StatusOK, code)

	data, ok := body["data"].([]interface{})
	require.True(t, ok)
	require.NotEmpty(t, data)
	for _, raw := range data {
		assert.Equal(t, "zombie", raw.(map[string]interface{})["idle_state"])
	}
}

func TestContractIdleDetection_FilterZombieAndIdle(t *testing.T) {
	app, identityHeader, ctx := setupContractTestApp(t)
	seedContractIdleStates(t, ctx)

	code, body := contractGETWithQuery(t, app, identityHeader,
		"/api/cost-management/v1/recommendations/openshift",
		map[string]string{"filter[idle_state]": "zombie,idle", "limit": "50"})
	require.Equal(t, http.StatusOK, code)

	data, ok := body["data"].([]interface{})
	require.True(t, ok)
	require.GreaterOrEqual(t, len(data), 2)
	states := make(map[string]struct{})
	for _, raw := range data {
		states[raw.(map[string]interface{})["idle_state"].(string)] = struct{}{}
	}
	assert.Contains(t, states, "zombie")
	assert.Contains(t, states, "idle")
	assert.NotContains(t, states, "active")
}

func TestContractIdleDetection_SavingsSummaryGroupByIdleState(t *testing.T) {
	app, identityHeader, ctx := setupContractTestApp(t)
	seedContractIdleStates(t, ctx)

	code, body := contractGET(t, app, identityHeader,
		"/api/cost-management/v1/recommendations/openshift/savings-summary?group_by%5Bidle_state%5D=*")
	require.Equal(t, http.StatusOK, code)

	data, ok := body["data"].([]interface{})
	require.True(t, ok)
	require.NotEmpty(t, data)

	row := data[0].(map[string]interface{})
	assert.NotEmpty(t, row["idle_state"])
	assert.NotNil(t, row["container_count"])
	assertMoneyAmountShape(t, row["estimated_monthly_waste"])

	meta, ok := body["meta"].(map[string]interface{})
	require.True(t, ok)
	assert.NotNil(t, meta["count"])
}

func TestContractIdleDetection_CSVExportIdleColumns(t *testing.T) {
	app, identityHeader, ctx := setupContractTestApp(t)
	seedContractIdleStates(t, ctx)

	rec := contractGETRaw(t, app, identityHeader,
		"/api/cost-management/v1/recommendations/openshift?format=csv&limit=10")
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Header().Get(echo.HeaderContentType), "text/csv")

	reader := csv.NewReader(strings.NewReader(rec.Body.String()))
	records, err := reader.ReadAll()
	require.NoError(t, err)
	require.NotEmpty(t, records)
	assert.Equal(t, api.NativeCSVHeader, records[0])

	required := []string{
		"idle_state",
		"idle_since",
		"idle_duration_days",
		"estimated_monthly_waste",
		"estimated_monthly_waste_currency",
	}
	for _, col := range required {
		assert.Contains(t, records[0], col, "CSV header missing %s", col)
	}
}
