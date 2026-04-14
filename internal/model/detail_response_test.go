package model

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildDetailResponse_StructureMatchesKruizeShape(t *testing.T) {
	cpuReq := int64(500)
	cpuLim := int64(1000)
	memReq := int64(2048)
	memLim := int64(4096)
	curCPUReq := int64(250)
	curCPULim := int64(500)
	curMemReq := int64(1024)
	curMemLim := int64(2048)
	varCPU := float32(-15.2)
	varMem := float32(10.5)

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
					CPURequestMillicores:   &cpuReq,
					CPULimitMillicores:     &cpuLim,
					MemRequestKiB:         &memReq,
					MemLimitKiB:           &memLim,
					CurrentCPURequestMC:    &curCPUReq,
					CurrentCPULimitMC:      &curCPULim,
					CurrentMemRequestKiB:   &curMemReq,
					CurrentMemLimitKiB:     &curMemLim,
					VariationCPURequestPct: &varCPU,
					VariationMemRequestPct: &varMem,
					Notifications: map[string]notifications.NotificationEntry{
						"1": {Type: "info", Message: "Short Term Available", Code: 1},
					},
				},
				Performance: &EngineRecommendation{
					CPURequestMillicores: &cpuReq,
					MemRequestKiB:       &memReq,
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

	assert.Equal(t, "test-uuid", detail.ID)
	assert.Equal(t, "my-cluster", detail.ClusterAlias)
	assert.Equal(t, "2026-03-25T00:00:00Z", detail.Recommendations.MonitoringEndTime)

	shortTerm, ok := detail.Recommendations.RecommendationTerms["short_term"]
	require.True(t, ok)
	assert.Equal(t, float64(24), shortTerm.DurationInHours)

	// Verify plots
	require.NotNil(t, shortTerm.Plots)
	assert.Equal(t, 4, shortTerm.Plots.DataPoints)

	// Verify engine config (nested Kruize shape)
	require.NotNil(t, shortTerm.RecommendationEngines)
	cost := shortTerm.RecommendationEngines.Cost
	require.NotNil(t, cost)
	require.NotNil(t, cost.Config)
	require.NotNil(t, cost.Config.Requests)
	require.NotNil(t, cost.Config.Requests.CPU)
	assert.InDelta(t, 0.5, cost.Config.Requests.CPU.Amount, 0.001)
	assert.Equal(t, "cores", cost.Config.Requests.CPU.Format)
	require.NotNil(t, cost.Config.Requests.Memory)
	assert.InDelta(t, 2.0, cost.Config.Requests.Memory.Amount, 0.001) // 2048 KiB = 2 MiB
	assert.Equal(t, "MiB", cost.Config.Requests.Memory.Format)
	require.NotNil(t, cost.Config.Limits)
	require.NotNil(t, cost.Config.Limits.CPU)
	assert.InDelta(t, 1.0, cost.Config.Limits.CPU.Amount, 0.001)
	require.NotNil(t, cost.Config.Limits.Memory)
	assert.InDelta(t, 4.0, cost.Config.Limits.Memory.Amount, 0.001) // 4096 KiB = 4 MiB

	// Verify variation
	require.NotNil(t, cost.Variation)
	require.NotNil(t, cost.Variation.Requests)
	require.NotNil(t, cost.Variation.Requests.CPU)
	assert.InDelta(t, -15.2, cost.Variation.Requests.CPU.Amount, 0.01)
	assert.Equal(t, "percentage", cost.Variation.Requests.CPU.Format)
	require.NotNil(t, cost.Variation.Requests.Memory)
	assert.InDelta(t, 10.5, cost.Variation.Requests.Memory.Amount, 0.01)

	// Verify engine-level notifications
	require.NotNil(t, cost.Notifications)
	assert.Contains(t, cost.Notifications, "1")

	// Verify current resource config
	require.NotNil(t, detail.Recommendations.Current)
	require.NotNil(t, detail.Recommendations.Current.Requests)
	require.NotNil(t, detail.Recommendations.Current.Requests.CPU)
	assert.InDelta(t, 0.25, detail.Recommendations.Current.Requests.CPU.Amount, 0.001)
	assert.Equal(t, "cores", detail.Recommendations.Current.Requests.CPU.Format)
	require.NotNil(t, detail.Recommendations.Current.Requests.Memory)
	assert.InDelta(t, 1.0, detail.Recommendations.Current.Requests.Memory.Amount, 0.001)
	require.NotNil(t, detail.Recommendations.Current.Limits)
	require.NotNil(t, detail.Recommendations.Current.Limits.CPU)
	assert.InDelta(t, 0.5, detail.Recommendations.Current.Limits.CPU.Amount, 0.001)
	require.NotNil(t, detail.Recommendations.Current.Limits.Memory)
	assert.InDelta(t, 2.0, detail.Recommendations.Current.Limits.Memory.Amount, 0.001)

	// Verify term-level notifications (aggregated from engines)
	require.NotNil(t, shortTerm.Notifications)
	assert.Contains(t, shortTerm.Notifications, "1")

	// Verify top-level notifications (aggregated from all terms)
	require.NotNil(t, detail.Recommendations.Notifications)
	assert.Contains(t, detail.Recommendations.Notifications, "1")
}

func TestBuildDetailResponse_JSONKeys(t *testing.T) {
	cpuReq := int64(100)
	curCPU := int64(50)

	native := &NativeContainerResult{
		ID:           "test-uuid",
		ClusterUUID:  "11111111-1111-1111-1111-111111111111",
		Container:    "main",
		Project:      "default",
		Workload:     "api-deploy",
		WorkloadType: "deployment",
		LastReported: "2026-03-25T10:00:00Z",
		Recommendations: map[string]TermRecommendation{
			"short_term": {
				Cost: &EngineRecommendation{
					CPURequestMillicores: &cpuReq,
					CurrentCPURequestMC:  &curCPU,
				},
			},
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
	assert.True(t, hasTerms, "should have 'recommendation_terms'")

	_, hasMET := recs["monitoring_end_time"]
	assert.True(t, hasMET, "should have 'monitoring_end_time'")

	_, hasCurrent := recs["current"]
	assert.True(t, hasCurrent, "should have 'current'")

	// Drill into the engine config shape
	terms := recs["recommendation_terms"].(map[string]interface{})
	st := terms["short_term"].(map[string]interface{})
	engines := st["recommendation_engines"].(map[string]interface{})
	costEng := engines["cost"].(map[string]interface{})

	_, hasConfig := costEng["config"]
	assert.True(t, hasConfig, "engine should have 'config'")

	config := costEng["config"].(map[string]interface{})
	_, hasRequests := config["requests"]
	assert.True(t, hasRequests, "config should have 'requests'")

	requests := config["requests"].(map[string]interface{})
	cpu := requests["cpu"].(map[string]interface{})
	_, hasAmount := cpu["amount"]
	assert.True(t, hasAmount, "cpu should have 'amount'")
	_, hasFormat := cpu["format"]
	assert.True(t, hasFormat, "cpu should have 'format'")
}

func TestBuildDetailResponse_NoPlots(t *testing.T) {
	cpuReq := int64(50)
	native := &NativeContainerResult{
		ID:          "test-uuid",
		ClusterUUID: "11111111-1111-1111-1111-111111111111",
		Container:   "main",
		Project:     "default",
		Workload:    "api-deploy",
		Recommendations: map[string]TermRecommendation{
			"short_term": {Cost: &EngineRecommendation{CPURequestMillicores: &cpuReq}},
		},
	}

	detail := BuildDetailResponse(native, nil, time.Time{})

	shortTerm := detail.Recommendations.RecommendationTerms["short_term"]
	assert.Nil(t, shortTerm.Plots)
	assert.Equal(t, "", detail.Recommendations.MonitoringEndTime)

	// Config should still be present even without plots
	require.NotNil(t, shortTerm.RecommendationEngines.Cost.Config)
	require.NotNil(t, shortTerm.RecommendationEngines.Cost.Config.Requests.CPU)
	assert.InDelta(t, 0.05, shortTerm.RecommendationEngines.Cost.Config.Requests.CPU.Amount, 0.001)
}

func TestBuildDetailResponse_NoCurrent(t *testing.T) {
	cpuReq := int64(100)
	native := &NativeContainerResult{
		ID:          "test-uuid",
		ClusterUUID: "11111111-1111-1111-1111-111111111111",
		Container:   "main",
		Project:     "default",
		Workload:    "api-deploy",
		Recommendations: map[string]TermRecommendation{
			"short_term": {
				Cost: &EngineRecommendation{
					CPURequestMillicores: &cpuReq,
				},
			},
		},
	}

	detail := BuildDetailResponse(native, nil, time.Time{})
	assert.Nil(t, detail.Recommendations.Current, "current should be nil when no current_* fields")
}

func TestBuildDetailResponse_NoNotifications(t *testing.T) {
	cpuReq := int64(100)
	native := &NativeContainerResult{
		ID:          "test-uuid",
		ClusterUUID: "11111111-1111-1111-1111-111111111111",
		Container:   "main",
		Project:     "default",
		Workload:    "api-deploy",
		Recommendations: map[string]TermRecommendation{
			"short_term": {
				Cost: &EngineRecommendation{
					CPURequestMillicores: &cpuReq,
					Notifications:        map[string]notifications.NotificationEntry{},
				},
			},
		},
	}

	detail := BuildDetailResponse(native, nil, time.Time{})
	assert.Nil(t, detail.Recommendations.Notifications, "top-level notifications should be nil when empty")
	shortTerm := detail.Recommendations.RecommendationTerms["short_term"]
	assert.Nil(t, shortTerm.Notifications, "term notifications should be nil when empty")
}
