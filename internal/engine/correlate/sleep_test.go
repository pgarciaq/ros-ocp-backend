package correlate

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// W8 sleep-candidate fixtures (ADR-0348). Distinct UUID prefixes from the
// zombie fixtures so org-scoped cleanup never collides.
const (
	sHC1   = "bbbbbbbb-1111-1111-1111-111111111111"
	sHC2   = "bbbbbbbb-2222-2222-2222-222222222222"
	sHC3   = "bbbbbbbb-3333-3333-3333-333333333333"
	sHC4   = "bbbbbbbb-4444-4444-4444-444444444444"
	sHC5   = "bbbbbbbb-5555-5555-5555-555555555555"
	sHC6   = "bbbbbbbb-6666-6666-6666-666666666666"
	sHC7   = "bbbbbbbb-7777-7777-7777-777777777777"
	sHC8   = "bbbbbbbb-8888-8888-8888-888888888888"
	sMgmt1 = "cccccccc-1111-1111-1111-111111111111"
)

// sleepDay is one seeded (namespace, workload) day: BH-stream and
// all-hours-stream CPU in mC. The outside-BH value is all - bh.
type sleepDay struct {
	bh  int64
	all int64
}

func sleepWindow() (start, end time.Time) {
	end = time.Now().UTC().Truncate(24 * time.Hour)
	return end.AddDate(0, 0, -14), end
}

// bhSignatureHosted builds 14 days: one user workload following the
// business-hours signature (busy in hours, idle outside — outside value
// comes from the override) plus busy platform rows on both streams (prove
// pipeline liveness + the shared platform exclusion).
func bhSignatureHosted(bh, outside int64) map[int]map[nswl]sleepDay {
	spec := map[int]map[nswl]sleepDay{}
	for i := 0; i < 14; i++ {
		spec[i] = map[nswl]sleepDay{
			{"demo-app", "web"}:                    {bh: bh, all: bh + outside},
			{"kube-system", "coredns"}:             {bh: 2000, all: 2100},
			{"koku-metrics-operator", "collector"}: {bh: 5000, all: 5200},
		}
	}
	return spec
}

// idleAlwaysHosted builds 14 days of zero-activity user workload on both
// streams (platform rows stay busy — liveness + exclusion proof).
func idleAlwaysHosted() map[int]map[nswl]sleepDay {
	return bhSignatureHosted(0, 0)
}

// seedSleepDigests writes both digest streams for the hosted cluster.
// partialDay (if >= 0) seeds only the all-hours stream on that day
// (partial BH coverage must read unknown, never zero-activity).
func seedSleepDigests(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, hc string, spec map[int]map[nswl]sleepDay, partialDay int) {
	t.Helper()
	start, _ := sleepWindow()
	months := map[string]time.Time{}
	for i := 0; i < 14; i++ {
		d := start.AddDate(0, 0, i)
		ms := time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)
		months[ms.Format("200601")] = ms
	}
	for _, ms := range months {
		require.NoError(t, ingestion.EnsureDigestPartitionMonth(ctx, pool, ms))
	}
	for i := 0; i < 14; i++ {
		day := start.AddDate(0, 0, i).Format("2006-01-02")
		for k, w := range spec[i] {
			if i != partialDay {
				_, err := pool.Exec(ctx, `
					INSERT INTO daily_container_digests (
						bucket_date, org_id, cluster_uuid, namespace, workload,
						workload_type, container_name, cpu_usage_p95_mc, cpu_usage_p50_mc, schedule_type
					) VALUES ($1,$2,$3,$4,$5,'Deployment','c0',$6,$6,'business_hours')
					ON CONFLICT DO NOTHING`, day, orgID, hc, k.ns, k.wl, w.bh)
				require.NoError(t, err)
			}
			_, err := pool.Exec(ctx, `
				INSERT INTO daily_container_digests (
					bucket_date, org_id, cluster_uuid, namespace, workload,
					workload_type, container_name, cpu_usage_p95_mc, cpu_usage_p50_mc, schedule_type
				) VALUES ($1,$2,$3,$4,$5,'Deployment','c0',$6,$6,'all_hours')
				ON CONFLICT DO NOTHING`, day, orgID, hc, k.ns, k.wl, w.all)
			require.NoError(t, err)
		}
	}
}

// seedSleepSnapshot writes a fresh complete association snapshot with the
// given tier (the #715 operator emission shape; latest-wins on read).
func seedSleepSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, mgmt, hc, tier string, observed time.Time) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO manifest_hcp_snapshots (
			manifest_id, org_id, cluster_uuid, hcp_namespace,
			hosted_cluster_id, observed_at, complete, hc_tier
		) VALUES ($1,$2,$3,$4,$5,$6,TRUE,$7)
		ON CONFLICT (manifest_id, hcp_namespace) DO UPDATE SET hc_tier = EXCLUDED.hc_tier`,
		"sleep-manifest-"+t.Name()+"-"+tier, orgID, mgmt, "clusters-s9", hc, observed.UTC(), tier)
	require.NoError(t, err)
}

// seedSleepSchedule writes a business-hours schedule row. namespace==""
// means cluster scope; the sentinel UUID means org-wide default.
func seedSleepSchedule(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, cluster, namespace string) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO business_hours_schedules (
			org_id, cluster_uuid, namespace, timezone, days, start_time, end_time, enabled
		) VALUES ($1,$2::uuid,$3,'UTC',ARRAY['monday','tuesday','wednesday','thursday','friday'],'09:00','17:00',TRUE)
		ON CONFLICT (org_id, cluster_uuid, namespace) DO UPDATE SET enabled = TRUE`,
		orgID, cluster, namespace)
	require.NoError(t, err)
}

func seedNodepoolRollup(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, hc string, replicas int64) {
	t.Helper()
	ms := time.Now().UTC()
	monthStart := time.Date(ms.Year(), ms.Month(), 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, ingestion.EnsureNodepoolPartitionsForMonth(ctx, pool, monthStart))
	_, err := pool.Exec(ctx, `
		INSERT INTO hosted_nodepool_rollups (
			window_start, window_end, org_id, cluster_uuid, hc_cluster_id,
			pool_name, spec_replicas, status_replicas, collected_at
		) VALUES (now() - interval '1 hour', now(), $1, $2::uuid, $3, 'workers', $4, $4, now())`,
		orgID, sMgmt1, hc, replicas)
	require.NoError(t, err)
}

// sleepFixture assembles the standard declared+labeled+scheduled shape.
func sleepFixture(t *testing.T, orgID, hc, tier string, hosted map[int]map[nswl]sleepDay, partialDay int, withSchedule bool) *pgxpool.Pool {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	seedSleepDigests(t, ctx, pool, orgID, hc, hosted, partialDay)
	seedSleepSnapshot(t, ctx, pool, orgID, sMgmt1, hc, tier, time.Now().UTC().Add(-time.Hour))
	if withSchedule {
		seedSleepSchedule(t, ctx, pool, orgID, hc, "")
	}
	seedNodepoolRollup(t, ctx, pool, orgID, hc, 2)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM manifest_hcp_snapshots WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM hcp_correlation_advisories WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM business_hours_schedules WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM hosted_nodepool_rollups WHERE org_id = $1`, orgID)
	})
	return pool
}

func sleepAdvisories(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID string) []map[string]any {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT verdict, confidence, h_p99_s, h_threshold_s, n_signals
		FROM hcp_correlation_advisories WHERE org_id = $1 AND verdict = $2`,
		orgID, sleepVerdict)
	require.NoError(t, err)
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var verdict, conf string
		var bhPeak, outsidePeak float64
		var signals []string
		require.NoError(t, rows.Scan(&verdict, &conf, &bhPeak, &outsidePeak, &signals))
		out = append(out, map[string]any{"verdict": verdict, "confidence": conf, "bhPeak": bhPeak, "outsidePeak": outsidePeak, "signals": signals})
	}
	require.NoError(t, rows.Err())
	return out
}

func mustContainSignal(t *testing.T, signals []string, prefix string) string {
	t.Helper()
	for _, s := range signals {
		if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
			return s
		}
	}
	t.Fatalf("signal %q not found in %v", prefix, signals)
	return ""
}

// Review Focus 1: declared + BH signature fires with typed evidence.
func TestSleepCandidate_DeclaredAndBHSignatureFires(t *testing.T) {
	org := testutil.TestOrgID + "-s-fire"
	pool := sleepFixture(t, org, sHC1, hcTierNonProd, bhSignatureHosted(300, 0), -1, true)
	ctx := context.Background()
	fired, err := RunSleepCandidateCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 1, fired)
	advs := sleepAdvisories(t, ctx, pool, org)
	require.Len(t, advs, 1)
	assert.Equal(t, "high", advs[0]["confidence"])
	assert.Equal(t, 300.0, advs[0]["bhPeak"], "BH-activity peak rides h_p99_s")
	assert.Equal(t, 0.0, advs[0]["outsidePeak"])
	signals := advs[0]["signals"].([]string)
	assert.Contains(t, mustContainSignal(t, signals, "declared_tier:"), hcTierNonProd)
	assert.Contains(t, mustContainSignal(t, signals, "bh_active_peak_mc:"), "300")
	assert.Contains(t, mustContainSignal(t, signals, "worker_nodes:"), "2")
	// Schedule mon-fri 09:00-17:00 UTC: 16 outside-hours/day * 5 = 80/week.
	assert.Contains(t, mustContainSignal(t, signals, "reclaim_node_hours_per_week:"), "160")
}

// Review Focus 2: the overnight-quiet-prod trap — declaration does not
// override the data gate; outside-BH activity breaks the signature.
func TestSleepCandidate_DeclaredOvernightActiveIsSilent(t *testing.T) {
	org := testutil.TestOrgID + "-s-night"
	pool := sleepFixture(t, org, sHC2, hcTierNonProd, bhSignatureHosted(300, 200), -1, true)
	ctx := context.Background()
	fired, err := RunSleepCandidateCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired)
	assert.Empty(t, sleepAdvisories(t, ctx, pool, org))
}

// Review Focus 3: unlabeled never fires in v1 (declared-only).
func TestSleepCandidate_UnlabeledIsSilent(t *testing.T) {
	org := testutil.TestOrgID + "-s-unlabeled"
	pool := sleepFixture(t, org, sHC3, "", bhSignatureHosted(300, 0), -1, true)
	ctx := context.Background()
	fired, err := RunSleepCandidateCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired)
}

// Review Focus 4: partition with the zombie rule — idle-always routes to
// zombie (when its legs pass), never to sleep.
func TestSleepCandidate_ZombiePartition_IdleAlwaysRoutesToZombie(t *testing.T) {
	org := testutil.TestOrgID + "-s-part-a"
	pool := sleepFixture(t, org, sHC4, hcTierNonProd, idleAlwaysHosted(), -1, true)
	ctx := context.Background()
	zStart, _ := sleepWindow()
	for i := 0; i < 14; i++ {
		seedZombieDigestDay(t, ctx, pool, org, sMgmt1, zStart.AddDate(0, 0, i).Format("2006-01-02"),
			map[nswl]zombieWorkloadDay{{"clusters-s9", "kube-apiserver"}: {usage: 50, req: 500}})
	}
	zFired, err := RunZombieCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 1, zFired, "idle-always routes to the zombie rule")
	sFired, err := RunSleepCandidateCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, sFired, "idle-always never routes to sleep (no BH activity)")
}

// Review Focus 4 (reverse direction): BH-active routes to sleep, never to
// zombie (active user workloads break the zombie idle leg).
func TestSleepCandidate_ZombiePartition_BHActiveRoutesToSleep(t *testing.T) {
	org := testutil.TestOrgID + "-s-part-b"
	pool := sleepFixture(t, org, sHC5, hcTierNonProd, bhSignatureHosted(300, 0), -1, true)
	ctx := context.Background()
	sFired, err := RunSleepCandidateCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 1, sFired, "BH-active routes to sleep")
	zFired, err := RunZombieCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, zFired, "BH-active never routes to zombie (active user workloads)")
}

// Review Focus 5: unknown days are silence — a day with only one digest
// stream (partial BH coverage) reads unknown, never zero-activity.
func TestSleepCandidate_UnknownDaysSilent(t *testing.T) {
	org := testutil.TestOrgID + "-s-unknown"
	pool := sleepFixture(t, org, sHC6, hcTierNonProd, bhSignatureHosted(300, 0), 5, true)
	ctx := context.Background()
	fired, err := RunSleepCandidateCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired)
}

// Review Focus 6: config-derived thresholds — non-default floors change the
// verdict for identical data (a hardcoded-threshold regression fails here).
func TestSleepCandidate_NonDefaultFloors(t *testing.T) {
	_, end := sleepWindow()
	start := end.AddDate(0, 0, -2)
	mkDays := func(bh, all int64) map[string]map[sleepKey]*sleepWorkloadDay {
		return map[string]map[sleepKey]*sleepWorkloadDay{
			start.Format("2006-01-02"): {
				{"demo-app", "web"}: {bhCPU: bh, allCPU: all, bhMeasured: true, allMeasured: true},
			},
			start.AddDate(0, 0, 1).Format("2006-01-02"): {
				{"demo-app", "web"}: {bhCPU: bh, allCPU: all, bhMeasured: true, allMeasured: true},
			},
		}
	}
	days := mkDays(300, 350)

	// Default-order floors: BH-active (300 >= 10) but outside 50 >= 10 breaks.
	_, _, match, known := evalSleepSignature(days, start, end, 10, 10, nil, nil)
	assert.True(t, known)
	assert.False(t, match, "outside 50 must break the signature at floor 10")

	// Non-default outside floor 100: outside 50 < 100, BH 300 >= 10 → match.
	_, _, match, known = evalSleepSignature(days, start, end, 10, 100, nil, nil)
	assert.True(t, known)
	assert.True(t, match, "non-default outside floor must admit outside 50")

	// Non-default BH floor 500: 300 < 500 → not active in hours.
	_, _, match, known = evalSleepSignature(days, start, end, 500, 100, nil, nil)
	assert.True(t, known)
	assert.False(t, match, "non-default BH floor must reject activity 300")
}

// Review Focus 7: in-place refresh — reruns upsert the same window row.
func TestSleepCandidate_AdvisoryLifecycle(t *testing.T) {
	org := testutil.TestOrgID + "-s-life"
	pool := sleepFixture(t, org, sHC7, hcTierNonProd, bhSignatureHosted(300, 0), -1, true)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		fired, err := RunSleepCandidateCycle(ctx, pool)
		require.NoError(t, err)
		assert.Equal(t, 1, fired, "run %d: upsert counts as fired", i+1)
	}
	var n int
	require.NoError(t, pool.QueryRow(ctx, `SELECT COUNT(*) FROM hcp_correlation_advisories WHERE org_id = $1 AND verdict = $2`, org, sleepVerdict).Scan(&n))
	assert.Equal(t, 1, n, "hourly reruns refresh in place — one row per window")
}

// ADR-0348 §3: latest-wins including empty — a mid-window relabel to
// non-prod takes effect immediately (majority-wins would lag).
func TestSleepCandidate_LabelChangeLatestWins_ReloadTakesEffect(t *testing.T) {
	org := testutil.TestOrgID + "-s-lw-on"
	pool := sleepFixture(t, org, sHC8, "", bhSignatureHosted(300, 0), -1, true)
	ctx := context.Background()
	seedSleepSnapshot(t, ctx, pool, org, sMgmt1, sHC8, hcTierNonProd, time.Now().UTC().Add(-30*time.Minute))
	fired, err := RunSleepCandidateCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 1, fired, "newest row carries non-prod — latest-wins fires")
}

// ADR-0348 §3 (removal direction): label removal stops the advisory
// immediately (latest-non-empty would mask removal).
func TestSleepCandidate_LabelChangeLatestWins_RemovalStops(t *testing.T) {
	org := testutil.TestOrgID + "-s-lw-off"
	pool := sleepFixture(t, org, sHC1, hcTierNonProd, bhSignatureHosted(300, 0), -1, true)
	ctx := context.Background()
	seedSleepSnapshot(t, ctx, pool, org, sMgmt1, sHC1, "", time.Now().UTC().Add(-30*time.Minute))
	fired, err := RunSleepCandidateCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired, "newest row carries no declaration — latest-wins silences")
}

// ADR-0348 §4: no schedule at all → no evaluation (no invented schedules).
func TestSleepCandidate_NoScheduleSilent(t *testing.T) {
	org := testutil.TestOrgID + "-s-nosched"
	pool := sleepFixture(t, org, sHC2, hcTierNonProd, bhSignatureHosted(300, 0), -1, false)
	ctx := context.Background()
	fired, err := RunSleepCandidateCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired)
}

// ADR-0348 §4: a namespace-scoped schedule never defines an HC's hours.
func TestSleepCandidate_NamespaceScheduleOnlySilent(t *testing.T) {
	org := testutil.TestOrgID + "-s-nssched"
	pool := sleepFixture(t, org, sHC3, hcTierNonProd, bhSignatureHosted(300, 0), -1, false)
	ctx := context.Background()
	seedSleepSchedule(t, ctx, pool, org, sHC3, "demo-app")
	fired, err := RunSleepCandidateCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired)
}

// ADR-0348 §4: the org-wide default (sentinel UUID) qualifies.
func TestSleepCandidate_OrgDefaultScheduleQualifies(t *testing.T) {
	org := testutil.TestOrgID + "-s-orgsched"
	pool := sleepFixture(t, org, sHC4, hcTierNonProd, bhSignatureHosted(300, 0), -1, false)
	ctx := context.Background()
	seedSleepSchedule(t, ctx, pool, org, sleepScheduleSentinel, "")
	fired, err := RunSleepCandidateCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 1, fired, "org-wide default defines the HC's hours")
}
