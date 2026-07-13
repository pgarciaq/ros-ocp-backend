package api_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/vm"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestVMRecHistory_APIEndpoint(t *testing.T) {
	orgID := "org-vm-hist-api-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedVMRecCluster(t, orgID)
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	now := time.Now().UTC()

	rec := model.VMRecommendation{
		OrgID: orgID, ClusterUUID: clusterID,
		VMName: "api-hist-vm", Namespace: "api-ns",
		Term: "short_term", Engine: "performance",
		RecommendedVCPU: 2, RecommendedMemoryGiB: 8,
		Confidence: "high", LastRecommendedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, vm.PersistVMRecommendations(context.Background(), pool, []model.VMRecommendation{rec}, nil))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/vms/:vm_name/history", api.GetVMRecommendationHistory)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/vms/api-hist-vm/history?cluster_uuid="+testutil.TestClusterUUID+"&namespace=api-ns&term=short_term&engine=performance&limit=10",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	var resp struct {
		Data []vm.VMRecommendationHistoryRow `json:"data"`
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &resp))
	assert.GreaterOrEqual(t, resp.Meta.Count, 1)
	assert.NotEmpty(t, resp.Data)
}

func setupVMHistoryEchoWithRBAC(t *testing.T, pool *pgxpool.Pool, perms map[string][]string) *echo.Echo {
	t.Helper()
	cfg := config.GetConfig()
	orig := cfg.RBACEnabled
	cfg.RBACEnabled = true
	t.Cleanup(func() { cfg.RBACEnabled = orig })

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("user.permissions", perms)
			return next(c)
		}
	})
	v1.GET("/recommendations/openshift/vms/:vm_name/history", api.GetVMRecommendationHistory)
	return app
}

func TestVMRecHistory_RBAC_FiltersUnauthorizedCluster(t *testing.T) {
	orgID := "org-vm-hist-rbac-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	allowedCluster := testutil.TestClusterUUID
	deniedCluster := "c2222222-2222-2222-2222-222222222222"
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	for _, cl := range []struct{ uuid, alias string }{
		{allowedCluster, "allowed"},
		{deniedCluster, "denied"},
	} {
		_, err = pool.Exec(ctx, `
			INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
			VALUES (1, $1::uuid, $2, 1, NOW()) ON CONFLICT DO NOTHING`, cl.uuid, cl.alias)
		require.NoError(t, err)
	}

	now := time.Now().UTC()
	for _, spec := range []struct {
		clusterID, vmName string
	}{
		{allowedCluster, "rbac-allowed-vm"},
		{deniedCluster, "rbac-denied-vm"},
	} {
		rec := model.VMRecommendation{
			OrgID: orgID, ClusterUUID: uuid.MustParse(spec.clusterID),
			VMName: spec.vmName, Namespace: "rbac-ns",
			Term: "short_term", Engine: "cost",
			RecommendedVCPU: 2, RecommendedMemoryGiB: 8,
			Confidence: "high", LastRecommendedAt: now, CreatedAt: now, UpdatedAt: now,
		}
		require.NoError(t, vm.PersistVMRecommendations(ctx, pool, []model.VMRecommendation{rec}, nil))
	}

	app := setupVMHistoryEchoWithRBAC(t, pool, map[string][]string{
		"openshift.cluster": {allowedCluster},
	})

	reqAllowed := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/vms/rbac-allowed-vm/history?cluster_uuid="+allowedCluster+"&namespace=rbac-ns&term=short_term&engine=cost",
		nil)
	reqAllowed.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	recAllowed := httptest.NewRecorder()
	app.ServeHTTP(recAllowed, reqAllowed)
	require.Equal(t, http.StatusOK, recAllowed.Code, recAllowed.Body.String())

	var allowedResp struct {
		Data []vm.VMRecommendationHistoryRow `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recAllowed.Body.Bytes(), &allowedResp))
	assert.NotEmpty(t, allowedResp.Data)

	reqDenied := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/vms/rbac-denied-vm/history?cluster_uuid="+deniedCluster+"&namespace=rbac-ns&term=short_term&engine=cost",
		nil)
	reqDenied.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	recDenied := httptest.NewRecorder()
	app.ServeHTTP(recDenied, reqDenied)
	require.Equal(t, http.StatusOK, recDenied.Code)

	var deniedResp struct {
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
		Data []vm.VMRecommendationHistoryRow `json:"data"`
	}
	require.NoError(t, json.Unmarshal(recDenied.Body.Bytes(), &deniedResp))
	assert.Equal(t, 0, deniedResp.Meta.Count)
	assert.Empty(t, deniedResp.Data)
}

func TestVMRecHistory_CSVExport(t *testing.T) {
	orgID := "org-vm-hist-csv-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedVMRecCluster(t, orgID)
	clusterID := uuid.MustParse(testutil.TestClusterUUID)
	now := time.Now().UTC()

	rec := model.VMRecommendation{
		OrgID: orgID, ClusterUUID: clusterID,
		VMName: "csv-hist-vm", Namespace: "csv-ns",
		Term: "short_term", Engine: "cost",
		RecommendedVCPU: 2, RecommendedMemoryGiB: 8,
		Confidence: "high", LastRecommendedAt: now, CreatedAt: now, UpdatedAt: now,
	}
	require.NoError(t, vm.PersistVMRecommendations(context.Background(), pool, []model.VMRecommendation{rec}, nil))

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/vms/:vm_name/history", api.GetVMRecommendationHistory)

	req := httptest.NewRequest(http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/vms/csv-hist-vm/history?cluster_uuid="+testutil.TestClusterUUID+"&namespace=csv-ns&term=short_term&engine=cost&format=csv",
		nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	recorder := httptest.NewRecorder()
	app.ServeHTTP(recorder, req)
	require.Equal(t, http.StatusOK, recorder.Code)
	assert.Contains(t, recorder.Header().Get("Content-Type"), "text/csv")

	records, err := csv.NewReader(strings.NewReader(recorder.Body.String())).ReadAll()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(records), 2)
	assert.Equal(t, "vm_name", records[0][2])
	assert.Equal(t, "csv-hist-vm", records[1][2])
}
