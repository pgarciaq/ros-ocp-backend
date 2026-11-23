package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// makeAPIIdentityHeader mirrors the api_test helper (this file is package api
// to reach the unexported group_by parsers).
func makeAPIIdentityHeader(t *testing.T, orgID string) string {
	t.Helper()
	payload, err := json.Marshal(map[string]interface{}{
		"identity": map[string]interface{}{
			"org_id":         orgID,
			"account_number": "test",
			"type":           "User",
			"user": map[string]interface{}{
				"username":     "test_user",
				"is_org_admin": true,
			},
		},
		"entitlements": map[string]interface{}{
			"cost_management": map[string]interface{}{"is_entitled": true},
		},
	})
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(payload)
}

// #922: out-of-vocabulary group_by values must be rejected with 400, not
// silently ignored to return the ungrouped list. Each group-capable endpoint
// validates against its own vocabulary via queryparams.ValidateGroupBy.

func newGroupByParseContext(t *testing.T, rawQuery string) echo.Context {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/?"+rawQuery, nil)
	return echo.New().NewContext(req, httptest.NewRecorder())
}

func TestParseVMListGroupBy_RejectsOutOfVocabulary(t *testing.T) {
	c := newGroupByParseContext(t, "group_by%5Bnode%5D=true")
	_, err := parseVMListGroupBy(c)
	require.Error(t, err, "group_by[node] on the VM list must be rejected — it is not a served dimension (#922)")
	assert.Contains(t, err.Error(), "invalid group_by value")
}

func TestParseVMListGroupBy_AcceptsVocabulary(t *testing.T) {
	for _, q := range []string{
		"group_by%5Bcluster%5D=true",
		"group_by%5Bproject%5D=true",
		"group_by%5Bnamespace%5D=true",
		"group_by=cluster",
	} {
		c := newGroupByParseContext(t, q)
		field, err := parseVMListGroupBy(c)
		require.NoError(t, err, q)
		assert.NotEmpty(t, field, q)
	}
}

func TestParseGPUMIGListGroupBy_RejectsOutOfVocabulary(t *testing.T) {
	c := newGroupByParseContext(t, "group_by%5Bnode%5D=true")
	_, _, err := parseGPUMIGListGroupBy(c)
	require.Error(t, err, "group_by[node] on the GPU MIG list must be rejected (#922)")
	assert.Contains(t, err.Error(), "invalid group_by value")
}

func TestParseStorageListGroupBy_RejectsOutOfVocabulary(t *testing.T) {
	c := newGroupByParseContext(t, "group_by%5Bnode%5D=true")
	_, _, err := parseStorageListGroupBy(c)
	require.Error(t, err, "group_by[node] on the storage lists must be rejected (#922)")
	assert.Contains(t, err.Error(), "invalid group_by value")
}

// The node-side group_by checks are inline in the handlers (no parser
// function); route-level reds cover them.
func TestGPUTimeslicingList_RejectsOutOfVocabularyGroupBy(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/gpu/timeslicing", GetNodeRecommendations)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/gpu/timeslicing?group_by%5Bproject%5D=true", nil)
	req.Header.Set("X-Rh-Identity", makeAPIIdentityHeader(t, testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code,
		"group_by[project] on the GPU timeslicing list must 400, got %d: %s (#922)", rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "invalid group_by value")
}

func TestNodeUtilizationList_RejectsOutOfVocabularyGroupBy(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/nodes", GetNodeUtilizationRecs)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/nodes?group_by%5Bproject%5D=true", nil)
	req.Header.Set("X-Rh-Identity", makeAPIIdentityHeader(t, testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code,
		"group_by[project] on the node utilization list must 400, got %d: %s (#922)", rec.Code, rec.Body.String())
	assert.Contains(t, rec.Body.String(), "invalid group_by value")
}
