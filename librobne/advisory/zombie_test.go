package advisory

import (
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/librobne/csv"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

func infLE() float64 { return math.Inf(1) }

// Ports of the server's zombie_test.go / zombie_short_test.go cases
// (internal/engine/correlate) — the parity contract: same inputs, same
// verdicts. Distinct values per field so a mis-mapping cannot pass by
// coincidence.
//
// Topology mirrors production routing: still-on digests live under the
// MANAGEMENT payload (context); idle-leg digests live under the HOSTED
// payload (primary) whose cluster ID is the HC identity (N-leg convention).

const (
	zAssocNS = "clusters-hc9"
	zMgmtHC  = "aaaaaaaa-1111-1111-1111-111111111111"
)

// zDay builds one hosted digest row (namespace, workload) on a day.
func zDay(day time.Time, ns, wl string, usage, request int64) types.KeyedDigest {
	return types.KeyedDigest{
		Key: types.ContainerKey{Namespace: ns, Workload: wl, WorkloadType: "Deployment", ContainerName: "c0"},
		Row: types.DigestRow{BucketDate: day, CPUUsageP50MC: usage, CPURequestP50MC: request},
	}
}

// zHostedIdle builds the idle hosted fixture: one user workload at usage
// (busyDay overrides) plus busy platform rows every day (pipeline liveness
// + the shared exclusion proof).
func zHostedIdle(now time.Time, windowDays int, usage int64, busyDay map[int]int64) []types.KeyedDigest {
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -windowDays)
	var out []types.KeyedDigest
	for i := 0; i < windowDays; i++ {
		d := start.AddDate(0, 0, i)
		u := usage
		if b, ok := busyDay[i]; ok {
			u = b
		}
		out = append(out, zDay(d, "demo-app", "web", u, 100))
		out = append(out, zDay(d, "kube-system", "coredns", 2000, 2000))
		out = append(out, zDay(d, "koku-metrics-operator", "collector", 5000, 5000))
	}
	return out
}

// zMgmtOn builds the provisioned-CP fixture over the association namespace.
func zMgmtOn(now time.Time, windowDays int, request int64) []types.KeyedDigest {
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -windowDays)
	var out []types.KeyedDigest
	for i := 0; i < windowDays; i++ {
		d := start.AddDate(0, 0, i)
		out = append(out, zDay(d, zAssocNS, "kube-apiserver", 50, request))
	}
	return out
}

func zInput(now time.Time, hosted, mgmt []types.KeyedDigest) ZombieInput {
	return ZombieInput{
		HcID:            zMgmtHC,
		Now:             now,
		HostedDigests:   hosted,
		MgmtDigests:     mgmt,
		AssocNamespaces: []string{zAssocNS},
		SnapshotFresh:   true,
		Policy:          DefaultZombiePolicy(),
		SyncedExact:     nil,
		SyncedPrefixes:  nil,
	}
}

func TestZombie_FiresOnTrueZombie(t *testing.T) {
	now := time.Now()
	got := EvaluateZombieStandard(zInput(now, zHostedIdle(now, 14, 0, nil), zMgmtOn(now, 14, 500)))
	require.NotNil(t, got, "user 0-CPU + provisioned CP must fire despite busy platform rows")
	assert.Equal(t, ZombieVerdict, got.Verdict)
	assert.Equal(t, "high", got.Confidence)
	assert.Equal(t, int64(0), got.Details["peak_user_cpu_mc"])
	assert.Equal(t, int64(10), got.Details["idle_floor_cpu_mc"])
	assert.Equal(t, int64(500), got.Details["control_plane_cpu_request_mc"])
	_, hasAPI := got.Details["api_max_daily_requests"]
	assert.False(t, hasAPI, "no SLO evidence means api context unknown, field absent")
	require.NotEmpty(t, got.Evidence)
	assert.Equal(t, "hc:"+zMgmtHC, got.Evidence[0])
}

func TestZombie_SilentAlive(t *testing.T) {
	now := time.Now()
	got := EvaluateZombieStandard(zInput(now, zHostedIdle(now, 14, 500, nil), zMgmtOn(now, 14, 500)))
	assert.Nil(t, got)
}

func TestZombie_SilentSpikeDay(t *testing.T) {
	now := time.Now()
	got := EvaluateZombieStandard(zInput(now, zHostedIdle(now, 14, 0, map[int]int64{5: 800}), zMgmtOn(now, 14, 500)))
	assert.Nil(t, got, "one busy day vetoes")
}

func TestZombie_SilentMissingDays(t *testing.T) {
	now := time.Now()
	hosted := zHostedIdle(now, 14, 0, nil)
	mgmt := zMgmtOn(now, 14, 500)
	// Keep only days 10..13 — 4/14 days is unknown, never proof of idleness.
	in := zInput(now, hosted[len(hosted)-12:], mgmt[len(mgmt)-12:])
	got := EvaluateZombieStandard(in)
	assert.Nil(t, got)
}

func TestZombie_MediumWithoutMgmtProof(t *testing.T) {
	now := time.Now()
	// Requests zero everywhere: the trailing-day SUM reads 0 — the offline
	// analogue of the server's NULL-request medium fixture (digests present
	// but the request column carries no values is modeled by absent rows).
	mgmt := zMgmtOn(now, 14, 500)
	var noReq []types.KeyedDigest
	for _, r := range mgmt {
		if r.Row.BucketDate.Before(now.UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)) {
			noReq = append(noReq, r)
		}
	}
	got := EvaluateZombieStandard(zInput(now, zHostedIdle(now, 14, 0, nil), noReq))
	require.NotNil(t, got, "snapshot+idle without CP proof fires medium, never silent-high")
	assert.Equal(t, "medium", got.Confidence)
	assert.Equal(t, int64(0), got.Details["control_plane_cpu_request_mc"])
}

func TestZombie_SilentZeroCP(t *testing.T) {
	now := time.Now()
	got := EvaluateZombieStandard(zInput(now, zHostedIdle(now, 14, 0, nil), zMgmtOn(now, 14, 0)))
	assert.Nil(t, got, "CP requests present-but-zero is known-not-on: silence, not medium")
}

func TestZombie_SilentStaleSnapshot(t *testing.T) {
	now := time.Now()
	in := zInput(now, zHostedIdle(now, 14, 0, nil), zMgmtOn(now, 14, 500))
	in.SnapshotFresh = false
	assert.Nil(t, EvaluateZombieStandard(in), "no fresh association snapshot means no evaluation")
}

func TestZombie_SilentNoAssociation(t *testing.T) {
	now := time.Now()
	in := zInput(now, zHostedIdle(now, 14, 0, nil), zMgmtOn(now, 14, 500))
	in.AssocNamespaces = nil
	assert.Nil(t, EvaluateZombieStandard(in), "no association namespaces means the still-on leg cannot run")
}

func TestZombie_APIMaxDailyContext(t *testing.T) {
	now := time.Now()
	in := zInput(now, zHostedIdle(now, 14, 0, nil), zMgmtOn(now, 14, 500))
	// Two +Inf snapshots per day, 30m+ apart, growing by 1000/day-ish: the
	// per-day delta feeds api_max_daily (context, never a gate).
	end := now.UTC().Truncate(24 * time.Hour)
	for d := 0; d < 14; d++ {
		day := end.AddDate(0, 0, -(d + 1))
		base := int64(10000 + d*1000)
		for i, bump := range []int64{0, 900} {
			ws := day.Add(10 * time.Hour).Add(time.Duration(i) * time.Hour)
			in.SLORows = append(in.SLORows, csv.SLORow{
				HCClusterID: zMgmtHC, WindowStart: ws, WindowEnd: ws.Add(time.Hour),
				VerbGroup: "mutating", Le: infLE(), BucketCount: base + bump,
				CollectedAt: ws.Add(40 * time.Minute), Source: "kubernetes",
			})
		}
	}
	got := EvaluateZombieStandard(in)
	require.NotNil(t, got)
	v, ok := got.Details["api_max_daily_requests"]
	require.True(t, ok, "SLO +Inf deltas must surface as api context")
	assert.Equal(t, float64(900), v, "per-day delta between the two snapshots")
}

func TestEvalZombieIdle_Boundary(t *testing.T) {
	now := time.Now()
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -14)
	sums := ZombieDaySums(zHostedIdle(now, 14, 9, nil))
	peak, idle, known := evalZombieIdle(sums, start, end, 10, nil, nil)
	assert.True(t, known)
	assert.True(t, idle, "9m every day with busy platform rows stays idle")
	assert.Equal(t, int64(9), peak)
	// Exactly-floor is active: boundary stays silent.
	hot := zHostedIdle(now, 14, 10, nil)
	_, idle, known = evalZombieIdle(ZombieDaySums(hot), start, end, 10, nil, nil)
	assert.True(t, known)
	assert.False(t, idle)
}

func TestZombie_SyncedExclusion(t *testing.T) {
	now := time.Now()
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -14)
	var hosted []types.KeyedDigest
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		hosted = append(hosted, zDay(d, "demo-app", "web", 9, 100))
		hosted = append(hosted, zDay(d, "acme-team", "web", 5000, 100))
	}
	sums := ZombieDaySums(hosted)
	peak, idle, known := evalZombieIdle(sums, start, end, 10, map[string]bool{"acme-team": true}, nil)
	assert.True(t, known)
	assert.True(t, idle, "synced admin CPU never counts as user activity")
	assert.Equal(t, int64(9), peak)
}

func TestZombieIsUserNamespace(t *testing.T) {
	assert.True(t, zombieIsUserNamespace("demo-app", nil, nil))
	assert.True(t, zombieIsUserNamespace("default", nil, nil), "default/ is user space, never excluded")
	assert.True(t, zombieIsUserNamespace("kubevirt-demo", nil, nil), "kubevirt lacks the kube- dash: user, not platform")
	assert.True(t, zombieIsUserNamespace("my-openshift-app", nil, nil), "mid-string match is not a prefix: user")
	assert.False(t, zombieIsUserNamespace("kube-system", nil, nil))
	assert.False(t, zombieIsUserNamespace("openshift-apiserver", nil, nil))
	assert.False(t, zombieIsUserNamespace("koku-metrics-operator", nil, nil))
	assert.False(t, zombieIsUserNamespace("open-cluster-management-agent-addon", nil, nil))
	assert.False(t, zombieIsUserNamespace("open-cluster-management-hc01", nil, nil), "the server's evidence-grounded lab addon ns is platform (#721 copy dropped it)")
	assert.False(t, zombieIsUserNamespace("local-path-storage", nil, nil))
	syncedExact := map[string]bool{"acme-team": true}
	syncedPrefixes := []string{"acme-"}
	assert.False(t, zombieIsUserNamespace("acme-team", syncedExact, nil), "synced exact excludes")
	assert.False(t, zombieIsUserNamespace("acme-frontend", nil, syncedPrefixes), "synced prefix excludes")
	assert.True(t, zombieIsUserNamespace("other-app", syncedExact, syncedPrefixes), "unknown stays user activity")
	assert.True(t, zombieIsUserNamespace("acme-team", nil, nil), "without sync, admin ns is user activity")
	assert.False(t, zombieIsUserNamespace("kube-system", syncedExact, syncedPrefixes), "compiled exclusion still applies")
}

// zPools builds hourly NodePool snapshots for days [0, n) from the short
// window start with constant spec/status.
func zPools(now time.Time, shortWindowDays, days, spec, status int64) []csv.NodepoolRow {
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -int(shortWindowDays))
	var out []csv.NodepoolRow
	for i := int64(0); i < days; i++ {
		for h := 0; h < 24; h++ {
			ws := start.AddDate(0, 0, int(i)).Add(time.Duration(h) * time.Hour)
			out = append(out, csv.NodepoolRow{
				HCClusterID: zMgmtHC, PoolName: "pool-a",
				WindowStart: ws, WindowEnd: ws.Add(time.Hour),
				SpecReplicas: spec, StatusReplicas: status,
				CollectedAt: ws.Add(time.Hour), Source: "hypershift",
			})
		}
	}
	return out
}

func TestZombieShort_FiresOnParked(t *testing.T) {
	now := time.Now()
	in := zInput(now, zHostedIdle(now, 3, 0, nil), zMgmtOn(now, 3, 500))
	in.NodepoolRows = zPools(now, 3, 3, 0, 0)
	got := EvaluateZombieShort(in)
	require.NotNil(t, got)
	assert.Equal(t, ZombieVerdict, got.Verdict)
	assert.Equal(t, "high", got.Confidence)
	assert.Equal(t, 72.0, got.WindowEnd.Sub(got.WindowStart).Hours(), "short window spans 3 days")
	assert.Contains(t, got.Evidence, "lane:short-3d")
	assert.Contains(t, got.Evidence, "pool_zero_days:3")
}

func TestZombieShort_SilentPartialCoverage(t *testing.T) {
	now := time.Now()
	in := zInput(now, zHostedIdle(now, 3, 0, nil), zMgmtOn(now, 3, 500))
	in.NodepoolRows = zPools(now, 3, 2, 0, 0)
	assert.Nil(t, EvaluateZombieShort(in), "2/3 pool days with default required=3 is unknown")
}

func TestZombieShort_SilentNonZeroDay(t *testing.T) {
	now := time.Now()
	in := zInput(now, zHostedIdle(now, 3, 0, nil), zMgmtOn(now, 3, 500))
	in.NodepoolRows = append(zPools(now, 3, 1, 0, 0), zPools(now, 2, 1, 2, 2)...)
	assert.Nil(t, EvaluateZombieShort(in), "one non-zero covered day vetoes like max-daily")
}

func TestZombieShort_SilentMidTransition(t *testing.T) {
	now := time.Now()
	in := zInput(now, zHostedIdle(now, 3, 0, nil), zMgmtOn(now, 3, 500))
	in.NodepoolRows = zPools(now, 3, 3, 0, 2)
	assert.Nil(t, EvaluateZombieShort(in), "spec-zero with live status is mid-transition: unknown, never idle-proof")
}

func TestZombieShort_SilentWithoutPoolEvidence(t *testing.T) {
	now := time.Now()
	in := zInput(now, zHostedIdle(now, 3, 0, nil), zMgmtOn(now, 3, 500))
	assert.Nil(t, EvaluateZombieShort(in), "no nodepool rows means no short evaluation")
}

func TestEvalPoolParked_Matrix(t *testing.T) {
	now := time.Now()
	end := now.UTC().Truncate(24 * time.Hour)
	start := end.AddDate(0, 0, -3)
	mk := func(days ...string) map[string]*poolDayState {
		m := map[string]*poolDayState{}
		for _, d := range days {
			m[d] = &poolDayState{day: d, snapshots: 2, allZero: true}
		}
		return m
	}
	var all []string
	for d := start; d.Before(end); d = d.AddDate(0, 0, 1) {
		all = append(all, d.UTC().Format("2006-01-02"))
	}
	covered, parked := evalPoolParked(mk(all...), start, 3, 3)
	assert.Equal(t, 3, covered)
	assert.True(t, parked)
	covered, parked = evalPoolParked(mk(all[:2]...), start, 3, 3)
	assert.Equal(t, 2, covered)
	assert.False(t, parked, "2/3 with required=3 is unknown")
	_, parked = evalPoolParked(mk(all[:2]...), start, 3, 2)
	assert.True(t, parked, "required=2 honors looser tenant tuning")
	nonzero := mk(all...)
	nonzero[all[1]].allZero = false
	_, parked = evalPoolParked(nonzero, start, 3, 1)
	assert.False(t, parked, "non-zero covered day vetoes at any required level")
	_, parked = evalPoolParked(map[string]*poolDayState{}, start, 3, 3)
	assert.False(t, parked, "no evidence is silence")
}

func TestZombie_CustomIdleWindow(t *testing.T) {
	now := time.Now()
	in := zInput(now, zHostedIdle(now, 5, 0, nil), zMgmtOn(now, 5, 500))
	in.Policy.IdleWindowDays = 5
	got := EvaluateZombieStandard(in)
	require.NotNil(t, got, "5 idle days must fire under a 5d custom window")
	assert.Equal(t, 120.0, got.WindowEnd.Sub(got.WindowStart).Hours(), "advisory window spans the custom 5 days")
}
