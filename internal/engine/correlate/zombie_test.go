package correlate

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

var zombieVerbs = []string{"mutating", "read", "other"}

// zombieDayAnchors returns the 14 UTC midnights ending at the last
// midnight before now (the production window shape).
func zombieDayAnchors() (start time.Time) {
	end := time.Now().UTC().Truncate(24 * time.Hour)
	return end.AddDate(0, 0, -zombieWindowDays)
}

// seedZombieBuckets writes 2 +Inf snapshots/day/verb (00:10 and 23:50 UTC,
// span > 30m min-data floor) with daily deltas from dailyTotals (len 14;
// day i grows by dailyTotals[i] split across verbs). Cumulative like
// production: each snapshot carries the running total.
func seedZombieBuckets(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, hc, cluster string, start time.Time, dailyTotals []float64) {
	t.Helper()
	require.Len(t, dailyTotals, zombieWindowDays)
	months := map[string]time.Time{}
	for i := 0; i < zombieWindowDays; i++ {
		ms := time.Date(start.AddDate(0, 0, i).Year(), start.AddDate(0, 0, i).Month(), 1, 0, 0, 0, 0, time.UTC)
		months[ms.Format("200601")] = ms
	}
	for _, ms := range months {
		require.NoError(t, ingestion.EnsureSLOPartitionsForMonth(ctx, pool, ms))
	}
	cum := map[string]float64{}
	for i := 0; i < zombieWindowDays; i++ {
		day := start.AddDate(0, 0, i)
		for _, verb := range zombieVerbs {
			share := dailyTotals[i] / float64(len(zombieVerbs))
			for j, at := range []time.Time{day.Add(10 * time.Minute), day.Add(23*time.Hour + 50*time.Minute)} {
				c := cum[verb]
				if j == 1 {
					c += share
					cum[verb] = c
				}
				ws := at.Add(-time.Hour)
				_, err := pool.Exec(ctx, `
					INSERT INTO hosted_api_bucket_rollups (
						window_start, window_end, org_id, cluster_uuid, hc_cluster_id,
						verb_group, le, bucket_count, collected_at, source
					) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'kubernetes')
					ON CONFLICT (org_id, cluster_uuid, hc_cluster_id, window_start, window_end, verb_group, le, source)
					DO UPDATE SET bucket_count = EXCLUDED.bucket_count, collected_at = EXCLUDED.collected_at`,
					ws.UTC(), at.UTC(), orgID, cluster, hc, verb, math.Inf(1), int64(c), at.UTC())
				require.NoError(t, err)
			}
		}
	}
}

// seedZombieSnapshot writes one fresh complete association snapshot.
func seedZombieSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, mgmt, hc, ns, manifest string, observed time.Time) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO manifest_hcp_snapshots (
			manifest_id, org_id, cluster_uuid, hcp_namespace,
			hosted_cluster_id, observed_at, complete
		) VALUES ($1,$2,$3,$4,$5,$6,TRUE)
		ON CONFLICT (manifest_id, hcp_namespace) DO NOTHING`,
		manifest, orgID, mgmt, ns, hc, observed.UTC())
	require.NoError(t, err)
}

// seedZombieDigest writes one container digest row with CP requests.
func seedZombieDigest(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, mgmt, ns string, date time.Time, reqMC int64) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO daily_container_digests (
			bucket_date, org_id, cluster_uuid, namespace, workload,
			workload_type, container_name, cpu_usage_p95_mc, cpu_request_p50_mc
		) VALUES ($1,$2,$3,$4,'kube-apiserver','Deployment','apiserver',$5,$6)
		ON CONFLICT DO NOTHING`,
		date.UTC().Format("2006-01-02"), orgID, mgmt, ns, reqMC/2, reqMC)
	require.NoError(t, err)
}

func zombieSetup(t *testing.T, orgID, mgmt, hc, ns string, daily []float64, withSnapshot, withDigest bool, digestReq int64) *pgxpool.Pool {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	start := zombieDayAnchors()
	seedZombieBuckets(t, ctx, pool, orgID, hc, mgmt, start, daily)
	if withSnapshot {
		seedZombieSnapshot(t, ctx, pool, orgID, mgmt, hc, ns, "z-manifest-"+t.Name(), time.Now().UTC().Add(-time.Hour))
	}
	if withDigest {
		seedZombieDigest(t, ctx, pool, orgID, mgmt, ns, time.Now().UTC(), digestReq)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM hosted_api_bucket_rollups WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM manifest_hcp_snapshots WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM hcp_correlation_advisories WHERE org_id = $1`, orgID)
	})
	return pool
}

func zombieAdvisories(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID string) []map[string]any {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT verdict, confidence, h_p99_s, h_threshold_s, n_signals
		FROM hcp_correlation_advisories WHERE org_id = $1 AND verdict = 'review_unused_hosted_cluster'`,
		orgID)
	require.NoError(t, err)
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var verdict, conf string
		var maxDaily, thr float64
		var signals []string
		require.NoError(t, rows.Scan(&verdict, &conf, &maxDaily, &thr, &signals))
		out = append(out, map[string]any{"verdict": verdict, "confidence": conf, "maxDaily": maxDaily, "threshold": thr, "signals": signals})
	}
	require.NoError(t, rows.Err())
	return out
}

func fourteen(v float64) []float64 {
	out := make([]float64, zombieWindowDays)
	for i := range out {
		out[i] = v
	}
	return out
}

func TestZombie_FiresOnTrueZombie(t *testing.T) {
	pool := zombieSetup(t, testutil.TestOrgID+"-z-fire", "11111111-1111-1111-1111-111111111111", "hc-zombie-1", "clusters-hc-z1", fourteen(10), true, true, 500)
	ctx := context.Background()
	fired, err := RunZombieCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 1, fired)
	advs := zombieAdvisories(t, ctx, pool, testutil.TestOrgID+"-z-fire")
	require.Len(t, advs, 1)
	assert.Equal(t, "high", advs[0]["confidence"])
	assert.InDelta(t, 10.0, advs[0]["maxDaily"], 2.0, "fractional per-verb shares truncate to int64 per snapshot")
	assert.Equal(t, 100.0, advs[0]["threshold"])
	assert.Contains(t, advs[0]["signals"].([]string)[0], "hc-zombie-1")
}

func TestZombie_SilentWeekendWarrior(t *testing.T) {
	org := testutil.TestOrgID + "-z-weekend"
	days := fourteen(10)
	days[5] = 5000 // one busy weekday vetoes via max
	pool := zombieSetup(t, org, "22222222-2222-2222-2222-222222222222", "hc-weekend-1", "clusters-hc-wk", days, true, true, 500)
	ctx := context.Background()
	fired, err := RunZombieCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired)
	assert.Empty(t, zombieAdvisories(t, ctx, pool, org))
}

func TestZombie_SilentLowButAlive(t *testing.T) {
	org := testutil.TestOrgID + "-z-alive"
	pool := zombieSetup(t, org, "33333333-3333-3333-3333-333333333333", "hc-alive-1", "clusters-hc-al", fourteen(500), true, true, 500)
	ctx := context.Background()
	fired, err := RunZombieCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired)
	assert.Empty(t, zombieAdvisories(t, ctx, pool, org))
}

func TestZombie_SilentMissingDays(t *testing.T) {
	org := testutil.TestOrgID + "-z-grace"
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	start := zombieDayAnchors()
	// Only 10 of 14 days: brand-new cluster, grace subsumed by coverage.
	partial := fourteen(10)[:10]
	seedZombieBuckets10(t, ctx, pool, org, "hc-new-1", "44444444-4444-4444-4444-444444444444", start.AddDate(0, 0, 4), partial)
	seedZombieSnapshot(t, ctx, pool, org, "44444444-4444-4444-4444-444444444444", "hc-new-1", "clusters-hc-new", "z-manifest-new", time.Now().UTC().Add(-time.Hour))
	seedZombieDigest(t, ctx, pool, org, "44444444-4444-4444-4444-444444444444", "clusters-hc-new", time.Now().UTC(), 500)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM hosted_api_bucket_rollups WHERE org_id = $1`, org)
		_, _ = pool.Exec(ctx, `DELETE FROM manifest_hcp_snapshots WHERE org_id = $1`, org)
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, org)
	})
	fired, err := RunZombieCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired, "10/14 days is unknown, never proof of idleness")
	assert.Empty(t, zombieAdvisories(t, ctx, pool, org))
}

// seedZombieBuckets10 seeds an arbitrary-length run (for partial windows).
func seedZombieBuckets10(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, hc, cluster string, start time.Time, dailyTotals []float64) {
	t.Helper()
	ms := time.Date(start.Year(), start.Month(), 1, 0, 0, 0, 0, time.UTC)
	me := ms.AddDate(0, 1, 0)
	require.NoError(t, ingestion.EnsureSLOPartitionsForMonth(ctx, pool, ms))
	if me.Month() != ms.Month() {
		require.NoError(t, ingestion.EnsureSLOPartitionsForMonth(ctx, pool, me))
	}
	cum := map[string]float64{}
	for i, total := range dailyTotals {
		day := start.AddDate(0, 0, i)
		for _, verb := range zombieVerbs {
			share := total / float64(len(zombieVerbs))
			for j, at := range []time.Time{day.Add(10 * time.Minute), day.Add(23*time.Hour + 50*time.Minute)} {
				c := cum[verb]
				if j == 1 {
					c += share
					cum[verb] = c
				}
				_, err := pool.Exec(ctx, `
					INSERT INTO hosted_api_bucket_rollups (
						window_start, window_end, org_id, cluster_uuid, hc_cluster_id,
						verb_group, le, bucket_count, collected_at, source
					) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,'kubernetes')
					ON CONFLICT (org_id, cluster_uuid, hc_cluster_id, window_start, window_end, verb_group, le, source)
					DO UPDATE SET bucket_count = EXCLUDED.bucket_count, collected_at = EXCLUDED.collected_at`,
					at.Add(-time.Hour).UTC(), at.UTC(), orgID, cluster, hc, verb, math.Inf(1), int64(c), at.UTC())
				require.NoError(t, err)
			}
		}
	}
}

func TestZombie_MediumWithoutDigests(t *testing.T) {
	org := testutil.TestOrgID + "-z-medium"
	pool := zombieSetup(t, org, "55555555-5555-5555-5555-555555555555", "hc-med-1", "clusters-hc-med", fourteen(10), true, false, 0)
	ctx := context.Background()
	fired, err := RunZombieCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 1, fired, "snapshot+idle without digest proof fires medium, never silent-high")
	advs := zombieAdvisories(t, ctx, pool, org)
	require.Len(t, advs, 1)
	assert.Equal(t, "medium", advs[0]["confidence"])
}

func TestZombie_SilentZeroCP(t *testing.T) {
	org := testutil.TestOrgID + "-z-zerocp"
	pool := zombieSetup(t, org, "66666666-6666-6666-6666-666666666666", "hc-zero-1", "clusters-hc-zero", fourteen(10), true, true, 0)
	ctx := context.Background()
	fired, err := RunZombieCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired, "digests present-but-zero is known-not-on: silence, not medium")
	assert.Empty(t, zombieAdvisories(t, ctx, pool, org))
}

func TestZombie_SilentWithoutEvidence(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	fired, err := RunZombieCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired)
}

func TestEvalZombieIdle_Boundary(t *testing.T) {
	end := time.Now().UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -zombieWindowDays)
	totals := map[string]float64{}
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		totals[d.UTC().Format("2006-01-02")] = 100.0
	}
	_, idle, known := evalZombieIdle(totals, start, end, 100.0)
	assert.True(t, known)
	assert.False(t, idle, "max == T is not below T: boundary stays silent")
	_, idle, _ = evalZombieIdle(totals, start, end, 100.5)
	assert.True(t, idle)
	_, _, known = evalZombieIdle(totals, start, end, 0)
	assert.False(t, known, "non-positive threshold is unknown, never fires")
}
