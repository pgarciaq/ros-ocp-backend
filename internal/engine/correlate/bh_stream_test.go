package correlate

// #907 regression coverage: under a BH-enabled org the processor stores TWO
// digest streams (all_hours + business_hours) for the same containers. The
// still-on and CP-stress reads must use ONE coherent stream — a filterless
// SUM blends them: the zombie CP request doubles (and a sub-floor CP flips
// the still-on leg to "on"), and evalC's stress ratio blends windows.
//
// The activity measure in loadZombieDayCPU is deliberately dual-stream
// (#746/#747 — robne's ZombieDaySums mirrors it) and is NOT under test here.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

// seedDigestSchedule writes one day's workload rows into a specific digest
// schedule stream — the second aggregate stream a BH org produces for the
// same containers.
func seedDigestSchedule(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, cluster, day string, rows map[nswl]zombieWorkloadDay, schedule string) {
	t.Helper()
	dayTime, err := time.Parse("2006-01-02", day)
	require.NoError(t, err)
	require.NoError(t, ingestion.EnsureDigestPartitionMonth(ctx, pool, dayTime))
	for k, w := range rows {
		var req any
		req = w.req
		if w.req < 0 {
			req = nil
		}
		_, err := pool.Exec(ctx, `
			INSERT INTO daily_container_digests (
				bucket_date, org_id, cluster_uuid, namespace, workload,
				workload_type, container_name, cpu_usage_p95_mc, cpu_usage_p50_mc, cpu_request_p50_mc,
				schedule_type
			) VALUES ($1,$2,$3,$4,$5,'Deployment','c0',$6,$6,$7,$8)
			ON CONFLICT DO NOTHING`,
			day, orgID, cluster, k.ns, k.wl, w.usage, req, schedule)
		require.NoError(t, err)
	}
}

// seedBHStreamMgmt writes business_hours duplicates of a 14-day mgmt spec —
// what a BH org's processor stores alongside the all_hours rows.
func seedBHStreamMgmt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, orgID, mgmt string, rows map[int]map[nswl]zombieWorkloadDay) {
	t.Helper()
	start, _ := zombieWindow()
	for i := 0; i < zombieWindowDays; i++ {
		day := start.AddDate(0, 0, i).Format("2006-01-02")
		if r, ok := rows[i]; ok {
			seedDigestSchedule(t, ctx, pool, orgID, mgmt, day, r, "business_hours")
		}
	}
}

// mgmtReq builds 14 days of management still-on rows at a given request.
func mgmtReq(req int64) map[int]map[nswl]zombieWorkloadDay {
	spec := map[int]map[nswl]zombieWorkloadDay{}
	for i := 0; i < zombieWindowDays; i++ {
		spec[i] = map[nswl]zombieWorkloadDay{
			{"clusters-hc9", "kube-apiserver"}: {usage: 50, req: req},
		}
	}
	return spec
}

func cpReqSignal(t *testing.T, advs []map[string]any) string {
	t.Helper()
	for _, s := range advs[0]["signals"].([]string) {
		if strings.HasPrefix(s, "cp_req_mc:") {
			return strings.TrimPrefix(s, "cp_req_mc:")
		}
	}
	t.Fatal("no cp_req_mc signal on the fired advisory")
	return ""
}

// TestZombie_StillOnBHStreamNotDoubleCounted: the still-on leg must read ONE
// coherent digest stream. With the CP provisioned at 500mC and a BH org
// storing the same request in both streams, the advisory must carry 500 —
// a filterless SUM reports the doubled 1000 (#907).
func TestZombie_StillOnBHStreamNotDoubleCounted(t *testing.T) {
	org := testutil.TestOrgID + "-z-bh2x"
	pool := zombieFixture(t, org, zMgmt4, zHC1, idleHosted(0, nil), mgmtOn())
	ctx := context.Background()
	seedBHStreamMgmt(t, ctx, pool, org, zMgmt4, mgmtOn())

	fired, err := RunZombieCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 1, fired, "idle user + provisioned CP must fire with both streams present")

	advs := zombieAdvisories(t, ctx, pool, org)
	require.Len(t, advs, 1)
	assert.Equal(t, "500", cpReqSignal(t, advs),
		"the still-on leg reads one coherent stream: the provisioned 500mC, not the doubled sum")
}

// TestZombie_StillOnSubFloorBHStaysKnownNotOn: the verdict-level distortion.
// A CP provisioned at 60mC sits below the 100mC floor — known-not-on, silence.
// Doubled to 120mC by a filterless SUM it flips the leg to "on" and fires (#907).
func TestZombie_StillOnSubFloorBHStaysKnownNotOn(t *testing.T) {
	org := testutil.TestOrgID + "-z-bh60"
	spec := mgmtReq(60)
	pool := zombieFixture(t, org, zMgmt5, zHC2, idleHosted(0, nil), spec)
	ctx := context.Background()
	seedBHStreamMgmt(t, ctx, pool, org, zMgmt5, spec)

	fired, err := RunZombieCycle(ctx, pool)
	require.NoError(t, err)
	assert.Equal(t, 0, fired, "sub-floor CP reads known-not-on even when the BH stream doubles the raw sum")
	assert.Empty(t, zombieAdvisories(t, ctx, pool, org))
}

// TestEvalC_SingleStreamUnderBH: the headroom CP-stress ratio must not blend
// the business_hours stream into the all-hours usage/requests ratio. The BH
// window carries different percentiles (usage 30 vs 50), so a filterless SUM
// moves the ratio — one coherent stream leaves it invariant (#907).
func TestEvalC_SingleStreamUnderBH(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	org := testutil.TestOrgID + "-evc-bh"
	assocNS := "clusters-hc9"
	seedZombieSnapshot(t, ctx, pool, org, zMgmt6, zHC3, assocNS, "evc-bh-manifest", time.Now().UTC().Add(-time.Hour))
	today := time.Now().UTC().Format("2006-01-02")

	seedDigestSchedule(t, ctx, pool, org, zMgmt6, today,
		map[nswl]zombieWorkloadDay{{assocNS, "kube-apiserver"}: {usage: 50, req: 500}}, "all_hours")
	r1, _, known1 := evalC(ctx, pool, org, zMgmt6, zHC3, DefaultPolicy())
	require.True(t, known1, "all-hours digests alone must read known")
	require.Greater(t, r1, 0.0)

	seedDigestSchedule(t, ctx, pool, org, zMgmt6, today,
		map[nswl]zombieWorkloadDay{{assocNS, "kube-apiserver"}: {usage: 30, req: 500}}, "business_hours")
	r2, _, known2 := evalC(ctx, pool, org, zMgmt6, zHC3, DefaultPolicy())
	require.True(t, known2)
	assert.InDelta(t, r1, r2, 1e-9,
		"the business_hours stream must not blend into the CP stress ratio (#907)")
}
