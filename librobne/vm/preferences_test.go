package vm

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestVMPreference_OverridesRatioClassification(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.EnableInstanceTypeMatching = true

	base := timeMustParse("2026-05-01")
	digests := vmDigestDays(base, 3, func(d *Digest) {
		d.Namespace = "production"
		d.VMName = "cpu-heavy"
		// Sized to fit a memory-optimized catalog entry (avoids general-purpose fallback in MatchInstanceType).
		d.CPURequestMC = 8000
		d.CPUUsageP95MC = 6000
		d.CPUUsageMaxMC = 6000
		d.MemRequestKiB = 32 * 1024 * 1024
		d.MemUsageP95KiB = 24 * 1024 * 1024
		d.MemUsageMaxKiB = 24 * 1024 * 1024
	})
	prefCtx := buildVMPreferenceContext(
		[]ClusterPreferenceRecord{{Name: "database", Class: "memory-intensive"}},
		map[string]string{"production/cpu-heavy": "database"},
	)

	rec, err := RecommendVM(digests, cfg, vmTestTerm(), vmEngineCost, nil, prefCtx, nil, nil)
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
	digests := vmDigestDays(base, 3, func(d *Digest) {
		d.Namespace = "production"
		d.VMName = "cpu-heavy"
		d.CPURequestMC = 20000
		d.CPUUsageP95MC = 20000
		d.CPUUsageMaxMC = 20000
		d.MemRequestKiB = 2 * 1024 * 1024
		d.MemUsageP95KiB = 1 * 1024 * 1024
		d.MemUsageMaxKiB = 1 * 1024 * 1024
	})

	rec, err := RecommendVM(digests, cfg, vmTestTerm(), vmEngineCost, nil, nil, nil, nil)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.NotNil(t, rec.RecommendedSeries)
	assert.Equal(t, vmSeriesComputeOptimized, *rec.RecommendedSeries)
}

func TestVMPreference_UnknownPreference_FallsBackToRatio(t *testing.T) {
	cfg := DefaultVMRecConfig()
	cfg.EnableInstanceTypeMatching = true

	base := timeMustParse("2026-05-01")
	digests := vmDigestDays(base, 3, func(d *Digest) {
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

	rec, err := RecommendVM(digests, cfg, vmTestTerm(), vmEngineCost, nil, prefCtx, nil, nil)
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
