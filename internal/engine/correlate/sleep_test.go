package correlate

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/bhschedule"
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

// sleepDay is one seeded (namespace, workload) day: the in-window (BH) and
// off-window CPU p50 sums in mC — the #760 evidence legs, direct measures.
// bhAbsent/offAbsent seed a NULL leg (the producer's normal shapes: a
// night-only workload has no in-window samples, so no BH leg).
type sleepDay struct {
	bh        int64
	off       int64
	bhAbsent  bool
	offAbsent bool
	// offP95 seeds a DISTINCT off-window p95 (#826): nil mirrors `off`
	// (the historical fixtures' semantics); set explicitly when the test
	// pins that the predicate consumes the p95 column, not the p50.
	offP95 *int64
}

func sleepWindow() (start, end time.Time) {
	end = time.Now().UTC().Truncate(24 * time.Hour)
	return end.AddDate(0, 0, -14), end
}

// bhSignatureHosted builds 14 days: one user workload following the
// business-hours signature (busy in hours, idle outside — the off leg is
// the direct outside-BH measure) plus platform rows on both legs (prove
// pipeline liveness + the shared platform exclusion). Distinct values so a
// mis-mapped leg cannot pass by coincidence.
func bhSignatureHosted(bh, outside int64) map[int]map[nswl]sleepDay {
	spec := map[int]map[nswl]sleepDay{}
	for i := 0; i < 14; i++ {
		spec[i] = map[nswl]sleepDay{
			{"demo-app", "web"}:                    {bh: bh, off: outside},
			{"kube-system", "coredns"}:             {bh: 2000, off: 100},
			{"koku-metrics-operator", "collector"}: {bh: 5000, off: 200},
		}
	}
	return spec
}

// idleAlwaysHosted builds 14 days of zero-activity user workload on both
// streams (platform rows stay busy — liveness + exclusion proof).
func idleAlwaysHosted() map[int]map[nswl]sleepDay {
	return bhSignatureHosted(0, 0)
}

// seedSleepEvidence writes the #760 evidence legs for the hosted cluster.
// partialDay (if >= 0) seeds NO rows on that day (partial coverage must
// read unknown, never zero-activity).
func seedSleepEvidence(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, hc string, spec map[int]map[nswl]sleepDay, partialDay int) {
	t.Helper()
	start, _ := sleepWindow()
	for i := 0; i < 14; i++ {
		if i == partialDay {
			continue
		}
		day := start.AddDate(0, 0, i).Format("2006-01-02")
		for k, w := range spec[i] {
			var bh, off, off95 *int64
			if !w.bhAbsent {
				v := w.bh
				bh = &v
			}
			if !w.offAbsent {
				v := w.off
				off = &v
				// #826: the p95 column mirrors the fixture's off value
				// unless the test pins a distinct one (the predicate grain).
				off95 = &v
				if w.offP95 != nil {
					off95 = w.offP95
				}
			}
			_, err := pool.Exec(ctx, `
				INSERT INTO hcp_sleep_evidence (
					org_id, cluster_uuid, namespace, workload, workload_type,
					container_name, bucket_date, bh_cpu_p50_mc, off_cpu_p50_mc, off_cpu_p95_mc
				) VALUES ($1,$2,$3,$4,'Deployment','c0',$5,$6,$7,$8)
				ON CONFLICT (org_id, cluster_uuid, namespace, workload, workload_type, container_name, bucket_date)
				DO UPDATE SET bh_cpu_p50_mc = EXCLUDED.bh_cpu_p50_mc,
					off_cpu_p50_mc = EXCLUDED.off_cpu_p50_mc,
					off_cpu_p95_mc = EXCLUDED.off_cpu_p95_mc`,
				orgID, hc, k.ns, k.wl, day, bh, off, off95)
			require.NoError(t, err)
		}
	}
}

// seedSleepSnapshot writes a fresh complete association snapshot with the
// given tier (the #715 operator emission shape; latest-wins on read).
func seedSleepSnapshot(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, hc, tier string, observed time.Time) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO manifest_hcp_snapshots (
			manifest_id, org_id, cluster_uuid, hcp_namespace,
			hosted_cluster_id, observed_at, complete, hc_tier
		) VALUES ($1,$2,$3,$4,$5,$6,TRUE,$7)
		ON CONFLICT (manifest_id, hcp_namespace) DO UPDATE SET hc_tier = EXCLUDED.hc_tier`,
		"sleep-manifest-"+t.Name()+"-"+tier, orgID, sMgmt1, "clusters-s9", hc, observed.UTC(), tier)
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

func seedNodepoolRollup(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, hc string) {
	t.Helper()
	ms := time.Now().UTC()
	monthStart := time.Date(ms.Year(), ms.Month(), 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, ingestion.EnsureNodepoolPartitionsForMonth(ctx, pool, monthStart))
	_, err := pool.Exec(ctx, `
		INSERT INTO hosted_nodepool_rollups (
			window_start, window_end, org_id, cluster_uuid, hc_cluster_id,
			pool_name, spec_replicas, status_replicas, collected_at
		) VALUES (now() - interval '1 hour', now(), $1, $2::uuid, $3, 'workers', 2, 2, now())`,
		orgID, sMgmt1, hc)
	require.NoError(t, err)
}

// sleepFixture assembles the standard declared+labeled+scheduled shape.
func sleepFixture(t *testing.T, orgID, hc, tier string, hosted map[int]map[nswl]sleepDay, partialDay int, withSchedule bool) *pgxpool.Pool {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	seedSleepEvidence(t, ctx, pool, orgID, hc, hosted, partialDay)
	seedSleepSnapshot(t, ctx, pool, orgID, hc, tier, time.Now().UTC().Add(-time.Hour))
	if withSchedule {
		seedSleepSchedule(t, ctx, pool, orgID, hc, "")
	}
	seedNodepoolRollup(t, ctx, pool, orgID, hc)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM hcp_sleep_evidence WHERE org_id = $1`, orgID)
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
		// The zombie idle leg reads hosted-cluster digests: the idle-always
		// user workload (the sleep fixture seeds only evidence rows now).
		seedZombieDigestDay(t, ctx, pool, org, sHC4, zStart.AddDate(0, 0, i).Format("2006-01-02"),
			map[nswl]zombieWorkloadDay{{"demo-app", "web"}: {usage: 0, req: -1}})
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
	mkDays := func(bh, off int64) map[string]map[sleepKey]*sleepWorkloadDay {
		return map[string]map[sleepKey]*sleepWorkloadDay{
			start.Format("2006-01-02"): {
				{"demo-app", "web"}: {bhCPU: bh, offCPU: off, bhMeasured: true, offMeasured: true},
			},
			start.AddDate(0, 0, 1).Format("2006-01-02"): {
				{"demo-app", "web"}: {bhCPU: bh, offCPU: off, bhMeasured: true, offMeasured: true},
			},
		}
	}
	days := mkDays(300, 50)

	// Default-order floors: BH-active (300 >= 10) but outside 50 >= 10 breaks.
	_, _, match, known := evalSleepSignature(days, start, end, 10, 10, nil, nil, nil)
	assert.True(t, known)
	assert.False(t, match, "outside 50 must break the signature at floor 10")

	// Non-default outside floor 100: outside 50 < 100, BH 300 >= 10 → match.
	_, _, match, known = evalSleepSignature(days, start, end, 10, 100, nil, nil, nil)
	assert.True(t, known)
	assert.True(t, match, "non-default outside floor must admit outside 50")

	// Non-default BH floor 500: 300 < 500 → not active in hours.
	_, _, match, known = evalSleepSignature(days, start, end, 500, 100, nil, nil, nil)
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
	seedSleepSnapshot(t, ctx, pool, org, sHC8, hcTierNonProd, time.Now().UTC().Add(-30*time.Minute))
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
	seedSleepSnapshot(t, ctx, pool, org, sHC1, "", time.Now().UTC().Add(-30*time.Minute))
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

// nightOnlyHosted is #761's (V15-11) audit fixture: the BH-signature web
// workload PLUS an exclusively overnight CronJob with all-hours data and NO
// BH row — the producer's normal shape for a workload with zero rows during
// business hours (GroupCSVRowsForStream drops weight-≤0 rows when
// off_hours_weight=0). Distinct values (web 300, night 2000) so a
// mis-attributed day cannot pass by coincidence.
func nightOnlyHosted() map[int]map[nswl]sleepDay {
	spec := bhSignatureHosted(300, 0)
	for i := 0; i < 14; i++ {
		spec[i][nswl{"batch-jobs", "night-cron"}] = sleepDay{off: 2000, bhAbsent: true}
	}
	return spec
}

// TestSleepCandidate_NightOnlyNoBHRowIsUnknown is #761's server repro: a
// user workload with all-hours data and no BH row must make the day
// unknown — never silently skipped while the web sibling establishes the
// day's coverage. Baseline: the night job's `continue` hid its
// outside=2000 from the safety test and high-confidence sleep advice fired
// for a cluster whose night job runs exactly in the hours the advisory
// would sleep away.
func TestSleepCandidate_NightOnlyNoBHRowIsUnknown(t *testing.T) {
	org := testutil.TestOrgID + "-s-761"
	pool := sleepFixture(t, org, sHC5, hcTierNonProd, nightOnlyHosted(), -1, true)
	ctx := context.Background()
	fired, err := RunSleepCandidateCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired,
		"a night-only user workload with no BH row must make the day unknown (#761)")
	assert.Empty(t, sleepAdvisories(t, ctx, pool, org),
		"sleep advice must not fire while a user workload's off-hours split is unknowable (#761)")
}

// Control: the night job ALONE (no sibling to cover the day) is silent on
// both the baseline and the fixed tree. On the baseline the mechanism is
// dayBHActive: the skipped night job never establishes in-hours activity
// (the platform row cannot); post-fix the per-workload gate also returns
// unknown. Pinned so the outcome is not mistaken for the fix's mechanism.
func TestSleepCandidate_NightOnlyAloneIsSilent(t *testing.T) {
	org := testutil.TestOrgID + "-s-761-alone"
	spec := map[int]map[nswl]sleepDay{}
	for i := 0; i < 14; i++ {
		spec[i] = map[nswl]sleepDay{
			{"batch-jobs", "night-cron"}: {off: 2000, bhAbsent: true},
			{"kube-system", "coredns"}:   {bh: 2000, off: 100},
		}
	}
	pool := sleepFixture(t, org, sHC6, hcTierNonProd, spec, -1, true)
	ctx := context.Background()
	fired, err := RunSleepCandidateCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired, "the night job alone cannot establish in-hours activity (dayBHActive), and post-fix the gate reads unknown")
	assert.Empty(t, sleepAdvisories(t, ctx, pool, org))
}

// Control: a SYSTEM-namespace workload with a missing BH row stays skipped —
// the platform exclusion is preserved and a valid sleep candidate still
// fires. (The seeder's platform rows carry both streams; this makes one's
// BH row absent.)
func TestSleepCandidate_SystemMissingBHRowStillFires(t *testing.T) {
	org := testutil.TestOrgID + "-s-761-sys"
	spec := bhSignatureHosted(300, 0)
	for i := 0; i < 14; i++ {
		spec[i][nswl{"kube-system", "coredns"}] = sleepDay{off: 2100, bhAbsent: true}
	}
	pool := sleepFixture(t, org, sHC7, hcTierNonProd, spec, -1, true)
	ctx := context.Background()
	fired, err := RunSleepCandidateCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 1, fired,
		"a system workload's stream shape is irrelevant — the exclusion is preserved (#761)")
	require.Len(t, sleepAdvisories(t, ctx, pool, org), 1)
}

// Control: the reverse shape — a user workload with a BH row and NO
// all-hours row — also reads unknown after the fix (structurally
// near-impossible from this producer, handled conservatively for free).
func TestSleepCandidate_BHOnlyUserWorkloadIsUnknown(t *testing.T) {
	org := testutil.TestOrgID + "-s-761-bhonly"
	spec := bhSignatureHosted(300, 0)
	for i := 0; i < 14; i++ {
		spec[i][nswl{"demo-app", "bh-only"}] = sleepDay{bh: 250, offAbsent: true}
	}
	pool := sleepFixture(t, org, sHC8, hcTierNonProd, spec, -1, true)
	ctx := context.Background()
	fired, err := RunSleepCandidateCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired,
		"a user workload with a BH row and no all-hours row must read unknown (#761)")
	assert.Empty(t, sleepAdvisories(t, ctx, pool, org))
}

// TestSleepCandidate_ConstantUsageIsNotOutsideQuiet is #760's (V15-10)
// server repro: a workload using 300m CONSTANTLY — all-hours p50=300 and BH
// p50=300, exactly what the canonical digest builders yield for constant
// hourly samples including every night — must not receive safe-sleep
// advice. The baseline subtracted the percentiles: outside = 300 - 300 = 0,
// and high-confidence schedule_nonprod_sleep fired for a cluster that
// never sleeps.
func TestSleepCandidate_ConstantUsageIsNotOutsideQuiet(t *testing.T) {
	org := testutil.TestOrgID + "-s-760"
	// Constant 300m usage: BOTH legs are 300 (the canonical builders' p50s
	// of constant hourly samples) — the shape the subtraction model could
	// not express (it read outside = 300 - 300 = 0).
	pool := sleepFixture(t, org, sHC4, hcTierNonProd, bhSignatureHosted(300, 300), -1, true)
	ctx := context.Background()
	fired, err := RunSleepCandidateCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired,
		"a constant 300m workload runs all night — the all-minus-bh percentile subtraction read outside=0 (#760)")
	assert.Empty(t, sleepAdvisories(t, ctx, pool, org),
		"safe-sleep advice must not fire while the workload's off-hours p50 is 300m (#760)")
}

// weekdayProducerHosted builds 14 days under a Mon-Fri schedule with the
// PRODUCER's true shape: weekdays carry both evidence legs; weekends have
// no in-window hours, so the producer emits no BH evidence — off legs only
// (#760: the seeder previously wrote weekend BH rows the producer would
// never emit, masking that weekday schedules could never fire).
func weekdayProducerHosted(bh, off int64) map[int]map[nswl]sleepDay {
	start, _ := sleepWindow()
	spec := map[int]map[nswl]sleepDay{}
	for i := 0; i < 14; i++ {
		d := start.AddDate(0, 0, i)
		weekend := d.UTC().Weekday() == time.Saturday || d.UTC().Weekday() == time.Sunday
		web := sleepDay{bh: bh, off: off}
		sys := sleepDay{bh: 2000, off: 100}
		if weekend {
			web = sleepDay{off: off, bhAbsent: true}
			sys = sleepDay{off: 100, bhAbsent: true}
		}
		spec[i] = map[nswl]sleepDay{
			{"demo-app", "web"}:        web,
			{"kube-system", "coredns"}: sys,
		}
	}
	return spec
}

// TestSleepCandidate_WeekdayScheduleWeekendProducerShape (#760): a Mon-Fri
// schedule's weekends have no in-window hours — the producer emits no BH
// evidence those days. The BH-activity requirement is vacuous on weekends
// (no business hours to be active in); the off leg still gates safety. The
// baseline (and the pre-#760 digest model) read those days as unknown and
// the sleep rule never fired for weekday schedules at all.
func TestSleepCandidate_WeekdayScheduleWeekendProducerShape(t *testing.T) {
	org := testutil.TestOrgID + "-s-760-wkday"
	pool := sleepFixture(t, org, sHC1, hcTierNonProd, weekdayProducerHosted(300, 0), -1, true)
	ctx := context.Background()
	fired, err := RunSleepCandidateCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 1, fired,
		"a weekday schedule with weekend off-only evidence must still fire — weekends have no business hours (#760)")
	advs := sleepAdvisories(t, ctx, pool, org)
	require.Len(t, advs, 1)
	assert.Equal(t, "high", advs[0]["confidence"])
}

// Control: weekend off-window activity still vetoes — the advice sleeps
// those hours, so the off leg gates them (#760).
func TestSleepCandidate_WeekendActivityVetoes(t *testing.T) {
	org := testutil.TestOrgID + "-s-760-wkend"
	pool := sleepFixture(t, org, sHC2, hcTierNonProd, weekdayProducerHosted(300, 200), -1, true)
	ctx := context.Background()
	fired, err := RunSleepCandidateCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired,
		"weekend off-window activity must veto — the advice sleeps those hours (#760)")
	assert.Empty(t, sleepAdvisories(t, ctx, pool, org))
}

// v16NightBurstCSV builds 14 days of hourly container rows: 300m during
// 09:00-17:00 and 2000m from 00:00-06:00 — #826's night-burst shape. The
// off-window p50 is 0 (10 of 16 off-hours idle) while the p95 is 2000.
func v16NightBurstCSV(start time.Time) string {
	var b strings.Builder
	b.WriteString("interval_start,interval_end,namespace,pod,workload,workload_type,container_name,cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg,memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count\n")
	for day := start; day.Before(start.AddDate(0, 0, 14)); day = day.AddDate(0, 0, 1) {
		for h := 0; h < 24; h++ {
			cpu := 0.0
			if h >= 9 && h < 17 {
				cpu = 0.3
			}
			if h < 6 {
				cpu = 2
			}
			ts := day.Add(time.Duration(h) * time.Hour)
			fmt.Fprintf(&b, "%s,%s,user-app,pod,web,Deployment,c0,0.3,3,%.3f,0,1048576,1048576,1048576,1048576,0\n",
				ts.Format("2006-01-02 15:04:05 +0000 UTC"), ts.Add(time.Hour).Format("2006-01-02 15:04:05 +0000 UTC"), cpu)
		}
	}
	return b.String()
}

// v16AlternatingCSV builds 14 days with TWO containers of one workload,
// each active (2000m) for a different 7 of the 16 off-hours: the workload
// is active 14/16 off-hours while each container's off-p50 is 0.
func v16AlternatingCSV(start time.Time) string {
	var b strings.Builder
	b.WriteString("interval_start,interval_end,namespace,pod,workload,workload_type,container_name,cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg,memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count\n")
	for day := start; day.Before(start.AddDate(0, 0, 14)); day = day.AddDate(0, 0, 1) {
		for h := 0; h < 24; h++ {
			for c := 0; c < 2; c++ {
				cpu := 0.0
				if h >= 9 && h < 17 {
					cpu = 0.3
				} else {
					offIndex := h
					if h >= 17 {
						offIndex = h - 8
					}
					if (c == 0 && offIndex < 7) || (c == 1 && offIndex >= 7 && offIndex < 14) {
						cpu = 2
					}
				}
				ts := day.Add(time.Duration(h) * time.Hour)
				fmt.Fprintf(&b, "%s,%s,user-app,pod,web,Deployment,c%d,0.3,3,%.3f,0,1048576,1048576,1048576,1048576,0\n",
					ts.Format("2006-01-02 15:04:05 +0000 UTC"), ts.Add(time.Hour).Format("2006-01-02 15:04:05 +0000 UTC"), c, cpu)
			}
		}
	}
	return b.String()
}

// v16RunRawSleepCycle ingests the CSV through the canonical path and runs
// the sleep cycle for one declared non-prod HC on a Mon-Sun schedule.
func v16RunRawSleepCycle(t *testing.T, csvText string) []map[string]any {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	start, _ := sleepWindow()
	org := "v16-raw-" + t.Name()
	require.NoError(t, ingestion.EnsureSLOPartitionsForMonth(ctx, pool, start))
	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: org, ClusterUUID: sHC1, Timezone: "UTC",
		Days:      []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"},
		StartTime: "09:00", EndTime: "17:00", Enabled: true,
	}))
	_, err := ingestion.ParseAndDigestCSV(ctx, pool, strings.NewReader(csvText), org, sHC1)
	require.NoError(t, err)
	seedSleepSnapshot(t, ctx, pool, org, sHC1, hcTierNonProd, time.Now().UTC().Add(-time.Hour))
	seedNodepoolRollup(t, ctx, pool, org, sHC1)
	_, err = RunSleepCandidateCycle(ctx, pool)
	require.NoError(t, err)
	return sleepAdvisories(t, ctx, pool, org)
}

// TestSleepCandidate_NightBurstIsVetoed is #826's end-to-end repro: a
// workload running 2000m from 00:00-06:00 every night must NOT receive
// sleep advice. Baseline: the off-leg p50 is 0 (10 of 16 off-hours idle),
// the summed evidence reads outside=0, and high-confidence advice fired.
func TestSleepCandidate_NightBurstIsVetoed(t *testing.T) {
	start, _ := sleepWindow()
	advs := v16RunRawSleepCycle(t, v16NightBurstCSV(start))
	assert.Empty(t, advs,
		"sustained nightly activity (2000m for 6 of 16 off-hours) must veto — the off-leg p50 hid it (#826)")
}

// TestSleepCandidate_AlternatingContainersAreVetoed is #826's second
// repro: two containers alternating across the off-window leave the
// workload active 14/16 off-hours while each container's off-p50 is 0.
func TestSleepCandidate_AlternatingContainersAreVetoed(t *testing.T) {
	start, _ := sleepWindow()
	advs := v16RunRawSleepCycle(t, v16AlternatingCSV(start))
	assert.Empty(t, advs,
		"alternating-container off-window activity must veto — per-container p50s summed to zero (#826)")
}

// TestSleepCandidate_PredicateReadsOffP95 pins the predicate grain with
// DISTINCT values: an off leg seeded p50=0 / p95=200 must veto — a
// same-value fixture could not detect the predicate reading the p50 column.
func TestSleepCandidate_PredicateReadsOffP95(t *testing.T) {
	org := testutil.TestOrgID + "-s-826-p95"
	p95 := int64(200)
	spec := bhSignatureHosted(300, 0)
	for i := 0; i < 14; i++ {
		day := spec[i][nswl{"demo-app", "web"}]
		day.offP95 = &p95
		spec[i][nswl{"demo-app", "web"}] = day
	}
	pool := sleepFixture(t, org, sHC1, hcTierNonProd, spec, -1, true)
	ctx := context.Background()
	fired, err := RunSleepCandidateCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired,
		"the off-leg p95 (200m) must drive the veto — a predicate reading off_cpu_p50_mc (0) fires instead (#826)")
	assert.Empty(t, sleepAdvisories(t, ctx, pool, org))
}

// TestSleepCandidate_PartialMigrationReadsUnknown (#826 review F1): a
// workload-day whose containers are SPLIT between migrated rows (p95 set,
// quiet) and legacy rows (p95 NULL, off-p50 carrying the activity) must
// read UNMEASURED — the #761 gate — never let the quiet sibling mask the
// legacy row's hidden activity. Seeded directly: c0 migrated (p95=0),
// c1 legacy (p50=2000, p95 NULL).
func TestSleepCandidate_PartialMigrationReadsUnknown(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	org := testutil.TestOrgID + "-s-826-partial"
	start, _ := sleepWindow()
	seedSleepScheduleRow(t, ctx, pool, org, sHC1)
	seedSleepSnapshot(t, ctx, pool, org, sHC1, hcTierNonProd, time.Now().UTC().Add(-time.Hour))
	seedNodepoolRollup(t, ctx, pool, org, sHC1)
	for i := 0; i < 14; i++ {
		day := start.AddDate(0, 0, i).Format("2006-01-02")
		// The quiet BH-active web workload (both legs measured).
		seedEvidenceRow(t, ctx, pool, org, day, "web", "c0", ptr(int64(300)), ptr(int64(0)), ptr(int64(0)))
		// The split workload: c0 migrated and quiet, c1 legacy with its
		// activity living only in the pre-#826 p50 column.
		seedEvidenceRow(t, ctx, pool, org, day, "rotator", "c0", ptr(int64(50)), ptr(int64(0)), ptr(int64(0)))
		seedEvidenceRow(t, ctx, pool, org, day, "rotator", "c1", ptr(int64(50)), ptr(int64(2000)), nil)
	}
	fired, err := RunSleepCandidateCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired,
		"a partially-migrated workload-day must read unmeasured (#761 gate) — the quiet migrated container must not mask the legacy row's activity (#826)")
	assert.Empty(t, sleepAdvisories(t, ctx, pool, org))
}

// TestSleepCandidate_LegacyRowsReadUnknown (#826 review, cold start): a
// fully legacy evidence set (no p95 anywhere) reads unknown — silence, the
// #760-accepted cold start — never zero-activity.
func TestSleepCandidate_LegacyRowsReadUnknown(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	org := testutil.TestOrgID + "-s-826-legacy"
	start, _ := sleepWindow()
	seedSleepScheduleRow(t, ctx, pool, org, sHC1)
	seedSleepSnapshot(t, ctx, pool, org, sHC1, hcTierNonProd, time.Now().UTC().Add(-time.Hour))
	seedNodepoolRollup(t, ctx, pool, org, sHC1)
	for i := 0; i < 14; i++ {
		day := start.AddDate(0, 0, i).Format("2006-01-02")
		seedEvidenceRow(t, ctx, pool, org, day, "web", "c0", ptr(int64(300)), ptr(int64(0)), nil)
	}
	fired, err := RunSleepCandidateCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired, "legacy rows (no p95) must read unknown — cold-start silence, never zero-activity")
	assert.Empty(t, sleepAdvisories(t, ctx, pool, org))
}

func ptr(v int64) *int64 { return &v }

func seedSleepScheduleRow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, cluster string) {
	t.Helper()
	require.NoError(t, bhschedule.UpsertSchedule(ctx, pool, bhschedule.Schedule{
		OrgID: orgID, ClusterUUID: cluster, Timezone: "UTC",
		Days:      []string{"monday", "tuesday", "wednesday", "thursday", "friday", "saturday", "sunday"},
		StartTime: "09:00", EndTime: "17:00", Enabled: true,
	}))
}

func seedEvidenceRow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, day, wl, cn string, bhP50, offP50, offP95 *int64) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO hcp_sleep_evidence (
			org_id, cluster_uuid, namespace, workload, workload_type,
			container_name, bucket_date, bh_cpu_p50_mc, off_cpu_p50_mc, off_cpu_p95_mc
		) VALUES ($1,$2,$3,$4,'Deployment',$5,$6,$7,$8,$9)
		ON CONFLICT (org_id, cluster_uuid, namespace, workload, workload_type, container_name, bucket_date)
		DO UPDATE SET bh_cpu_p50_mc = EXCLUDED.bh_cpu_p50_mc,
			off_cpu_p50_mc = EXCLUDED.off_cpu_p50_mc,
			off_cpu_p95_mc = EXCLUDED.off_cpu_p95_mc`,
		orgID, sHC1, "demo-app", wl, cn, day, bhP50, offP50, offP95)
	require.NoError(t, err)
}

// TestSleepCandidate_RawCSVQuietFires (#826 review F5): the quiet
// positive control through the canonical raw-CSV path — the seeded controls
// express levels; this proves the computed p95. 300m in BH, 0 outside →
// off-p95 = 0 → fires. (Split from the constant control: SetupTestDB's
// shared-pool mutex is held per test, so one test cannot run two cycles.)
func TestSleepCandidate_RawCSVQuietFires(t *testing.T) {
	start, _ := sleepWindow()
	quiet := rawSleepCSV(start, func(h int) float64 {
		if h >= 9 && h < 17 {
			return 0.3
		}
		return 0
	})
	advs := v16RunRawSleepCycle(t, quiet)
	require.Len(t, advs, 1, "the quiet raw-CSV control must still fire (off-p95=0)")
	assert.Equal(t, "high", advs[0]["confidence"])
}

// TestSleepCandidate_RawCSVConstantVetoed: constant 300m activity —
// off-p95 = 300 ≥ floor → vetoed, through the canonical raw-CSV path.
func TestSleepCandidate_RawCSVConstantVetoed(t *testing.T) {
	start, _ := sleepWindow()
	constant := rawSleepCSV(start, func(h int) float64 { return 0.3 })
	advs := v16RunRawSleepCycle(t, constant)
	assert.Empty(t, advs, "constant 300m activity must stay vetoed (off-p95=300 ≥ floor)")
}

// rawSleepCSV builds 14 days of single-container hourly rows from a usage
// function of the hour.
func rawSleepCSV(start time.Time, usage func(h int) float64) string {
	var b strings.Builder
	b.WriteString("interval_start,interval_end,namespace,pod,workload,workload_type,container_name,cpu_request_container_avg,cpu_limit_container_avg,cpu_usage_container_avg,cpu_throttle_container_avg,memory_request_container_avg,memory_limit_container_avg,memory_usage_container_avg,memory_rss_usage_container_avg,oom_count\n")
	for day := start; day.Before(start.AddDate(0, 0, 14)); day = day.AddDate(0, 0, 1) {
		for h := 0; h < 24; h++ {
			ts := day.Add(time.Duration(h) * time.Hour)
			fmt.Fprintf(&b, "%s,%s,user-app,pod,web,Deployment,c0,0.3,3,%.3f,0,1048576,1048576,1048576,1048576,0\n",
				ts.Format("2006-01-02 15:04:05 +0000 UTC"), ts.Add(time.Hour).Format("2006-01-02 15:04:05 +0000 UTC"), usage(h))
		}
	}
	return b.String()
}
