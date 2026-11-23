package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// #915 Wave B: the storage tabs' classification filter. The koku-ui PVC and
// snapshot toolbars send filter[classification] with the entity's
// recommendation_type values as the options; the backend silently ignored
// the param (#841's mechanism). It now filters recommendation_type
// (multi-value) and rejects out-of-vocabulary values with 400.

func newStorageFilterApp(t *testing.T, register func(v1 *echo.Group)) *echo.Echo {
	t.Helper()
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	register(v1)
	return app
}

func doStorageList(t *testing.T, app *echo.Echo, orgID, path, query string) (int, []byte) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1"+path+"?"+query, nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func TestGetPVCRecommendations_FilterClassification(t *testing.T) {
	orgID := "org-pvc-class-" + uuid.NewString()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })

	seedPVCRecCluster(t, orgID)
	insertPVCRecommendation(t, orgID, "apps", "pvc-ov", "gp3-csi", "oversized", "", "", 0.05)
	insertPVCRecommendation(t, orgID, "apps", "pvc-hl", "gp3-csi", "healthy", "", "", 0.50)
	insertPVCRecommendation(t, orgID, "apps", "pvc-nf", "gp3-csi", "near_full", "", "", 0.90)

	app := newStorageFilterApp(t, func(v1 *echo.Group) {
		v1.GET("/recommendations/openshift/pvcs", api.GetPVCRecommendations)
	})

	t.Run("single value narrows", func(t *testing.T) {
		code, body := doStorageList(t, app, orgID, "/recommendations/openshift/pvcs", "limit=20&filter[classification]=oversized")
		require.Equal(t, http.StatusOK, code, string(body))
		var resp struct {
			Data []struct {
				PVC string `json:"persistentvolumeclaim"`
			} `json:"data"`
			Meta struct {
				Count int `json:"count"`
			} `json:"meta"`
		}
		require.NoError(t, json.Unmarshal(body, &resp))
		assert.Equal(t, 1, resp.Meta.Count, "filter[classification]=oversized must narrow to the oversized row")
		require.Len(t, resp.Data, 1)
		assert.Equal(t, "pvc-ov", resp.Data[0].PVC)
	})

	t.Run("multi value narrows", func(t *testing.T) {
		code, body := doStorageList(t, app, orgID, "/recommendations/openshift/pvcs", "limit=20&filter[classification]=oversized,healthy")
		require.Equal(t, http.StatusOK, code, string(body))
		var resp struct {
			Meta struct {
				Count int `json:"count"`
			} `json:"meta"`
		}
		require.NoError(t, json.Unmarshal(body, &resp))
		assert.Equal(t, 2, resp.Meta.Count)
	})

	t.Run("invalid value rejected", func(t *testing.T) {
		code, body := doStorageList(t, app, orgID, "/recommendations/openshift/pvcs", "limit=20&filter[classification]=bogus")
		require.Equal(t, http.StatusBadRequest, code,
			"filter[classification]=bogus must 400, got %d: %s (#915 Wave B)", code, string(body))
		assert.Contains(t, string(body), "invalid filter[classification] value")
	})
}

func TestGetSnapshotRecommendations_FilterClassification(t *testing.T) {
	orgID := "org-snap-class-" + uuid.NewString()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	t.Cleanup(func() { database.Pool = nil })
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'snap-class', 1, NOW()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)
	for _, s := range []struct{ name, typ string }{
		{"snap-stale", "stale"},
		{"snap-active", "active"},
	} {
		_, err = pool.Exec(ctx, `INSERT INTO snapshot_recommendation_sets
			(org_id, cluster_uuid, namespace, snapshot_name, recommendation_type, estimated_cost_cents, age_days, creation_timestamp)
			VALUES ($1, $2, 'apps', $3, $4, 500, 10, now())`,
			orgID, testutil.TestClusterUUID, s.name, s.typ)
		require.NoError(t, err)
	}

	app := newStorageFilterApp(t, func(v1 *echo.Group) {
		v1.GET("/recommendations/openshift/snapshots", api.GetSnapshotRecommendations)
	})

	t.Run("single value narrows", func(t *testing.T) {
		code, body := doStorageList(t, app, orgID, "/recommendations/openshift/snapshots", "limit=20&filter[classification]=stale")
		require.Equal(t, http.StatusOK, code, string(body))
		var resp struct {
			Data []struct {
				Name string `json:"snapshot_name"`
			} `json:"data"`
			Meta struct {
				Count int `json:"count"`
			} `json:"meta"`
		}
		require.NoError(t, json.Unmarshal(body, &resp))
		assert.Equal(t, 1, resp.Meta.Count, "filter[classification]=stale must narrow to the stale row")
		require.Len(t, resp.Data, 1)
		assert.Equal(t, "snap-stale", resp.Data[0].Name)
	})

	t.Run("invalid value rejected", func(t *testing.T) {
		code, body := doStorageList(t, app, orgID, "/recommendations/openshift/snapshots", "limit=20&filter[classification]=bogus")
		require.Equal(t, http.StatusBadRequest, code,
			"filter[classification]=bogus must 400, got %d: %s (#915 Wave B)", code, string(body))
		assert.Contains(t, string(body), "invalid filter[classification] value")
	})
}
