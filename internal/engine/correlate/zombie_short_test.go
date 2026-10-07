package correlate

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// seedShortDigestDays writes per-workload daily digest rows for days
// [0, n) from start.
func seedShortDigestDays(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, cluster string, start time.Time, n int, rows map[nswl]zombieWorkloadDay) {
	t.Helper()
	months := map[string]time.Time{}
	for i := 0; i < n; i++ {
		d := start.AddDate(0, 0, i)
		ms := time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)
		months[ms.Format("200601")] = ms
	}
	for _, ms := range months {
		require.NoError(t, ingestion.EnsureDigestPartitionMonth(ctx, pool, ms))
	}
	for i := 0; i < n; i++ {
		day := start.AddDate(0, 0, i).Format("2006-01-02")
		for k, w := range rows {
			var req any
			req = w.req
			if w.req < 0 {
				req = nil
			}
			_, err := pool.Exec(ctx, `
				INSERT INTO daily_container_digests (
					bucket_date, org_id, cluster_uuid, namespace, workload,
					workload_type, container_name, cpu_usage_p95_mc, cpu_usage_p50_mc, cpu_request_p50_mc
				) VALUES ($1,$2,$3,$4,$5,'Deployment','c0',$6,$6,$7)
				ON CONFLICT DO NOTHING`,
				day, orgID, cluster, k.ns, k.wl, w.usage, req)
			require.NoError(t, err)
		}
	}
}

// seedShortPoolRows writes hourly nodepool snapshots for days [0, n)
// from start with constant spec/status.
func seedShortPoolRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, mgmt, hc string, start time.Time, n int, spec, status int64) {
	t.Helper()
	months := map[string]time.Time{}
	end := start.AddDate(0, 0, n)
	for ws := start; ws.Before(end); ws = ws.Add(time.Hour) {
		ms := time.Date(ws.Year(), ws.Month(), 1, 0, 0, 0, 0, time.UTC)
		months[ms.Format("200601")] = ms
	}
	for _, ms := range months {
		require.NoError(t, ingestion.EnsureNodepoolPartitionsForMonth(ctx, pool, ms))
	}
	for ws := start; ws.Before(end); ws = ws.Add(time.Hour) {
		we := ws.Add(time.Hour)
		_, err := pool.Exec(ctx, `
			INSERT INTO hosted_nodepool_rollups (
				window_start, window_end, org_id, cluster_uuid, hc_cluster_id,
				pool_name, spec_replicas, status_replicas, autoscaling,
				collected_at, source
			) VALUES ($1,$2,$3,$4,$5,'pool-a',$6,$7,'',$8,'hypershift')
			ON CONFLICT (org_id, cluster_uuid, hc_cluster_id, pool_name, window_start, window_end, source)
			DO NOTHING`,
			ws.UTC(), we.UTC(), orgID, mgmt, hc, spec, status, we.UTC())
		require.NoError(t, err)
	}
}

func shortWindow() (start, end time.Time) {
	end = time.Now().UTC().Truncate(24 * time.Hour)
	return end.AddDate(0, 0, -3), end
}

var shortHostedIdle = map[nswl]zombieWorkloadDay{
	{"demo-app", "web"}:                {usage: 0, req: 100},
	{"kube-system", "coredns"}:         {usage: 2000, req: 2000},
	{"koku-metrics-operator", "agent"}: {usage: 5000, req: 5000},
}

var shortMgmtOn = map[nswl]zombieWorkloadDay{
	{"clusters-hc9", "kube-apiserver"}: {usage: 50, req: 500},
}

func shortFixture(t *testing.T, orgID, mgmt, hc, assocNS string, poolDays int, poolSpec, poolStatus int64) *pgxpool.Pool {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	start, _ := shortWindow()
	seedShortDigestDays(t, ctx, pool, orgID, hc, start, 3, shortHostedIdle)
	seedShortDigestDays(t, ctx, pool, orgID, mgmt, start, 3, shortMgmtOn)
	seedZombieSnapshot(t, ctx, pool, orgID, mgmt, hc, assocNS, "z-short-manifest-"+t.Name(), time.Now().UTC().Add(-time.Hour))
	if poolDays > 0 {
		seedShortPoolRows(t, ctx, pool, orgID, mgmt, hc, start.AddDate(0, 0, 3-poolDays), poolDays, poolSpec, poolStatus)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM manifest_hcp_snapshots WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM hosted_nodepool_rollups WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM hcp_correlation_advisories WHERE org_id = $1`, orgID)
	})
	return pool
}

func shortAdvisories(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID string) []map[string]any {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT confidence, h_p99_s, h_threshold_s, n_signals, window_start, window_end
		FROM hcp_correlation_advisories WHERE org_id = $1 AND verdict = 'review_unused_hosted_cluster'`,
		orgID)
	require.NoError(t, err)
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var conf string
		var peak, floor float64
		var signals []string
		var ws, we time.Time
		require.NoError(t, rows.Scan(&conf, &peak, &floor, &signals, &ws, &we))
		out = append(out, map[string]any{"confidence": conf, "peak": peak, "floor": floor, "signals": signals, "ws": ws, "we": we})
	}
	require.NoError(t, rows.Err())
	return out
}

const (
	zShortHC = "eeeeeeee-7777-7777-7777-777777777777"
	zShortMg = "77777777-7777-7777-7777-777777777777"
)

func TestZombieShort_FiresOnParked(t *testing.T) {
	org := testutil.TestOrgID + "-z-short-fire"
	pool := shortFixture(t, org, zShortMg, zShortHC, "clusters-hc9", 3, 0, 0)
	ctx := context.Background()
	fired, err := RunZombieShortCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 1, fired)
	advs := shortAdvisories(t, ctx, pool, org)
	require.Len(t, advs, 1)
	assert.Equal(t, "high", advs[0]["confidence"])
	signals := advs[0]["signals"].([]string)
	assert.Contains(t, signals, "lane:short-3d")
	assert.Contains(t, signals, "pool_zero_days:3")
	ws := advs[0]["ws"].(time.Time)
	we := advs[0]["we"].(time.Time)
	assert.Equal(t, 72.0, we.Sub(ws).Hours(), "short window spans 3 days")
}

func TestZombieShort_SilentPartialCoverage(t *testing.T) {
	org := testutil.TestOrgID + "-z-short-partial"
	pool := shortFixture(t, org, "88888888-8888-8888-8888-888888888888", "ffffffff-8888-8888-8888-888888888888", "clusters-hc9", 2, 0, 0)
	ctx := context.Background()
	fired, err := RunZombieShortCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired, "2/3 pool days with default required=3 is unknown")
	assert.Empty(t, shortAdvisories(t, ctx, pool, org))
}

func TestZombieShort_SilentNonZeroDay(t *testing.T) {
	org := testutil.TestOrgID + "-z-short-veto"
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	start, _ := shortWindow()
	mgmt := "99999999-9999-9999-9999-999999999999"
	hc := "ffffffff-9999-9999-9999-999999999999"
	seedShortDigestDays(t, ctx, pool, org, hc, start, 3, shortHostedIdle)
	seedShortDigestDays(t, ctx, pool, org, mgmt, start, 3, shortMgmtOn)
	seedZombieSnapshot(t, ctx, pool, org, mgmt, hc, "clusters-hc9", "z-short-veto", time.Now().UTC().Add(-time.Hour))
	// Day 1 carries a scaled pool: vetoes despite days 0,2 parked.
	seedShortPoolRows(t, ctx, pool, org, mgmt, hc, start, 1, 0, 0)
	seedShortPoolRows(t, ctx, pool, org, mgmt, hc, start.AddDate(0, 0, 1), 1, 2, 2)
	seedShortPoolRows(t, ctx, pool, org, mgmt, hc, start.AddDate(0, 0, 2), 1, 0, 0)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, org)
		_, _ = pool.Exec(ctx, `DELETE FROM manifest_hcp_snapshots WHERE org_id = $1`, org)
		_, _ = pool.Exec(ctx, `DELETE FROM hosted_nodepool_rollups WHERE org_id = $1`, org)
		_, _ = pool.Exec(ctx, `DELETE FROM hcp_correlation_advisories WHERE org_id = $1`, org)
	})
	fired, err := RunZombieShortCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired, "one non-zero covered day vetoes like max-daily")
	assert.Empty(t, shortAdvisories(t, ctx, pool, org))
}

func TestZombieShort_SilentMidTransition(t *testing.T) {
	org := testutil.TestOrgID + "-z-short-mid"
	pool := shortFixture(t, org, "aaaaaaaa-0000-4000-8000-000000000000", "bbbbbbbb-0000-4000-8000-000000000000", "clusters-hc9", 3, 0, 0)
	ctx := context.Background()
	// Flip status mid-window to scaling-down: spec 0 but status live.
	_, err := pool.Exec(ctx, `UPDATE hosted_nodepool_rollups SET status_replicas = 2 WHERE org_id = $1`, org)
	require.NoError(t, err)
	fired, err := RunZombieShortCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired, "spec-zero with live status is mid-transition: unknown, never idle-proof")
	assert.Empty(t, shortAdvisories(t, ctx, pool, org))
}

func TestZombieShort_SilentWithoutPoolEvidence(t *testing.T) {
	org := testutil.TestOrgID + "-z-short-nopool"
	pool := shortFixture(t, org, "cccccccc-0000-4000-8000-000000000000", "dddddddd-0000-4000-8000-000000000000", "clusters-hc9", 0, 0, 0)
	ctx := context.Background()
	fired, err := RunZombieShortCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired, "no nodepool rows means no short evaluation (standard lane owns it)")
	assert.Empty(t, shortAdvisories(t, ctx, pool, org))
}

func TestEvalPoolParked_Matrix(t *testing.T) {
	end := time.Now().UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -3)
	mk := func(days ...string) map[string]*poolDayState {
		m := map[string]*poolDayState{}
		for _, d := range days {
			m[d] = &poolDayState{day: d, snapshots: 2, allZero: true}
		}
		return m
	}
	all := []string{}
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		all = append(all, d.UTC().Format("2006-01-02"))
	}
	covered, parked := evalPoolParked(mk(all...), start, 3, 3)
	assert.Equal(t, 3, covered)
	assert.True(t, parked)
	covered, parked = evalPoolParked(mk(all[:2]...), start, 3, 3)
	assert.Equal(t, 2, covered)
	assert.False(t, parked, "2/3 with required=3 is unknown")
	covered, parked = evalPoolParked(mk(all[:2]...), start, 3, 2)
	assert.True(t, parked, "required=2 honors looser tenant tuning")
	nonzero := mk(all...)
	nonzero[all[1]].allZero = false
	_, parked = evalPoolParked(nonzero, start, 3, 1)
	assert.False(t, parked, "non-zero covered day vetoes at any required level")
	_, parked = evalPoolParked(map[string]*poolDayState{}, start, 3, 3)
	assert.False(t, parked, "no evidence is silence")
}

func TestZombieCustomIdleWindow_EndToEnd(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testutil.TestOrgID + "-z-custom-window"
	mgmt := "eeeeeeee-0000-4000-8000-000000000001"
	hc := "ffffffff-0000-4000-8000-000000000001"

	body := `{"h_p99_threshold_s": 0.3, "h_baseline_multiple": 3, "c_cpu_pct": 80, "c_etcd_p99_s": 0.01, "window_h": 1, "skew_m": 5, "freshness_h": 2, "expiry_h": 24, "z_idle_req_per_day": 100, "z_idle_cpu_floor_mc": 10, "z_short_window_days": 3, "z_short_required_days": 3, "z_idle_window_days": 5}`
	require.NoError(t, engine.UpdateHCPCorrelationSettings(ctx, pool, orgID, json.RawMessage(body)))

	end := time.Now().UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -5)
	seedShortDigestDays(t, ctx, pool, orgID, hc, start, 5, shortHostedIdle)
	seedShortDigestDays(t, ctx, pool, orgID, mgmt, start, 5, shortMgmtOn)
	seedZombieSnapshot(t, ctx, pool, orgID, mgmt, hc, "clusters-hc9", "z-custom-window", time.Now().UTC().Add(-time.Hour))
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM manifest_hcp_snapshots WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM hcp_correlation_advisories WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM recommendation_thresholds WHERE org_id = $1`, orgID)
	})

	fired, err := RunZombieCycle(ctx, pool)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, fired, 1, "5 idle days must fire under a 5d custom window (would be silent under default 14d)")

	advs := shortAdvisories(t, ctx, pool, orgID)
	require.NotEmpty(t, advs)
	ws := advs[0]["ws"].(time.Time)
	we := advs[0]["we"].(time.Time)
	assert.Equal(t, 120.0, we.Sub(ws).Hours(), "advisory window spans the custom 5 days")
}
