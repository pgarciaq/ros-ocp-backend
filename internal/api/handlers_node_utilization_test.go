package api

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

func TestGroupNodeUtilizationRows_NestsTermsAndEngines(t *testing.T) {
	updated := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	savingsCostCents := int64(45000)
	savingsPerfCents := int64(12000)

	rows := []nodeUtilRow{
		{
			Node: "worker-1", ClusterUUID: "c-1", Term: "medium", Engine: "cost",
			CPUUtilP50: 0.25, CPUUtilP95: 0.28, MemUtilP50: 0.30, MemUtilP95: 0.35,
			Category: "underutilized", PodCount: 5,
			RecommendedCPUCores: sqlNullFloat(4), RecommendedMemoryGiB: sqlNullFloat(16),
			NodeCountReduction: 1, EstimatedMonthlySavings: sqlNullInt64(savingsCostCents),
			UpdatedAt: updated,
		},
		{
			Node: "worker-1", ClusterUUID: "c-1", Term: "medium", Engine: "performance",
			RecommendedCPUCores: sqlNullFloat(7), RecommendedMemoryGiB: sqlNullFloat(28),
			NodeCountReduction: 0, EstimatedMonthlySavings: sqlNullInt64(savingsPerfCents),
			UpdatedAt: updated,
		},
	}

	out := groupNodeUtilizationRows(rows, "", "", false)
	require.Len(t, out, 1)

	rec := out[0]
	assert.Equal(t, "worker-1", rec.Node)
	assert.Equal(t, "cpu_memory_utilization", rec.RecommendationType)
	assert.Equal(t, "underutilized", rec.Classification.Category)
	assert.InDelta(t, 0.25, float64(rec.Metrics.CPUUtilP50), 0.001)

	medium, ok := rec.RecommendationTerms["medium_term"]
	require.True(t, ok)
	require.NotNil(t, medium.RecommendationEngines)
	require.NotNil(t, medium.RecommendationEngines.Cost)
	require.NotNil(t, medium.RecommendationEngines.Performance)
	assert.InDelta(t, 4, float64(medium.RecommendationEngines.Cost.RecommendedCPUCores), 0.001)
	require.NotNil(t, medium.RecommendationEngines.Cost.EstimatedMonthlySavings)
	assert.Equal(t, "450.00", medium.RecommendationEngines.Cost.EstimatedMonthlySavings.Value)
	assert.InDelta(t, 7, float64(medium.RecommendationEngines.Performance.RecommendedCPUCores), 0.001)
}

func TestGroupNodeUtilizationRows_TermFilterSelectsPrimary(t *testing.T) {
	rows := []nodeUtilRow{
		{
			Node: "worker-1", ClusterUUID: "c-1", Term: "medium", Engine: "cost",
			CPUUtilP95: 0.10, Category: "underutilized",
		},
		{
			Node: "worker-1", ClusterUUID: "c-1", Term: "short", Engine: "cost",
			CPUUtilP95: 0.55, Category: "optimized",
		},
	}

	out := groupNodeUtilizationRows(rows, "", "short", false)
	require.Len(t, out, 1)
	assert.InDelta(t, 0.55, float64(out[0].Metrics.CPUUtilP95), 0.001)
	assert.Equal(t, "optimized", out[0].Classification.Category)
}

func TestGroupNodeUtilizationRows_EngineFilter(t *testing.T) {
	rows := []nodeUtilRow{
		{Node: "n1", ClusterUUID: "c-1", Term: "medium", Engine: "cost"},
		{Node: "n1", ClusterUUID: "c-1", Term: "medium", Engine: "performance"},
	}

	out := groupNodeUtilizationRows(rows, "cost", "", false)
	require.Len(t, out, 1)
	engines := out[0].RecommendationTerms["medium_term"].RecommendationEngines
	require.NotNil(t, engines.Cost)
	assert.Nil(t, engines.Performance)
}

func sqlNullFloat(v float64) sql.NullFloat64 {
	return sql.NullFloat64{Valid: true, Float64: v}
}

func sqlNullInt64(v int64) sql.NullInt64 {
	return sql.NullInt64{Valid: true, Int64: v}
}

func TestComputePodSchedulingHeadroom(t *testing.T) {
	h := computePodSchedulingHeadroom(90, sqlNullInt64(100))
	require.NotNil(t, h)
	assert.InDelta(t, 0.1, float64(*h), 0.001)

	assert.Nil(t, computePodSchedulingHeadroom(10, sql.NullInt64{}))
	assert.Nil(t, computePodSchedulingHeadroom(10, sqlNullInt64(0)))
}

func TestGroupNodeUtilizationRows_PodCapacityFields(t *testing.T) {
	rows := []nodeUtilRow{
		{
			Node: "worker-1", ClusterUUID: "c-1", Term: "medium", Engine: "cost",
			PodCount: 95, PodCapacity: sqlNullInt64(100),
		},
	}

	out := groupNodeUtilizationRows(rows, "", "", false)
	require.Len(t, out, 1)
	require.NotNil(t, out[0].PodCapacity)
	assert.Equal(t, int64(100), *out[0].PodCapacity)
	require.NotNil(t, out[0].PodSchedulingHeadroom)
	assert.InDelta(t, 0.05, float64(*out[0].PodSchedulingHeadroom), 0.001)
}

func TestGroupNodeUtilizationRows_OmitsPodCapacityWhenMissing(t *testing.T) {
	rows := []nodeUtilRow{
		{Node: "worker-1", ClusterUUID: "c-1", Term: "medium", Engine: "cost", PodCount: 12},
	}

	out := groupNodeUtilizationRows(rows, "", "", false)
	require.Len(t, out, 1)
	assert.Nil(t, out[0].PodCapacity)
	assert.Nil(t, out[0].PodSchedulingHeadroom)
}

func TestFlattenNodeUtilizationForCSV(t *testing.T) {
	savings := "12.50"
	rows := flattenNodeUtilizationForCSV([]model.NodeUtilizationRec{{
		Node:        "n1",
		ClusterUUID: "cluster-1",
		Metrics:     model.NodeUtilizationMetrics{CPUUtilP95: 0.42, MemUtilP95: 0.51},
		Classification: model.NodeUtilizationClassification{
			Category: "underutilized",
		},
		RecommendationTerms: map[string]model.NodeUtilizationTermRec{
			"medium_term": {
				RecommendationEngines: &model.NodeUtilizationEngines{
					Cost: &model.NodeUtilizationEngineRec{
						RecommendedCPUCores:     4,
						RecommendedMemoryGiB:    16,
						EstimatedMonthlySavings: &money.MoneyAmount{Value: savings},
					},
				},
			},
		},
	}})
	require.Len(t, rows, 1)
	assert.Equal(t, "n1", rows[0].Node)
	assert.Equal(t, "medium", rows[0].Term)
	assert.Equal(t, "cost", rows[0].Engine)
	assert.Equal(t, "underutilized", rows[0].Category)
	assert.Equal(t, savings, rows[0].EstimatedMonthlySavings)
}

func TestNodeUtilClassificationCategory(t *testing.T) {
	rec := model.NodeUtilizationRec{
		Classification: model.NodeUtilizationClassification{
			Category: "overcommitted",
		},
	}
	assert.Equal(t, "overcommitted", rec.Classification.Category)
}
