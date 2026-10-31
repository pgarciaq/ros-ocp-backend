package api_test

import (
	"context"
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

func TestGetHCPAdvisories_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	mgmtA := "11111111-1111-1111-1111-111111111111"
	mgmtB := "22222222-2222-2222-2222-222222222222"
	cRatio := float32(0.92)

	seed := func(org, hc, mgmt, verdict, confidence string, p99, thr float32, cr *float32, signals []string, expired bool) {
		expiry := "now() + interval '24 hours'"
		if expired {
			expiry = "now() - interval '24 hours'"
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO hcp_correlation_advisories (
				org_id, hc_cluster_id, management_cluster_uuid, window_start, window_end,
				verdict, confidence, h_p99_s, h_threshold_s, c_ratio, n_signals, expires_at
			) VALUES ($1, $2, $3::uuid, '2026-10-16T00:00:00Z', '2026-10-30T00:00:00Z', $4, $5, $6, $7, $8, $9, `+expiry+`)
			ON CONFLICT (org_id, hc_cluster_id, window_start) DO UPDATE SET
				verdict = EXCLUDED.verdict, confidence = EXCLUDED.confidence,
				h_p99_s = EXCLUDED.h_p99_s, h_threshold_s = EXCLUDED.h_threshold_s,
				c_ratio = EXCLUDED.c_ratio, n_signals = EXCLUDED.n_signals, expires_at = EXCLUDED.expires_at`,
			org, hc, mgmt, verdict, confidence, p99, thr, cr, signals)
		require.NoError(t, err)
	}

	// Zombie (high confidence, full evidence) — distinct values so a mis-mapped
	// field cannot pass by coincidence.
	seed(testutil.TestOrgID, "hc-alpha", mgmtA, "review_unused_hosted_cluster", "high",
		1500, 10, nil,
		[]string{"hc:hc-alpha", "peak_user_mc:1500", "idle_floor_mc:10", "cp_req_mc:3980", "api_max_daily:13870175 (context, no gate)"}, false)
	// Zombie (medium confidence, api context unknown — api_max_daily_requests omitted).
	seed(testutil.TestOrgID, "hc-beta", mgmtA, "review_unused_hosted_cluster", "medium",
		900, 10, nil,
		[]string{"hc:hc-beta", "peak_user_mc:900", "idle_floor_mc:10", "cp_req_mc:0", "api_max_daily:unknown (context, no gate)"}, false)
	// Headroom (W2 columns carry their own semantics here).
	seed(testutil.TestOrgID, "hc-gamma", mgmtB, "do_not_add_workers_first", "high",
		2.4, 1.8, &cRatio, []string{"hc:hc-gamma"}, false)
	// Api-tax (management-cluster-scoped: empty hc; c_ratio carries the
	// rejected rate per writeAPITaxAdvisory).
	rejectedRate := float32(0.31)
	seed(testutil.TestOrgID, "", mgmtA, "tune_noisy_webhook", "high",
		4.1, 3.0, &rejectedRate, []string{"webhook:/mutate", "p99:4.100", "rejected_rate:0.3100"}, false)
	// Sleep candidate (W8 #716): h_p99_s carries the BH-activity peak,
	// h_threshold_s the outside-BH peak; the rest parses from signals.
	seed(testutil.TestOrgID, "hc-delta", mgmtB, "schedule_nonprod_sleep", "high",
		300, 0, nil,
		[]string{"hc:hc-delta", "declared_tier:non-prod", "bh_active_peak_mc:300", "outside_bh_peak_mc:0", "bh_schedule:monday..friday 09:00-17:00 UTC", "worker_nodes:2", "reclaim_node_hours_per_week:160", "action:nodepool-scale-to-zero + pausedUntil (runbook); CP keeps running; wake ~= 20 min reprovision"}, false)
	// Other org — must be invisible.
	seed("9999999", "hc-other-org", mgmtA, "review_unused_hosted_cluster", "high",
		1, 1, nil, []string{"hc:hc-other-org"}, false)
	// Expired — must be excluded.
	seed(testutil.TestOrgID, "hc-expired", mgmtA, "review_unused_hosted_cluster", "high",
		1, 1, nil, []string{"hc:hc-expired"}, true)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/hcp/advisories", api.GetHCPAdvisories)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/hcp/advisories", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())

	var resp api.HCPAdvisoriesResponse
	err := json.Unmarshal(rec.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, 5, resp.Meta.Count)
	require.Len(t, resp.Data, 5)

	byVerdict := map[string][]api.HCPAdvisory{}
	for _, row := range resp.Data {
		byVerdict[row.Verdict] = append(byVerdict[row.Verdict], row)
	}
	require.Len(t, byVerdict["review_unused_hosted_cluster"], 2)
	require.Len(t, byVerdict["do_not_add_workers_first"], 1)
	require.Len(t, byVerdict["tune_noisy_webhook"], 1)
	require.Len(t, byVerdict["schedule_nonprod_sleep"], 1)

	// Zombie, high confidence: every typed field mapped with distinct values.
	var zAlpha api.HCPAdvisory
	for _, r := range byVerdict["review_unused_hosted_cluster"] {
		if r.Confidence == "high" {
			zAlpha = r
		}
	}
	require.NotNil(t, zAlpha.HostedClusterID)
	assert.Equal(t, "hc-alpha", *zAlpha.HostedClusterID)
	assert.Equal(t, mgmtA, zAlpha.ManagementClusterUUID)
	assert.Equal(t, float64(1500), zAlpha.Details["peak_user_cpu_mc"])
	assert.Equal(t, float64(10), zAlpha.Details["idle_floor_cpu_mc"])
	assert.Equal(t, float64(3980), zAlpha.Details["control_plane_cpu_request_mc"])
	assert.Equal(t, float64(13870175), zAlpha.Details["api_max_daily_requests"])
	assert.NotEmpty(t, zAlpha.Evidence, "evidence rides alongside details")

	// Zombie, medium confidence: unknown api context omits api_max_daily_requests.
	var zBeta api.HCPAdvisory
	for _, r := range byVerdict["review_unused_hosted_cluster"] {
		if r.Confidence == "medium" {
			zBeta = r
		}
	}
	_, hasAPI := zBeta.Details["api_max_daily_requests"]
	assert.False(t, hasAPI, "unknown api_max_daily must be omitted, not zero")

	// Headroom: the W2 columns keep their own semantics.
	h := byVerdict["do_not_add_workers_first"][0]
	assert.InDelta(t, 2.4, h.Details["hosted_api_p99_seconds"], 0.001)
	assert.InDelta(t, 1.8, h.Details["hosted_api_threshold_seconds"], 0.001)
	assert.InDelta(t, 0.92, h.Details["control_plane_capacity_ratio"], 0.001)

	// Sleep candidate: typed fields per the ADR-0349 field table.
	sl := byVerdict["schedule_nonprod_sleep"][0]
	require.NotNil(t, sl.HostedClusterID)
	assert.Equal(t, "hc-delta", *sl.HostedClusterID)
	assert.Equal(t, float64(300), sl.Details["bh_active_peak_cpu_mc"])
	assert.Equal(t, float64(0), sl.Details["outside_bh_peak_cpu_mc"])
	assert.Equal(t, "non-prod", sl.Details["declared_tier"])
	assert.Equal(t, float64(2), sl.Details["worker_node_count"])
	assert.Equal(t, float64(160), sl.Details["reclaim_node_hours_per_week"])

	// Api-tax: cluster-scoped row has null hosted_cluster_id.
	w := byVerdict["tune_noisy_webhook"][0]
	assert.Nil(t, w.HostedClusterID)
	assert.Equal(t, "/mutate", w.Details["webhook"])
	assert.InDelta(t, 4.1, w.Details["webhook_p99_seconds"], 0.001)
	assert.InDelta(t, 3.0, w.Details["webhook_p99_threshold_seconds"], 0.001)
	assert.InDelta(t, 0.31, w.Details["webhook_rejected_rate"], 0.001)
}

func TestGetHCPAdvisories_EmptyState_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/hcp/advisories", api.GetHCPAdvisories)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/hcp/advisories", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader("9999888"))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp api.HCPAdvisoriesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Meta.Count)
	assert.Empty(t, resp.Data)
}

// TestGetHCPAdvisories_UpsertInvariant_Integration pins ADR-0349 §3: the
// table admits one advisory per (org, HC, window); a second verdict written
// under the same key replaces the first (last writer wins).
func TestGetHCPAdvisories_UpsertInvariant_Integration(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	database.DB = testutil.OpenTestGORM(pool)
	database.Pool = pool
	t.Cleanup(func() { database.DB = nil; database.Pool = nil })

	upsert := func(verdict string, p99 float32) {
		_, err := pool.Exec(ctx, `
			INSERT INTO hcp_correlation_advisories (
				org_id, hc_cluster_id, management_cluster_uuid, window_start, window_end,
				verdict, confidence, h_p99_s, h_threshold_s, c_ratio, n_signals, expires_at
			) VALUES ($1, 'hc-invariant', '33333333-3333-3333-3333-333333333333'::uuid, '2026-10-16T00:00:00Z', '2026-10-30T00:00:00Z',
				$2, 'high', $3, 1, NULL, '{}', now() + interval '24 hours')
			ON CONFLICT (org_id, hc_cluster_id, window_start) DO UPDATE SET
				verdict = EXCLUDED.verdict, h_p99_s = EXCLUDED.h_p99_s`,
			testutil.TestOrgID, verdict, p99)
		require.NoError(t, err)
	}
	upsert("do_not_add_workers_first", 2.4)
	upsert("review_unused_hosted_cluster", 1500)

	app := echo.New()
	v1 := app.Group("/api/cost-management/v1")
	v1.Use(ros_middleware.Identity)
	v1.GET("/recommendations/openshift/hcp/advisories", api.GetHCPAdvisories)

	req := httptest.NewRequest(http.MethodGet, "/api/cost-management/v1/recommendations/openshift/hcp/advisories", nil)
	req.Header.Set("X-Rh-Identity", makeIdentityHeader(testutil.TestOrgID))
	rec := httptest.NewRecorder()
	app.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code, "body: %s", rec.Body.String())
	var resp api.HCPAdvisoriesResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	var forHC []api.HCPAdvisory
	for _, r := range resp.Data {
		if r.HostedClusterID != nil && *r.HostedClusterID == "hc-invariant" {
			forHC = append(forHC, r)
		}
	}
	require.Len(t, forHC, 1, "one advisory per (org, HC, window) — ADR-0349 §3")
	assert.Equal(t, "review_unused_hosted_cluster", forHC[0].Verdict, "last writer wins")
}
