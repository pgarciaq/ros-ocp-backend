package api_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	ros_middleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	"github.com/redhatinsights/ros-ocp-backend/internal/clustercache"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// #947: a substring cluster filter matching SEVERAL clusters must return
// every matching cluster's rows on ALL surfaces — union semantics, matching
// koku's alias contract and the main lists. Pre-#947 the GPU time-slicing and
// MIG surfaces normalized the alias to its FIRST-matched cluster UUID (the
// #798 entry fix for the 503), silently returning one cluster's rows where
// the nodes tab returns all.
func TestClusterFilter_SubstringAliasIsUnionOnAllSurfaces(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := "org-substralias-union"

	// Two clusters sharing an alias prefix; the substring 'team-a' matches both.
	clusterA := "aaaa0000-0000-4000-8000-00000000a001"
	clusterB := "aaaa0000-0000-4000-8000-00000000a002"
	ensureUnionAccountCluster(t, pool, orgID, clusterA, "team-a-frontend")
	ensureUnionAccountCluster(t, pool, orgID, clusterB, "team-a-backend")

	// Node recs on both clusters (the main-list control surface).
	for _, c := range []string{clusterA, clusterB} {
		_, err := pool.Exec(ctx, `
			INSERT INTO node_recommendations (
				org_id, cluster_uuid, node, term, engine, notification_codes, estimated_savings_cents, updated_at
			) VALUES ($1, $2, 'worker-1', 'medium', 'cost', '{}', 1000, NOW())
			ON CONFLICT DO NOTHING`, orgID, c)
		require.NoError(t, err)
	}

	// Persisted GPU TS recs on both clusters.
	for _, c := range []string{clusterA, clusterB} {
		_, err := pool.Exec(ctx, `
			INSERT INTO node_gpu_timeslicing_recommendations (
				org_id, cluster_uuid, node_name, gpu_model, term,
				recommended_replicas, confidence, confidence_level,
				candidate_count, impacted_count,
				candidate_containers, impacted_containers,
				notification_codes
			) VALUES ($1, $2, 'gpu-node', 'NVIDIA T4', 'medium',
				4, 0.75, 0.75, 1, 0,
				'[{"namespace":"ns","workload":"w","container":"c","sm_active_avg":0.12,"classification":"underutilized"}]'::jsonb, '[]'::jsonb, '{}')
			ON CONFLICT (org_id, cluster_uuid, node_name, gpu_model, term) DO NOTHING`, orgID, c)
		require.NoError(t, err)
	}

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/nodes", api.GetNodeUtilizationRecs)
	v1.GET("/recommendations/openshift/gpu/timeslicing", api.GetNodeRecommendations)
	v1.GET("/recommendations/openshift/gpu", api.GetGPUMIGRecommendations)

	id := unionIdentity(orgID)
	getClusters := func(path string) map[string]int {
		req := httptest.NewRequest(http.MethodGet,
			"/api/cost-management/v1"+path+"?filter[cluster]=team-a", nil)
		req.Header.Set("X-Rh-Identity", id)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "%s: body: %s", path, rec.Body.String())
		var parsed struct {
			Data []map[string]interface{} `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &parsed), "%s: body: %s", path, rec.Body.String())
		out := map[string]int{}
		for _, row := range parsed.Data {
			if c, ok := row["cluster_uuid"].(string); ok {
				out[c]++
			}
		}
		return out
	}

	t.Run("nodes list (the control — already union)", func(t *testing.T) {
		clusters := getClusters("/recommendations/openshift/nodes")
		assert.Equal(t, map[string]int{clusterA: 1, clusterB: 1}, clusters,
			"the main list returns every matching cluster's rows")
	})

	t.Run("gpu timeslicing list (the #947 fix target)", func(t *testing.T) {
		clusters := getClusters("/recommendations/openshift/gpu/timeslicing")
		assert.Equal(t, map[string]int{clusterA: 1, clusterB: 1}, clusters,
			"a multi-match substring alias must return every matching cluster's rows (#947)")
	})

	// MIG recs on both clusters (a different table from TS).
	for _, c := range []string{clusterA, clusterB} {
		_, err := pool.Exec(ctx, `INSERT INTO gpu_mig_recommendation_sets
			(org_id, cluster_uuid, namespace, workload, workload_type, container_name, node_name, gpu_model_name, term, recommended_gpu_profile)
			VALUES ($1, $2, 'ns', 'w', 'deployment', 'c', 'gpu-node', 'NVIDIA T4', 'short', '2g.10gb')
			ON CONFLICT DO NOTHING`, orgID, c)
		require.NoError(t, err)
	}

	t.Run("gpu mig list (the #947 fix target)", func(t *testing.T) {
		clusters := getClusters("/recommendations/openshift/gpu")
		// The MIG list groups by node/model; assert both clusters appear.
		assert.Contains(t, clusters, clusterA,
			"cluster A's rows must appear under a multi-match alias (#947)")
		assert.Contains(t, clusters, clusterB,
			"cluster B's rows must appear under a multi-match alias (#947)")
	})

	t.Run("unknown alias stays 200 + count 0", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet,
			"/api/cost-management/v1/recommendations/openshift/gpu/timeslicing?filter[cluster]=no-such-prefix", nil)
		req.Header.Set("X-Rh-Identity", id)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
		var parsed struct {
			Meta struct {
				Count int `json:"count"`
			} `json:"meta"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &parsed))
		assert.Equal(t, 0, parsed.Meta.Count,
			"the unknown-alias contract (#790) holds under the predicate path")
	})

	// #953: the two union surfaces scope ROWS with the multi-match resolver,
	// so a lookup failure must be distinguishable from "nothing matched" —
	// otherwise a database blip renders as a confident empty list. The org
	// cache is warmed first so getClustersForOrg still succeeds from cache and
	// the failure lands on the filter resolver, not the earlier clusters 503.
	t.Run("a failed alias lookup is 503, not an empty 200", func(t *testing.T) {
		_, err := clustercache.GetClustersForOrg(ctx, orgID)
		require.NoError(t, err)
		live := database.GetPool()
		require.NotNil(t, live)
		dead, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/db?connect_timeout=2")
		require.NoError(t, err)
		t.Cleanup(dead.Close)
		database.SetForceTestPool(dead)
		t.Cleanup(func() { database.SetForceTestPool(live) })

		for _, path := range []string{
			"/recommendations/openshift/gpu/timeslicing",
			"/recommendations/openshift/gpu",
		} {
			req := httptest.NewRequest(http.MethodGet,
				"/api/cost-management/v1"+path+"?filter[cluster]=team-a", nil)
			req.Header.Set("X-Rh-Identity", id)
			rec := httptest.NewRecorder()
			app.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusServiceUnavailable, rec.Code,
				"%s: a failed cluster-filter lookup must not render as an empty list (#953); body: %s",
				path, rec.Body.String())
		}
	})
}

func ensureUnionAccountCluster(t *testing.T, pool *pgxpool.Pool, orgID, clusterUUID, alias string) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx,
		`INSERT INTO rh_accounts (org_id) VALUES ($1) ON CONFLICT (org_id) DO NOTHING`, orgID)
	require.NoError(t, err)
	var tenantID int
	err = pool.QueryRow(ctx, `SELECT id FROM rh_accounts WHERE org_id = $1`, orgID).Scan(&tenantID)
	require.NoError(t, err)
	_, err = pool.Exec(ctx,
		`INSERT INTO clusters (tenant_id, cluster_uuid, cluster_alias, source_id, last_reported_at)
		 VALUES ($1, $2, $3, $4, now()) ON CONFLICT DO NOTHING`,
		tenantID, clusterUUID, alias, "src-"+alias)
	require.NoError(t, err)
}

func unionIdentity(orgID string) string {
	id := map[string]interface{}{
		"identity": map[string]interface{}{
			"org_id": orgID, "account_number": "test", "type": "User",
			"user": map[string]interface{}{"username": "t", "is_org_admin": true},
		},
		"entitlements": map[string]interface{}{
			"cost_management": map[string]interface{}{"is_entitled": true},
		},
	}
	b, _ := json.Marshal(id)
	return base64.StdEncoding.EncodeToString(b)
}
