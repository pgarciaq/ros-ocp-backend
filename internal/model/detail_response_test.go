package model

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildDetailResponse_StructureMatchesKruizeShape(t *testing.T) {
	native := &NativeContainerResult{
		ID:           "test-uuid",
		ClusterAlias: "my-cluster",
		ClusterUUID:  "11111111-1111-1111-1111-111111111111",
		Container:    "main",
		Project:      "default",
		Workload:     "api-deploy",
		WorkloadType: "deployment",
		SourceID:     "src-1",
		LastReported: "2026-03-25T10:00:00Z",
		Recommendations: map[string]TermRecommendation{
			"short_term": {
				Cost: &EngineRecommendation{
					CPURequestMillicores: int64Ptr(100),
					MemRequestKiB:       int64Ptr(1024),
				},
				Performance: &EngineRecommendation{
					CPURequestMillicores: int64Ptr(200),
					MemRequestKiB:       int64Ptr(2048),
				},
			},
		},
	}

	plots := map[string]*NativePlot{
		"short_term": {
			DataPoints: 4,
			PlotsData: map[string]NativePlotsData{
				"2026-03-25T00:00:00.000Z": {
					CPUUsage:    &BoxPlotDetails{Min: 0.1, Q1: 0.2, Median: 0.3, Q3: 0.4, Max: 0.5, Format: "cores"},
					MemoryUsage: &BoxPlotDetails{Min: 100, Q1: 200, Median: 300, Q3: 400, Max: 500, Format: "MiB"},
				},
			},
		},
	}
	met := time.Date(2026, 3, 25, 0, 0, 0, 0, time.UTC)

	detail := BuildDetailResponse(native, plots, met)

	// Verify the top-level structure
	assert.Equal(t, "test-uuid", detail.ID)
	assert.Equal(t, "my-cluster", detail.ClusterAlias)
	assert.Equal(t, "2026-03-25T00:00:00Z", detail.Recommendations.MonitoringEndTime)

	// Verify recommendation_terms exists
	shortTerm, ok := detail.Recommendations.RecommendationTerms["short_term"]
	require.True(t, ok)
	assert.Equal(t, float64(24), shortTerm.DurationInHours)

	// Verify plots are attached
	require.NotNil(t, shortTerm.Plots)
	assert.Equal(t, 4, shortTerm.Plots.DataPoints)

	// Verify engines are attached
	require.NotNil(t, shortTerm.RecommendationEngines)
	require.NotNil(t, shortTerm.RecommendationEngines.Cost)
	assert.Equal(t, int64(100), *shortTerm.RecommendationEngines.Cost.CPURequestMillicores)
}

func TestBuildDetailResponse_JSONKeys(t *testing.T) {
	native := &NativeContainerResult{
		ID:           "test-uuid",
		ClusterUUID:  "11111111-1111-1111-1111-111111111111",
		Container:    "main",
		Project:      "default",
		Workload:     "api-deploy",
		WorkloadType: "deployment",
		LastReported: "2026-03-25T10:00:00Z",
		Recommendations: map[string]TermRecommendation{
			"short_term": {},
		},
	}

	met := time.Date(2026, 3, 25, 0, 0, 0, 0, time.UTC)
	detail := BuildDetailResponse(native, nil, met)

	b, err := json.Marshal(detail)
	require.NoError(t, err)

	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(b, &raw))

	recs, ok := raw["recommendations"].(map[string]interface{})
	require.True(t, ok, "should have 'recommendations' key")

	_, hasTerms := recs["recommendation_terms"]
	assert.True(t, hasTerms, "should have 'recommendation_terms' key inside recommendations")

	_, hasMET := recs["monitoring_end_time"]
	assert.True(t, hasMET, "should have 'monitoring_end_time' key inside recommendations")
}

func TestBuildDetailResponse_NoPlots(t *testing.T) {
	native := &NativeContainerResult{
		ID:          "test-uuid",
		ClusterUUID: "11111111-1111-1111-1111-111111111111",
		Container:   "main",
		Project:     "default",
		Workload:    "api-deploy",
		Recommendations: map[string]TermRecommendation{
			"short_term": {Cost: &EngineRecommendation{CPURequestMillicores: int64Ptr(50)}},
		},
	}

	detail := BuildDetailResponse(native, nil, time.Time{})

	shortTerm := detail.Recommendations.RecommendationTerms["short_term"]
	assert.Nil(t, shortTerm.Plots)
	assert.Equal(t, "", detail.Recommendations.MonitoringEndTime)
}

func int64Ptr(v int64) *int64 { return &v }
