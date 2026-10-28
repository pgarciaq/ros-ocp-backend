package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/librobne/pvc"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
	"github.com/redhatinsights/ros-ocp-backend/librobne/vm"
)

// termsFixtureLoad builds a fileLoad with no user YAML (documented defaults)
// for the term-window detector tests (#705, ADR-0346).
func termsFixtureLoad(t *testing.T, now time.Time) fileLoad {
	t.Helper()
	return fileLoad{
		cfg:       fileConfig{},
		orgID:     "1234567",
		clusterID: "11111111-2222-3333-4444-555555555555",
		now:       now,
	}
}

// TestVMTerms_ThirteenDayWindowWithholdsLong is the #705 VM detector
// (ADR-0346): 13 days of data against the documented VM table — short 7d
// (min 3), medium 15d (min 7, clamps to 13 available), long 30d (min 15,
// WITHHELD at 13 days). Pre-fix, robne feeds the container-family table
// (1/7/15, min 1/3/7): long_term emits with 13 days, the short window
// collapses to 1 day, and medium to 7 — all three assertions below fail.
// Stock nise templates cannot detect this (window-insensitive); this
// fixture deliberately straddles the long-term minimum.
func TestVMTerms_ThirteenDayWindowWithholdsLong(t *testing.T) {
	base := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC) // 13 days: Oct 14..26
	var digests []vm.DailyVMDigest
	for i := 0; i < 13; i++ {
		digests = append(digests, vm.DailyVMDigest{
			VMName: "det-vm", Namespace: "production", GuestOS: "linux",
			BucketDate:    base.AddDate(0, 0, i),
			CPUUsageP95MC: 2000, CPURequestMC: 4000,
			MemUsageP95KiB: 8 << 20, MemRequestKiB: 16 << 20, // 8 GiB usage, 16 GiB request
			SampleCount: 24,
		})
	}
	recs, err := recommendVMsFrom(termsFixtureLoad(t, base.AddDate(0, 0, 13)), digests)
	require.NoError(t, err)

	byTerm := map[string]*vm.VMRecommendation{}
	for i := range recs {
		byTerm[recs[i].Term] = &recs[i]
	}
	assert.NotContains(t, byTerm, "long_term",
		"13 days is below the documented 15-day VM long-term minimum; the term must be withheld")
	require.Contains(t, byTerm, "short_term")
	require.NotNil(t, byTerm["short_term"].ExplDataDays)
	assert.Equal(t, 7, *byTerm["short_term"].ExplDataDays,
		"the documented VM short window is 7 days, not the container table's 1")
	require.Contains(t, byTerm, "medium_term")
	require.NotNil(t, byTerm["medium_term"].ExplDataDays)
	assert.Equal(t, 13, *byTerm["medium_term"].ExplDataDays,
		"the documented VM medium window (15d) clamps to all 13 available days, not 7")
}

// TestPVCTerms_MediumUsesAllAvailableDays is the #705 PVC detector
// (ADR-0346): a window-sensitive fixture — near-full usage (9.8 of 10 GiB)
// on days 1–2, 6.5 GiB after, 13 days. Documented PVC table: short 7 /
// medium 30 / long 90. medium (clamped to all 13 days) must see the early
// near-full days and classify near_full; short (the last 7 days) sees only
// the post-drop usage and must not. Pre-fix, robne's medium falls back to
// the 7-day container window and classifies like short — the assertion on
// medium fails. The stock ocp_report_pvc_rightsizing template is
// window-insensitive by construction and cannot detect this.
func TestPVCTerms_MediumUsesAllAvailableDays(t *testing.T) {
	base := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	var rows []pvc.PVCDigestRow
	for i := 0; i < 13; i++ {
		usage := int64(6988000000) // ≈ 6.5 GiB post-drop
		if i < 2 {
			usage = int64(10520000000) // ≈ 9.8 GiB near-full
		}
		rows = append(rows, pvc.PVCDigestRow{
			BucketDate:    base.AddDate(0, 0, i),
			Namespace:     "e2e",
			PVC:           "det-pvc",
			CapacityBytes: 10 << 30,
			RequestBytes:  10 << 30,
			UsageBytesMin: usage,
			UsageBytesMax: usage,
			UsageBytesAvg: usage,
			LastSeenPod:   "holder",
			StorageClass:  "gp3-csi",
		})
	}
	fl := termsFixtureLoad(t, base.AddDate(0, 0, 13))
	fl.pvcGrouped = map[pvc.PVCKey][]pvc.PVCDigestRow{
		{Namespace: "e2e", PVC: "det-pvc"}: rows,
	}
	recs, err := recommendPVCs(fl)
	require.NoError(t, err)

	byTerm := map[string]pvc.PVCRec{}
	for _, r := range recs {
		byTerm[r.Term] = r
	}
	require.Contains(t, byTerm, "short")
	require.Contains(t, byTerm, "medium")
	require.Contains(t, byTerm, "long")
	assert.NotEqual(t, "near_full", byTerm["short"].RecommendationType,
		"the 7-day short window sees only post-drop usage")
	assert.Equal(t, "near_full", byTerm["medium"].RecommendationType,
		"the medium window (30d, clamped to 13) must see the early near-full days")
}

func windowDays(ts []types.TermConfig) []int {
	out := make([]int, len(ts))
	for i, t := range ts {
		out[i] = t.WindowDays
	}
	return out
}

// TestTerms_DefaultsMatchDocumentedTables pins that every term-bearing
// plugin resolves its documented table when nothing is configured — the
// container family shares 1/7/15 (the top-level terms: source), VM is
// 7/15/30, PVC is 7/30/90 (ADR-0346 single-source constants).
func TestTerms_DefaultsMatchDocumentedTables(t *testing.T) {
	cfg := fileConfig{}
	for _, p := range []string{"container", "namespace", "node", "gpu"} {
		assert.Equal(t, []int{1, 7, 15}, windowDays(effectiveEngineTerms(p, cfg)),
			"%s uses the container-family table", p)
	}
	assert.Equal(t, []int{7, 15, 30}, windowDays(effectiveEngineTerms("vm", cfg)))
	assert.Equal(t, []int{7, 30, 90}, windowDays(effectiveEngineTerms("pvc", cfg)))
}

// TestReadWindow_MaxEffectiveLookback pins the Path-A SELECT window rule
// (ADR-0346): the read window is the maximum lookback across all effective
// tables, not the container maximum — 15d would truncate VM (30d) and PVC
// (90d) long-term data.
func TestReadWindow_MaxEffectiveLookback(t *testing.T) {
	assert.Equal(t, 90, maxEffectiveLookbackDays(fileConfig{}),
		"defaults: PVC long (90d) governs the read window")
	cfg := fileConfig{PluginTerms: map[string][]termYAML{
		"pvc": {{Name: "long", WindowDays: 45}},
	}}
	assert.Equal(t, 45, maxEffectiveLookbackDays(cfg),
		"an override replaces that plugin's table; the global max follows")
	cfg = fileConfig{PluginTerms: map[string][]termYAML{
		"vm": {{Name: "long_term", WindowDays: 20}},
	}}
	assert.Equal(t, 90, maxEffectiveLookbackDays(cfg),
		"narrowing one plugin never truncates the others' read window")
}

// TestPluginTerms_UnknownPluginError pins the unknown-key rule (spec §2):
// typos and term-less plugins alike are errors naming the six valid keys.
func TestPluginTerms_UnknownPluginError(t *testing.T) {
	for _, key := range []string{"containers", "snapshot", "quota", "cluster-quota", "hcp"} {
		cfg := fileConfig{PluginTerms: map[string][]termYAML{
			key: {{Name: "short", WindowDays: 7, MinDataDays: 3}},
		}}
		err := validatePluginTerms(cfg)
		require.Error(t, err, "plugin key %q must be rejected", key)
		assert.Contains(t, err.Error(), "unknown plugin")
		assert.Contains(t, err.Error(), "container, namespace, node, gpu, vm, pvc")
	}
}

// TestPluginTerms_AmbiguityError pins the both-keys rule: terms: is
// container-family-scoped, so also setting plugin_terms.container is
// ambiguous and a hard error.
func TestPluginTerms_AmbiguityError(t *testing.T) {
	cfg := fileConfig{
		Terms:       []termYAML{{Name: "short", WindowDays: 1, MinDataDays: 1}},
		PluginTerms: map[string][]termYAML{"container": {{Name: "short", WindowDays: 2, MinDataDays: 1}}},
	}
	err := validatePluginTerms(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "both set")
	// A terms: list plus a NON-container plugin_terms entry is fine.
	cfg.PluginTerms = map[string][]termYAML{"vm": {{Name: "short_term", WindowDays: 10, MinDataDays: 5}}}
	assert.NoError(t, validatePluginTerms(cfg))
}

// TestPluginTerms_BoundsAndEmptiness pins the validation bounds mirrored
// from the server's Settings API (VM/container max 90, PVC max 365;
// min 1..window) and the empty-list rule.
func TestPluginTerms_BoundsAndEmptiness(t *testing.T) {
	cases := []struct {
		name string
		cfg  fileConfig
		want string
	}{
		{"window over PVC max", fileConfig{PluginTerms: map[string][]termYAML{"pvc": {{Name: "long", WindowDays: 366, MinDataDays: 30}}}}, "1..365"},
		{"window over VM max", fileConfig{PluginTerms: map[string][]termYAML{"vm": {{Name: "long_term", WindowDays: 91, MinDataDays: 15}}}}, "1..90"},
		{"window zero", fileConfig{PluginTerms: map[string][]termYAML{"vm": {{Name: "short_term", WindowDays: 0, MinDataDays: 1}}}}, "1..90"},
		{"min over window", fileConfig{PluginTerms: map[string][]termYAML{"vm": {{Name: "short_term", WindowDays: 7, MinDataDays: 8}}}}, "1..window_days"},
		{"min zero", fileConfig{PluginTerms: map[string][]termYAML{"pvc": {{Name: "short", WindowDays: 7, MinDataDays: 0}}}}, "specify it fully"},
		{"empty list", fileConfig{PluginTerms: map[string][]termYAML{"vm": {}}}, "empty term list"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePluginTerms(tc.cfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestPluginTerms_OverrideIsolation pins requirement 3 (#705): overriding
// one plugin's terms affects only that plugin — the container family keeps
// resolving the documented table (or the terms: list), byte-for-byte.
func TestPluginTerms_OverrideIsolation(t *testing.T) {
	base := fileConfig{}
	vmOverride := fileConfig{PluginTerms: map[string][]termYAML{
		"vm": {{Name: "short_term", WindowDays: 10, MinDataDays: 5}},
	}}
	assert.Equal(t, windowDays(effectiveEngineTerms("vm", vmOverride)), []int{10},
		"the override replaces the VM table")
	assert.Equal(t, effectiveEngineTerms("container", vmOverride), effectiveEngineTerms("container", base),
		"container is untouched by a VM override")
	assert.Equal(t, effectiveEngineTerms("pvc", vmOverride), effectiveEngineTerms("pvc", base),
		"pvc is untouched by a VM override")

	nsOverride := fileConfig{PluginTerms: map[string][]termYAML{
		"namespace": {{Name: "short", WindowDays: 2, MinDataDays: 1}},
	}}
	assert.Equal(t, windowDays(effectiveEngineTerms("namespace", nsOverride)), []int{2})
	assert.Equal(t, effectiveEngineTerms("container", nsOverride), effectiveEngineTerms("container", base),
		"a namespace override does not leak into container (requirement 3)")

	// The top-level terms: list stays family-scoped: it reaches the
	// container family but never VM or PVC.
	familyCfg := fileConfig{Terms: []termYAML{{Name: "short", WindowDays: 3, MinDataDays: 1}}}
	assert.Equal(t, []int{3}, windowDays(effectiveEngineTerms("node", familyCfg)),
		"terms: reaches the container family (back-compat)")
	assert.Equal(t, effectiveEngineTerms("vm", familyCfg), effectiveEngineTerms("vm", base),
		"terms: never reaches VM (the #705 fix)")
	assert.Equal(t, effectiveEngineTerms("pvc", familyCfg), effectiveEngineTerms("pvc", base),
		"terms: never reaches PVC (the #705 fix)")
}

// TestPluginTerms_YAMLLoadAndResolve runs the full config path: a YAML
// file with plugin_terms loads, validates, and resolves.
func TestPluginTerms_YAMLLoadAndResolve(t *testing.T) {
	cwd := t.TempDir()
	yaml := "org_id: \"1234567\"\ncluster_uuid: \"11111111-2222-3333-4444-555555555555\"\n" +
		"plugin_terms:\n  vm:\n    - name: short_term\n      window_days: 10\n      min_data_days: 5\n"
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "robne.yaml"), []byte(yaml), 0o600))
	cfg, err := loadFileConfig(overlayEnv{NoUser: true, Cwd: cwd}, "")
	require.NoError(t, err)
	require.NoError(t, validateFileConfig(cfg))
	assert.Equal(t, []int{10}, windowDays(effectiveEngineTerms("vm", cfg)))
	assert.Equal(t, []int{7, 15, 30}, windowDays(effectiveEngineTerms("vm", fileConfig{})),
		"defaults are untouched by an unrelated config file")
}
