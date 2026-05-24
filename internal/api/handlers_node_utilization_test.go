package api

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGroupNodeUtilizationRows_NestsTermsAndEngines(t *testing.T) {
	updated := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	savingsCost := 450.0
	savingsPerf := 120.0

	rows := []nodeUtilRow{
		{
			Node: "worker-1", ClusterUUID: "c-1", Term: "medium", Engine: "cost",
			CPUUtilP50: 0.25, CPUUtilP95: 0.28, MemUtilP50: 0.30, MemUtilP95: 0.35,
			IsUnderutilized: true, PodCount: 5,
			RecommendedCPUCores:  sqlNullFloat(4), RecommendedMemoryGiB: sqlNullFloat(16),
			NodeCountReduction: 1, EstimatedMonthlySavings: sqlNullFloat(savingsCost),
			UpdatedAt: updated,
		},
		{
			Node: "worker-1", ClusterUUID: "c-1", Term: "medium", Engine: "performance",
			RecommendedCPUCores: sqlNullFloat(7), RecommendedMemoryGiB: sqlNullFloat(28),
			NodeCountReduction: 0, EstimatedMonthlySavings: sqlNullFloat(savingsPerf),
			UpdatedAt: updated,
		},
	}

	out := groupNodeUtilizationRows(rows, "", "")
	require.Len(t, out, 1)

	rec := out[0]
	assert.Equal(t, "worker-1", rec.Node)
	assert.Equal(t, "cpu_memory_utilization", rec.RecommendationType)
	assert.True(t, rec.Classification.IsUnderutilized)
	assert.InDelta(t, 0.25, float64(rec.Metrics.CPUUtilP50), 0.001)

	medium, ok := rec.RecommendationTerms["medium_term"]
	require.True(t, ok)
	require.NotNil(t, medium.RecommendationEngines)
	require.NotNil(t, medium.RecommendationEngines.Cost)
	require.NotNil(t, medium.RecommendationEngines.Performance)
	assert.InDelta(t, 4, float64(medium.RecommendationEngines.Cost.RecommendedCPUCores), 0.001)
	assert.InDelta(t, 450, float64(*medium.RecommendationEngines.Cost.EstimatedMonthlySavingsUSD), 0.01)
	assert.InDelta(t, 7, float64(medium.RecommendationEngines.Performance.RecommendedCPUCores), 0.001)
}

func TestGroupNodeUtilizationRows_EngineFilter(t *testing.T) {
	rows := []nodeUtilRow{
		{Node: "n1", ClusterUUID: "c-1", Term: "medium", Engine: "cost"},
		{Node: "n1", ClusterUUID: "c-1", Term: "medium", Engine: "performance"},
	}

	out := groupNodeUtilizationRows(rows, "cost", "")
	require.Len(t, out, 1)
	engines := out[0].RecommendationTerms["medium_term"].RecommendationEngines
	require.NotNil(t, engines.Cost)
	assert.Nil(t, engines.Performance)
}

func sqlNullFloat(v float64) sql.NullFloat64 {
	return sql.NullFloat64{Valid: true, Float64: v}
}
