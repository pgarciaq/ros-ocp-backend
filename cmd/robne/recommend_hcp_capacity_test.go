package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	roscsv "github.com/redhatinsights/ros-ocp-backend/librobne/csv"
)

// #700 Task 4: the capacity compute path. Fixtures are the real hcp-mgmt
// rows captured 2026-10-26 (identity-free by CSV contract; the manifest
// supplies the cluster) plus the synthetic recollect pair. Expected values
// hand-verified against CalculateCapacity and cross-checked against the
// live SNO API: request/low/average = 7 memory_request (headroom +4),
// medium = 3 cpu_usage (0), high = 2 cpu_usage (−1); single_replica basis
// → illustrative_only; warnings pressure_unknown + shared_infrastructure
// + mce_may_be_stale (QPS moved while MCE refs froze).

func loadCapacityFixture(t *testing.T, dir string) []roscsv.HCPCapacityRow {
	t.Helper()
	loaded, err := roscsv.Load(dir)
	require.NoError(t, err)
	require.NotEmpty(t, loaded.HCPCapacityRows)
	return loaded.HCPCapacityRows
}

func TestAssembleCapacitySeries_SortOrder(t *testing.T) {
	// File order is ascending (KMO appends); the series is newest-first.
	rows := loadCapacityFixture(t, "testdata/hcp-capacity")
	series, drops := assembleCapacitySeries(rows, time.Date(2026, 10, 26, 22, 30, 0, 0, time.UTC))
	require.Empty(t, drops)
	require.Len(t, series, 3)
	assert.Equal(t, "2026-10-26T21:59:59Z", series[0].ObservedAt.Format(time.RFC3339))
	assert.Equal(t, "2026-10-26T19:59:59Z", series[2].ObservedAt.Format(time.RFC3339))
}

func TestAssembleCapacitySeries_MonthBoundaryMerge(t *testing.T) {
	// Two files — a September row older than the October series — merge
	// into one newest-first series attributed to the single cluster.
	rows := loadCapacityFixture(t, "testdata/hcp-capacity")
	sep := loadCapacityFixture(t, "testdata/hcp-capacity")[0]
	sep.ObservedAt = time.Date(2026, 9, 30, 21, 59, 59, 0, time.UTC)
	merged := append(append([]roscsv.HCPCapacityRow{}, rows...), sep)
	series, _ := assembleCapacitySeries(merged, time.Date(2026, 10, 26, 22, 30, 0, 0, time.UTC))
	require.Len(t, series, 4)
	assert.Equal(t, "2026-10-26T21:59:59Z", series[0].ObservedAt.Format(time.RFC3339))
	assert.Equal(t, "2026-09-30T21:59:59Z", series[3].ObservedAt.Format(time.RFC3339))
}

func TestAssembleCapacitySeries_RecollectLastWins(t *testing.T) {
	// Two rows share observed_at (a KMO recollect with changed values:
	// 46500 → 55500 eligible cpu). Backend last-write-wins convergence:
	// the LAST of the tied group in read order wins — keeping the first
	// would silently select the stale row (round-8 W1).
	rows := loadCapacityFixture(t, "testdata/hcp-capacity-recollect")
	require.Len(t, rows, 2)
	series, drops := assembleCapacitySeries(rows, time.Date(2026, 10, 26, 22, 30, 0, 0, time.UTC))
	require.Empty(t, drops)
	require.Len(t, series, 1, "same-observed_at recollect dedups to one row")
	assert.Equal(t, int64(55500), series[0].EligibleCPUMillicores, "the recollected (last) value wins")
}

func runCapacityRecommend(t *testing.T, dir, now string, extra func(*commonFlags)) (recommendResult, error) {
	t.Helper()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	_ = cwd
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	f := commonFlags{
		input:        dir,
		noUserConfig: true,
		now:          now,
		format:       "json",
	}
	if extra != nil {
		extra(&f)
	}
	return computeRecommendations(f)
}

func TestRecommend_HCPCapacityParity(t *testing.T) {
	result, err := runCapacityRecommend(t, "testdata/hcp-capacity", "2026-10-26T22:30:00Z", func(f *commonFlags) {
		f.plugins = "hcp"
	})
	require.NoError(t, err)
	require.Len(t, result.HCPCapacity, 3, "full series in the result")

	latest := result.HCPCapacity[0]
	assert.Equal(t, "2026-10-26T21:59:59Z", latest.ObservedAt.Format(time.RFC3339))
	assert.Equal(t, "11111111-1111-1111-1111-111111111111", latest.ClusterUUID, "manifest cluster_id labels the sibling")
	assert.Equal(t, "hcp-published-sizing-v1", latest.NativeModelVersion)

	// Scenario parity with the service (hand-verified + live API).
	require.Len(t, latest.Scenarios, 5)
	want := []struct {
		name  string
		max   int64
		limit string
		head  *int64
	}{
		{"request", 7, "memory_request", int64p(4)},
		{"low_qps", 7, "memory_request", int64p(4)},
		{"average_qps", 7, "memory_request", int64p(4)},
		{"medium_qps", 3, "cpu_usage", int64p(0)},
		{"high_qps", 2, "cpu_usage", int64p(-1)},
	}
	for i, w := range want {
		s := latest.Scenarios[i]
		assert.Equal(t, w.name, s.Name)
		assert.Equal(t, w.max, s.MaxHCs)
		assert.Equal(t, w.limit, s.LimitingResource)
		require.NotNil(t, s.Headroom, "%s headroom", w.name)
		assert.Equal(t, *w.head, *s.Headroom)
		assert.Equal(t, "illustrative_only", s.Status, "single_replica basis")
	}

	// Warnings: the live hcp-mgmt pattern.
	assert.True(t, latest.Warnings.PressureUnknown)
	assert.True(t, latest.Warnings.SharedInfrastructure)
	require.NotNil(t, latest.Warnings.MCEMayBeStale)
	assert.True(t, *latest.Warnings.MCEMayBeStale, "QPS moved 19.48→19.80 while MCE refs froze at 8/8/8/4/2")
	assert.False(t, latest.Warnings.Stale, "30min age under the 2h window")

	// Envelope v12 + sibling shape via the JSON writer.
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, result, "json"))
	var env struct {
		Version            int              `json:"version"`
		HCPRecommendations []HCPCapacityOut `json:"hcp_recommendations"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))
	assert.Equal(t, 12, env.Version)
	require.Len(t, env.HCPRecommendations, 3)
	assert.Equal(t, "11111111-1111-1111-1111-111111111111", env.HCPRecommendations[0].ClusterUUID)
}

func TestRecommend_HCPCapacityClock(t *testing.T) {
	// Capacity-only input without --now: the engine clock (parseNow)
	// never resolves — maxEnd is zero — so the capacity clock takes over
	// (flag > YAML > wall). Must succeed, never "cannot resolve engine
	// clock" (round-9 W8).
	result, err := runCapacityRecommend(t, "testdata/hcp-capacity", "", func(f *commonFlags) {
		f.plugins = "hcp"
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.HCPCapacity)
}

func TestRecommend_HCPCapacityUnknownCluster(t *testing.T) {
	// No manifest, no YAML cluster_uuid: the sibling label is "unknown"
	// and the envelope cluster_id stays empty (ADR-0345).
	dir := t.TempDir()
	body, err := os.ReadFile(filepath.Join("testdata", "hcp-capacity", "ros-openshift-hcp-capacity-202610.csv"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ros-openshift-hcp-capacity-202610.csv"), body, 0o600)) //nolint:gosec // G304/G703: t.TempDir test fixture

	result, err := runCapacityRecommend(t, dir, "2026-10-26T22:30:00Z", func(f *commonFlags) {
		f.plugins = "hcp"
	})
	require.NoError(t, err)
	require.NotEmpty(t, result.HCPCapacity)
	assert.Equal(t, "unknown", result.HCPCapacity[0].ClusterUUID)
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, result, "json"))
	assert.NotContains(t, buf.String(), `"cluster_id":"11111111`)
}

func TestRequireExplicitPlugins_MissingCapacity(t *testing.T) {
	// Explicit hcp on a payload without a capacity CSV: friendly nonzero
	// message naming the emission source.
	dir := t.TempDir()
	container := "interval_start,interval_end,namespace,workload,workload_type,container_name,pod,cpu_request_container_avg,cpu_usage_container_avg,memory_request_container_avg,memory_usage_container_avg\n" +
		"2026-08-01 00:00:00 +0000 UTC,2026-08-01 01:00:00 +0000 UTC,app,api,deployment,api,api-0,0.1,0.05,102400,51200\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ocp_ros_usage.csv"), []byte(container), 0o600)) //nolint:gosec // G304/G703: t.TempDir test fixture

	loaded, err := roscsv.Load(dir)
	require.NoError(t, err)
	err = requireExplicitFilePlugins([]string{"container", "hcp"}, loaded)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "capacity snapshots are emitted by HyperShift management clusters")
}

func TestCompute_HCPCapacityAllSkewDroppedDiagnosis(t *testing.T) {
	// Every row future-dated beyond the 5-minute tolerance with explicit
	// hcp: the error names clock skew, never "no capacity snapshot"
	// (round-9 W2). Rows were loaded — the compute path rejected them.
	result, err := runCapacityRecommend(t, "testdata/hcp-capacity", "2026-10-26T10:00:00Z", func(f *commonFlags) {
		f.plugins = "hcp"
	})
	_ = result
	require.Error(t, err)
	assert.Contains(t, err.Error(), "future-dated")
	assert.Contains(t, err.Error(), "clock skew")
	assert.NotContains(t, err.Error(), "no capacity snapshot")
}

func TestRecommend_CapacityWarningsPrintedToStderr(t *testing.T) {
	// W4 (spec-locked): Load warnings print to stderr right after Load —
	// a poisoned capacity file beside valid rows warns and continues.
	dir := t.TempDir()
	container := "interval_start,interval_end,namespace,workload,workload_type,container_name,pod,cpu_request_container_avg,cpu_usage_container_avg,memory_request_container_avg,memory_usage_container_avg\n" +
		"2026-08-01 00:00:00 +0000 UTC,2026-08-01 01:00:00 +0000 UTC,app,api,deployment,api,api-0,0.1,0.05,102400,51200\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ocp_ros_usage.csv"), []byte(container), 0o600))                                                           //nolint:gosec // G304/G703: t.TempDir test fixture
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ros-openshift-hcp-capacity-202610.csv"), []byte("observed_at,operator_version\nnope,deadbeef\n"), 0o600)) //nolint:gosec // G304/G703: t.TempDir test fixture

	var stderr bytes.Buffer
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	_, err := runCapacityRecommend(t, dir, "2026-08-01T02:00:00Z", nil)
	_ = w.Close()
	os.Stderr = oldStderr
	_, _ = stderr.ReadFrom(r)
	require.NoError(t, err)
	out := stderr.String()
	assert.Contains(t, out, "ros-openshift-hcp-capacity-202610.csv", "warning names the file")
}

func int64p(v int64) *int64 { return &v }

var _ = strings.TrimSpace

func TestRecommend_BHAndCapacityEnvelope(t *testing.T) {
	// Round-9 N8: a version-number unit test cannot pin sibling presence —
	// this is the full-envelope golden. BH enabled + capacity rows: the
	// envelope is v12 (the ladder's capacity step is last, after BH) and
	// the hcp_recommendations sibling is present; BH never suppresses it.
	dir := t.TempDir()
	body, err := os.ReadFile(filepath.Join("testdata", "hcp-capacity", "ros-openshift-hcp-capacity-202610.csv"))
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ros-openshift-hcp-capacity-202610.csv"), body, 0o600)) //nolint:gosec // G304/G703: t.TempDir test fixture
	require.NoError(t, os.WriteFile(filepath.Join(dir, "robne.yaml"), []byte(
		"business_hours:\n  enabled: true\n  timezone: UTC\n  days: [monday]\n  start_time: \"09:00\"\n  end_time: \"17:00\"\n"), 0o600)) //nolint:gosec // G304/G703: t.TempDir test fixture

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	result, err := computeRecommendations(commonFlags{
		input:        dir,
		noUserConfig: true,
		now:          "2026-10-26T22:30:00Z",
		format:       "json",
		plugins:      "hcp",
	})
	require.NoError(t, err)
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, result, "json"))
	var env struct {
		Version            int              `json:"version"`
		HCPRecommendations []HCPCapacityOut `json:"hcp_recommendations"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &env))
	assert.Equal(t, 12, env.Version, "capacity step is last in the ladder: BH does not pin the envelope at 10/11")
	require.Len(t, env.HCPRecommendations, 3, "the capacity sibling rides the BH envelope")
}
