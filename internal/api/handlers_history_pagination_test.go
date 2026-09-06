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
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// Limit/offset parsing precedes db.GetPool() in both history handlers, so
// these 400s are hermetic — no testcontainers needed (#531).
func TestHistoryEndpoints_InvalidPagination_Returns400(t *testing.T) {
	config.ResetForTest()
	t.Setenv("ROS_API_MAX_OFFSET", "10000")
	_ = config.GetConfig()

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/gpu/timeslicing/history", api.GetNodeGPUTimeslicingRecommendationHistory)
	v1.GET("/recommendations/openshift/vms/:vm_name/history", api.GetVMRecommendationHistory)

	base := map[string]string{
		"gpu": "/api/cost-management/v1/recommendations/openshift/gpu/timeslicing/history?cluster_uuid=c1&node_name=n1",
		"vm":  "/api/cost-management/v1/recommendations/openshift/vms/vm1/history?cluster_uuid=c1&namespace=ns1",
	}
	for _, tc := range []struct {
		name  string
		query string
	}{
		{"non-numeric limit", "&limit=abc"},
		{"negative limit", "&limit=-5"},
		{"over-max offset", "&offset=10001"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, path := range []string{base["gpu"], base["vm"]} {
				req := httptest.NewRequest(http.MethodGet, path+tc.query, nil)
				req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
				rec := httptest.NewRecorder()
				app.ServeHTTP(rec, req)
				require.Equal(t, http.StatusBadRequest, rec.Code, "%s %s: body %s", path, tc.query, rec.Body.String())

				var body map[string]string
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
				assert.Equal(t, "error", body["status"])
			}
		})
	}
}
