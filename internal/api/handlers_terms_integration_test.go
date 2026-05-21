package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

type termsResponse struct {
	Terms []termItem `json:"terms"`
}

type termItem struct {
	Name               string  `json:"name"`
	WindowDays         int     `json:"window_days"`
	MinDataDays        int     `json:"min_data_days"`
	DecayHalfLifeHours float64 `json:"decay_halflife_hours"`
	IsDefault          bool    `json:"is_default"`
}

func setupTermsApp(t *testing.T) (*echo.Echo, string) {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	connStr := pool.Config().ConnString()
	gormDB, err := gorm.Open(postgres.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	database.DB = gormDB
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/settings/terms", api.GetTermSettings)
	v1.PUT("/recommendations/openshift/settings/terms", api.PutTermSettings)
	v1.DELETE("/recommendations/openshift/settings/terms", api.DeleteTermSettings)

	return app, makeIdentityHeader(testutil.TestOrgID)
}

func TestGetTermSettings_ReturnsDefaults(t *testing.T) {
	app, identity := setupTermsApp(t)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/terms", nil)
	req.Header.Set("X-Rh-Identity", identity)
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp termsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Terms, 3)

	assert.Equal(t, "short", resp.Terms[0].Name)
	assert.Equal(t, 1, resp.Terms[0].WindowDays)
	assert.Equal(t, 1, resp.Terms[0].MinDataDays)
	assert.True(t, resp.Terms[0].IsDefault)

	assert.Equal(t, "medium", resp.Terms[1].Name)
	assert.Equal(t, 7, resp.Terms[1].WindowDays)

	assert.Equal(t, "long", resp.Terms[2].Name)
	assert.Equal(t, 15, resp.Terms[2].WindowDays)
}

func TestPutTermSettings_CustomTerms(t *testing.T) {
	app, identity := setupTermsApp(t)

	body := `{"terms":[
		{"name":"short","window_days":3,"decay_halflife_hours":0},
		{"name":"medium","window_days":14,"decay_halflife_hours":200},
		{"name":"long","window_days":30,"decay_halflife_hours":400}
	]}`

	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/terms", strings.NewReader(body))
	req.Header.Set("X-Rh-Identity", identity)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp termsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Terms, 3)

	assert.Equal(t, "short", resp.Terms[0].Name)
	assert.Equal(t, 3, resp.Terms[0].WindowDays)
	assert.False(t, resp.Terms[0].IsDefault)

	assert.Equal(t, "medium", resp.Terms[1].Name)
	assert.Equal(t, 14, resp.Terms[1].WindowDays)

	assert.Equal(t, "long", resp.Terms[2].Name)
	assert.Equal(t, 30, resp.Terms[2].WindowDays)
	assert.Equal(t, 15, resp.Terms[2].MinDataDays)

	// GET should now return custom terms
	req2 := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/terms", nil)
	req2.Header.Set("X-Rh-Identity", identity)
	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)

	var resp2 termsResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))
	assert.Equal(t, 3, resp2.Terms[0].WindowDays)
	assert.False(t, resp2.Terms[0].IsDefault)
}

func TestDeleteTermSettings_RestoresDefaults(t *testing.T) {
	app, identity := setupTermsApp(t)

	// First set custom terms
	body := `{"terms":[{"name":"short","window_days":5},{"name":"medium","window_days":20}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/terms", strings.NewReader(body))
	req.Header.Set("X-Rh-Identity", identity)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	// DELETE to restore defaults
	req2 := httptest.NewRequest(http.MethodDelete, "/api/cost-management/v1/recommendations/openshift/settings/terms", nil)
	req2.Header.Set("X-Rh-Identity", identity)
	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)

	var resp termsResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp))
	require.Len(t, resp.Terms, 3)
	assert.Equal(t, 1, resp.Terms[0].WindowDays)
	assert.True(t, resp.Terms[0].IsDefault)
}

func TestPutTermSettings_ValidationErrors(t *testing.T) {
	app, identity := setupTermsApp(t)

	tests := []struct {
		name string
		body string
	}{
		{"empty terms", `{"terms":[]}`},
		{"window_days too low", `{"terms":[{"name":"short","window_days":0}]}`},
		{"window_days too high", `{"terms":[{"name":"short","window_days":91}]}`},
		{"invalid name", `{"terms":[{"name":"daily","window_days":1}]}`},
		{"negative decay", `{"terms":[{"name":"short","window_days":1,"decay_halflife_hours":-1}]}`},
		{"decay exceeds max", `{"terms":[{"name":"medium","window_days":14,"decay_halflife_hours":9000}]}`},
		{"too many terms", `{"terms":[{"name":"short","window_days":1},{"name":"medium","window_days":7},{"name":"long","window_days":15},{"name":"short","window_days":2}]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/terms", strings.NewReader(tt.body))
			req.Header.Set("X-Rh-Identity", identity)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusBadRequest, rec.Code, "case %q: body: %s", tt.name, rec.Body.String())
		})
	}
}

func TestPutTermSettings_MalformedJSON(t *testing.T) {
	app, identity := setupTermsApp(t)

	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/terms", strings.NewReader(`{not valid json`))
	req.Header.Set("X-Rh-Identity", identity)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPutTermSettings_DuplicateTermNames(t *testing.T) {
	app, identity := setupTermsApp(t)

	body := `{"terms":[
		{"name":"short","window_days":3},
		{"name":"short","window_days":5}
	]}`

	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/terms", strings.NewReader(body))
	req.Header.Set("X-Rh-Identity", identity)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	// The second "short" should win via ON CONFLICT DO UPDATE
	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp termsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	shortCount := 0
	for _, term := range resp.Terms {
		if term.Name == "short" {
			shortCount++
			assert.Equal(t, 5, term.WindowDays, "last write should win")
		}
	}
	assert.Equal(t, 1, shortCount, "should have exactly one short term in response")
}

func TestPutTermSettings_PartialUpdate_ReplacesAll(t *testing.T) {
	app, identity := setupTermsApp(t)

	// First set all 3 custom terms
	body1 := `{"terms":[
		{"name":"short","window_days":3},
		{"name":"medium","window_days":14},
		{"name":"long","window_days":30}
	]}`
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/terms", strings.NewReader(body1))
	req.Header.Set("X-Rh-Identity", identity)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	// Now PUT only medium — this should DELETE short and long overrides
	body2 := `{"terms":[{"name":"medium","window_days":21}]}`
	req2 := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/terms", strings.NewReader(body2))
	req2.Header.Set("X-Rh-Identity", identity)
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)

	var resp termsResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp))

	// Only medium should be custom; LoadTermConfig returns only the custom rows
	// when any exist, so we expect just 1 term
	require.Len(t, resp.Terms, 1)
	assert.Equal(t, "medium", resp.Terms[0].Name)
	assert.Equal(t, 21, resp.Terms[0].WindowDays)
}

func TestPutTermSettings_OrgIsolation(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	connStr := pool.Config().ConnString()
	gormDB, err := gorm.Open(postgres.Open(connStr), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)
	database.DB = gormDB
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/settings/terms", api.GetTermSettings)
	v1.PUT("/recommendations/openshift/settings/terms", api.PutTermSettings)

	org1Header := makeIdentityHeader("org-alpha")
	org2Header := makeIdentityHeader("org-beta")

	// org1 sets custom terms
	body := `{"terms":[{"name":"short","window_days":10}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/cost-management/v1/recommendations/openshift/settings/terms", strings.NewReader(body))
	req.Header.Set("X-Rh-Identity", org1Header)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	// org2 should still see defaults
	req2 := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/terms", nil)
	req2.Header.Set("X-Rh-Identity", org2Header)
	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusOK, rec2.Code)

	var resp termsResponse
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp))
	require.Len(t, resp.Terms, 3, "org2 should see all 3 default terms")
	assert.Equal(t, 1, resp.Terms[0].WindowDays, "org2 short term should be default 1d")
	assert.True(t, resp.Terms[0].IsDefault)
}
