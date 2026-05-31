package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func TestNormalizePreferenceClass(t *testing.T) {
	assert.Equal(t, vmSeriesComputeOptimized, NormalizePreferenceClass("compute-intensive"))
	assert.Equal(t, vmSeriesComputeOptimized, NormalizePreferenceClass("compute"))
	assert.Equal(t, vmSeriesMemoryOptimized, NormalizePreferenceClass("memory-intensive"))
	assert.Equal(t, vmSeriesMemoryOptimized, NormalizePreferenceClass("memory"))
	assert.Equal(t, vmSeriesGeneralPurpose, NormalizePreferenceClass("general-purpose"))
	assert.Equal(t, "", NormalizePreferenceClass("unknown-label"))
}

func TestVMPreferenceContext_SeriesForVM(t *testing.T) {
	ctx := buildVMPreferenceContext(
		[]ClusterPreferenceRecord{{Name: "database", Class: "memory-intensive"}},
		map[string]string{"finance/db": "database"},
	)
	assert.Equal(t, vmSeriesMemoryOptimized, ctx.SeriesForVM("finance", "db", vmSeriesComputeOptimized))
	assert.Equal(t, vmSeriesComputeOptimized, ctx.SeriesForVM("finance", "other", vmSeriesComputeOptimized))
}

func TestParseClusterInstanceTypesJSON_WithPreferences(t *testing.T) {
	raw := strings.NewReader(`{
		"cluster_uuid": "550e8400-e29b-41d4-a716-446655440000",
		"collected_at": "2026-05-31T20:00:00Z",
		"instance_types": [{"name": "u1.large", "series": "general-purpose", "vcpu": 2, "memory_gib": 8}],
		"preferences": [{"name": "database", "class": "memory-intensive"}],
		"vm_preferences": {"production/db-server-01": "database"}
	}`)
	doc, err := ParseClusterInstanceTypesJSON(raw)
	require.NoError(t, err)
	require.Len(t, doc.Preferences, 1)
	assert.Equal(t, "database", doc.Preferences[0].Name)
	assert.Equal(t, "database", doc.VMPreferences["production/db-server-01"])
}

func TestParseClusterInstanceTypesJSON_NoPreferences(t *testing.T) {
	raw := strings.NewReader(`{
		"cluster_uuid": "550e8400-e29b-41d4-a716-446655440000",
		"instance_types": [{"name": "u1.large", "series": "general-purpose", "vcpu": 2, "memory_gib": 8}]
	}`)
	doc, err := ParseClusterInstanceTypesJSON(raw)
	require.NoError(t, err)
	assert.Empty(t, doc.Preferences)
	assert.Nil(t, doc.VMPreferences)
}

func TestVMPreference_OverridesRatioClassification(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.EnableInstanceTypeMatching = true

	base := timeMustParse("2026-05-01")
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.Namespace = "production"
		d.VMName = "cpu-heavy"
		d.CPURequestMC = 20000
		d.CPUUsageP95MC = 20000
		d.CPUUsageMaxMC = 20000
		d.MemRequestKiB = 2 * 1024 * 1024
		d.MemUsageP95KiB = 1 * 1024 * 1024
		d.MemUsageMaxKiB = 1 * 1024 * 1024
	})
	prefCtx := buildVMPreferenceContext(
		[]ClusterPreferenceRecord{{Name: "database", Class: "memory-intensive"}},
		map[string]string{"production/cpu-heavy": "database"},
	)

	rec, err := RecommendVM(digests, cfg, vmTestTerm(), vmEngineCost, nil, prefCtx)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.NotNil(t, rec.RecommendedSeries)
	assert.Equal(t, vmSeriesMemoryOptimized, *rec.RecommendedSeries)
	require.NotNil(t, rec.RecommendedInstanceType)
}

func TestVMPreference_NoPreference_UsesRatio(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.EnableInstanceTypeMatching = true

	base := timeMustParse("2026-05-01")
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.Namespace = "production"
		d.VMName = "cpu-heavy"
		d.CPURequestMC = 20000
		d.CPUUsageP95MC = 20000
		d.CPUUsageMaxMC = 20000
		d.MemRequestKiB = 2 * 1024 * 1024
		d.MemUsageP95KiB = 1 * 1024 * 1024
		d.MemUsageMaxKiB = 1 * 1024 * 1024
	})

	rec, err := RecommendVM(digests, cfg, vmTestTerm(), vmEngineCost, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.NotNil(t, rec.RecommendedSeries)
	assert.Equal(t, vmSeriesComputeOptimized, *rec.RecommendedSeries)
}

func TestVMPreference_UnknownPreference_FallsBackToRatio(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.EnableInstanceTypeMatching = true

	base := timeMustParse("2026-05-01")
	digests := vmDigestDays(base, 3, func(d *model.DailyVMDigest) {
		d.Namespace = "production"
		d.VMName = "cpu-heavy"
		d.CPURequestMC = 20000
		d.CPUUsageP95MC = 20000
		d.CPUUsageMaxMC = 20000
		d.MemRequestKiB = 2 * 1024 * 1024
		d.MemUsageP95KiB = 1 * 1024 * 1024
		d.MemUsageMaxKiB = 1 * 1024 * 1024
	})
	prefCtx := buildVMPreferenceContext(
		[]ClusterPreferenceRecord{{Name: "custom", Class: "unknown-series"}},
		map[string]string{"production/cpu-heavy": "custom"},
	)

	rec, err := RecommendVM(digests, cfg, vmTestTerm(), vmEngineCost, nil, prefCtx)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.NotNil(t, rec.RecommendedSeries)
	assert.Equal(t, vmSeriesComputeOptimized, *rec.RecommendedSeries)
}

func timeMustParse(s string) time.Time {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		panic(err)
	}
	return t
}
