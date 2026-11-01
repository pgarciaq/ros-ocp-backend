package api_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/api"
	rosmiddleware "github.com/redhatinsights/ros-ocp-backend/internal/api/middleware"
	"github.com/redhatinsights/ros-ocp-backend/internal/asyncjobs"
	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetheatmap"
	"github.com/redhatinsights/ros-ocp-backend/internal/fleetsummary"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/internal/utils"
)

const settingsRBACRoot = "/api/cost-management/v1/recommendations/openshift/settings/"
const settingsRBACCluster = "11111111-1111-4111-8111-111111111111"

type settingsRBACMutationCase struct {
	name, path, table, seed, change string
	putRecalc, deleteRecalc, bh     bool
}

func settingsRBACMutationCases() []settingsRBACMutationCase {
	var cases []settingsRBACMutationCase
	for _, kind := range []string{"container", "namespace", "node", "gpu", "pvc"} {
		seed, change := `{"min_margin":1.2,"max_margin":1.4}`, `{"min_margin":1.3,"max_margin":1.5}`
		switch kind {
		case "node":
			seed, change = `{"underutil_threshold":0.4}`, `{"underutil_threshold":0.5}`
		case "gpu":
			seed, change = `{"idle_threshold":0.05}`, `{"idle_threshold":0.06}`
		case "pvc":
			seed, change = `{"min_trend_days":4}`, `{"min_trend_days":5}`
		}
		for _, path := range []string{kind, "thresholds?recommendation_type=" + kind} {
			cases = append(cases, settingsRBACMutationCase{
				name: path, path: settingsRBACRoot + path, table: "recommendation_thresholds",
				seed: seed, change: change, putRecalc: true,
			})
		}
	}
	for _, tp := range plugin.ByTrait[plugin.TermProvider]() {
		cases = append(cases, settingsRBACMutationCase{
			name: "terms/" + tp.Name(), path: settingsRBACRoot + "terms?recommendation_type=" + tp.Name(), table: "org_recommendation_terms",
			seed:   `{"terms":[{"name":"short","window_days":2,"min_data_days":1}]}`,
			change: `{"terms":[{"name":"short","window_days":3,"min_data_days":1}]}`,
		})
	}
	hcpSeed := `{"h_p99_threshold_s":0.5,"h_baseline_multiple":4,"c_cpu_pct":85,"c_etcd_p99_s":0.02,"window_h":2,"skew_m":10,"freshness_h":4,"expiry_h":48,"z_idle_req_per_day":50,"z_idle_cpu_floor_mc":25,"z_short_window_days":5,"z_short_required_days":3,"z_idle_window_days":14,"sleep_window_days":10,"sleep_bh_activity_floor_mc":20,"sleep_outside_bh_idle_floor_mc":5}`
	cases = append(cases,
		settingsRBACMutationCase{name: "vm", path: settingsRBACRoot + "vm", table: "recommendation_thresholds", seed: `{"disk":{"projection_window_days":21}}`, change: `{"disk":{"projection_window_days":22}}`},
		settingsRBACMutationCase{name: "vm/terms", path: settingsRBACRoot + "vm/terms", table: "org_recommendation_terms", seed: `{"terms":[{"name":"short_term","window_days":2,"min_data_days":1}]}`, change: `{"terms":[{"name":"short_term","window_days":3,"min_data_days":1}]}`},
		settingsRBACMutationCase{name: "snapshot", path: settingsRBACRoot + "snapshot", table: "snapshot_settings", seed: `{"stale_days":120}`, change: `{"stale_days":121}`, putRecalc: true},
		settingsRBACMutationCase{name: "idle-detection", path: settingsRBACRoot + "idle-detection", table: "recommendation_thresholds", seed: `{"idle_detection":{"thresholds":{"cpu_utilization_percent":5}}}`, change: `{"idle_detection":{"thresholds":{"cpu_utilization_percent":7}}}`, putRecalc: true, deleteRecalc: true},
		settingsRBACMutationCase{name: "hcp-correlation", path: settingsRBACRoot + "hcp-correlation", table: "recommendation_thresholds", seed: hcpSeed, change: strings.Replace(hcpSeed, `"h_p99_threshold_s":0.5`, `"h_p99_threshold_s":0.6`, 1)},
	)
	for _, kind := range []string{"quota", "cluster-quota"} {
		cases = append(cases, settingsRBACMutationCase{name: kind, path: settingsRBACRoot + kind, table: "recommendation_thresholds",
			seed:   `{"headroom_percent":15,"high_risk_threshold_percent":90,"medium_risk_threshold_percent":70}`,
			change: `{"headroom_percent":20,"high_risk_threshold_percent":90,"medium_risk_threshold_percent":70}`, putRecalc: true})
	}
	bhSeed := `{"timezone":"UTC","schedule":{"days":["monday","tuesday","wednesday","thursday","friday"],"start_time":"09:00","end_time":"17:00"},"off_hours_weight":0,"enabled":true}`
	for _, suffix := range []string{"", "/clusters/" + settingsRBACCluster, "/clusters/" + settingsRBACCluster + "/namespaces/rbac-ns"} {
		cases = append(cases, settingsRBACMutationCase{name: "business-hours" + suffix, path: settingsRBACRoot + "business-hours" + suffix, table: "business_hours_schedules",
			seed: bhSeed, change: strings.Replace(bhSeed, `"enabled":true`, `"enabled":false`, 1), bh: true})
	}
	return cases
}

type settingsRBACTransport func(*http.Request) (*http.Response, error)

func (f settingsRBACTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type settingsRBACEffects struct{ recalc, reship atomic.Int64 }

func (e *settingsRBACEffects) TriggerReship(context.Context, string, uuid.UUID) error {
	e.reship.Add(1)
	return nil
}

func settingsRBACIdentity(orgID, username string) string {
	raw := fmt.Sprintf(`{"identity":{"org_id":%q,"type":"User","user":{"username":%q,"is_org_admin":false}},"entitlements":{"cost_management":{"is_entitled":true}}}`, orgID, username)
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

func settingsRBACApp(t *testing.T, effects *settingsRBACEffects) *echo.Echo {
	t.Helper()
	t.Cleanup(config.ResetForTest)
	for key, value := range map[string]string{
		"RBAC_ENABLE": "true", "ROS_RBAC_CACHE_TTL": "0", "ROS_SETTINGS_LOCKED": "false",
		"ROS_BUSINESS_HOURS_ENABLED": "true", "ROS_ENABLED_PLUGINS": "", "ROS_DISABLED_PLUGINS": "",
		"ROS_THRESHOLD_RECALCULATION_ENABLED": "true",
	} {
		t.Setenv(key, value)
	}
	config.ResetForTest()
	oldClient := utils.HTTPClient
	utils.HTTPClient = &http.Client{Transport: settingsRBACTransport(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/rbac/v1/access/" {
			return nil, fmt.Errorf("unexpected external request in settings RBAC test: %s %s", r.Method, r.URL.Path)
		}
		b, err := base64.StdEncoding.DecodeString(r.Header.Get("x-rh-identity"))
		if err != nil {
			return nil, err
		}
		var id struct {
			Identity struct{ User struct{ Username string } }
		}
		if err := json.Unmarshal(b, &id); err != nil {
			return nil, err
		}
		access := `{"permission":"cost-management:openshift.cluster:read","resourceDefinitions":[]}`
		if id.Identity.User.Username == "settings-writer" {
			access += `,{"permission":"cost-management:settings:write","resourceDefinitions":[]}`
		}
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":[` + access + `],"links":{"next":""}}`))}, nil
	})}
	t.Cleanup(func() { utils.HTTPClient = oldClient })
	engine.SetThresholdRecalcHookForTest(func(string, string) { effects.recalc.Add(1) })
	t.Cleanup(engine.ClearThresholdRecalcHookForTest)
	e := echo.New()
	v1 := e.Group("/api/cost-management/v1")
	v1.Use(rosmiddleware.Identity, rosmiddleware.CostManagementEntitlement, rosmiddleware.Rbac)
	api.RegisterV1RoutesForTest(v1, effects)
	return e
}

func settingsRBACRequest(e *echo.Echo, org, user, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("x-rh-identity", settingsRBACIdentity(org, user))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

func settingsRBACDrain(t *testing.T) {
	t.Helper()
	require.NoError(t, asyncjobs.WaitForTest(10*time.Second))
}

func settingsRBACState(t *testing.T, pool *pgxpool.Pool, orgID string) map[string]string {
	t.Helper()
	state := make(map[string]string)
	for _, table := range []string{"recommendation_thresholds", "org_recommendation_terms", "snapshot_settings", "business_hours_schedules", "daily_container_digests", "clusters", "cluster_threshold_recalc_state"} {
		var raw []byte
		err := pool.QueryRow(context.Background(), `SELECT COALESCE(jsonb_agg(to_jsonb(s) ORDER BY to_jsonb(s)::text), '[]'::jsonb) FROM `+pgx.Identifier{table}.Sanitize()+` s WHERE org_id=$1`, orgID).Scan(&raw)
		require.NoError(t, err, table)
		state[table] = string(raw)
	}
	return state
}

func settingsRBACPolicyRows(t *testing.T, raw string) []map[string]any {
	t.Helper()
	var rows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(raw), &rows))
	for _, row := range rows {
		// A timestamp-only update must not make the authorized policy-value
		// control pass. Negative cases still compare complete, unmodified rows.
		delete(row, "updated_at")
		delete(row, "reship_pending_since")
		delete(row, "reship_forward_only_since")
	}
	return rows
}

func seedSettingsRBACCluster(t *testing.T, pool *pgxpool.Pool, orgID string) {
	t.Helper()
	var tenant int64
	require.NoError(t, pool.QueryRow(context.Background(), `INSERT INTO rh_accounts(org_id) VALUES($1) RETURNING id`, orgID).Scan(&tenant))
	_, err := pool.Exec(context.Background(), `INSERT INTO clusters(tenant_id, cluster_uuid, cluster_alias, source_id) VALUES($1,$2,'rbac-cluster','rbac-source')`, tenant, settingsRBACCluster)
	require.NoError(t, err)
}

func TestSettingsRBAC_MutationsPreserveState(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	previous := database.Pool
	database.Pool = pool
	t.Cleanup(func() { database.Pool = previous })
	effects := &settingsRBACEffects{}
	e := settingsRBACApp(t, effects)
	cases := settingsRBACMutationCases()
	covered := make(map[string]bool)
	for _, tc := range cases {
		path := strings.SplitN(tc.path, "?", 2)[0]
		path = strings.ReplaceAll(path, settingsRBACCluster, "{cluster_id}")
		path = strings.ReplaceAll(path, "/namespaces/rbac-ns", "/namespaces/{namespace}")
		for _, method := range []string{http.MethodPut, http.MethodDelete} {
			covered[method+" "+strings.TrimPrefix(path, "/api/cost-management/v1")] = true
		}
	}
	for path, raw := range loadOpenAPISpec(t).Paths {
		if !strings.Contains(path, "/settings/") {
			continue
		}
		var item openAPIPathItem
		require.NoError(t, json.Unmarshal(raw, &item))
		if item.Put != nil {
			require.True(t, covered[http.MethodPut+" "+path], "missing state-preservation case for PUT %s", path)
		}
		if item.Delete != nil {
			require.True(t, covered[http.MethodDelete+" "+path], "missing state-preservation case for DELETE %s", path)
		}
	}
	for _, tc := range cases {
		for _, method := range []string{http.MethodPut, http.MethodDelete} {
			t.Run(tc.name+"/"+method, func(t *testing.T) {
				orgID := "org-settings-rbac-" + uuid.NewString()
				if tc.bh {
					seedSettingsRBACCluster(t, pool, orgID)
				}
				seed := settingsRBACRequest(e, orgID, "settings-writer", http.MethodPut, tc.path, tc.seed)
				require.True(t, seed.Code == http.StatusOK || seed.Code == http.StatusAccepted, "valid unlocked seed PUT: %d %s", seed.Code, seed.Body.String())
				settingsRBACDrain(t)
				if tc.bh {
					testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{OrgID: orgID, ClusterUUID: settingsRBACCluster,
						Namespace: "rbac-ns", Workload: "web", WorkloadType: "deployment", ContainerName: "main",
						BucketDate: time.Now().UTC().Truncate(24 * time.Hour), ScheduleType: "business_hours", SampleCount: 60, CPUUsageP50MC: 100})
				}
				before := settingsRBACState(t, pool, orgID)
				require.NotEqual(t, "[]", before[tc.table], "denied DELETE must target an existing override")
				read := settingsRBACRequest(e, orgID, "settings-reader", http.MethodGet, tc.path, "")
				require.Equal(t, http.StatusOK, read.Code, read.Body.String())
				effects.recalc.Store(0)
				effects.reship.Store(0)
				key := fleetheatmap.CacheKey(orgID, false, nil, "cpu", "medium", "cost", "")
				fleetheatmap.Put(key, "unchanged-settings-cache")
				fleetsummary.Put(orgID, false, nil, fleetsummary.CachedSummary{TotalContainers: 7})
				denied := settingsRBACRequest(e, orgID, "settings-reader", method, tc.path, tc.change)
				settingsRBACDrain(t)
				// Full persisted values (including BH digests/pending stamps) must not change.
				assert.Equal(t, before, settingsRBACState(t, pool, orgID), "permission-denied request mutated persisted settings or dependent state")
				assert.Zero(t, effects.recalc.Load(), "permission denial triggered recalculation")
				assert.Zero(t, effects.reship.Load(), "permission denial triggered reship")
				cached, ok := fleetheatmap.Get(key)
				assert.True(t, ok, "permission denial invalidated the heatmap cache")
				assert.Equal(t, "unchanged-settings-cache", cached)
				_, ok = fleetsummary.Get(orgID, false, nil)
				assert.True(t, ok, "permission denial invalidated the fleet cache")
				require.Equal(t, http.StatusForbidden, denied.Code)
				assert.JSONEq(t, `{"status":"error","message":"User does not have permission to modify settings"}`, denied.Body.String())
				if strings.Contains(tc.path, "thresholds?") {
					assert.Equal(t, "true", denied.Header().Get("Deprecation"))
				}

				// The exact attempted PUT must be valid; locks/validation cannot explain no mutation.
				control := settingsRBACRequest(e, orgID, "settings-writer", http.MethodPut, tc.path, tc.change)
				require.True(t, control.Code == http.StatusOK || control.Code == http.StatusAccepted, "positive PUT control: %d %s", control.Code, control.Body.String())
				settingsRBACDrain(t)
				assert.NotEqual(t, settingsRBACPolicyRows(t, before[tc.table]), settingsRBACPolicyRows(t, settingsRBACState(t, pool, orgID)[tc.table]), "writer PUT must change a real stored policy value, not only a timestamp")
				if tc.putRecalc {
					assert.Positive(t, effects.recalc.Load(), "positive writer control must exercise recalculation")
				}
				if tc.bh {
					assert.Positive(t, effects.reship.Load(), "positive writer control must exercise reship")
					assert.Equal(t, "[]", settingsRBACState(t, pool, orgID)["daily_container_digests"], "positive control must exercise BH pruning")
				}
				if method == http.MethodDelete {
					effects.recalc.Store(0)
					control = settingsRBACRequest(e, orgID, "settings-writer", http.MethodDelete, tc.path, "")
					require.True(t, control.Code == http.StatusOK || control.Code == http.StatusNoContent, "positive DELETE control: %d %s", control.Code, control.Body.String())
					settingsRBACDrain(t)
					remaining := settingsRBACState(t, pool, orgID)[tc.table]
					if tc.bh {
						// DELETE deliberately retains/recreates a non-inheriting pending-reship
						// marker; it must remove the real schedule, not that bookkeeping row.
						var rows []map[string]any
						require.NoError(t, json.Unmarshal([]byte(remaining), &rows))
						require.Len(t, rows, 1)
						assert.Equal(t, settingsRBACCluster, rows[0]["cluster_uuid"])
						assert.Equal(t, "", rows[0]["namespace"])
						assert.Equal(t, false, rows[0]["enabled"])
						assert.Equal(t, "UTC", rows[0]["timezone"])
						assert.Equal(t, []any{"monday"}, rows[0]["days"])
						assert.Equal(t, "00:00:00", rows[0]["start_time"])
						assert.Equal(t, "23:59:00", rows[0]["end_time"])
						assert.NotNil(t, rows[0]["reship_pending_since"])
					} else {
						assert.Equal(t, "[]", remaining, "writer DELETE must remove the real override")
					}
					if tc.deleteRecalc {
						assert.Positive(t, effects.recalc.Load())
					}
				}
				fleetheatmap.InvalidateOrg(orgID)
				fleetsummary.InvalidateOrg(orgID)
			})
		}
	}
}
