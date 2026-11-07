package correlate

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
	"github.com/redhatinsights/ros-ocp-backend/librobne/topology"
)

const corrMgmt = "22222222-2222-2222-2222-222222222222"
const corrNS = "clusters-hc9"

var corrLEs = []float64{0.005, 0.025, 0.05, 0.1, 0.2, 0.4, 0.8, 1.5, 3, 6, 15, 60}

// seedBucketWindow writes one hourly window of cumulative bucket snapshots
// for verb group mutating with the p99 landing at peakLE, at two collection
// seedBucketWindow writes one hourly window as a single cumulative snapshot
// with the p99 landing at peakLE. Storage upserts collapse each window to its
// latest per (verb, le), so one instant per window is both necessary and
// sufficient; callers seed consecutive windows for deltas to form. Counts
// concentrate ~99% mass at the peak boundary so interpolation lands inside it.
func seedBucketWindow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, hc string, windowStart time.Time, peakLE, peakCount float64) {
	t.Helper()
	// Month-boundary safety: windows near month start reach into the prior
	// month, whose partition the migration-time pre-creation may not cover.
	require.NoError(t, ingestion.EnsureSLOPartitionsForMonth(ctx, pool, windowStart))
	collected := windowStart.Add(50 * time.Minute)
	cum := 0.0
	for _, le := range corrLEs {
		d := 1.0
		if le == peakLE {
			d = peakCount
		}
		cum += d
		_, err := pool.Exec(ctx, `
				INSERT INTO hosted_api_bucket_rollups (
					window_start, window_end, org_id, cluster_uuid, hc_cluster_id,
					verb_group, le, bucket_count, collected_at, source
				) VALUES ($1, $2, $3, $4, $5, 'mutating', $6, $7, $8, 'kubernetes')
				ON CONFLICT (org_id, cluster_uuid, hc_cluster_id, window_start, window_end, verb_group, le, source)
				DO UPDATE SET bucket_count = EXCLUDED.bucket_count, collected_at = EXCLUDED.collected_at`,
			windowStart.UTC(), windowStart.Add(time.Hour).UTC(), orgID, hc, hc,
			le, int64(cum), collected.UTC())
		require.NoError(t, err)
	}
}

// seedWindowPair writes a previous window (low peak, given count) plus the
// target window so cross-window deltas form. Storage collapses each window
// to its latest snapshot, so pairing is structural, not incidental.
func seedWindowPair(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, hc string, windowStart time.Time, peakLE, base float64) {
	t.Helper()
	// Consecutive snapshots MUST grow like production cumulative counters:
	// identical levels delta to zero and correctly read as no measurable
	// traffic. base scales with recency so cross-day pairs stay positive.
	seedBucketWindow(t, ctx, pool, orgID, hc, windowStart.Add(-time.Hour), peakLE, base)
	seedBucketWindow(t, ctx, pool, orgID, hc, windowStart, peakLE, base*1.05)
}

// seedBaselineWeek writes 7 daily low-pain windows (p99 ~0.03) for the
// threshold baseline, each paired with its previous window.
func seedBaselineWeek(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, hc string, fireStart time.Time) {
	t.Helper()
	for d := 1; d <= 7; d++ {
		dayStart := fireStart.AddDate(0, 0, -d)
		seedWindowPair(t, ctx, pool, orgID, hc, dayStart, 0.025, 1000*(1+0.1*float64(8-d)))
	}
}

func seedHCManagement(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, hc string) {
	t.Helper()
	now := time.Now().UTC()
	require.NoError(t, pgrec.EnsureIngestClusterRow(ctx, pool, orgID, "src-corr", corrMgmt, corrMgmt, now))
	require.NoError(t, pgrec.UpdateHCPNamespaces(ctx, pool, orgID, "src-corr", corrMgmt, []string{corrNS}))
	require.NoError(t, pgrec.UpsertHCPSnapshots(ctx, pool, orgID, corrMgmt, "m-corr",
		[]topology.HCPSnapshotEntry{{
			HCPNamespace: corrNS, HostedClusterID: hc, HcUID: "uid-" + hc,
			ObservedAt: now.Format(time.RFC3339), Complete: true,
		}}))
	testutil.SeedContainerDigest(t, pool, testutil.ContainerDigestRow{
		BucketDate: now.AddDate(0, 0, -1), OrgID: orgID, ClusterUUID: corrMgmt,
		Namespace: corrNS, Workload: "etcd", WorkloadType: "statefulset", ContainerName: "etcd",
		CPURequestP50MC: 1000, CPUUsageP95MC: 950,
	})
}

func seedNodeRecs(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, hc string, codes string) {
	t.Helper()
	_, err := pool.Exec(ctx, `
		INSERT INTO node_recommendations (org_id, cluster_uuid, node, term, engine, notification_codes, updated_at)
		VALUES ($1, $2, 'worker-0', 'short', 'cost', $3::smallint[], now()), ($1, $2, 'worker-1', 'short', 'cost', '{}', now())
		ON CONFLICT (org_id, cluster_uuid, node, term, engine)
		DO UPDATE SET notification_codes = EXCLUDED.notification_codes, updated_at = now()`,
		orgID, hc, codes)
	require.NoError(t, err)
}

func corrFireWindow() time.Time {
	end := time.Now().UTC().Truncate(time.Hour)
	return end.Add(-time.Hour)
}

func corrAdvisories(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, hc string) []map[string]interface{} {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT verdict, confidence, h_p99_s, h_threshold_s, c_ratio, expires_at > now() AS live
		FROM hcp_correlation_advisories WHERE org_id = $1 AND hc_cluster_id = $2`,
		orgID, hc)
	require.NoError(t, err)
	defer rows.Close()
	var out []map[string]interface{}
	for rows.Next() {
		var verdict, confidence string
		var p99, threshold, ratio float64
		var live bool
		require.NoError(t, rows.Scan(&verdict, &confidence, &p99, &threshold, &ratio, &live))
		out = append(out, map[string]interface{}{
			"verdict": verdict, "confidence": confidence,
			"p99": p99, "threshold": threshold, "ratio": ratio, "live": live,
		})
	}
	require.NoError(t, rows.Err())
	return out
}

// TestCorrelator_FiresOnPainfulCP CalmWorkers is the positive lab control:
// high hosted p99 + stressed CP + calm workers emits one advisory with evidence.
func TestCorrelator_FiresOnPainfulCPCalmWorkers(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testutil.TestOrgID + "-corr-fire"
	hc := "aaaaaaaa-1111-1111-1111-111111111111"
	start := corrFireWindow()

	seedWindowPair(t, ctx, pool, orgID, hc, start, 0.8, 2000)
	seedBaselineWeek(t, ctx, pool, orgID, hc, start)
	seedHCManagement(t, ctx, pool, orgID, hc)
	seedNodeRecs(t, ctx, pool, orgID, hc, "{}")

	res, err := RunCycle(ctx, pool)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, res.Fired, 1, "painful CP with calm workers must fire")

	advs := corrAdvisories(t, ctx, pool, orgID, hc)
	require.Len(t, advs, 1)
	assert.Equal(t, "do_not_add_workers_first", advs[0]["verdict"])
	assert.Equal(t, "high", advs[0]["confidence"])
	assert.Greater(t, advs[0]["p99"], advs[0]["threshold"], "evidence must show pain above threshold")
	assert.Equal(t, true, advs[0]["live"], "advisory must not be expired at write")
}

// TestCorrelator_SilentWhenWorkersPressured is the negative control H && N:
// identical pain with hot workers emits nothing (normal node advice stands).
func TestCorrelator_SilentWhenWorkersPressured(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testutil.TestOrgID + "-corr-hn"
	hc := "bbbbbbbb-2222-2222-2222-222222222222"
	start := corrFireWindow()

	seedWindowPair(t, ctx, pool, orgID, hc, start, 0.8, 2000)
	seedBaselineWeek(t, ctx, pool, orgID, hc, start)
	seedHCManagement(t, ctx, pool, orgID, hc)
	seedNodeRecs(t, ctx, pool, orgID, hc, "{12}")

	res, err := RunCycle(ctx, pool)
	require.NoError(t, err)
	assert.Empty(t, corrAdvisories(t, ctx, pool, orgID, hc), "H && N must stay silent")
	_ = res
}

// TestCorrelator_SilentWithoutEvidence proves absence paths: an HC with no
// buckets at all yields no advisory and no error.
func TestCorrelator_SilentWithoutEvidence(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testutil.TestOrgID + "-corr-empty"

	res, err := RunCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, res.Fired)
	assert.Empty(t, corrAdvisories(t, ctx, pool, orgID, "cccccccc-3333-3333-3333-333333333333"))
}

// TestCorrelator_PreMigrationDegrades proves code ahead of the SLO tables
// degrades to an empty silent run instead of error-looping hourly.
func TestCorrelator_PreMigrationDegrades(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test (requires testcontainers/Docker)")
	}
	ctx := context.Background()
	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("migrate_test"),
		postgres.WithUsername("postgres"),
		postgres.WithPassword("postgres"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pgContainer.Terminate(ctx) })
	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	m, err := migrate.New("file://"+testutil.MigrationsPath(), connStr)
	require.NoError(t, err)
	require.NoError(t, m.Migrate(200))
	srcErr, dbErr := m.Close()
	require.NoError(t, srcErr)
	require.NoError(t, dbErr)

	pool, err := pgxpool.New(ctx, connStr)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	res, err := RunCycle(ctx, pool)
	require.NoError(t, err, "pre-SLO-tables run must degrade, not error")
	assert.Equal(t, CycleResult{}, res, "pre-migration run emits nothing and counts nothing")
}

// TestCorrelator_SweepsExpired proves each run self-cleans lapsed advisories.
func TestCorrelator_SweepsExpired(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testutil.TestOrgID + "-corr-sweep"
	hc := "dddddddd-4444-4444-4444-444444444444"

	_, err := pool.Exec(ctx, `
		INSERT INTO hcp_correlation_advisories (
			org_id, hc_cluster_id, management_cluster_uuid,
			window_start, window_end, expires_at
		) VALUES ($1, $2, $3, now() - interval '30 hours', now() - interval '29 hours', now() - interval '5 hours')`,
		orgID, hc, corrMgmt)
	require.NoError(t, err)

	_, err = RunCycle(ctx, pool)
	require.NoError(t, err)
	assert.Empty(t, corrAdvisories(t, ctx, pool, orgID, hc), "expired advisories must be swept")
}

// seedBucketWindowSource is seedBucketWindow with an explicit source (#762):
// the non-gating metrics-server snapshots for the mixing repro.
func seedBucketWindowSource(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, hc, source string, windowStart time.Time, peakLE, peakCount, countScale float64) {
	t.Helper()
	seedBucketWindowSourceAt(t, ctx, pool, orgID, hc, source, windowStart, peakLE, peakCount, countScale, windowStart.Add(50*time.Minute))
}

// seedBucketWindowSourceAt: the explicit collected timestamp — the #762 repro
// needs the non-gating source LATER than kubernetes (the original latest-wins
// replaces on After; equal collected times keep the first-seen row and mask
// the mechanism).
func seedBucketWindowSourceAt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, hc, source string, windowStart time.Time, peakLE, peakCount, countScale float64, collected time.Time) {
	t.Helper()
	require.NoError(t, ingestion.EnsureSLOPartitionsForMonth(ctx, pool, windowStart))
	cum := 0.0
	for _, le := range corrLEs {
		d := countScale
		if le == peakLE {
			d = peakCount
		}
		cum += d
		_, err := pool.Exec(ctx, `
				INSERT INTO hosted_api_bucket_rollups (
					window_start, window_end, org_id, cluster_uuid, hc_cluster_id,
					verb_group, le, bucket_count, collected_at, source
				) VALUES ($1, $2, $3, $4, $5, 'mutating', $6, $7, $8, $9)
				ON CONFLICT (org_id, cluster_uuid, hc_cluster_id, window_start, window_end, verb_group, le, source)
				DO UPDATE SET bucket_count = EXCLUDED.bucket_count, collected_at = EXCLUDED.collected_at`,
			windowStart.UTC(), windowStart.Add(time.Hour).UTC(), orgID, hc, hc,
			le, int64(cum), collected.UTC(), source)
		require.NoError(t, err)
	}
}

// TestCorrelator_NonGatingSourceDoesNotFire is #762's server-side repro: a
// calm Kubernetes window plus a coherent metrics-server snapshot one minute
// later (slow buckets, same verb/bounds) must NOT fire. On the baseline the
// loadBucketSamples/windowP99 key (verb, le) let the metrics-server sample
// replace the Kubernetes one and produced do_not_add_workers_first with the
// audit's numbers (p99 ~2.98 vs threshold ~0.3).
func TestCorrelator_NonGatingSourceDoesNotFire(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testutil.TestOrgID + "-corr-762"
	hc := "dddddddd-4444-4444-4444-444444444444"
	start := corrFireWindow()

	// Calm Kubernetes baseline week + a CALM kubernetes fire window: the
	// volume concentrated at le=0.025 (the seedBaselineWeek shape — the
	// default boundary count is 1, the peak carries the growth).
	seedBaselineWeek(t, ctx, pool, orgID, hc, start)
	seedBucketWindowSource(t, ctx, pool, orgID, hc, "kubernetes", start.Add(-time.Hour), 0.025, 1000, 1.0)
	seedBucketWindowSource(t, ctx, pool, orgID, hc, "kubernetes", start, 0.025, 1050, 1.0)
	// The non-gating source with its own coherent progression (a previous
	// window too) and slow buckets concentrated at le=2.98.
	seedBucketWindowSource(t, ctx, pool, orgID, hc, "metrics-server", start.Add(-time.Hour), 3.0, 500, 1.0)
	seedBucketWindowSourceAt(t, ctx, pool, orgID, hc, "metrics-server", start, 3.0, 2500, 1.0, start.Add(51*time.Minute))
	seedHCManagement(t, ctx, pool, orgID, hc)
	seedNodeRecs(t, ctx, pool, orgID, hc, "{}")

	_, err := RunCycle(ctx, pool)
	require.NoError(t, err)
	assert.Empty(t, corrAdvisories(t, ctx, pool, orgID, hc),
		"a non-gating metrics-server sample must not fire the H leg — the (verb,le) latest-wins key let it replace the Kubernetes sample (#762)")
}

// Reverse timing + genuine-pain control for the server side: the slow
// buckets on the KUBERNETES source (with the metrics-server rows also
// present) must still fire — the gate must not anesthetize.
func TestCorrelator_GenuineKubernetesPainStillFires(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	orgID := testutil.TestOrgID + "-corr-762-real"
	hc := "eeeeeeee-5555-5555-5555-555555555555"
	start := corrFireWindow()

	seedBaselineWeek(t, ctx, pool, orgID, hc, start)
	// Non-gating rows present in the same windows (the mixing temptation).
	seedBucketWindowSource(t, ctx, pool, orgID, hc, "metrics-server", start.Add(-time.Hour), 0.025, 30, 900)
	seedBucketWindowSource(t, ctx, pool, orgID, hc, "metrics-server", start, 0.025, 30, 950)
	// The genuine kubernetes pain.
	seedWindowPair(t, ctx, pool, orgID, hc, start, 0.8, 2000)
	seedHCManagement(t, ctx, pool, orgID, hc)
	seedNodeRecs(t, ctx, pool, orgID, hc, "{}")

	res, err := RunCycle(ctx, pool)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, res.Fired, 1, "genuine Kubernetes pain must fire with the non-gating rows present (#762)")
	advs := corrAdvisories(t, ctx, pool, orgID, hc)
	require.Len(t, advs, 1)
	assert.Equal(t, "do_not_add_workers_first", advs[0]["verdict"])
}
