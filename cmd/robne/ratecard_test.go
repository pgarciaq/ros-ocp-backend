package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/librobne/csv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRateCardOverlay_MergesByClusterID(t *testing.T) {
	home := t.TempDir()
	cwd := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "robne"), 0o700))
	user := rateCardFile{
		Clusters: map[string]clusterRates{
			"cluster-power-prod": {CPU: cpuRates{DefaultDollarsPerCoreHour: 0.055, ByArchitecture: map[string]float64{"s390x": 0.060}}},
			"cluster-arm-gpu":    {CPU: cpuRates{DefaultDollarsPerCoreHour: 0.031}},
		},
	}
	writeRateCard(t, filepath.Join(home, ".config", "robne", "rate-card.json"), user)
	proj := rateCardFile{
		Clusters: map[string]clusterRates{
			"cluster-power-prod": {CPU: cpuRates{DefaultDollarsPerCoreHour: 0.099}},
		},
	}
	writeRateCard(t, filepath.Join(cwd, "rate-card.json"), proj)

	got, err := loadRateCardFile(overlayEnv{Home: home, Cwd: cwd}, "")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, 0.099, got.Clusters["cluster-power-prod"].CPU.DefaultDollarsPerCoreHour)
	assert.Empty(t, got.Clusters["cluster-power-prod"].CPU.ByArchitecture, "later cluster object replaces nested maps")
	assert.Equal(t, 0.031, got.Clusters["cluster-arm-gpu"].CPU.DefaultDollarsPerCoreHour)
}

func TestLookupCPU_ArchReplacesDefault(t *testing.T) {
	cpu := cpuRates{
		DefaultDollarsPerCoreHour: 0.031,
		ByArchitecture:            map[string]float64{"arm64": 0.022},
		ByInstanceType:            map[string]float64{"m6g.xlarge": 0.040},
	}
	assert.Equal(t, 0.040, lookupCPU(cpu, csv.RowMeta{InstanceType: "m6g.xlarge", Arch: "arm64"}))
	assert.Equal(t, 0.022, lookupCPU(cpu, csv.RowMeta{Arch: "arm64"}))
	assert.Equal(t, 0.031, lookupCPU(cpu, csv.RowMeta{}))
}

func TestRateCardForRow_MissingCluster(t *testing.T) {
	file := &rateCardFile{Clusters: map[string]clusterRates{"a": {}}}
	_, err := rateCardForRow(file, "missing", csv.RowMeta{}, 744)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no cluster")
}

func writeRateCard(t *testing.T, path string, card rateCardFile) {
	t.Helper()
	b, err := json.Marshal(card)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, b, 0o600))
}
