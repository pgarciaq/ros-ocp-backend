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

// Topology under test mirrors production routing: snapshots + still-on
// digests live under the MANAGEMENT cluster; idle-leg digests live under
// the HOSTED cluster whose UUID is the HC ID (N-leg convention).
const (
	zHC1   = "aaaaaaaa-1111-1111-1111-111111111111"
	zMgmt1 = "11111111-1111-1111-1111-111111111111"
	zMgmt2 = "22222222-2222-2222-2222-222222222222"
	zMgmt3 = "33333333-3333-3333-3333-333333333333"
	zMgmt4 = "44444444-4444-4444-4444-444444444444"
	zMgmt5 = "55555555-5555-5555-5555-555555555555"
	zMgmt6 = "66666666-6666-6666-6666-666666666666"
	zHC2   = "aaaaaaaa-2222-2222-2222-222222222222"
	zHC3   = "aaaaaaaa-3333-3333-3333-333333333333"
)

// zombieWorkloadDay is one seeded workload-day: usage/request in mC
// (request -1 encodes SQL NULL for the unknown-proof leg).
type zombieWorkloadDay struct {
	usage int64
	req   int64
}

type nswl struct {
	ns string
	wl string
}

func zombieWindow() (start, end time.Time) {
	end = time.Now().UTC().Truncate(24 * time.Hour)
	return end.AddDate(0, 0, -zombieWindowDays), end
}

func seedZombieDigestDay(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, cluster, day string, rows map[nswl]zombieWorkloadDay) {
	t.Helper()
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

// seedZombieRun writes 14 days of digest rows. hosted maps day offset to
// workload rows on the HOSTED cluster (idle leg); mgmt maps day offset
// to rows on the MANAGEMENT cluster under assocNS (still-on leg).
func seedZombieRun(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, mgmt, hc, assocNS string, hosted, mgmtRows map[int]map[nswl]zombieWorkloadDay) {
	t.Helper()
	start, _ := zombieWindow()
	months := map[string]time.Time{}
	for i := 0; i < zombieWindowDays; i++ {
		d := start.AddDate(0, 0, i)
		ms := time.Date(d.Year(), d.Month(), 1, 0, 0, 0, 0, time.UTC)
		months[ms.Format("200601")] = ms
	}
	for _, ms := range months {
		require.NoError(t, ingestion.EnsureDigestPartitionMonth(ctx, pool, ms))
	}
	for i := 0; i < zombieWindowDays; i++ {
		day := start.AddDate(0, 0, i).Format("2006-01-02")
		if rows, ok := hosted[i]; ok {
			seedZombieDigestDay(t, ctx, pool, orgID, hc, day, rows)
		}
		if rows, ok := mgmtRows[i]; ok {
			seedZombieDigestDay(t, ctx, pool, orgID, mgmt, day, rows)
		}
	}
	seedZombieSnapshot(t, ctx, pool, orgID, mgmt, hc, assocNS, "z-manifest-"+t.Name(), time.Now().UTC().Add(-time.Hour))
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

// idleHosted builds 14 days of hosted rows: one user workload at
// usage (+ busyDay override) plus busy platform rows every day (prove
// pipeline liveness + platform exclusion).
func idleHosted(usage int64, busyDay map[int]int64) map[int]map[nswl]zombieWorkloadDay {
	spec := map[int]map[nswl]zombieWorkloadDay{}
	for i := 0; i < zombieWindowDays; i++ {
		u := usage
		if b, ok := busyDay[i]; ok {
			u = b
		}
		spec[i] = map[nswl]zombieWorkloadDay{
			{"demo-app", "web"}:                    {usage: u, req: 100},
			{"kube-system", "coredns"}:             {usage: 2000, req: 2000},
			{"koku-metrics-operator", "collector"}: {usage: 5000, req: 5000},
		}
	}
	return spec
}

// mgmtOn builds 14 days of management still-on rows (provisioned CP).
func mgmtOn(req int64) map[int]map[nswl]zombieWorkloadDay {
	spec := map[int]map[nswl]zombieWorkloadDay{}
	for i := 0; i < zombieWindowDays; i++ {
		spec[i] = map[nswl]zombieWorkloadDay{
			{"clusters-hc9", "kube-apiserver"}: {usage: 50, req: req},
		}
	}
	return spec
}

func zombieFixture(t *testing.T, orgID, mgmt, hc, assocNS string, hosted, mgmtRows map[int]map[nswl]zombieWorkloadDay) *pgxpool.Pool {
	t.Helper()
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	seedZombieRun(t, ctx, pool, orgID, mgmt, hc, assocNS, hosted, mgmtRows)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, orgID)
		_, _ = pool.Exec(ctx, `DELETE FROM manifest_hcp_snapshots WHERE org_id = $1`, orgID)
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
		var peak, floor float64
		var signals []string
		require.NoError(t, rows.Scan(&verdict, &conf, &peak, &floor, &signals))
		out = append(out, map[string]any{"verdict": verdict, "confidence": conf, "peak": peak, "floor": floor, "signals": signals})
	}
	require.NoError(t, rows.Err())
	return out
}

func TestZombie_FiresOnTrueZombie(t *testing.T) {
	org := testutil.TestOrgID + "-z-fire"
	pool := zombieFixture(t, org, zMgmt1, zHC1, "clusters-hc9", idleHosted(0, nil), mgmtOn(500))
	ctx := context.Background()
	fired, err := RunZombieCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 1, fired, "user 0-CPU + provisioned CP must fire despite busy platform rows")
	advs := zombieAdvisories(t, ctx, pool, org)
	require.Len(t, advs, 1)
	assert.Equal(t, "high", advs[0]["confidence"])
	assert.Equal(t, 0.0, advs[0]["peak"])
	assert.Equal(t, 10.0, advs[0]["floor"])
	assert.Contains(t, advs[0]["signals"].([]string)[0], zHC1)
}

func TestZombie_SilentAlive(t *testing.T) {
	org := testutil.TestOrgID + "-z-alive"
	pool := zombieFixture(t, org, zMgmt2, zHC2, "clusters-hc9", idleHosted(500, nil), mgmtOn(500))
	ctx := context.Background()
	fired, err := RunZombieCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired)
	assert.Empty(t, zombieAdvisories(t, ctx, pool, org))
}

func TestZombie_SilentSpikeDay(t *testing.T) {
	org := testutil.TestOrgID + "-z-spike"
	pool := zombieFixture(t, org, zMgmt3, zHC3, "clusters-hc9", idleHosted(0, map[int]int64{5: 800}), mgmtOn(500))
	ctx := context.Background()
	fired, err := RunZombieCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired, "one busy day vetoes")
	assert.Empty(t, zombieAdvisories(t, ctx, pool, org))
}

func TestZombie_SilentMissingDays(t *testing.T) {
	org := testutil.TestOrgID + "-z-grace"
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	fullHosted := idleHosted(0, nil)
	fullMgmt := mgmtOn(500)
	partialH, partialM := map[int]map[nswl]zombieWorkloadDay{}, map[int]map[nswl]zombieWorkloadDay{}
	for i := 10; i < zombieWindowDays; i++ {
		partialH[i] = fullHosted[i]
		partialM[i] = fullMgmt[i]
	}
	hc := "bbbbbbbb-4444-4444-4444-444444444444"
	seedZombieRun(t, ctx, pool, org, zMgmt4, hc, "clusters-hc9", partialH, partialM)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM daily_container_digests WHERE org_id = $1`, org)
		_, _ = pool.Exec(ctx, `DELETE FROM manifest_hcp_snapshots WHERE org_id = $1`, org)
	})
	fired, err := RunZombieCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired, "10/14 days is unknown, never proof of idleness")
	assert.Empty(t, zombieAdvisories(t, ctx, pool, org))
}

func TestZombie_MediumWithoutMgmtProof(t *testing.T) {
	org := testutil.TestOrgID + "-z-medium"
	// Requests NULL on management: usage measurable nowhere there, so CP
	// proof is unknown while hosted idle is known.
	nullMgmt := map[int]map[nswl]zombieWorkloadDay{}
	for i := 0; i < zombieWindowDays; i++ {
		nullMgmt[i] = map[nswl]zombieWorkloadDay{
			{"clusters-hc9", "kube-apiserver"}: {usage: 50, req: -1},
		}
	}
	hc := "cccccccc-5555-5555-5555-555555555555"
	pool := zombieFixture(t, org, zMgmt5, hc, "clusters-hc9", idleHosted(0, nil), nullMgmt)
	ctx := context.Background()
	fired, err := RunZombieCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 1, fired, "snapshot+idle without CP proof fires medium, never silent-high")
	advs := zombieAdvisories(t, ctx, pool, org)
	require.Len(t, advs, 1)
	assert.Equal(t, "medium", advs[0]["confidence"])
}

func TestZombie_SilentZeroCP(t *testing.T) {
	org := testutil.TestOrgID + "-z-zerocp"
	zeroMgmt := map[int]map[nswl]zombieWorkloadDay{}
	for i := 0; i < zombieWindowDays; i++ {
		zeroMgmt[i] = map[nswl]zombieWorkloadDay{
			{"clusters-hc9", "kube-apiserver"}: {usage: 0, req: 0},
		}
	}
	hc := "dddddddd-6666-6666-6666-666666666666"
	pool := zombieFixture(t, org, zMgmt6, hc, "clusters-hc9", idleHosted(0, nil), zeroMgmt)
	ctx := context.Background()
	fired, err := RunZombieCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired, "CP requests present-but-zero is known-not-on: silence, not medium")
	assert.Empty(t, zombieAdvisories(t, ctx, pool, org))
}

func TestZombie_SilentWithoutEvidence(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	fired, err := RunZombieCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired)
}

func TestIsZombieUserNamespace(t *testing.T) {
	assert.True(t, isZombieUserNamespace("demo-app", nil, nil))
	assert.True(t, isZombieUserNamespace("default", nil, nil), "default/ is user space, never excluded")
	assert.True(t, isZombieUserNamespace("kubevirt-demo", nil, nil), "kubevirt lacks the kube- dash: user, not platform")
	assert.True(t, isZombieUserNamespace("my-openshift-app", nil, nil), "mid-string match is not a prefix: user")
	assert.False(t, isZombieUserNamespace("kube-system", nil, nil))
	assert.False(t, isZombieUserNamespace("openshift-apiserver", nil, nil))
	assert.False(t, isZombieUserNamespace("koku-metrics-operator", nil, nil))
	assert.False(t, isZombieUserNamespace("open-cluster-management-hc01", nil, nil))
	assert.False(t, isZombieUserNamespace("local-path-storage", nil, nil))
}

func TestEvalZombieIdle_Boundary(t *testing.T) {
	start, end := zombieWindow()
	day := func(d time.Time, ns, wl string, total int64, measured bool) zombieDayCPU {
		return zombieDayCPU{day: d.UTC().Format("2006-01-02"), namespace: ns, workload: wl, totalMC: total, measured: measured}
	}
	var rows []zombieDayCPU
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		rows = append(rows, day(d, "demo-app", "web", 9, true))
		rows = append(rows, day(d, "kube-system", "coredns", 5000, true))
	}
	peak, idle, known := evalZombieIdle(rows, start, end, 10, nil, nil)
	assert.True(t, known)
	assert.True(t, idle, "9m every day with busy platform rows stays idle")
	assert.Equal(t, int64(9), peak)
	rows[0].totalMC = 10
	_, idle, known = evalZombieIdle(rows, start, end, 10, nil, nil)
	assert.True(t, known)
	assert.False(t, idle, "exactly-floor is active: boundary stays silent")
	rows2 := []zombieDayCPU{{day: start.UTC().Format("2006-01-02"), namespace: "demo-app", workload: "web", measured: false}}
	_, _, known = evalZombieIdle(rows2, start, start.AddDate(0, 0, 1), 10, nil, nil)
	assert.False(t, known, "all-NULL day is unmeasurable, never zero")
}

func TestIsZombieUserNamespace_SyncedAugment(t *testing.T) {
	syncedExact := map[string]bool{"acme-team": true}
	syncedPrefixes := []string{"acme-"}
	assert.False(t, isZombieUserNamespace("acme-team", syncedExact, nil), "synced exact excludes")
	assert.False(t, isZombieUserNamespace("acme-frontend", nil, syncedPrefixes), "synced prefix excludes")
	assert.True(t, isZombieUserNamespace("other-app", syncedExact, syncedPrefixes), "unknown stays user activity")
	assert.True(t, isZombieUserNamespace("acme-team", nil, nil), "without sync, admin ns is user activity")
	assert.False(t, isZombieUserNamespace("kube-system", syncedExact, syncedPrefixes), "compiled exclusion still applies")
}

func TestEvalZombieIdle_SyncedExclusion(t *testing.T) {
	start, end := zombieWindow()
	day := func(d time.Time, ns string, total int64) zombieDayCPU {
		return zombieDayCPU{day: d.UTC().Format("2006-01-02"), namespace: ns, workload: "web", totalMC: total, measured: true}
	}
	var rows []zombieDayCPU
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		rows = append(rows, day(d, "demo-app", 9))
		rows = append(rows, day(d, "acme-team", 5000))
	}
	syncedExact := map[string]bool{"acme-team": true}
	peak, idle, known := evalZombieIdle(rows, start, end, 10, syncedExact, nil)
	assert.True(t, known)
	assert.True(t, idle, "synced admin CPU never counts as user activity")
	assert.Equal(t, int64(9), peak)
}
