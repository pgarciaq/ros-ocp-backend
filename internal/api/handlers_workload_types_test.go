package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/redhatinsights/platform-go-middlewares/identity"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func setupWorkloadTypesHandler(t *testing.T, orgID string) *echo.Echo {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

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
	v1.GET("/recommendations/openshift/workload-types", GetWorkloadTypes)
	return e
}

func TestGetWorkloadTypes_Unauthorized_Returns401(t *testing.T) {
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/workload-types", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)

	err := GetWorkloadTypes(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestGetWorkloadTypes_EmptyOrg_ReturnsEmptyArray(t *testing.T) {
	orgID := "org-wt-empty-" + uuid.New().String()[:8]
	e := setupWorkloadTypesHandler(t, orgID)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/workload-types", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp map[string][]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.NotNil(t, resp["data"])
	assert.Empty(t, resp["data"])
}

func TestGetWorkloadTypes_ReturnsDistinctSorted(t *testing.T) {
	orgID := "org-wt-distinct-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	ctx := context.Background()
	clusterUUID := uuid.New()

	// Insert test data with various workload types (including duplicates and empty).
	rows := []struct {
		ns, workload, workloadType, container string
	}{
		{"ns1", "deploy-a", "deployment", "app"},
		{"ns1", "deploy-b", "deployment", "sidecar"},
		{"ns2", "ds-a", "daemonset", "agent"},
		{"ns3", "sts-a", "statefulset", "db"},
		{"ns4", "custom-a", "domain", "runner"},
		{"ns5", "empty-wt", "", "box"},
	}
	for _, r := range rows {
		_, err := pool.Exec(ctx,
			`INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name)
			 VALUES ($1, $2, $3, $4, $5, $6)
			 ON CONFLICT DO NOTHING`,
			orgID, clusterUUID, r.ns, r.workload, r.workloadType, r.container,
		)
		require.NoError(t, err)
	}

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
	v1.GET("/recommendations/openshift/workload-types", GetWorkloadTypes)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/workload-types", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp map[string][]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))

	expected := []string{"daemonset", "deployment", "domain", "statefulset"}
	assert.Equal(t, expected, resp["data"])
}

func TestGetWorkloadTypes_IsolatedByOrg(t *testing.T) {
	orgA := "org-wt-iso-a-" + uuid.New().String()[:8]
	orgB := "org-wt-iso-b-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	ctx := context.Background()
	clusterUUID := uuid.New()

	_, err := pool.Exec(ctx,
		`INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name)
		 VALUES ($1, $2, 'ns1', 'wl1', 'deployment', 'c1')`,
		orgA, clusterUUID,
	)
	require.NoError(t, err)

	_, err = pool.Exec(ctx,
		`INSERT INTO org_container_keys (org_id, cluster_uuid, namespace, workload, workload_type, container_name)
		 VALUES ($1, $2, 'ns1', 'wl1', 'cronjob', 'c1')`,
		orgB, clusterUUID,
	)
	require.NoError(t, err)

	// Query as orgA — should only see "deployment"
	e := echo.New()
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("Identity", identity.XRHID{
				Identity: identity.Identity{OrgID: orgA},
			})
			c.Set("user.permissions", map[string][]string{"*": {}})
			return next(c)
		}
	})
	v1 := e.Group("/api/cost-management/v1")
	v1.GET("/recommendations/openshift/workload-types", GetWorkloadTypes)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/workload-types", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	var resp map[string][]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, []string{"deployment"}, resp["data"])
}
