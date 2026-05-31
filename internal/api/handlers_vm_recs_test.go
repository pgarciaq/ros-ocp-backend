package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/platform-go-middlewares/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func setupVMRecommendationsHandler(t *testing.T, orgID string) *echo.Echo {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	config.ResetForTest()
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	_ = config.GetConfig()

	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("Identity", identity.XRHID{
				Identity: identity.Identity{OrgID: orgID},
			})
			c.Set("user.permissions", map[string][]string{"*": {}})
			return next(c)
		}
	})
	v1 := e.Group("/api/cost-management/v1")
	v1.GET("/recommendations/openshift/vm", GetVMRecommendations)
	v1.GET("/recommendations/openshift/vm/detail", GetVMRecommendationDetail)
	v1.GET("/recommendations/openshift/settings/vm", GetVMSettings)
	return e
}

func TestVMRecommendations_ListEmpty(t *testing.T) {
	orgID := "org-vm-rec-empty-" + uuid.New().String()[:8]
	e := setupVMRecommendationsHandler(t, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/vm", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp VMRecommendationListResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Meta.Count)
	assert.NotNil(t, resp.Data)
	assert.Empty(t, resp.Data)
}

func TestVMRecommendations_ListAcceptsFilters(t *testing.T) {
	orgID := "org-vm-rec-filters-" + uuid.New().String()[:8]
	e := setupVMRecommendationsHandler(t, orgID)

	url := "/api/cost-management/v1/recommendations/openshift/vm" +
		"?filter[namespace]=prod&filter[vm_name]=web&filter[cluster]=00000000-0000-0000-0000-000000000099" +
		"&filter[term]=short_term&filter[engine]=cost&filter[is_idle]=true&limit=5&offset=0"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
}

func TestVMRecommendations_DetailMissingParams(t *testing.T) {
	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("Identity", identity.XRHID{
				Identity: identity.Identity{OrgID: "org-vm-detail"},
			})
			return next(c)
		}
	})
	v1 := e.Group("/api/cost-management/v1")
	v1.GET("/recommendations/openshift/vm/detail", GetVMRecommendationDetail)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/vm/detail", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "error", body["status"])
}

func TestVMSettings_GET_ReturnsConfig(t *testing.T) {
	orgID := "org-vm-settings-get-" + uuid.New().String()[:8]
	e := setupVMRecommendationsHandler(t, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/settings/vm", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Contains(t, resp, "thresholds")
	assert.Contains(t, resp, "disk")
	assert.Contains(t, resp, "io")
}
