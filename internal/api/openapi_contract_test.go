package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/platform-go-middlewares/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
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

func assertResponseHasSpecProperties(t *testing.T, body []byte, schema map[string]interface{}) {
	t.Helper()
	require.NotNil(t, schema, "response schema must be resolved from openapi.json")

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(body, &resp))

	// Fields documented in OpenAPI but omitted from JSON when empty.
	optionalFields := map[string]struct{}{
		"warnings": {},
	}

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
	api.RegisterTestInternalRoutes(e)
	e.GET("/status", api.GetAppStatus)
}

func setupContractTestEcho(t *testing.T, pool *pgxpool.Pool, orgID string) *echo.Echo {
	t.Helper()
	enableAllPluginsForContractTest(t)
	if pool != nil {
		prev := database.Pool
		database.Pool = pool
		connStr := pool.Config().ConnString()
		gormDB, err := gorm.Open(postgres.Open(connStr), &gorm.Config{
			Logger: logger.Default.LogMode(logger.Silent),
		})
		require.NoError(t, err)
		prevGorm := database.DB
		database.DB = gormDB
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
		if !strings.HasPrefix(route.Path, apiV1Prefix) && route.Path != "/status" {
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

func TestOpenAPI_ThresholdSettings_ResponseFields(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	spec := loadOpenAPISpec(t)
	pool := testutil.SetupTestDB(t)
	orgID := "org-openapi-thresholds"
	e := setupContractTestEcho(t, pool, orgID)

	rec := makeContractRequest(t, e, http.MethodGet,
		apiV1Prefix+"/recommendations/openshift/settings/thresholds?recommendation_type=container")
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	schema := getResponseSchema(spec, "/recommendations/openshift/settings/thresholds", http.MethodGet, "200")
	assertResponseHasSpecProperties(t, rec.Body.Bytes(), schema)
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

func TestOpenAPI_AllRoutesHaveSpecEntry(t *testing.T) {
	spec := loadOpenAPISpec(t)
	enableAllPluginsForContractTest(t)

	e := echo.New()
	registerContractTestRoutes(e)
	registered := collectRegisteredRoutePatterns(e)
	specRoutes := collectOpenAPIRoutePatterns(spec)

	skipRoutes := map[string]struct{}{
		"GET /recommendations/openshift/openapi.json": {},
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
