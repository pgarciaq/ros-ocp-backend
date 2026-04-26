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
