package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func insertNativeNamespaceRec(t *testing.T, orgID, namespace string, stale bool) {
	t.Helper()
	ctx := context.Background()
	pool := database.GetPool()
	monEnd := time.Now().UTC().Add(-24 * time.Hour)
	monStart := monEnd.Add(-7 * 24 * time.Hour)
	_, err := pool.Exec(ctx, `
		DELETE FROM namespace_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2 AND namespace_name = $3
		  AND term = 'medium' AND engine = 'cost' AND schedule_type = 'all_hours'`,
		orgID, testutil.TestClusterUUID, namespace,
	)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO namespace_recommendation_sets (
			org_id, cluster_uuid, namespace_name, term, engine, schedule_type, stale,
			rec_cpu_request_millicores, rec_cpu_limit_millicores,
			rec_memory_request_kib, rec_memory_limit_kib,
			current_cpu_request_millicores, current_cpu_limit_millicores,
			current_memory_request_kib, current_memory_limit_kib,
			variation_cpu_request_pct, variation_cpu_limit_pct,
			variation_memory_request_pct, variation_memory_limit_pct,
			confidence_level, notification_codes, monitoring_start_time, monitoring_end_time, updated_at
		) VALUES (
			$1, $2, $3, 'medium', 'cost', 'all_hours', $4,
			4000, 8000, 8388608, 16777216,
			5000, 10000, 10485760, 20971520,
			-10, -10, -10, -10,
			0.9, '{}', $6, $5, NOW()
		)`,
		orgID, testutil.TestClusterUUID, namespace, stale, monEnd, monStart,
	)
	require.NoError(t, err)
}

func setupNamespaceListEcho() *echo.Echo {
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/namespaces", api.GetNamespaceRecommendationSetListWithFallback)
	return app
}

func initNamespaceTestGORM(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() { database.DB = nil })
}

func TestGetNamespaceRecommendations_StaleFilter_DefaultExcludesStale(t *testing.T) {
	orgID := "org-ns-stale-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	initNamespaceTestGORM(t, pool)
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(context.Background(), `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'ns-stale-cluster', 1, NOW()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	insertNativeNamespaceRec(t, orgID, "fresh-ns", false)
	insertNativeNamespaceRec(t, orgID, "stale-ns", true)

	app := setupNamespaceListEcho()
	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/namespaces?limit=50", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Data []model.NativeNamespaceResult `json:"data"`
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "fresh-ns", resp.Data[0].Project)
}

func TestGetNamespaceRecommendations_StaleFilter_Only(t *testing.T) {
	orgID := "org-ns-stale-only-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	initNamespaceTestGORM(t, pool)
	t.Cleanup(func() { database.Pool = nil })

	_, err := pool.Exec(context.Background(), `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(context.Background(), `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'ns-stale-cluster', 1, NOW()) ON CONFLICT DO NOTHING`, testutil.TestClusterUUID)
	require.NoError(t, err)

	insertNativeNamespaceRec(t, orgID, "fresh-ns-2", false)
	insertNativeNamespaceRec(t, orgID, "stale-ns-2", true)

	app := setupNamespaceListEcho()
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/cost-management/v1/recommendations/openshift/namespaces?filter[stale]=only&limit=50",
		nil,
	)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var resp struct {
		Data []model.NativeNamespaceResult `json:"data"`
		Meta struct {
			Count int `json:"count"`
		} `json:"meta"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 1, resp.Meta.Count)
	require.Len(t, resp.Data, 1)
	assert.Equal(t, "stale-ns-2", resp.Data[0].Project)
}
