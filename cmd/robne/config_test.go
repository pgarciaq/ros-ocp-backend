package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestYAMLOverlay_ReplacesWholeTopLevelKey(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "robne"), 0o700))
	user := []byte("sizing:\n  cpu_cost_percentile: 0.60\n  min_margin: 1.40\nidle:\n  enabled: true\n")
	require.NoError(t, os.WriteFile(filepath.Join(home, ".config", "robne", "robne.yaml"), user, 0o600))
	proj := []byte("plugins:\n  - container\nsizing:\n  cpu_cost_percentile: 0.80\n")
	require.NoError(t, os.WriteFile(filepath.Join(cwd, "robne.yaml"), proj, 0o600))

	cfg, err := loadFileConfig(overlayEnv{Home: home, Cwd: cwd}, "")
	require.NoError(t, err)
	assert.Equal(t, 0.80, cfg.Sizing.CPUCostPercentile)
	assert.Equal(t, 0.0, cfg.Sizing.MinMargin, "project sizing: replaces the whole key; min_margin must not leak from the user file")
	assert.True(t, cfg.Idle.Enabled, "idle from the user file is kept when the project file omits it")
	assert.Equal(t, []string{"container"}, cfg.Plugins)
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

func TestResolvePlugins_NotPhase1(t *testing.T) {
	err := validatePlugins(fileConfig{Plugins: []string{"node"}}, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Phase 1")
}
