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

// --- Task 5: table/CSV rendering (latest observation only) + the fourth
// poison-matrix cell ---

func capacityRenderResult(t *testing.T) recommendResult {
	t.Helper()
	result, err := runCapacityRecommend(t, "testdata/hcp-capacity", "2026-10-26T22:30:00Z", func(f *commonFlags) {
		f.plugins = "hcp"
	})
	require.NoError(t, err)
	require.Len(t, result.HCPCapacity, 3)
	return result
}

func TestWriteRecs_HCPCapacityTable(t *testing.T) {
	result := capacityRenderResult(t)
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, result, "table"))
	out := buf.String()

	// Latest observation only: the 21:59 row's five scenarios render; the
	// 19:59/20:59 observations do not.
	assert.Contains(t, out, "SCENARIO")
	assert.Contains(t, out, "request")
	assert.Contains(t, out, "memory_request")
	assert.Contains(t, out, "high_qps")
	assert.Equal(t, 5, strings.Count(out, "21:59:59Z"), "five scenario lines from the latest observation only")
	assert.NotContains(t, out, "19:59:59Z", "older observations do not render")
	assert.NotContains(t, out, "20:59:59Z", "older observations do not render")
	assert.Contains(t, out, "11111111-1111-1111-1111-111111111111")
	// Signed headroom and the withheld marker both visible.
	assert.Contains(t, out, "-1")
}

func TestWriteRecs_HCPCapacityCSV(t *testing.T) {
	result := capacityRenderResult(t)
	var buf bytes.Buffer
	require.NoError(t, writeRecs(&buf, result, "csv"))
	out := buf.String()

	lines := strings.Split(strings.TrimSpace(out), "\n")
	require.Len(t, lines, 6, "header + five scenario rows")
	assert.Contains(t, lines[0], "scenario")
	assert.Contains(t, lines[0], "max_hcs")
	assert.Contains(t, lines[0], "headroom")
	// The high_qps row: max 2, limiting cpu_usage, headroom -1.
	var highLine string
	for _, l := range lines {
		if strings.Contains(l, ",high_qps,") {
			highLine = l
		}
	}
	require.NotEmpty(t, highLine, "high_qps row present")
	assert.Contains(t, highLine, "2")
	assert.Contains(t, highLine, "cpu_usage")
	assert.Contains(t, highLine, "-1")
}

func TestRecommend_CapacityOnlyPoisonedExplicit(t *testing.T) {
	// The fourth poison-matrix cell (round-9 W4): capacity-only poisoned
	// CSV + explicit hcp → warnings printed to stderr, then the friendly
	// nonzero exit.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ros-openshift-hcp-capacity-202610.csv"), []byte("observed_at,operator_version\nnope,deadbeef\n"), 0o600)) //nolint:gosec // G304/G703: t.TempDir test fixture

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	oldStderr := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w
	_, err := computeRecommendations(commonFlags{
		input:        dir,
		noUserConfig: true,
		now:          "2026-10-26T22:30:00Z",
		format:       "json",
		plugins:      "hcp",
	})
	_ = w.Close()
	os.Stderr = oldStderr
	var stderr bytes.Buffer
	_, _ = stderr.ReadFrom(r)

	require.Error(t, err, "explicit hcp with no parseable capacity rows is a nonzero exit")
	assert.Contains(t, err.Error(), "capacity snapshots are emitted by HyperShift management clusters")
	assert.Contains(t, stderr.String(), "warning:", "the poison warning printed before the exit")
}

// --- Task 6: validate / diff / explain ---

func TestValidate_HCPCapacity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	var out bytes.Buffer
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := runValidate(commonFlags{input: "testdata/hcp-capacity", noUserConfig: true, plugins: "hcp"})
	_ = w.Close()
	os.Stdout = oldStdout
	_, _ = out.ReadFrom(r)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "hcp_capacity_rows: 3")
}

func TestValidate_HCPCapacitySkippedReasons(t *testing.T) {
	// A row with a bad value (inventory_complete is not a strict
	// boolean): the skip surfaces with its reason (observed_at when
	// field 0 parses, else the row index).
	dir := t.TempDir()
	body := "observed_at,mce_sampled_at,operator_version,ocp_version,mce_version,native_model_version,collection_interval_minutes,upload_cycle_minutes,inventory_complete,current_hc_count,ready_hcp_count,average_ready_hcp_qps,ready_hcp_qps_known,hosted_availability_basis,eligible_worker_cpu_allocatable_millicores,eligible_worker_memory_allocatable_bytes,eligible_worker_max_pods,worker_placement_known,pressure_state,mce_request_max_hcs,mce_low_qps_max_hcs,mce_average_qps_max_hcs,mce_medium_qps_max_hcs,mce_high_qps_max_hcs,shared_infrastructure\n" +
		"2026-10-26T21:59:59Z,,deadbeef,4.22.17,,hcp-published-sizing-v1,60,60,maybe,3,3,19.8,true,single_replica,46500,147688947712,750,true,unknown,8,8,8,4,2,true\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "ros-openshift-hcp-capacity-202610.csv"), []byte(body), 0o600)) //nolint:gosec // G304/G703: t.TempDir test fixture

	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	var out bytes.Buffer
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	err := runValidate(commonFlags{input: dir, noUserConfig: true, plugins: "hcp"})
	_ = w.Close()
	os.Stdout = oldStdout
	_, _ = out.ReadFrom(r)
	require.NoError(t, err)
	assert.Contains(t, out.String(), "hcp_capacity_rows: 0")
	assert.Contains(t, out.String(), "skipped row 2026-10-26T21:59:59Z: inventory_complete")
}

func TestValidate_HCPCapacityOnlyNoClock(t *testing.T) {
	// Round-10 W4: validate on capacity-only input must not hit the
	// engine-clock wall (validate has its own now derivation path).
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	err := runValidate(commonFlags{input: "testdata/hcp-capacity", noUserConfig: true, plugins: "hcp"})
	require.NoError(t, err)
}

func hcpEnvRows(t *testing.T, at time.Time, max int64, limit string) []HCPCapacityOut {
	t.Helper()
	return []HCPCapacityOut{{
		ClusterUUID: "c1", ObservedAt: at, CurrentHCCount: 3,
		Scenarios: []hcpCapacityScenarioOut{{Name: "request", MaxHCs: max, LimitingResource: limit}},
	}}
}

func hcpEnvelope(rows []HCPCapacityOut) recommendJSON {
	env := recommendJSON{Version: 12}
	if rows != nil {
		env.HCPRecommendations = &rows
	}
	return env
}

func TestDiff_HCPCapacity(t *testing.T) {
	// Latest-per-side (round-9 W3): envelopes whose latest rows carry
	// different observed_at values compare latest-vs-latest, never
	// added/removed rows.
	left := hcpEnvelope(hcpEnvRows(t, time.Date(2026, 10, 26, 20, 59, 59, 0, time.UTC), 7, "memory_request"))
	right := hcpEnvelope(hcpEnvRows(t, time.Date(2026, 10, 26, 21, 59, 59, 0, time.UTC), 6, "cpu_request"))
	out, differs, err := compareEnvelopes(left, right)
	require.NoError(t, err)
	assert.True(t, differs)
	assert.Contains(t, out, "hcp_recommendations")
	assert.NotContains(t, out, "missing", "different observed_at is latest-vs-latest, not added/removed")

	// Missing side.
	empty := hcpEnvelope(nil)
	out2, differs2, err := compareEnvelopes(left, empty)
	require.NoError(t, err)
	assert.True(t, differs2)
	assert.Contains(t, out2, "hcp_recommendations")
}

func TestExplain_HCPCapacity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")
	var out bytes.Buffer
	err := runExplain(&out, explainFlags{
		commonFlags: commonFlags{input: "testdata/hcp-capacity", noUserConfig: true, now: "2026-10-26T22:30:00Z", plugins: "hcp", format: "json"},
		scenario:    "medium_qps",
	})
	require.NoError(t, err)
	body := out.String()
	assert.Contains(t, body, "cpu_usage", "medium scenario limiting resource")
	assert.Contains(t, body, "published-sizing")
	assert.Contains(t, body, "46500", "eligible cpu in the source")
	assert.Contains(t, body, "single_replica")
}

func TestExplain_HCPCapacitySelectors(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()+"/xdg-missing")
	t.Setenv("ROBNE_NO_USER_CONFIG", "1")

	// --observed-at selects the row.
	var out bytes.Buffer
	err := runExplain(&out, explainFlags{
		commonFlags: commonFlags{input: "testdata/hcp-capacity", noUserConfig: true, now: "2026-10-26T22:30:00Z", plugins: "hcp", format: "json"},
		scenario:    "request",
		observedAt:  "2026-10-26T20:59:59Z",
	})
	require.NoError(t, err)
	assert.Contains(t, out.String(), "2026-10-26T20:59:59Z")

	// Bad scenario name → error.
	err = runExplain(&bytes.Buffer{}, explainFlags{
		commonFlags: commonFlags{input: "testdata/hcp-capacity", noUserConfig: true, now: "2026-10-26T22:30:00Z", plugins: "hcp", format: "json"},
		scenario:    "turbo_qps",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown scenario")

	// BH schedule rejected for hcp.
	err = runExplain(&bytes.Buffer{}, explainFlags{
		commonFlags: commonFlags{input: "testdata/hcp-capacity", noUserConfig: true, now: "2026-10-26T22:30:00Z", plugins: "hcp", format: "json"},
		scenario:    "request",
		schedule:    "business_hours",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "business_hours")
}

func TestDiff_HCPCapacityClusterMismatch(t *testing.T) {
	// #700 acceptance: "focused tests for valid, incomplete, and
	// mismatched-cluster inputs" — cross-cluster capacity diffs are
	// allowed and flagged as the existing cluster_id meta delta, not a
	// new error (review-2 W4).
	left := hcpEnvelope(hcpEnvRows(t, time.Date(2026, 10, 26, 21, 59, 59, 0, time.UTC), 7, "memory_request"))
	left.ClusterID = "11111111-1111-1111-1111-111111111111"
	right := hcpEnvelope(hcpEnvRows(t, time.Date(2026, 10, 26, 21, 59, 59, 0, time.UTC), 7, "memory_request"))
	right.ClusterID = "22222222-2222-2222-2222-222222222222"
	out, differs, err := compareEnvelopes(left, right)
	require.NoError(t, err, "cross-cluster capacity diff is allowed, not an error")
	assert.True(t, differs)
	assert.Contains(t, out, "cluster_id: 11111111-1111-1111-1111-111111111111 → 22222222-2222-2222-2222-222222222222")
}
