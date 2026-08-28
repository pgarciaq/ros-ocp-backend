package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/platform-go-middlewares/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/vm"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/reship"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"

	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins"
)

const apiV1Prefix = "/api/cost-management/v1"

type openAPISpecDoc struct {
	Paths      map[string]json.RawMessage `json:"paths"`
	Components struct {
		Schemas map[string]json.RawMessage `json:"schemas"`
	} `json:"components"`
}

type openAPIPathItem struct {
	Get    *openAPIOperation `json:"get,omitempty"`
	Put    *openAPIOperation `json:"put,omitempty"`
	Post   *openAPIOperation `json:"post,omitempty"`
	Delete *openAPIOperation `json:"delete,omitempty"`
}

type openAPIOperation struct {
	Parameters []struct {
		Name string `json:"name"`
		In   string `json:"in"`
	} `json:"parameters"`
	Responses map[string]openAPIResponse `json:"responses"`
}

type openAPIResponse struct {
	Content map[string]struct {
		Schema json.RawMessage `json:"schema"`
	} `json:"content"`
}

func openapiJSONPath() string {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		panic("unable to determine openapi_contract_test.go path")
	}
	return filepath.Join(filepath.Dir(thisFile), "..", "..", "openapi.json")
}

func loadOpenAPISpec(t *testing.T) openAPISpecDoc {
	t.Helper()
	raw, err := os.ReadFile(openapiJSONPath())
	require.NoError(t, err, "openapi.json should be readable at repo root")

	var spec openAPISpecDoc
	require.NoError(t, json.Unmarshal(raw, &spec))
	require.NotEmpty(t, spec.Paths)
	return spec
}

func (spec openAPISpecDoc) resolveSchema(raw json.RawMessage) map[string]interface{} {
	var node map[string]interface{}
	if err := json.Unmarshal(raw, &node); err != nil {
		return nil
	}
	if ref, ok := node["$ref"].(string); ok {
		const prefix = "#/components/schemas/"
		if strings.HasPrefix(ref, prefix) {
			name := strings.TrimPrefix(ref, prefix)
			if schemaRaw, ok := spec.Components.Schemas[name]; ok {
				_ = json.Unmarshal(schemaRaw, &node)
				return node
			}
		}
	}
	return node
}

func getResponseSchema(spec openAPISpecDoc, path, method, status string) map[string]interface{} {
	pathRaw, ok := spec.Paths[path]
	if !ok {
		return nil
	}
	var item openAPIPathItem
	if err := json.Unmarshal(pathRaw, &item); err != nil {
		return nil
	}
	var op *openAPIOperation
	switch strings.ToUpper(method) {
	case http.MethodGet:
		op = item.Get
	case http.MethodPut:
		op = item.Put
	case http.MethodPost:
		op = item.Post
	case http.MethodDelete:
		op = item.Delete
	}
	if op == nil {
		return nil
	}
	resp, ok := op.Responses[status]
	if !ok {
		return nil
	}
	content, ok := resp.Content["application/json"]
	if !ok {
		return nil
	}
	return spec.resolveSchema(content.Schema)
}

func schemaPropertyNames(schema map[string]interface{}) []string {
	if schema == nil {
		return nil
	}
	props, ok := schema["properties"].(map[string]interface{})
	if !ok {
		return nil
	}
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (spec openAPISpecDoc) componentSchema(name string) map[string]interface{} {
	raw, ok := spec.Components.Schemas[name]
	if !ok {
		return nil
	}
	return spec.resolveSchema(raw)
}

func openAPIOptionalPropertyFields() map[string]struct{} {
	return map[string]struct{}{
		"warnings":                     {},
		"settings_locked":              {},
		"locked_fields":                {},
		"gpu":                          {},
		"daily_digests":                {},
		"daily_digests_business_hours": {},
		"sm_active_avg":                {},
		"dram_active_avg":              {},
		"tensor_pipe_active_avg":       {},
		"fb_usage_max_mib":             {},
		"total_fb_mib":                 {},
		"instance_type":                {},
		"machineset_name":              {},
		"suggested_instance_type":      {},
		"instance_type_reason":         {},
		"pod_capacity":                 {},
		"pod_scheduling_headroom":      {},
		"notifications":                {},
		"estimated_savings":            {},
		"count":                        {},
		"quota_name":                   {},
		"capacity_freed":               {},
		"idle_since":                   {},
		"idle_duration_days":           {},
		"recommended_bytes":            {},
		"days_to_full":                 {},
		"growth_bytes_per_day":         {},
		"mounted_by":                   {},
		"vm_name":                      {},
		"persistentvolume":             {},
		"resize_note":                  {},
		"estimated_monthly_savings":    {},
		"usage_bytes_max":              {},
		"data_days":                    {},
		"historical_usage":             {},
		"terms":                        {},
		"has_next":                     {},
		"next_cursor":                  {},
		"currency":                     {},
		"analytics_incomplete":         {},
		"analytics_incomplete_at":      {},
		"ingest_hooks_failed":          {},
		"ingest_hooks_failed_at":       {},
		"explanation":                  {},
		"estimated_monthly_waste":      {},
		"cpu_savings":                  {},
		"memory_savings":               {},
		"category":                     {},
		"id":                           {},
	}
}

func assertObjectHasSpecProperties(t *testing.T, obj map[string]interface{}, schema map[string]interface{}, label string) {
	t.Helper()
	require.NotNil(t, schema, "%s schema must be resolved from openapi.json", label)
	optionalFields := openAPIOptionalPropertyFields()
	for _, prop := range schemaPropertyNames(schema) {
		if _, skip := optionalFields[prop]; skip {
			continue
		}
		_, ok := obj[prop]
		assert.True(t, ok, "%s missing spec-defined property %q", label, prop)
	}
}

func assertResponseHasSpecProperties(t *testing.T, body []byte, schema map[string]interface{}) {
	t.Helper()
	require.NotNil(t, schema, "response schema must be resolved from openapi.json")

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &resp))

	optionalFields := openAPIOptionalPropertyFields()

	if required, ok := schema["required"].([]interface{}); ok && len(required) > 0 {
		for _, r := range required {
			name, ok := r.(string)
			require.True(t, ok)
			_, exists := resp[name]
			assert.True(t, exists, "response missing required property %q", name)
		}
		return
	}

	for _, prop := range schemaPropertyNames(schema) {
		if _, skip := optionalFields[prop]; skip {
			continue
		}
		_, ok := resp[prop]
		assert.True(t, ok, "response missing spec-defined property %q", prop)
	}
}

func assertResponseDataItemsHaveSpecProperties(t *testing.T, body []byte, itemSchema map[string]interface{}) {
	t.Helper()
	require.NotNil(t, itemSchema)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &resp))
	data, ok := resp["data"].([]interface{})
	require.True(t, ok, "response must have data array")
	require.NotEmpty(t, data, "data array must have at least one item for schema validation")

	first, ok := data[0].(map[string]interface{})
	require.True(t, ok)
	assertObjectHasSpecProperties(t, first, itemSchema, "data[0]")
}

func seedOpenAPIVMRecommendation(t *testing.T, pool *pgxpool.Pool, orgID string) (clusterUUID, vmName, namespace string) {
	t.Helper()
	ctx := context.Background()
	clusterUUID = testutil.TestClusterUUID
	vmName = "openapi-vm-01"
	namespace = "openapi-ns"

	_, err := pool.Exec(ctx,
		`INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'openapi-vm', 'src-openapi', now()) ON CONFLICT DO NOTHING`,
		clusterUUID,
	)
	require.NoError(t, err)

	clusterID := uuid.MustParse(clusterUUID)
	now := time.Now().UTC()
	rec := model.VMRecommendation{
		OrgID:                orgID,
		ClusterUUID:          clusterID,
		VMName:               vmName,
		Namespace:            namespace,
		GuestOS:              "linux",
		CurrentVCPU:          4,
		CurrentMemoryGiB:     8,
		RecommendedVCPU:      4,
		RecommendedMemoryGiB: 16,
		Confidence:           "high",
		Term:                 "short_term",
		Engine:               "cost",
		Notifications:        []byte(`[]`),
		LastRecommendedAt:    now,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	require.NoError(t, vm.PersistVMRecommendations(ctx, pool, []model.VMRecommendation{rec}, nil))

	bucket := time.Now().UTC().Truncate(24 * time.Hour)
	_, err = pool.Exec(ctx, `
		INSERT INTO daily_vm_digests (
			org_id, cluster_uuid, vm_name, namespace, bucket_date, sample_count
		) VALUES ($1, $2, $3, $4, $5, 4)
		ON CONFLICT DO NOTHING`,
		orgID, clusterUUID, vmName, namespace, bucket,
	)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO vm_recommendation_history (
			org_id, cluster_id, vm_name, namespace, term, engine,
			recommended_vcpu, recommended_memory_gib, confidence
		) VALUES ($1, $2, $3, $4, 'short_term', 'cost', 4, 16, 'high')`,
		orgID, clusterUUID, vmName, namespace,
	)
	require.NoError(t, err)
	return clusterUUID, vmName, namespace
}

func enableAllPluginsForContractTest(t *testing.T) {
	t.Helper()
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	t.Setenv("ROS_BUSINESS_HOURS_ENABLED", "true")
	config.ResetForTest()
	_ = config.GetConfig()
	engine.InitThresholdDefaults(config.GetConfig())
}

func contractTestIdentityMiddleware(orgID string) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("Identity", identity.XRHID{
				Identity: identity.Identity{OrgID: orgID},
			})
			c.Set("user.permissions", map[string][]string{"*": {}})
			return next(c)
		}
	}
}

func registerContractTestRoutes(e *echo.Echo) {
	v1 := e.Group(apiV1Prefix)
	api.RegisterV1RoutesForTest(v1, &reship.NoopTriggerer{})
	api.RegisterTestReferenceRoutes(e)
	api.RegisterTestInternalRoutes(e)
	e.GET("/status", api.GetAppStatus)
	e.GET("/healthz", api.GetHealthz)
	e.GET("/readyz", api.GetReadyz)
}

func setupContractTestEcho(t *testing.T, pool *pgxpool.Pool, orgID string) *echo.Echo {
	t.Helper()
	enableAllPluginsForContractTest(t)
	if pool != nil {
		prev := database.Pool
		database.Pool = pool
		prevGorm := database.DB
		database.DB = testutil.OpenTestGORM(pool)
		t.Cleanup(func() {
			database.Pool = prev
			database.DB = prevGorm
		})
	}

	e := echo.New()
	e.Use(contractTestIdentityMiddleware(orgID))
	registerContractTestRoutes(e)
	return e
}

func makeContractRequest(t *testing.T, e *echo.Echo, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

var routeParamPattern = regexp.MustCompile(`\{[^}]+\}|:[^/]+`)

func normalizeRoutePattern(path string) string {
	path = strings.TrimSuffix(path, "/*")
	if strings.HasPrefix(path, apiV1Prefix) {
		path = strings.TrimPrefix(path, apiV1Prefix)
	}
	return routeParamPattern.ReplaceAllString(path, "{*}")
}

func collectRegisteredRoutePatterns(e *echo.Echo) map[string]struct{} {
	out := make(map[string]struct{})
	for _, route := range e.Routes() {
		if !strings.HasPrefix(route.Path, apiV1Prefix) && route.Path != "/status" && route.Path != "/healthz" && route.Path != "/readyz" {
			continue
		}
		if route.Method == http.MethodHead {
			continue
		}
		key := route.Method + " " + normalizeRoutePattern(route.Path)
		out[key] = struct{}{}
	}
	return out
}

func collectOpenAPIRoutePatterns(spec openAPISpecDoc) map[string]struct{} {
	out := make(map[string]struct{})
	for path, raw := range spec.Paths {
		var item openAPIPathItem
		if err := json.Unmarshal(raw, &item); err != nil {
			continue
		}
		if item.Get != nil {
			out[http.MethodGet+" "+normalizeRoutePattern(path)] = struct{}{}
		}
		if item.Put != nil {
			out[http.MethodPut+" "+normalizeRoutePattern(path)] = struct{}{}
		}
		if item.Post != nil {
			out[http.MethodPost+" "+normalizeRoutePattern(path)] = struct{}{}
		}
		if item.Delete != nil {
			out[http.MethodDelete+" "+normalizeRoutePattern(path)] = struct{}{}
		}
	}
	return out
}

func TestOpenAPI_SpecIsValidJSON(t *testing.T) {
	spec := loadOpenAPISpec(t)
	assert.NotEmpty(t, spec.Components.Schemas)
	assert.Greater(t, len(spec.Paths), 20)
}

func TestOpenAPI_NodeBusinessHoursRecommendationSchema(t *testing.T) {
	spec := loadOpenAPISpec(t)
	bh := spec.componentSchema("NodeBusinessHoursRecommendation")
	require.NotNil(t, bh, "NodeBusinessHoursRecommendation must exist in components.schemas")
	props, ok := bh["properties"].(map[string]interface{})
	require.True(t, ok)
	for _, name := range []string{"recommended_cpu_cores", "recommended_memory_gib", "reason", "notifications"} {
		_, has := props[name]
		assert.True(t, has, "NodeBusinessHoursRecommendation missing %s", name)
	}
	detail := spec.componentSchema("NodeUtilizationDetailRec")
	require.NotNil(t, detail)
	rtProps, _ := detail["properties"].(map[string]interface{})
	require.Contains(t, rtProps, "recommendation_terms")
	rt, _ := rtProps["recommendation_terms"].(map[string]interface{})
	desc, _ := rt["description"].(string)
	assert.Contains(t, desc, "business_hours")
	assert.Contains(t, desc, "all-hours")
}

func TestOpenAPI_VMBusinessHoursRecommendationSchema(t *testing.T) {
	spec := loadOpenAPISpec(t)
	bh := spec.componentSchema("VMBusinessHoursRecommendation")
	require.NotNil(t, bh, "VMBusinessHoursRecommendation must exist in components.schemas")
	props, ok := bh["properties"].(map[string]interface{})
	require.True(t, ok)
	for _, name := range []string{"recommended_vcpu", "recommended_memory_gib", "reason", "notifications"} {
		_, has := props[name]
		assert.True(t, has, "VMBusinessHoursRecommendation missing %s", name)
	}
	desc, _ := bh["description"].(string)
	assert.Contains(t, desc, "thin")
	assert.Contains(t, desc, "82")
	assert.Contains(t, desc, "Kruize")
	assert.Contains(t, desc, "not a copy of the full VM recommendation")
	detail := getResponseSchema(spec, "/recommendations/openshift/vm/detail", http.MethodGet, "200")
	require.NotNil(t, detail)
	detailProps, _ := detail["properties"].(map[string]interface{})
	require.Contains(t, detailProps, "business_hours")
}

func TestOpenAPI_ThresholdSettings_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	orgID := "org-openapi-thresholds"
	e := setupContractTestEcho(t, pool, orgID)

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/settings/container")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(spec, "/recommendations/openshift/settings/container", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), schema)

	deprecatedRec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/settings/thresholds?recommendation_type=container")
	require.Equal(t, http.StatusOK, deprecatedRec.Code, "body: %s", deprecatedRec.Body.String())
	assert.Equal(t, "true", deprecatedRec.Header().Get("Deprecation"))
}

func TestOpenAPI_SavingsSummary_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	orgID := "org-openapi-savings"
	e := setupContractTestEcho(t, pool, orgID)

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/savings-summary")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(spec, "/recommendations/openshift/savings-summary", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), schema)
}

func TestOpenAPI_NodeUtilizationDetail_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	orgID := "org-openapi-node-detail"
	e := setupContractTestEcho(t, pool, orgID)
	clusterUUID := testutil.TestClusterUUID

	_, err := pool.Exec(context.Background(), `
		INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'openapi-detail', 'src-od', now()) ON CONFLICT DO NOTHING`, clusterUUID)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, category, idle_state,
			stranded_resource, pod_count, trend_slope, notification_codes
		) VALUES ($1, $2::uuid, 'openapi-worker', 'medium', 'cost',
			0.1, 0.2, 0.15, 0.25, 1.0, 'underutilized', 'active', NULL, 5, 0, '{}')`,
		orgID, clusterUUID)
	require.NoError(t, err)

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/nodes/openapi-worker")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(spec, "/recommendations/openshift/nodes/{node}", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), schema)
}

func TestOpenAPI_NodeRecommendations_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	orgID := "org-openapi-nodes"
	e := setupContractTestEcho(t, pool, orgID)

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/nodes?limit=1")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(spec, "/recommendations/openshift/nodes", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), schema)
}

func seedOpenAPINodeGPUTimeslicingRecommendation(t *testing.T, pool *pgxpool.Pool, orgID string) {
	t.Helper()
	ctx := context.Background()
	clusterUUID := testutil.TestClusterUUID
	nodeName := "openapi-gpu-ts-node"

	_, err := pool.Exec(ctx,
		`INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'openapi-gpu-ts', 'src-openapi-gpu-ts', now()) ON CONFLICT DO NOTHING`,
		clusterUUID,
	)
	require.NoError(t, err)

	candidates := `[{"namespace":"ml","workload":"train","container":"gpu-1","sm_active_avg":0.12,"classification":"underutilized"}]`
	_, err = pool.Exec(ctx, `
		INSERT INTO node_gpu_timeslicing_recommendations (
			org_id, cluster_uuid, node_name, gpu_model, term,
			recommended_replicas, confidence, confidence_level,
			candidate_count, impacted_count,
			candidate_containers, impacted_containers,
			notification_codes, estimated_savings_cents, savings_per_gpu_cents
		) VALUES (
			$1, $2, $3, 'NVIDIA T4', 'medium',
			4, 0.75, 0.75, 1, 0,
			$4::jsonb, '[]'::jsonb,
			'{36}', 40000, 10000
		)`,
		orgID, clusterUUID, nodeName, candidates,
	)
	require.NoError(t, err)
}

func TestOpenAPI_GPUTimeslicingRecommendations_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	orgID := "org-openapi-gpu-ts"
	seedOpenAPINodeGPUTimeslicingRecommendation(t, pool, orgID)
	e := setupContractTestEcho(t, pool, orgID)

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/gpu/timeslicing?limit=10")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(spec, "/recommendations/openshift/gpu/timeslicing", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), schema)
}

func seedOpenAPINodeGPUTimeslicingHistory(t *testing.T, pool *pgxpool.Pool, orgID string) (clusterUUID, nodeName string) {
	t.Helper()
	ctx := context.Background()
	clusterUUID = testutil.TestClusterUUID
	nodeName = "openapi-gpu-ts-hist-node"

	_, err := pool.Exec(ctx,
		`INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'openapi-gpu-ts-hist', 'src-openapi-gpu-ts-hist', now()) ON CONFLICT DO NOTHING`,
		clusterUUID,
	)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO node_gpu_timeslicing_recommendation_history (
			org_id, cluster_uuid, node_name, gpu_model, term,
			recommended_replicas, confidence, candidate_count, impacted_count,
			estimated_savings_cents
		) VALUES (
			$1, $2::uuid, $3, 'NVIDIA T4', 'medium',
			4, 0.75, 2, 1, 40000
		)`,
		orgID, clusterUUID, nodeName,
	)
	require.NoError(t, err)
	return clusterUUID, nodeName
}

func TestOpenAPI_GPUTimeslicingHistory_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	orgID := "org-openapi-gpu-ts-hist"
	clusterUUID, nodeName := seedOpenAPINodeGPUTimeslicingHistory(t, pool, orgID)
	e := setupContractTestEcho(t, pool, orgID)

	rec := makeContractRequest(t, e, http.MethodGet,
		fmt.Sprintf("%s/recommendations/openshift/gpu/timeslicing/history?cluster_uuid=%s&node_name=%s&limit=10&offset=0",
			apiV1Prefix, clusterUUID, nodeName))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	listSchema := getResponseSchema(spec, "/recommendations/openshift/gpu/timeslicing/history", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), listSchema)
	assertResponseDataItemsHaveSpecProperties(t, rec.Body.Bytes(), spec.componentSchema("NodeGPUTimeslicingHistoryRow"))
}

func TestOpenAPI_VMRecommendations_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	orgID := "org-openapi-vm"
	e := setupContractTestEcho(t, pool, orgID)

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/vm?limit=1")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(spec, "/recommendations/openshift/vm", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), schema)
}

func TestOpenAPI_VMRecommendationDetail_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	orgID := "org-openapi-vm-detail"
	e := setupContractTestEcho(t, pool, orgID)
	clusterUUID, vmName, namespace := seedOpenAPIVMRecommendation(t, pool, orgID)

	rec := makeContractRequest(t, e, http.MethodGet,
		fmt.Sprintf("%s/recommendations/openshift/vm/detail?cluster_uuid=%s&vm_name=%s&namespace=%s&term=short_term&engine=cost",
			apiV1Prefix, clusterUUID, vmName, namespace))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(spec, "/recommendations/openshift/vm/detail", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), schema)

	var detail map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &detail))
	digests, ok := detail["daily_digests"].([]interface{})
	assert.True(t, ok && len(digests) > 0, "detail should include daily_digests when digest data is seeded")
}

func TestOpenAPI_VMRecommendationHistory_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	orgID := "org-openapi-vm-history"
	e := setupContractTestEcho(t, pool, orgID)
	clusterUUID, vmName, namespace := seedOpenAPIVMRecommendation(t, pool, orgID)

	rec := makeContractRequest(t, e, http.MethodGet,
		fmt.Sprintf("%s/recommendations/openshift/vms/%s/history?cluster_uuid=%s&namespace=%s&term=short_term&engine=cost&limit=10&offset=0",
			apiV1Prefix, vmName, clusterUUID, namespace))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	listSchema := getResponseSchema(spec, "/recommendations/openshift/vms/{vm_name}/history", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), listSchema)
	assertResponseDataItemsHaveSpecProperties(t, rec.Body.Bytes(), spec.componentSchema("VMRecommendationHistoryRow"))
}

func TestOpenAPI_VMSettings_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	orgID := "org-openapi-vm-settings"
	e := setupContractTestEcho(t, pool, orgID)

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/settings/vm")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(spec, "/recommendations/openshift/settings/vm", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), schema)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assertObjectHasSpecProperties(t, resp["thresholds"].(map[string]interface{}), spec.componentSchema("VMThresholdSettings"), "thresholds")
	assertObjectHasSpecProperties(t, resp["memory_floors"].(map[string]interface{}), spec.componentSchema("VMMemoryFloorsSettings"), "memory_floors")
	assertObjectHasSpecProperties(t, resp["stability"].(map[string]interface{}), spec.componentSchema("VMStabilitySettings"), "stability")
	assertObjectHasSpecProperties(t, resp["disk"].(map[string]interface{}), spec.componentSchema("VMDiskSettings"), "disk")
	assertObjectHasSpecProperties(t, resp["io"].(map[string]interface{}), spec.componentSchema("VMIOSettings"), "io")
	assertObjectHasSpecProperties(t, resp["gpu"].(map[string]interface{}), spec.componentSchema("VMGPUSettings"), "gpu")
	_, ok := resp["cpu_adaptive_margin_enabled"].(bool)
	assert.True(t, ok, "cpu_adaptive_margin_enabled must be a boolean")
}

func TestOpenAPI_VMTermSettings_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	orgID := "org-openapi-vm-terms"
	e := setupContractTestEcho(t, pool, orgID)

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/settings/vm/terms")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(spec, "/recommendations/openshift/settings/vm/terms", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), schema)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	terms, ok := resp["terms"].([]interface{})
	require.True(t, ok)
	require.NotEmpty(t, terms)
	first, ok := terms[0].(map[string]interface{})
	require.True(t, ok)
	for _, prop := range []string{"name", "window_days", "min_data_days", "decay_halflife_hours", "locked", "is_default"} {
		_, exists := first[prop]
		assert.True(t, exists, "terms[0] missing property %q", prop)
	}
}

func TestOpenAPI_AllSpecPathsHaveRoutes(t *testing.T) {
	spec := loadOpenAPISpec(t)
	enableAllPluginsForContractTest(t)

	e := echo.New()
	registerContractTestRoutes(e)
	registered := collectRegisteredRoutePatterns(e)
	specRoutes := collectOpenAPIRoutePatterns(spec)

	skipPaths := map[string]struct{}{
		"GET /recommendations/openshift/openapi.json": {},
	}

	var missing []string
	for key := range specRoutes {
		if _, skip := skipPaths[key]; skip {
			continue
		}
		if _, ok := registered[key]; !ok {
			missing = append(missing, key)
		}
	}
	sort.Strings(missing)
	assert.Empty(t, missing, "OpenAPI paths without registered routes")
}

func seedOpenAPINamespaceRecommendation(t *testing.T, pool *pgxpool.Pool, orgID string) (namespaceID string) {
	t.Helper()
	ctx := context.Background()
	namespaceName := "openapi-ns-hist"

	if orgID != testutil.TestOrgID {
		orgID = testutil.TestOrgID
	}
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'openapi-ns-cluster', 'src-1', now()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	testutil.SeedNamespaceDigestSeries(t, pool, namespaceName, 7, 200, 10, 524288, 1024)
	end := testutil.BaseDate.AddDate(0, 0, 6)
	results, err := engine.RecommendAllNamespaces(ctx, pool, orgID, testutil.TestClusterUUID, testutil.BaseDate, end)
	require.NoError(t, err)
	require.NotEmpty(t, results)
	require.NoError(t, engine.WriteNamespaceRecommendations(ctx, pool, results))
	require.NoError(t, engine.WriteNamespaceRecommendationHistory(ctx, pool, results))

	return model.NativeNamespaceID(testutil.TestClusterUUID, namespaceName)
}

func TestOpenAPI_NamespaceRecommendations_List_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	orgID := testutil.TestOrgID
	_ = seedOpenAPINamespaceRecommendation(t, pool, orgID)
	e := setupContractTestEcho(t, pool, orgID)

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/namespaces?limit=1")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(spec, "/recommendations/openshift/namespaces", http.MethodGet, "200")
	if schema == nil {
		// Canonical list path omits 200 content in openapi.json; use component schema.
		schema = spec.componentSchema("NamespaceRecommendationList")
	}
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), schema)
}

func TestOpenAPI_NamespaceRecommendations_List_OmitsBusinessHours(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	orgID := testutil.TestOrgID
	_ = seedOpenAPINamespaceRecommendation(t, pool, orgID)
	e := setupContractTestEcho(t, pool, orgID)

	// limit>1 so shouldSkipListEnrichment is false (limit<=1 is not the #497 path).
	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/namespaces?limit=10")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	assert.NotContains(t, rec.Body.String(), `"business_hours"`)
}

func TestOpenAPI_NamespaceRecommendations_Detail_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	orgID := testutil.TestOrgID
	namespaceID := seedOpenAPINamespaceRecommendation(t, pool, orgID)
	e := setupContractTestEcho(t, pool, orgID)

	rec := makeContractRequest(t, e, http.MethodGet,
		fmt.Sprintf("%s/recommendations/openshift/namespaces/%s", apiV1Prefix, namespaceID))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(spec, "/recommendations/openshift/namespaces/{recommendation-id}", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), schema)
}

func TestOpenAPI_NamespaceRecommendations_History_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	orgID := testutil.TestOrgID
	namespaceID := seedOpenAPINamespaceRecommendation(t, pool, orgID)
	e := setupContractTestEcho(t, pool, orgID)

	rec := makeContractRequest(t, e, http.MethodGet,
		fmt.Sprintf("%s/recommendations/openshift/namespaces/%s/history?filter[term]=short_term&filter[engine]=cost&limit=10",
			apiV1Prefix, namespaceID))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	listSchema := getResponseSchema(spec, "/recommendations/openshift/namespaces/{recommendation-id}/history", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), listSchema)
	assertResponseDataItemsHaveSpecProperties(t, rec.Body.Bytes(), spec.componentSchema("NamespaceRecommendationHistoryRow"))
}

func TestOpenAPI_NamespaceSettings_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	orgID := "org-openapi-ns-settings"
	e := setupContractTestEcho(t, pool, orgID)

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/settings/namespace")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(spec, "/recommendations/openshift/settings/namespace", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), schema)
}

func assertThresholdPluginFields(t *testing.T, body []byte, fields []string) {
	t.Helper()
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &resp))
	for _, field := range fields {
		_, ok := resp[field]
		assert.True(t, ok, "threshold settings missing field %q", field)
	}
}

func seedContractTestBHCluster(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID string) {
	t.Helper()
	ctx := context.Background()
	var tenantID int64
	err := pool.QueryRow(ctx, `
		INSERT INTO rh_accounts (org_id) VALUES ($1)
		ON CONFLICT (org_id) DO UPDATE SET org_id = EXCLUDED.org_id
		RETURNING id`, orgID).Scan(&tenantID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES ($1, $2::uuid, 'openapi-bh-cluster', 'src-bh-contract', now()) ON CONFLICT DO NOTHING`,
		tenantID, clusterUUID)
	require.NoError(t, err)
}

func assertBusinessHoursSettingsResponse(t *testing.T, body []byte, schema map[string]interface{}) {
	t.Helper()
	require.NotNil(t, schema, "business hours schema must be resolved from openapi.json")

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &resp))

	_, ok := resp["enabled"].(bool)
	assert.True(t, ok, "business hours response must include enabled boolean")

	bhOptional := map[string]struct{}{
		"timezone":            {},
		"schedule":            {},
		"off_hours_weight":    {},
		"reship_status":       {},
		"reship_status_since": {},
		"settings_locked":     {},
		"locked_fields":       {},
	}
	for _, prop := range schemaPropertyNames(schema) {
		if _, skip := bhOptional[prop]; skip {
			continue
		}
		_, exists := resp[prop]
		assert.True(t, exists, "business hours response missing property %q", prop)
	}
}

func TestOpenAPI_NodeThresholdSettings_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	e := setupContractTestEcho(t, pool, "org-openapi-node-settings")

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/settings/node")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(spec, "/recommendations/openshift/settings/node", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), schema)
	assertThresholdPluginFields(t, rec.Body.Bytes(), []string{
		"cost_target_utilization", "underutil_threshold", "locked_fields",
	})
}

func TestOpenAPI_GPUThresholdSettings_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	e := setupContractTestEcho(t, pool, "org-openapi-gpu-settings")

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/settings/gpu")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(spec, "/recommendations/openshift/settings/gpu", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), schema)
	assertThresholdPluginFields(t, rec.Body.Bytes(), []string{
		"idle_threshold", "underutilized_sm_threshold", "locked_fields",
	})
}

func seedOpenAPIPVCRecommendation(t *testing.T, pool *pgxpool.Pool, orgID string) (namespace, pvcName string) {
	t.Helper()
	ctx := context.Background()
	namespace = "openapi-pvc-ns"
	pvcName = "openapi-pvc-01"

	_, err := pool.Exec(ctx,
		`INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'openapi-pvc', 1, NOW()) ON CONFLICT DO NOTHING`,
		testutil.TestClusterUUID,
	)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO pvc_recommendation_sets (
			org_id, cluster_uuid, namespace, persistentvolumeclaim, term,
			storageclass, recommendation_type, usage_ratio, capacity_bytes, usage_bytes_max,
			notification_codes, data_days, updated_at
		) VALUES ($1, $2, $3, $4, 'medium', 'gp3-csi', 'oversized', 0.1, 10737418240, 1073741824, '{}', 14, NOW())
		ON CONFLICT (org_id, cluster_uuid, namespace, persistentvolumeclaim, term) DO NOTHING`,
		orgID, testutil.TestClusterUUID, namespace, pvcName,
	)
	require.NoError(t, err)
	return namespace, pvcName
}

func seedOpenAPIPVCDetailData(t *testing.T, pool *pgxpool.Pool, orgID, namespace, pvcName string) {
	t.Helper()
	ctx := context.Background()
	for _, term := range []string{"short", "medium", "long"} {
		_, err := pool.Exec(ctx, `
			INSERT INTO pvc_recommendation_sets (
				org_id, cluster_uuid, namespace, persistentvolumeclaim, term,
				recommendation_type, usage_ratio, capacity_bytes, usage_bytes_max,
				notification_codes, data_days, updated_at
			) VALUES ($1, $2, $3, $4, $5, 'near_full', 0.9, 10737418240, 9663676416, '{}', 30, NOW())
			ON CONFLICT (org_id, cluster_uuid, namespace, persistentvolumeclaim, term) DO NOTHING`,
			orgID, testutil.TestClusterUUID, namespace, pvcName, term,
		)
		require.NoError(t, err)
	}

	bucketDate := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	monthEnd := bucketDate.AddDate(0, 1, 0)
	partName := fmt.Sprintf("daily_pvc_digests_%s", bucketDate.Format("200601"))
	_, err := pool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF daily_pvc_digests FOR VALUES FROM ('%s') TO ('%s')`,
		partName, bucketDate.Format("2006-01-02"), monthEnd.Format("2006-01-02"),
	))
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO daily_pvc_digests (
			bucket_date, org_id, cluster_uuid, namespace, persistentvolumeclaim,
			capacity_bytes, usage_bytes_min, usage_bytes_max, usage_bytes_avg, sample_count
		) VALUES ($1::date, $2, $3, $4, $5, 10737418240, 5000000000, 9000000000, 7000000000, 24)
		ON CONFLICT (cluster_uuid, namespace, persistentvolumeclaim, bucket_date) DO NOTHING`,
		bucketDate.Format("2006-01-02"), orgID, testutil.TestClusterUUID, namespace, pvcName,
	)
	require.NoError(t, err)
}

func TestOpenAPI_PVCList_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	orgID := "org-openapi-pvc-list"
	_, _ = seedOpenAPIPVCRecommendation(t, pool, orgID)
	e := setupContractTestEcho(t, pool, orgID)

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/pvcs?limit=1")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	listSchema := getResponseSchema(spec, "/recommendations/openshift/pvcs", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), listSchema)
	assertResponseDataItemsHaveSpecProperties(t, rec.Body.Bytes(), spec.componentSchema("PVCRecommendation"))
}

func TestOpenAPI_PVCDetail_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	orgID := "org-openapi-pvc-detail"
	namespace, pvcName := seedOpenAPIPVCRecommendation(t, pool, orgID)
	seedOpenAPIPVCDetailData(t, pool, orgID, namespace, pvcName)
	e := setupContractTestEcho(t, pool, orgID)

	rec := makeContractRequest(t, e, http.MethodGet,
		fmt.Sprintf("%s/recommendations/openshift/pvcs/detail?cluster_uuid=%s&namespace=%s&persistentvolumeclaim=%s",
			apiV1Prefix, testutil.TestClusterUUID, namespace, pvcName))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	detailSchema := getResponseSchema(spec, "/recommendations/openshift/pvcs/detail", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), detailSchema)
}

func TestOpenAPI_PVCThresholdSettings_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	e := setupContractTestEcho(t, pool, "org-openapi-pvc-settings")

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/settings/pvc")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(spec, "/recommendations/openshift/settings/pvc", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), schema)
	assertThresholdPluginFields(t, rec.Body.Bytes(), []string{
		"oversized_threshold", "near_full_threshold", "locked_fields",
	})
}

func seedOpenAPIQuotaRecommendation(t *testing.T, pool *pgxpool.Pool, orgID string) (clusterUUID, namespace string) {
	t.Helper()
	ctx := context.Background()
	clusterUUID = testutil.TestClusterUUID
	namespace = "openapi-quota-ns"

	_, err := pool.Exec(ctx, `
		INSERT INTO quota_recommendation_sets (
			org_id, cluster_uuid, namespace,
			cpu_request_hard_millicores, cpu_request_used_millicores,
			cpu_request_recommended_millicores,
			cpu_request_utilization_bp, recommendation_type, risk_level,
			last_observed_at
		) VALUES ($1, $2::uuid, $3, 100000, 25000, 36000, 2500, 'tighten', 'low', NOW())
		ON CONFLICT (org_id, cluster_uuid, namespace, quota_name) DO UPDATE SET
			recommendation_type = EXCLUDED.recommendation_type`,
		orgID, clusterUUID, namespace,
	)
	require.NoError(t, err)

	_, err = pool.Exec(ctx, `
		INSERT INTO quota_recommendation_history (
			org_id, cluster_uuid, namespace, quota_name,
			resource, recommendation_type, risk_level,
			recommended_hard, current_hard, current_used, utilization_percent,
			recorded_at
		) VALUES ($1, $2::uuid, $3, '', 'cpu_request', 'tighten', 'low', 36000, 100000, 50000, 50, NOW() - INTERVAL '2 days')`,
		orgID, clusterUUID, namespace,
	)
	require.NoError(t, err)

	return clusterUUID, namespace
}

func TestOpenAPI_QuotaList_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	orgID := "org-openapi-quota-list"
	_, _ = seedOpenAPIQuotaRecommendation(t, pool, orgID)
	e := setupContractTestEcho(t, pool, orgID)

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/quota?limit=1")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	listSchema := getResponseSchema(spec, "/recommendations/openshift/quota", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), listSchema)
	assertResponseDataItemsHaveSpecProperties(t, rec.Body.Bytes(), spec.componentSchema("QuotaRecommendation"))
}

func TestOpenAPI_QuotaDetail_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	orgID := "org-openapi-quota-detail"
	clusterUUID, namespace := seedOpenAPIQuotaRecommendation(t, pool, orgID)
	e := setupContractTestEcho(t, pool, orgID)

	rec := makeContractRequest(t, e, http.MethodGet,
		fmt.Sprintf("%s/recommendations/openshift/quota/detail?cluster_uuid=%s&namespace=%s",
			apiV1Prefix, clusterUUID, namespace))
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	detailSchema := getResponseSchema(spec, "/recommendations/openshift/quota/detail", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), detailSchema)

	var detail map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &detail))
	history, ok := detail["history"].([]interface{})
	require.True(t, ok, "detail response must include history array")
	require.NotEmpty(t, history, "seeded quota history must appear in detail response")
	first, ok := history[0].(map[string]interface{})
	require.True(t, ok)
	assertObjectHasSpecProperties(t, first, spec.componentSchema("QuotaRecommendationHistoryEntry"), "history[0]")
}

func TestOpenAPI_QuotaSettings_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	e := setupContractTestEcho(t, pool, "org-openapi-quota-settings")

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/settings/quota")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(spec, "/recommendations/openshift/settings/quota", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), schema)
}

func TestOpenAPI_ClusterQuotaSettings_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	e := setupContractTestEcho(t, pool, "org-openapi-cluster-quota-settings")

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/settings/cluster-quota")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(spec, "/recommendations/openshift/settings/cluster-quota", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), schema)
}

func TestOpenAPI_SnapshotSettings_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	e := setupContractTestEcho(t, pool, "org-openapi-snapshot-settings")

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/settings/snapshot")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(spec, "/recommendations/openshift/settings/snapshot", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), schema)
	assertThresholdPluginFields(t, rec.Body.Bytes(), []string{
		"orphan_age_days",
		"never_restored_days",
		"stale_days",
		"redundant_threshold",
		"cost_per_gib_month_usd",
		"inventory_fresh_hours",
		"locked_fields",
	})
}

func TestOpenAPI_IdleDetectionSettings_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	e := setupContractTestEcho(t, pool, "org-openapi-idle-settings")

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/settings/idle-detection")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(spec, "/recommendations/openshift/settings/idle-detection", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), schema)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	idleDetection, ok := resp["idle_detection"].(map[string]interface{})
	require.True(t, ok, "idle_detection object must be present")
	_, ok = idleDetection["enabled"].(bool)
	assert.True(t, ok, "idle_detection.enabled must be a boolean")
	assertObjectHasSpecProperties(t, idleDetection, spec.componentSchema("IdleDetectionSettings"), "idle_detection")
}

func TestOpenAPI_TermSettings_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	e := setupContractTestEcho(t, pool, "org-openapi-term-settings")

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/settings/terms?recommendation_type=container")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(spec, "/recommendations/openshift/settings/terms", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), schema)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "container", resp["recommendation_type"])
	terms, ok := resp["terms"].([]interface{})
	require.True(t, ok)
	require.NotEmpty(t, terms)
	first, ok := terms[0].(map[string]interface{})
	require.True(t, ok)
	for _, prop := range []string{"name", "window_days", "min_data_days", "decay_halflife_hours", "locked", "is_default"} {
		_, exists := first[prop]
		assert.True(t, exists, "terms[0] missing property %q", prop)
	}
}

func TestOpenAPI_Capabilities_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	e := setupContractTestEcho(t, pool, "org-openapi-capabilities")

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/settings/capabilities")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(spec, "/recommendations/openshift/settings/capabilities", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), schema)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	types, ok := resp["recommendation_types"].([]interface{})
	require.True(t, ok)
	require.NotEmpty(t, types)
	first, ok := types[0].(map[string]interface{})
	require.True(t, ok)
	assertObjectHasSpecProperties(t, first, spec.componentSchema("CapabilityItem"), "recommendation_types[0]")
	_, ok = resp["business_hours"].(bool)
	assert.True(t, ok, "business_hours must be a boolean")
}

func TestOpenAPI_BusinessHoursSettings_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	e := setupContractTestEcho(t, pool, "org-openapi-bh-settings")

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/settings/business-hours")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(spec, "/recommendations/openshift/settings/business-hours", http.MethodGet, "200")
	assertBusinessHoursSettingsResponse(t, rec.Body.Bytes(), schema)
}

func TestOpenAPI_BusinessHoursClusterSettings_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	orgID := "org-openapi-bh-cluster"
	clusterUUID := testutil.TestClusterUUID
	seedContractTestBHCluster(t, pool, orgID, clusterUUID)
	e := setupContractTestEcho(t, pool, orgID)

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/settings/business-hours/clusters/"+clusterUUID)
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(
		spec,
		"/recommendations/openshift/settings/business-hours/clusters/{cluster_id}",
		http.MethodGet,
		"200",
	)
	assertBusinessHoursSettingsResponse(t, rec.Body.Bytes(), schema)
}

func TestOpenAPI_BusinessHoursNamespaceSettings_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	orgID := "org-openapi-bh-namespace"
	clusterUUID := testutil.TestClusterUUID
	seedContractTestBHCluster(t, pool, orgID, clusterUUID)
	e := setupContractTestEcho(t, pool, orgID)

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/settings/business-hours/clusters/"+clusterUUID+"/namespaces/openshift-console")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(
		spec,
		"/recommendations/openshift/settings/business-hours/clusters/{cluster_id}/namespaces/{namespace}",
		http.MethodGet,
		"200",
	)
	assertBusinessHoursSettingsResponse(t, rec.Body.Bytes(), schema)
}

func openAPIPathHasQueryParam(spec openAPISpecDoc, path, method, paramName string) bool {
	pathRaw, ok := spec.Paths[path]
	if !ok {
		return false
	}
	var item openAPIPathItem
	if err := json.Unmarshal(pathRaw, &item); err != nil {
		return false
	}
	var op *openAPIOperation
	switch strings.ToUpper(method) {
	case http.MethodGet:
		op = item.Get
	case http.MethodPut:
		op = item.Put
	case http.MethodPost:
		op = item.Post
	case http.MethodDelete:
		op = item.Delete
	}
	if op == nil {
		return false
	}
	for _, p := range op.Parameters {
		if p.In == "query" && p.Name == paramName {
			return true
		}
	}
	return false
}

func TestOpenAPI_TagFilter_QueryParameters(t *testing.T) {
	spec := loadOpenAPISpec(t)
	tagFilter := "filter[tag:environment]"
	tagGroupBy := "group_by[tag:environment]"

	pathsWithTagFilter := []string{
		"/recommendations/openshift",
		"/recommendations/openshift/history",
		"/recommendations/openshift/namespaces",
		"/recommendations/openshift/nodes",
		"/recommendations/openshift/pvcs",
		"/recommendations/openshift/vm",
		"/recommendations/openshift/gpu/mig",
		"/recommendations/openshift/gpu/timeslicing",
		"/recommendations/openshift/quota",
		"/recommendations/openshift/cluster-quota",
	}
	for _, path := range pathsWithTagFilter {
		assert.True(t, openAPIPathHasQueryParam(spec, path, http.MethodGet, tagFilter),
			"path %s should document %s", path, tagFilter)
	}

	assert.False(t, openAPIPathHasQueryParam(spec, "/recommendations/openshift/snapshots", http.MethodGet, tagFilter),
		"snapshot list must not expose tag filters")
	assert.True(t, openAPIPathHasQueryParam(spec, "/recommendations/openshift/savings-summary", http.MethodGet, tagGroupBy),
		"savings-summary should document group_by[tag:environment]")
}

func TestOpenAPI_AllRoutesHaveSpecEntry(t *testing.T) {
	spec := loadOpenAPISpec(t)
	enableAllPluginsForContractTest(t)

	e := echo.New()
	registerContractTestRoutes(e)
	registered := collectRegisteredRoutePatterns(e)
	specRoutes := collectOpenAPIRoutePatterns(spec)

	skipRoutes := map[string]struct{}{
		"GET /recommendations/openshift/openapi.json": {},
		// Operational/internal routes not part of the public API spec
		"GET /healthz":                            {},
		"GET /readyz":                             {},
		"GET /status":                             {},
		"GET /internal/tags/status":               {},
		"POST /internal/tags/sync":                {},
		"POST /internal/recalculate-savings":      {},
		"POST /internal/backfill-gpu-timeslicing": {},
		// Legacy route kept for backward compatibility
		"GET /openshift/namespace/recommendations": {},
	}

	var undocumented []string
	for key := range registered {
		if _, skip := skipRoutes[key]; skip {
			continue
		}
		if _, ok := specRoutes[key]; !ok {
			undocumented = append(undocumented, key)
		}
	}
	sort.Strings(undocumented)
	assert.Empty(t, undocumented, "registered routes missing from OpenAPI spec")
}
