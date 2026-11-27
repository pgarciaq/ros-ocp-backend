package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
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

// Batch E matrix (#957, #958, #959): one parameter must mean one thing on
// every surface that serves it. These tests pin the cross-surface contracts,
// not per-handler behaviour — a fix that makes one router agree while leaving
// another router on the old rule must fail here.

const filterMatrixBase = "/api/cost-management/v1/recommendations/openshift"

// filterMatrixEcho mounts every surface the three issues touch.
func filterMatrixEcho() *echo.Echo {
	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/nodes", api.GetNodeUtilizationRecs)
	v1.GET("/recommendations/openshift/nodes/:node", api.GetNodeUtilizationDetail)
	v1.GET("/recommendations/openshift/gpu/timeslicing", api.GetNodeRecommendations)
	v1.GET("/recommendations/openshift/gpu/timeslicing/history", api.GetNodeGPUTimeslicingRecommendationHistory)
	v1.GET("/recommendations/openshift/gpu/timeslicing/:node", api.GetNodeGPUTimeslicingDetail)
	v1.GET("/recommendations/openshift/machinesets", api.GetMachineSetRecommendations)
	v1.GET("/recommendations/openshift/vm", api.GetVMRecommendations)
	v1.GET("/recommendations/openshift/vm/detail", api.GetVMRecommendationDetail)
	v1.GET("/recommendations/openshift/vms/:vm_name/history", api.GetVMRecommendationHistory)
	v1.GET("/recommendations/openshift/fleet-heatmap", api.GetFleetHeatmap)
	v1.GET("/recommendations/openshift/savings-summary", api.GetFleetSavingsSummary)
	v1.GET("/recommendations/openshift/namespaces/:recommendation-id/history", api.GetNamespaceRecommendationHistoryWithFallback)
	v1.GET("/recommendations/openshift/snapshots/summary", api.GetSnapshotSummary)
	return app
}

type filterMatrixFixture struct {
	orgID     string
	clusterID string
}

// seedFilterMatrixFixture creates one org with deliberately adversarial
// values: a mixed-case node name, a literal underscore that must not act as an
// ILIKE wildcard, and two GPU models that share a substring.
func seedFilterMatrixFixture(t *testing.T) filterMatrixFixture {
	t.Helper()

	orgID := "org-filter-matrix-" + uuid.New().String()[:8]
	pool := testutil.SetupTestDB(t)
	database.Pool = pool
	database.DB = testutil.OpenTestGORM(pool)
	t.Cleanup(func() {
		database.Pool = nil
		database.DB = nil
	})

	config.ResetForTest()
	t.Setenv("ROS_ENABLED_PLUGINS", "")
	t.Setenv("ROS_DISABLED_PLUGINS", "")
	_ = config.GetConfig()

	ctx := context.Background()
	_, err := pool.Exec(ctx, `INSERT INTO rh_accounts (id, org_id) VALUES (1, $1) ON CONFLICT DO NOTHING`, orgID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx, `
		INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		VALUES (1, $1, 'matrix-cluster', 'src-matrix', now())`, testutil.TestClusterUUID)
	require.NoError(t, err)

	// Nodes: one mixed-case name (the #958 mechanism), one bare-lowercase
	// control, and one row per term so the term matrix has a stable count.
	// machineset_name carries a literal underscore (ms_infra) next to a
	// look-alike that differs by one character (msXinfra), so `*_` can only
	// match the first if `_` matches literally (#957).
	_, err = pool.Exec(ctx, `
		INSERT INTO node_recommendations (
			org_id, cluster_uuid, node, term, engine,
			cpu_util_p50, cpu_util_p95, mem_util_p50, mem_util_p95,
			cpu_overcommit_ratio, category, idle_state,
			stranded_resource, pod_count, trend_slope, notification_codes,
			machineset_name, instance_type, node_count_reduction,
			estimated_savings_cents, updated_at
		) VALUES
			($1, $2::uuid, 'node-ros-A', 'short',  'cost', 0.2, 0.2, 0.2, 0.2, 1.0, 'underutilized', 'active', NULL, 5, 0, '{}', 'ms_infra',  'm5.large',  1, 5000, now()),
			($1, $2::uuid, 'node-ros-b', 'medium', 'cost', 0.5, 0.5, 0.4, 0.4, 1.0, 'optimized',     'active', NULL, 5, 0, '{}', 'msXinfra', 'm5.large',  0, 0,    now()),
			($1, $2::uuid, 'node-ros-c', 'long',   'cost', 0.7, 0.7, 0.6, 0.6, 1.0, 'optimized',     'active', NULL, 5, 0, '{}', 'ms_infra',  'm5.xlarge', 0, 0,    now()),
			($1, $2::uuid, 'node-ros-d', 'medium', 'cost', 0.4, 0.4, 0.3, 0.3, 1.0, 'optimized',     'active', NULL, 5, 0, '{}', 'ms_infra',  'm5.xlarge', 0, 0,    now())`,
		orgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	// GPU time-slicing: 'Tesla T4' and 'NVIDIA T4' share the substring 'T4',
	// so substring matching and exact matching give different counts.
	_, err = pool.Exec(ctx, `
		INSERT INTO node_gpu_timeslicing_recommendations (
			org_id, cluster_uuid, node_name, gpu_model, term,
			recommended_replicas, confidence, confidence_level,
			candidate_count, impacted_count,
			candidate_containers, impacted_containers,
			notification_codes, estimated_savings_cents, savings_per_gpu_cents
		) VALUES
			($1, $2::uuid, 'node-ros-A', 'Tesla T4',  'short',  4, 0.7, 0.7, 1, 0, '[]'::jsonb, '[]'::jsonb, '{36}', 40000, 10000),
			($1, $2::uuid, 'node-ros-A', 'Tesla T4',  'medium', 4, 0.7, 0.7, 1, 0, '[]'::jsonb, '[]'::jsonb, '{36}', 40000, 10000),
			($1, $2::uuid, 'node-ros-A', 'Tesla T4',  'long',   4, 0.7, 0.7, 1, 0, '[]'::jsonb, '[]'::jsonb, '{36}', 40000, 10000),
			($1, $2::uuid, 'node-ros-b', 'NVIDIA T4', 'medium', 2, 0.6, 0.6, 1, 0, '[]'::jsonb, '[]'::jsonb, '{36}', 20000, 10000)`,
		orgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	// History: same model family so the substring leg has rows to match.
	_, err = pool.Exec(ctx, `
		INSERT INTO node_gpu_timeslicing_recommendation_history (
			org_id, cluster_uuid, node_name, gpu_model, term,
			recommended_replicas, confidence, candidate_count, impacted_count,
			estimated_savings_cents
		) VALUES ($1, $2::uuid, 'node-ros-A', 'Tesla T4', 'medium', 4, 0.75, 2, 1, 40000)`,
		orgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	// VMs: the storage spelling is `_term` (#959); guest_os carries a space
	// so substring matching is observable.
	now := time.Now().UTC()
	base := model.VMRecommendation{
		OrgID:                orgID,
		ClusterUUID:          uuid.MustParse(testutil.TestClusterUUID),
		Namespace:            "test-ns",
		GuestOS:              "RHEL 9.2",
		CurrentVCPU:          4,
		CurrentMemoryGiB:     8,
		RecommendedVCPU:      2,
		RecommendedMemoryGiB: 4,
		Confidence:           "high",
		Engine:               "cost",
		Category:             model.VMCategoryIdle,
		Notifications:        []byte(`[{"code":18,"type":"warning","message":"idle"}]`),
		LastRecommendedAt:    now,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	var recs []model.VMRecommendation
	for i, term := range []string{"short_term", "medium_term", "long_term"} {
		rec := base
		rec.VMName = "vm-" + term
		rec.Term = term
		if i == 2 {
			rec.GuestOS = "Ubuntu 22.04"
		}
		recs = append(recs, rec)
	}
	require.NoError(t, vm.PersistVMRecommendations(ctx, pool, recs, nil))

	// Snapshots: one namespace with a literal underscore, one look-alike
	// without it, so `*_` is distinguishable from `*`.
	_, err = pool.Exec(ctx, `
		INSERT INTO snapshot_recommendation_sets (
			org_id, cluster_uuid, namespace, snapshot_name,
			recommendation_type, age_days, restore_size_bytes,
			estimated_cost_cents, creation_timestamp, updated_at
		) VALUES
			($1, $2::uuid, 'apps_prod', 'snap-1', 'orphaned', 30, 1073741824, 1000, now() - interval '30 days', now()),
			($1, $2::uuid, 'appsXprod', 'snap-2', 'orphaned', 30, 1073741824, 1000, now() - interval '30 days', now())`,
		orgID, testutil.TestClusterUUID)
	require.NoError(t, err)

	return filterMatrixFixture{orgID: orgID, clusterID: testutil.TestClusterUUID}
}

// getJSON performs an authenticated request against the matrix app and returns
// the status code and decoded body.
func getJSON(t *testing.T, app *echo.Echo, orgID, path string, params url.Values) (int, map[string]any) {
	t.Helper()
	target := path
	if len(params) > 0 {
		target += "?" + params.Encode()
	}
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(orgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	var body map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
	}
	return rec.Code, body
}

func metaCount(body map[string]any) int {
	meta, _ := body["meta"].(map[string]any)
	n, _ := meta["count"].(float64)
	return int(n)
}

func metaMessage(body map[string]any) string {
	msg, _ := body["message"].(string)
	return msg
}

// TestFilterNodeCase_OneRuleEverywhere pins #958: filter[node] and {node} are
// exact, case-sensitive identifiers on every surface.
func TestFilterNodeCase_OneRuleEverywhere(t *testing.T) {
	fx := seedFilterMatrixFixture(t)
	app := filterMatrixEcho()

	t.Run("nodes list", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, filterMatrixBase+"/nodes", url.Values{"filter[node]": {"node-ros-A"}})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 1, metaCount(body), "the stored spelling must match")

		code, body = getJSON(t, app, fx.orgID, filterMatrixBase+"/nodes", url.Values{"filter[node]": {"node-ros-a"}})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 0, metaCount(body), "a case variant must not match: %s", metaMessage(body))
	})

	t.Run("nodes detail", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, filterMatrixBase+"/nodes/node-ros-A", nil)
		require.Equal(t, http.StatusOK, code, metaMessage(body))

		code, body = getJSON(t, app, fx.orgID, filterMatrixBase+"/nodes/node-ros-a", nil)
		require.Equal(t, http.StatusNotFound, code, "a case variant must 404 like the list returns empty; body: %v", body)
	})

	t.Run("gpu timeslicing list", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, filterMatrixBase+"/gpu/timeslicing", url.Values{"filter[node]": {"node-ros-A"}})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 3, metaCount(body), "the stored spelling must match all three terms")

		code, body = getJSON(t, app, fx.orgID, filterMatrixBase+"/gpu/timeslicing", url.Values{"filter[node]": {"node-ros-a"}})
		require.Equal(t, http.StatusOK, code, metaMessage(body))
		assert.Equal(t, 0, metaCount(body), "the GPU list must follow the same rule as /nodes")
	})

	t.Run("gpu timeslicing detail", func(t *testing.T) {
		code, body := getJSON(t, app, fx.orgID, filterMatrixBase+"/gpu/timeslicing/node-ros-A", nil)
		require.Equal(t, http.StatusOK, code, metaMessage(body))

		code, _ = getJSON(t, app, fx.orgID, filterMatrixBase+"/gpu/timeslicing/node-ros-a", nil)
		require.Equal(t, http.StatusNotFound, code, "a case variant must 404 on the detail route too")
	})
}
