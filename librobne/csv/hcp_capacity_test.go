package csv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The 25-column header is the operator-side contract (KMO
// TestHCPCapacityRow_Contract pins the same list); changes coordinate both
// sides plus the shared fixture.
const hcpCapacityTestHeader = "observed_at,mce_sampled_at,operator_version,ocp_version,mce_version," +
	"native_model_version,collection_interval_minutes,upload_cycle_minutes," +
	"inventory_complete,current_hc_count,ready_hcp_count," +
	"average_ready_hcp_qps,ready_hcp_qps_known,hosted_availability_basis," +
	"eligible_worker_cpu_allocatable_millicores,eligible_worker_memory_allocatable_bytes," +
	"eligible_worker_max_pods,worker_placement_known,pressure_state," +
	"mce_request_max_hcs,mce_low_qps_max_hcs,mce_average_qps_max_hcs," +
	"mce_medium_qps_max_hcs,mce_high_qps_max_hcs,shared_infrastructure"

// validHCPCapacityRow mirrors the hand-verified librobne/hcp fixture: 64
// vCPU / 128 GiB / 1500 maxPods, 4 HCs, 2 ready, 300 QPS observed mean.
const validHCPCapacityRow = "2026-10-25T14:59:59Z,2026-10-25T14:59:00Z,deadbeef,4.22.17,," +
	"hcp-published-sizing-v1,60,360,true,4,2,300,true,highly_available," +
	"64000,137438953472,1500,true,unknown,8,,8,4,2,false"

func hcpCapacityCSV(rows ...string) string {
	return hcpCapacityTestHeader + "\n" + strings.Join(rows, "\n") + "\n"
}

func TestParseHCPCapacityRows_Valid(t *testing.T) {
	rows, skips, _, err := ParseHCPCapacityRows(strings.NewReader(hcpCapacityCSV(validHCPCapacityRow)))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Empty(t, skips)

	r := rows[0]
	assert.Equal(t, "2026-10-25T14:59:59Z", r.ObservedAt.Format(time.RFC3339))
	require.NotNil(t, r.MCESampledAt)
	assert.Equal(t, "2026-10-25T14:59:00Z", r.MCESampledAt.Format(time.RFC3339))
	assert.Equal(t, "deadbeef", r.OperatorVersion)
	assert.Equal(t, "4.22.17", r.OCPVersion)
	assert.Empty(t, r.MCEVersion)
	assert.True(t, r.InventoryComplete)
	assert.Equal(t, int64(4), r.CurrentHCCount)
	assert.Equal(t, int64(2), r.ReadyHCPCount)
	assert.InDelta(t, 300.0, r.AverageReadyHCPQPS, 1e-9)
	assert.True(t, r.ReadyHCPQPSKnown)
	assert.Equal(t, "highly_available", r.HostedAvailabilityBasis)
	assert.Equal(t, int64(64000), r.EligibleCPUMillicores)
	assert.Equal(t, int64(137438953472), r.EligibleMemoryBytes)
	assert.Equal(t, int64(1500), r.EligibleMaxPods)
	assert.True(t, r.WorkerPlacementKnown)
	assert.False(t, r.SharedInfrastructure)
	require.NotNil(t, r.MCERequestMaxHCs)
	assert.Equal(t, int64(8), *r.MCERequestMaxHCs)
	assert.Nil(t, r.MCELowQPSMaxHCs, "absent MCE reference parses nil, never zero")
}

// Malformed or invalid rows skip with reasons — never zero-filled, never a
// file abort (the poison contract: a capacity file must not gate other
// entities). Every skip carries a non-empty Reason naming the mechanism.
func TestParseHCPCapacityRows_SkipMalformed(t *testing.T) {
	bad := func(mutate func(fields []string) []string) string {
		f := strings.Split(validHCPCapacityRow, ",")
		return strings.Join(mutate(f), ",")
	}
	cases := []struct {
		name   string
		row    string
		why    string
		reason string
	}{
		{"bad timestamp", strings.Replace(validHCPCapacityRow, "2026-10-25T14:59:59Z", "not-a-time", 1), "unparseable observed_at skips", "observed_at"},
		{"bad boolean", bad(func(f []string) []string { f[8] = "yes"; return f }), "non-boolean inventory_complete skips", "boolean"},
		{"bad int", bad(func(f []string) []string { f[9] = "four"; return f }), "unparseable count skips", "current_hc_count"},
		{"negative qps known", bad(func(f []string) []string { f[11] = "-3"; return f }), "negative QPS is measurement error", "qps"},
		{"nan qps known", bad(func(f []string) []string { f[11] = "NaN"; return f }), "non-finite QPS skips", "qps"},
		{"qps value when unknown", bad(func(f []string) []string { f[12] = "false"; return f }), "unknown QPS with a non-empty value skips", "qps"},
		{"invalid basis", bad(func(f []string) []string { f[13] = "HighlyAvailable"; return f }), "raw controlPlaneTopology spellings are rejected", "basis"},
		{"wrong model version", bad(func(f []string) []string { f[5] = "hcp-published-sizing-v0"; return f }), "model version must match librobne/hcp", "model"},
		{"bad mce ref", bad(func(f []string) []string { f[19] = "eight"; return f }), "unparseable MCE reference skips", "mce"},
		{"bad pressure", bad(func(f []string) []string { f[18] = "clear"; return f }), "v1 never claims clear — only pressured/unknown", "pressure"},
		{"bad shared infra", bad(func(f []string) []string { f[24] = "yes"; return f }), "non-boolean shared_infrastructure skips", "boolean"},
		{"zero cadence", bad(func(f []string) []string { f[6] = "0"; return f }), "zero collection cadence degenerates the staleness window", "cadence"},
		{"huge cadence", bad(func(f []string) []string { f[7] = "9223372036854775807"; return f }), "absurd upload cadence overflows the staleness window", "cadence"},
		{"short row", strings.Join(strings.Split(validHCPCapacityRow, ",")[:20], ","), "wrong field count skips", "columns"},
	}
	for _, tc := range cases {
		rows, skips, _, err := ParseHCPCapacityRows(strings.NewReader(hcpCapacityCSV(tc.row)))
		require.NoError(t, err, tc.name)
		assert.Empty(t, rows, "%s: %s", tc.name, tc.why)
		require.Len(t, skips, 1, "%s: %s", tc.name, tc.why)
		assert.NotEmpty(t, skips[0].Reason, "%s: skip must carry a reason", tc.name)
		assert.Contains(t, skips[0].Reason, tc.reason, "%s: reason names the mechanism (got %q)", tc.name, skips[0].Reason)
		assert.Equal(t, 1, skips[0].Index, "%s: row index is 1-based after the header", tc.name)
	}
}

func TestParseHCPCapacityRows_HeaderMismatch(t *testing.T) {
	_, _, _, err := ParseHCPCapacityRows(strings.NewReader("observed_at,operator_version\n2026-10-25T14:59:59Z,deadbeef\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "columns")
}

// The future-skew rule is deliberately split from time-independent
// validation: Load has no clock; compute (CLI --now / ingestion wall clock)
// applies the skew check. HCPCapacitySkewCheck is the single source.
func TestHCPCapacitySkewCheck(t *testing.T) {
	now := time.Date(2026, 10, 25, 15, 0, 0, 0, time.UTC)
	rowAt := func(offset time.Duration) HCPCapacityRow {
		f := strings.Split(strings.Replace(validHCPCapacityRow, "2026-10-25T14:59:59Z", now.Add(offset).Format(time.RFC3339), 1), ",")
		rows, _, _, err := ParseHCPCapacityRows(strings.NewReader(hcpCapacityCSV(strings.Join(f, ","))))
		require.NoError(t, err)
		require.Len(t, rows, 1)
		return rows[0]
	}
	rows := []HCPCapacityRow{rowAt(10 * time.Minute), rowAt(3 * time.Minute), rowAt(-time.Hour)}
	kept, dropped := HCPCapacitySkewCheck(rows, now)
	require.Len(t, kept, 2, "3-min-ahead and past rows stay")
	require.Len(t, dropped, 1, "10-min-future row drops")
	assert.Contains(t, dropped[0].Reason, "future-dated")
}

// Parity with the legacy inline rule (round-9 W1): the boundary semantics
// must not change in the split — exactly-at-tolerance is kept (the legacy
// check is strictly After), one second beyond drops.
func TestHCPCapacitySkew_ParityVsLegacy(t *testing.T) {
	now := time.Date(2026, 10, 25, 15, 0, 0, 0, time.UTC)
	const tolerance = 5 * time.Minute
	for _, tc := range []struct {
		name   string
		offset time.Duration
		legacy bool // legacy rule: skip iff observedAt.After(now.Add(tolerance))
	}{
		{"exactly at tolerance", tolerance, false},
		{"one second beyond", tolerance + time.Second, true},
		{"one second within", tolerance - time.Second, false},
		{"deep past", -90 * 24 * time.Hour, false},
	} {
		observed := now.Add(tc.offset)
		legacyDrop := observed.After(now.Add(tolerance))
		assert.Equal(t, tc.legacy, legacyDrop, "%s: legacy expectation", tc.name)

		row := HCPCapacityRow{ObservedAt: observed}
		kept, dropped := HCPCapacitySkewCheck([]HCPCapacityRow{row}, now)
		if legacyDrop {
			assert.Empty(t, kept, "%s: new rule drops", tc.name)
			assert.Len(t, dropped, 1, "%s: new rule drops with reason", tc.name)
		} else {
			assert.Len(t, kept, 1, "%s: new rule keeps", tc.name)
			assert.Empty(t, dropped, "%s: new rule keeps", tc.name)
		}
	}
}

// --- Load wiring (poison contract: a capacity file never gates others) ---

func writeCapacityFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600))
}

func containerBody() string {
	return niseHeader() + "\n" +
		niseRow("app", "api", "2026-08-01 00:00:00 +0000 UTC", "2026-08-01 01:00:00 +0000 UTC", "0.1", "0.05") + "\n"
}

func TestLoad_CapacityRows(t *testing.T) {
	dir := t.TempDir()
	writeCapacityFixture(t, dir, "ros-openshift-hcp-capacity-202610.csv", hcpCapacityCSV(validHCPCapacityRow))
	got, err := Load(dir)
	require.NoError(t, err)
	require.Len(t, got.HCPCapacityRows, 1)
	assert.Empty(t, got.Warnings)
	assert.Equal(t, int64(4), got.HCPCapacityRows[0].CurrentHCCount)
}

func TestLoad_CapacityPoisonMixed(t *testing.T) {
	dir := t.TempDir()
	writeCapacityFixture(t, dir, "ocp_ros_usage.csv", containerBody())
	// Header mismatch is file-level poison: warn and continue — the
	// container rows must survive (W4 dispatch contract).
	writeCapacityFixture(t, dir, "ros-openshift-hcp-capacity-202610.csv", "observed_at,operator_version\nnope,deadbeef\n")
	got, err := Load(dir)
	require.NoError(t, err, "poisoned capacity file must not abort the load")
	require.Len(t, got.Rows, 1, "container rows unaffected")
	require.Len(t, got.Warnings, 1)
	assert.Equal(t, "hcp_capacity", got.Warnings[0].Kind)
	assert.Equal(t, "ros-openshift-hcp-capacity-202610.csv", got.Warnings[0].File)
	assert.NotEmpty(t, got.Warnings[0].Reason)
}

func TestLoad_CapacityOnlyPoisonedAuto(t *testing.T) {
	dir := t.TempDir()
	writeCapacityFixture(t, dir, "ros-openshift-hcp-capacity-202610.csv", "observed_at,operator_version\nnope,deadbeef\n")
	got, err := Load(dir)
	require.NoError(t, err, "capacity-only poisoned input loads (warning) instead of ErrNoROSFiles")
	assert.Empty(t, got.HCPCapacityRows)
	require.Len(t, got.Warnings, 1)
}

func TestLoad_CapacityOnlyHeaderOnly(t *testing.T) {
	// Header-only with zero data rows counts as a capacity attempt
	// (snapshot precedent, round-9 N5): Load succeeds, no rows, no error.
	dir := t.TempDir()
	writeCapacityFixture(t, dir, "ros-openshift-hcp-capacity-202610.csv", hcpCapacityTestHeader+"\n")
	got, err := Load(dir)
	require.NoError(t, err)
	assert.Empty(t, got.HCPCapacityRows)
	assert.Empty(t, got.Warnings)
}

// buildExtraColumnHeader appends a 26th name to the contract header.
func buildExtraColumnHeader() string {
	return hcpCapacityTestHeader + ",future_operator_column"
}

// TestParseHCPCapacityRows_ExtraColumnTolerated (#704 option 3): an
// operator adding a 26th column must not break old readers — the row
// parses, and the extra name is reported for the caller to warn.
// Pre-fix (exact-sequence gate): the 26-name header was a file-level
// rejection, fleet-wide.
func TestParseHCPCapacityRows_ExtraColumnTolerated(t *testing.T) {
	row := validHCPCapacityRow + ",42"
	rows, _, extras, err := ParseHCPCapacityRows(strings.NewReader(buildExtraColumnHeader() + "\n" + row + "\n"))
	require.NoError(t, err, "an extra column is tolerated (name-based, #704)")
	require.Len(t, rows, 1)
	assert.Equal(t, "deadbeef", rows[0].OperatorVersion, "values land by name, not position")
	assert.Equal(t, []string{"future_operator_column"}, extras, "the extra name is reported")
}

// TestParseHCPCapacityRows_ReorderHarmless (#704 option 3): a reordered
// header parses correctly — order-insensitive like every other ROS CSV
// parser. Pre-fix: file-level rejection.
func TestParseHCPCapacityRows_ReorderHarmless(t *testing.T) {
	names := strings.Split(hcpCapacityTestHeader, ",")
	vals := strings.Split(validHCPCapacityRow, ",")
	// Swap two non-adjacent columns (operator_version <-> ocp_version).
	names[2], names[3] = names[3], names[2]
	vals[2], vals[3] = vals[3], vals[2]
	rows, _, extras, err := ParseHCPCapacityRows(strings.NewReader(strings.Join(names, ",") + "\n" + strings.Join(vals, ",") + "\n"))
	require.NoError(t, err, "a reordered header is harmless (name-based, #704)")
	require.Len(t, rows, 1)
	assert.Equal(t, "deadbeef", rows[0].OperatorVersion, "operator_version found by name after the swap")
	assert.Empty(t, extras)
}

// TestParseHCPCapacityRows_MissingColumnNamesTheColumn (#704 option 3):
// a missing or renamed required name is a named file-level error listing
// what is absent — loud, never silent.
func TestParseHCPCapacityRows_MissingColumnNamesTheColumn(t *testing.T) {
	names := strings.Split(hcpCapacityTestHeader, ",")
	names = append(names[:4], names[5:]...) // drop mce_version (index 4)
	_, _, _, err := ParseHCPCapacityRows(strings.NewReader(strings.Join(names, ",") + "\n" + validHCPCapacityRow + "\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mce_version", "the error names the missing column")
	assert.Contains(t, err.Error(), "missing columns", "and the failure class")
}
