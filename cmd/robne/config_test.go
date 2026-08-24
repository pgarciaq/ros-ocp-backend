package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestYAMLOverlay_ReplacesWholeTopLevelKey(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "robne"), 0o700))
	user := completeSizingYAML(0.60, 1.40) + "idle:\n  enabled: true\n"
	require.NoError(t, os.WriteFile(filepath.Join(home, ".config", "robne", "robne.yaml"), []byte(user), 0o600))
	proj := "plugins:\n  - container\n" + completeSizingYAML(0.80, 1.15)
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "robne.yaml"), []byte(proj), 0o600))

	cfg, err := loadFileConfig(overlayEnv{Home: home, Cwd: cwd}, "")
	require.NoError(t, err)
	assert.Equal(t, 0.80, cfg.Sizing.CPUCostPercentile)
	assert.Equal(t, 1.15, cfg.Sizing.MinMargin, "project sizing: replaces the whole key; min_margin must not leak from the user file")
	assert.True(t, cfg.Idle.Enabled, "idle from the user file is kept when the project file omits it")
	assert.Equal(t, []string{"container"}, cfg.Plugins)
}

func TestYAMLOverlay_IncompleteSizingError(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "robne.yaml"), []byte("sizing:\n  cpu_cost_percentile: 0.80\n"), 0o600))
	_, err := loadFileConfig(overlayEnv{Home: t.TempDir(), Cwd: cwd, NoUser: true}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sizing:")
	assert.Contains(t, err.Error(), "min_margin")
}

func completeSizingYAML(cpuCost, minMargin float64) string {
	return "sizing:\n" +
		"  cpu_cost_percentile: " + formatFloat(cpuCost) + "\n" +
		"  cpu_perf_percentile: 0.98\n" +
		"  mem_cost_percentile: 0.95\n" +
		"  mem_perf_percentile: 1.0\n" +
		"  min_margin: " + formatFloat(minMargin) + "\n" +
		"  max_margin: 1.50\n" +
		"  limit_multiplier: 1.05\n" +
		"  cpu_floor_mc: 25\n" +
		"  mem_floor_kib: 4096\n" +
		"  idle_cpu_threshold_mc: 10\n" +
		"  idle_mem_threshold_kib: 10240\n" +
		"  mem_trend_slope_threshold: 100.0\n" +
		"  low_confidence_threshold: 0.5\n" +
		"  sparse_data_threshold: 2\n"
}

func formatFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func TestYAMLOverlay_UnknownKey(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "robne.yaml"), []byte("not_a_real_key: 1\n"), 0o600))
	_, err := loadFileConfig(overlayEnv{Home: t.TempDir(), Cwd: cwd, NoUser: true}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not_a_real_key")
}

func TestYAMLOverlay_EmptyTermsError(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "robne.yaml"), []byte("terms: []\n"), 0o600))
	_, err := loadFileConfig(overlayEnv{Home: t.TempDir(), Cwd: cwd, NoUser: true}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "terms")
}

func TestYAMLOverlay_BusinessHoursEnabledRequiresFields(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "robne.yaml"), []byte("business_hours:\n  enabled: true\n"), 0o600))
	_, err := loadFileConfig(overlayEnv{Home: t.TempDir(), Cwd: cwd, NoUser: true}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timezone")
}

func TestYAMLOverlay_BusinessHoursValid(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "robne.yaml"), []byte(validBusinessHoursYAML()), 0o600))
	cfg, err := loadFileConfig(overlayEnv{Home: t.TempDir(), Cwd: cwd, NoUser: true}, "")
	require.NoError(t, err)
	require.True(t, businessHoursEnabled(cfg))
	assert.Equal(t, "Europe/Madrid", cfg.BusinessHours.Timezone)
	assert.Equal(t, "09:00", cfg.BusinessHours.StartTime)
	assert.Equal(t, "20:00", cfg.BusinessHours.EndTime)
}

func TestYAMLOverlay_BusinessHoursOvernightOK(t *testing.T) {
	cwd := t.TempDir()
	body := `business_hours:
  enabled: true
  timezone: UTC
  days: [monday]
  start_time: "22:00"
  end_time: "06:00"
`
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "robne.yaml"), []byte(body), 0o600))
	cfg, err := loadFileConfig(overlayEnv{Home: t.TempDir(), Cwd: cwd, NoUser: true}, "")
	require.NoError(t, err)
	require.True(t, businessHoursEnabled(cfg))
}

func TestYAMLOverlay_BusinessHoursEqualTimesError(t *testing.T) {
	cwd := t.TempDir()
	body := `business_hours:
  enabled: true
  timezone: UTC
  days: [monday]
  start_time: "09:00"
  end_time: "09:00"
`
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "robne.yaml"), []byte(body), 0o600))
	_, err := loadFileConfig(overlayEnv{Home: t.TempDir(), Cwd: cwd, NoUser: true}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "zero-width")
}

func TestYAMLOverlay_BusinessHoursInvalidTimezone(t *testing.T) {
	cwd := t.TempDir()
	body := `business_hours:
  enabled: true
  timezone: Not/A_Zone
  days: [monday]
  start_time: "09:00"
  end_time: "17:00"
`
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "robne.yaml"), []byte(body), 0o600))
	_, err := loadFileConfig(overlayEnv{Home: t.TempDir(), Cwd: cwd, NoUser: true}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "IANA")
}

func TestYAMLOverlay_BusinessHoursUnknownNestedKey(t *testing.T) {
	cwd := t.TempDir()
	body := validBusinessHoursYAML() + "  not_a_field: 1\n"
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "robne.yaml"), []byte(body), 0o600))
	_, err := loadFileConfig(overlayEnv{Home: t.TempDir(), Cwd: cwd, NoUser: true}, "")
	require.Error(t, err)
}

func TestYAMLOverlay_BusinessHoursDisabledOK(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "robne.yaml"), []byte("business_hours:\n  enabled: false\n"), 0o600))
	cfg, err := loadFileConfig(overlayEnv{Home: t.TempDir(), Cwd: cwd, NoUser: true}, "")
	require.NoError(t, err)
	assert.False(t, businessHoursEnabled(cfg))
}

func TestYAMLOverlay_BusinessHoursOverlayReplaces(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "robne"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".config", "robne", "robne.yaml"), []byte(validBusinessHoursYAML()), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "robne.yaml"), []byte("business_hours:\n  enabled: false\n"), 0o600))
	cfg, err := loadFileConfig(overlayEnv{Home: home, Cwd: cwd}, "")
	require.NoError(t, err)
	assert.False(t, businessHoursEnabled(cfg))
	require.NotNil(t, cfg.BusinessHours)
	assert.Empty(t, cfg.BusinessHours.Timezone, "later file replaces the whole business_hours: key")
}

func TestYAMLOverlay_BusinessHoursCapitalDayError(t *testing.T) {
	cwd := t.TempDir()
	body := `business_hours:
  enabled: true
  timezone: UTC
  days: [Monday]
  start_time: "09:00"
  end_time: "17:00"
`
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "robne.yaml"), []byte(body), 0o600))
	_, err := loadFileConfig(overlayEnv{Home: t.TempDir(), Cwd: cwd, NoUser: true}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "lowercase")
}

func TestYAMLOverlay_BusinessHoursOmittedIsOff(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "robne.yaml"), []byte("org_id: \"1234567\"\n"), 0o600))
	cfg, err := loadFileConfig(overlayEnv{Home: t.TempDir(), Cwd: cwd, NoUser: true}, "")
	require.NoError(t, err)
	assert.False(t, businessHoursEnabled(cfg))
	assert.Nil(t, cfg.BusinessHours)
}

func validBusinessHoursYAML() string {
	return `business_hours:
  enabled: true
  timezone: Europe/Madrid
  days: [monday, tuesday, wednesday, thursday, friday]
  start_time: "09:00"
  end_time: "20:00"
`
}

func TestYAMLOverlay_ConfigFlagSkipsCwd(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "robne.yaml"), []byte("org_id: \"from-cwd\"\n"), 0o600))
	flagPath := filepath.Join(t.TempDir(), "ci.yaml")
	require.NoError(t, os.WriteFile(flagPath, []byte("org_id: \"from-flag\"\n"), 0o600))
	cfg, err := loadFileConfig(overlayEnv{Home: t.TempDir(), Cwd: cwd, NoUser: true}, flagPath)
	require.NoError(t, err)
	assert.Equal(t, "from-flag", cfg.OrgID)
}

func TestValidatePlugins_ClusterQuotaAllowed(t *testing.T) {
	require.NoError(t, validatePlugins(fileConfig{Plugins: []string{"cluster_quota"}}, ""))
	require.NoError(t, validatePlugins(fileConfig{}, "container,cluster_quota"))
}

func TestValidatePlugins_UnknownStillRejected(t *testing.T) {
	err := validatePlugins(fileConfig{Plugins: []string{"not_a_plugin"}}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown plugin")
}

func TestValidatePlugins_SnapshotAllowed(t *testing.T) {
	require.NoError(t, validatePlugins(fileConfig{Plugins: []string{"snapshot"}}, ""))
	require.NoError(t, validatePlugins(fileConfig{}, "container,snapshot"))
}

func TestYAMLOverlay_SnapshotEnabled(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "robne.yaml"), []byte("snapshot:\n  enabled: true\n"), 0o600))
	_, err := loadFileConfig(overlayEnv{Home: t.TempDir(), Cwd: cwd, NoUser: true}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Phase 1")
}

func TestYAMLOverlay_EmptyPluginsError(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "robne.yaml"), []byte("plugins: []\n"), 0o600))
	_, err := loadFileConfig(overlayEnv{Home: t.TempDir(), Cwd: cwd, NoUser: true}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "plugins")
}

func TestResolvedPlugins_DefaultAllShipped(t *testing.T) {
	got := resolvedPlugins(fileConfig{}, "")
	assert.Equal(t, allShippedPlugins(), got)
	assert.False(t, pluginsExplicit(fileConfig{}, ""))
	assert.True(t, pluginsExplicit(fileConfig{Plugins: []string{"container"}}, ""))
	assert.True(t, pluginsExplicit(fileConfig{}, "snapshot"))
	assert.Equal(t, []string{"container"}, resolvedPlugins(fileConfig{Plugins: []string{"container"}}, ""))
	assert.Equal(t, []string{"snapshot"}, resolvedPlugins(fileConfig{}, "snapshot"))
}

func TestYAMLOverlay_ClusterQuotaEnabled(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "robne.yaml"), []byte("cluster_quota:\n  enabled: true\n"), 0o600))
	_, err := loadFileConfig(overlayEnv{Home: t.TempDir(), Cwd: cwd, NoUser: true}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Phase 1")
}

func TestValidatePlugins_NamespaceAllowed(t *testing.T) {
	require.NoError(t, validatePlugins(fileConfig{Plugins: []string{"namespace"}}, ""))
	require.NoError(t, validatePlugins(fileConfig{}, "container,namespace"))
}

func TestValidatePlugins_NodeGPUAllowed(t *testing.T) {
	require.NoError(t, validatePlugins(fileConfig{Plugins: []string{"node"}}, ""))
	require.NoError(t, validatePlugins(fileConfig{}, "gpu"))
	require.NoError(t, validatePlugins(fileConfig{}, "container,node,gpu"))
}

func TestValidatePlugins_YAMLBusinessHoursExplicitNodeGPUVMAllowed(t *testing.T) {
	enabled := true
	bh := &businessHoursYAML{
		Enabled:   &enabled,
		Timezone:  "America/New_York",
		Days:      []string{"monday"},
		StartTime: "08:00",
		EndTime:   "17:00",
	}
	require.NoError(t, validatePlugins(fileConfig{BusinessHours: bh}, "node"))
	require.NoError(t, validatePlugins(fileConfig{BusinessHours: bh}, "gpu"))
	require.NoError(t, validatePlugins(fileConfig{BusinessHours: bh}, "vm"))
	require.NoError(t, validatePlugins(fileConfig{BusinessHours: bh, Plugins: []string{"node"}}, ""))
	require.NoError(t, validatePlugins(fileConfig{BusinessHours: bh}, "container"))
	require.NoError(t, validatePlugins(fileConfig{BusinessHours: bh}, ""))
}

func TestValidatePlugins_PVCAllowed(t *testing.T) {
	require.NoError(t, validatePlugins(fileConfig{Plugins: []string{"pvc"}}, ""))
	require.NoError(t, validatePlugins(fileConfig{}, "container,pvc"))
}

func TestValidatePlugins_VMAllowed(t *testing.T) {
	require.NoError(t, validatePlugins(fileConfig{Plugins: []string{"vm"}}, ""))
	require.NoError(t, validatePlugins(fileConfig{}, "container,vm"))
}

func TestValidatePlugins_QuotaAllowed(t *testing.T) {
	require.NoError(t, validatePlugins(fileConfig{Plugins: []string{"quota"}}, ""))
	require.NoError(t, validatePlugins(fileConfig{}, "container,quota"))
}
