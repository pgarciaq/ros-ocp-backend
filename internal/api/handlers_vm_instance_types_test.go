package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/platform-go-middlewares/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func setupClusterInstanceTypesHandler(t *testing.T, orgID string) (*echo.Echo, uuid.UUID) {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	config.ResetForTest()
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	_ = config.GetConfig()

	clusterUUID := uuid.New()
	doc := engine.ClusterInstanceTypesPayload{
		ClusterUUID: clusterUUID.String(),
		CollectedAt: time.Date(2026, 5, 31, 20, 0, 0, 0, time.UTC),
		InstanceTypes: []engine.ClusterInstanceTypeRecord{
			{Name: "u1.large", Series: engine.NormalizeInstanceTypeSeries("general-purpose"), VCPU: 2, MemoryGiB: 8},
		},
	}
	require.NoError(t, engine.UpsertClusterInstanceTypes(context.Background(), pool, orgID, clusterUUID, doc))

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
	v1.GET("/recommendations/openshift/instance-types", GetClusterInstanceTypes)
	return e, clusterUUID
}

func TestGetClusterInstanceTypes_ReturnsCatalog(t *testing.T) {
	orgID := "org-vm-inst-types-" + uuid.New().String()[:8]
	e, clusterUUID := setupClusterInstanceTypesHandler(t, orgID)

	url := "/api/cost-management/v1/recommendations/openshift/instance-types?cluster_uuid=" + clusterUUID.String()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp clusterInstanceTypesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, clusterUUID.String(), resp.ClusterUUID)
	require.Len(t, resp.InstanceTypes, 1)
	assert.Equal(t, "u1.large", resp.InstanceTypes[0].Name)
}

func TestGetClusterInstanceTypes_MissingClusterUUID_Returns400(t *testing.T) {
	orgID := "org-vm-inst-types-bad-" + uuid.New().String()[:8]
	e, _ := setupClusterInstanceTypesHandler(t, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/instance-types", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}
