package api_test

import (
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
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// CSV export is not implemented (#536): requesting a CSV format must fail
// closed with 406, not serve JSON with a CSV content type (or crash). Empty
// tables still reach the format switch, so no seeds are needed.
func TestGetNamespaceRecommendationSetList_CSVFormat_Returns406(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/namespaces", api.GetNamespaceRecommendationSetList)

	for _, tc := range []struct {
		name   string
		header map[string]string
	}{
		{"accept header", map[string]string{"Accept": "text/csv"}},
		{"format param", map[string]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := "/api/cost-management/v1/recommendations/openshift/namespaces"
			if tc.name == "format param" {
				path += "?format=csv"
			}
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
			for k, v := range tc.header {
				req.Header.Set(k, v)
			}
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			require.Equal(t, http.StatusNotAcceptable, rec.Code, rec.Body.String())

			var body map[string]string
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Equal(t, "error", body["status"])
		})
	}
}
