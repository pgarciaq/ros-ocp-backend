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

func TestYAMLOverlay_BusinessHoursEnabled(t *testing.T) {
	cwd := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "robne.yaml"), []byte("business_hours:\n  enabled: true\n"), 0o600))
	_, err := loadFileConfig(overlayEnv{Home: t.TempDir(), Cwd: cwd, NoUser: true}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Phase 1")
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
	err := validatePlugins(fileConfig{Plugins: []string{"snapshot"}}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown plugin")
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
