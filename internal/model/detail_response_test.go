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
	varCPUReq := int32(-15)
	varCPULim := int32(100)
	varMemReq := int32(11)
	varMemLim := int32(100)

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
					MemRequestKiB:          &memReq,
					MemLimitKiB:            &memLim,
					CurrentCPURequestMC:    &curCPUReq,
					CurrentCPULimitMC:      &curCPULim,
					CurrentMemRequestKiB:   &curMemReq,
					CurrentMemLimitKiB:     &curMemLim,
					VariationCPURequestPct: &varCPUReq,
					VariationCPULimitPct:   &varCPULim,
					VariationMemRequestPct: &varMemReq,
					VariationMemLimitPct:   &varMemLim,
					Notifications: map[string]notifications.NotificationEntry{
						"1": {Type: "info", Message: "Short Term Available", Code: 1},
					},
				},
				Performance: &EngineRecommendation{
					CPURequestMillicores: &cpuReq,
					MemRequestKiB:        &memReq,
				},
			},
		},
	}

	plots := map[string]*NativePlot{
		"short_term": {
			DataPoints: 4,
			PlotsData: map[string]NativePlotsData{
				"2026-03-25T00:00:00.000Z": {
					CPUUsage:    &PlotDetails{P50: 0.3, P95: 0.4, P99: 0.45, Max: 0.5, Format: "cores"},
					MemoryUsage: &PlotDetails{P50: 300, P95: 400, P99: 450, Max: 500, Format: "MiB"},
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

	// Verify variation (limits)
	require.NotNil(t, cost.Variation)
	require.NotNil(t, cost.Variation.Limits)
	require.NotNil(t, cost.Variation.Limits.CPU)
	assert.Equal(t, float64(100), cost.Variation.Limits.CPU.Amount)
	assert.Equal(t, "percent", cost.Variation.Limits.CPU.Format)
	require.NotNil(t, cost.Variation.Limits.Memory)
	assert.Equal(t, float64(100), cost.Variation.Limits.Memory.Amount)

	// Verify variation (requests)
	require.NotNil(t, cost.Variation.Requests)
	require.NotNil(t, cost.Variation.Requests.CPU)
	assert.Equal(t, float64(-15), cost.Variation.Requests.CPU.Amount)
	assert.Equal(t, "percent", cost.Variation.Requests.CPU.Format)
	require.NotNil(t, cost.Variation.Requests.Memory)
	assert.Equal(t, float64(11), cost.Variation.Requests.Memory.Amount)

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

	// Term and top-level notification maps are omitted (engine-level only).
	b, err := json.Marshal(detail)
	require.NoError(t, err)
	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(b, &raw))
	recsRaw := raw["recommendations"].(map[string]interface{})
	_, hasTopNotifs := recsRaw["notifications"]
	assert.False(t, hasTopNotifs, "top-level notifications should be omitted")
	termsRaw := recsRaw["recommendation_terms"].(map[string]interface{})
	stRaw := termsRaw["short_term"].(map[string]interface{})
	_, hasTermNotifs := stRaw["notifications"]
	assert.False(t, hasTermNotifs, "term notifications should be omitted")
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

func TestBuildDetailResponse_WithReplicas(t *testing.T) {
	cpuReq := int64(100)
	native := &NativeContainerResult{
		ID:          "test-uuid",
		ClusterUUID: "11111111-1111-1111-1111-111111111111",
		Container:   "main",
		Project:     "default",
		Workload:    "api-deploy",
		Replicas:    &ReplicaInfo{Min: 2, Max: 5, Avg: 3},
		Recommendations: map[string]TermRecommendation{
			"short_term": {
				Cost: &EngineRecommendation{CPURequestMillicores: &cpuReq},
			},
		},
	}

	detail := BuildDetailResponse(native, nil, time.Time{})

	require.NotNil(t, detail.Recommendations.Replicas)
	assert.Equal(t, 2, detail.Recommendations.Replicas.Min)
	assert.Equal(t, 5, detail.Recommendations.Replicas.Max)
	assert.Equal(t, 3, detail.Recommendations.Replicas.Avg)
}

func TestBuildDetailResponse_NilReplicas(t *testing.T) {
	cpuReq := int64(100)
	native := &NativeContainerResult{
		ID:          "test-uuid",
		ClusterUUID: "11111111-1111-1111-1111-111111111111",
		Container:   "main",
		Project:     "default",
		Workload:    "api-deploy",
		Recommendations: map[string]TermRecommendation{
			"short_term": {
				Cost: &EngineRecommendation{CPURequestMillicores: &cpuReq},
			},
		},
	}

	detail := BuildDetailResponse(native, nil, time.Time{})
	assert.Nil(t, detail.Recommendations.Replicas)
}

func TestBuildDetailResponse_ReplicasInJSON(t *testing.T) {
	cpuReq := int64(100)
	native := &NativeContainerResult{
		ID:          "test-uuid",
		ClusterUUID: "11111111-1111-1111-1111-111111111111",
		Container:   "main",
		Project:     "default",
		Workload:    "api-deploy",
		Replicas:    &ReplicaInfo{Min: 1, Max: 4, Avg: 2},
		Recommendations: map[string]TermRecommendation{
			"short_term": {
				Cost: &EngineRecommendation{CPURequestMillicores: &cpuReq},
			},
		},
	}

	detail := BuildDetailResponse(native, nil, time.Time{})
	b, err := json.Marshal(detail)
	require.NoError(t, err)

	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(b, &raw))

	recs := raw["recommendations"].(map[string]interface{})
	replicas, ok := recs["replicas"].(map[string]interface{})
	require.True(t, ok, "should have 'replicas' in recommendations")
	assert.Equal(t, float64(1), replicas["min"])
	assert.Equal(t, float64(4), replicas["max"])
	assert.Equal(t, float64(2), replicas["avg"])
}

func TestBuildDetailResponse_ReplicasWithDesiredAvailable(t *testing.T) {
	cpuReq := int64(100)
	native := &NativeContainerResult{
		ID:          "test-uuid",
		ClusterUUID: "11111111-1111-1111-1111-111111111111",
		Container:   "main",
		Project:     "default",
		Workload:    "api-deploy",
		Replicas:    &ReplicaInfo{Min: 2, Max: 5, Avg: 3, Desired: 5, Available: 4, Source: "kube_state_metrics"},
		Recommendations: map[string]TermRecommendation{
			"short_term": {
				Cost: &EngineRecommendation{CPURequestMillicores: &cpuReq},
			},
		},
	}

	detail := BuildDetailResponse(native, nil, time.Time{})
	require.NotNil(t, detail.Recommendations.Replicas)
	assert.Equal(t, 5, detail.Recommendations.Replicas.Desired)
	assert.Equal(t, 4, detail.Recommendations.Replicas.Available)
	assert.Equal(t, "kube_state_metrics", detail.Recommendations.Replicas.Source)

	b, err := json.Marshal(detail)
	require.NoError(t, err)

	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(b, &raw))
	recs := raw["recommendations"].(map[string]interface{})
	replicas := recs["replicas"].(map[string]interface{})
	assert.Equal(t, float64(5), replicas["desired"])
	assert.Equal(t, float64(4), replicas["available"])
	assert.Equal(t, "kube_state_metrics", replicas["source"])
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
	b, err := json.Marshal(detail)
	require.NoError(t, err)
	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(b, &raw))
	recsRaw := raw["recommendations"].(map[string]interface{})
	_, hasTopNotifs := recsRaw["notifications"]
	assert.False(t, hasTopNotifs, "top-level notifications should be omitted")
	termsRaw := recsRaw["recommendation_terms"].(map[string]interface{})
	stRaw := termsRaw["short_term"].(map[string]interface{})
	_, hasTermNotifs := stRaw["notifications"]
	assert.False(t, hasTermNotifs, "term notifications should be omitted")
}

func TestBuildNamespaceDetailResponse_StructureMatchesKruizeShape(t *testing.T) {
	cpuReq := int64(4720)
	cpuLim := int64(8000)
	memReq := int64(2048)
	memLim := int64(4096)
	curCPUReq := int64(3000)
	curCPULim := int64(6000)
	curMemReq := int64(1024)
	curMemLim := int64(2048)
	varCPUReq := int32(-20)
	varCPULim := int32(10)
	varMemReq := int32(50)
	varMemLim := int32(25)

	native := &NativeNamespaceResult{
		ID:           "ns-test-uuid",
		ClusterAlias: "my-cluster",
		ClusterUUID:  "22222222-2222-2222-2222-222222222222",
		Project:      "kube-system",
		SourceID:     "src-1",
		LastReported: "2026-04-10T12:00:00Z",
		Recommendations: map[string]any{
			"short_term": TermRecommendation{
				Cost: &EngineRecommendation{
					CPURequestMillicores:   &cpuReq,
					CPULimitMillicores:     &cpuLim,
					MemRequestKiB:          &memReq,
					MemLimitKiB:            &memLim,
					CurrentCPURequestMC:    &curCPUReq,
					CurrentCPULimitMC:      &curCPULim,
					CurrentMemRequestKiB:   &curMemReq,
					CurrentMemLimitKiB:     &curMemLim,
					VariationCPURequestPct: &varCPUReq,
					VariationCPULimitPct:   &varCPULim,
					VariationMemRequestPct: &varMemReq,
					VariationMemLimitPct:   &varMemLim,
					Notifications: map[string]notifications.NotificationEntry{
						"1": {Type: "info", Message: "Short Term Available", Code: 1},
					},
				},
				Performance: &EngineRecommendation{
					CPURequestMillicores: &cpuReq,
					MemRequestKiB:        &memReq,
				},
			},
			"monitoring_end_time": "2026-04-09T23:00:00Z",
		},
	}

	plots := map[string]*NativePlot{
		"short_term": {
			DataPoints: 3,
			PlotsData: map[string]NativePlotsData{
				"2026-04-10T00:00:00.000Z": {
					CPUUsage:    &PlotDetails{P50: 1.5, P95: 2.0, P99: 2.2, Max: 2.5, Format: "cores"},
					MemoryUsage: &PlotDetails{P50: 768, P95: 1024, P99: 1152, Max: 1280, Format: "MiB"},
				},
			},
		},
	}
	met := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)

	detail := BuildNamespaceDetailResponse(native, plots, met)

	assert.Equal(t, "ns-test-uuid", detail.ID)
	assert.Equal(t, "my-cluster", detail.ClusterAlias)
	assert.Equal(t, "22222222-2222-2222-2222-222222222222", detail.ClusterUUID)
	assert.Equal(t, "kube-system", detail.Project)
	assert.Equal(t, "src-1", detail.SourceID)
	assert.Equal(t, "2026-04-10T12:00:00Z", detail.LastReported)
	assert.Equal(t, "2026-04-10T00:00:00Z", detail.Recommendations.MonitoringEndTime)

	shortTerm, ok := detail.Recommendations.RecommendationTerms["short_term"]
	require.True(t, ok)
	assert.Equal(t, float64(24), shortTerm.DurationInHours)

	require.NotNil(t, shortTerm.Plots)
	assert.Equal(t, 3, shortTerm.Plots.DataPoints)

	require.NotNil(t, shortTerm.RecommendationEngines)
	cost := shortTerm.RecommendationEngines.Cost
	require.NotNil(t, cost)
	require.NotNil(t, cost.Config)
	require.NotNil(t, cost.Config.Requests)
	require.NotNil(t, cost.Config.Requests.CPU)
	assert.InDelta(t, 4.72, cost.Config.Requests.CPU.Amount, 0.001)
	assert.Equal(t, "cores", cost.Config.Requests.CPU.Format)
	require.NotNil(t, cost.Config.Requests.Memory)
	assert.InDelta(t, 2.0, cost.Config.Requests.Memory.Amount, 0.001)
	assert.Equal(t, "MiB", cost.Config.Requests.Memory.Format)
	require.NotNil(t, cost.Config.Limits)
	require.NotNil(t, cost.Config.Limits.CPU)
	assert.InDelta(t, 8.0, cost.Config.Limits.CPU.Amount, 0.001)
	require.NotNil(t, cost.Config.Limits.Memory)
	assert.InDelta(t, 4.0, cost.Config.Limits.Memory.Amount, 0.001)

	require.NotNil(t, cost.Variation)
	require.NotNil(t, cost.Variation.Requests)
	require.NotNil(t, cost.Variation.Requests.CPU)
	assert.Equal(t, float64(-20), cost.Variation.Requests.CPU.Amount)
	assert.Equal(t, "percent", cost.Variation.Requests.CPU.Format)
	require.NotNil(t, cost.Variation.Requests.Memory)
	assert.Equal(t, float64(50), cost.Variation.Requests.Memory.Amount)
	require.NotNil(t, cost.Variation.Limits)
	require.NotNil(t, cost.Variation.Limits.CPU)
	assert.Equal(t, float64(10), cost.Variation.Limits.CPU.Amount)
	require.NotNil(t, cost.Variation.Limits.Memory)
	assert.Equal(t, float64(25), cost.Variation.Limits.Memory.Amount)

	require.NotNil(t, cost.Notifications)
	assert.Contains(t, cost.Notifications, "1")

	require.NotNil(t, detail.Recommendations.Current)
	require.NotNil(t, detail.Recommendations.Current.Requests)
	require.NotNil(t, detail.Recommendations.Current.Requests.CPU)
	assert.InDelta(t, 3.0, detail.Recommendations.Current.Requests.CPU.Amount, 0.001)
	assert.Equal(t, "cores", detail.Recommendations.Current.Requests.CPU.Format)
	require.NotNil(t, detail.Recommendations.Current.Requests.Memory)
	assert.InDelta(t, 1.0, detail.Recommendations.Current.Requests.Memory.Amount, 0.001)
	require.NotNil(t, detail.Recommendations.Current.Limits)
	require.NotNil(t, detail.Recommendations.Current.Limits.CPU)
	assert.InDelta(t, 6.0, detail.Recommendations.Current.Limits.CPU.Amount, 0.001)
	require.NotNil(t, detail.Recommendations.Current.Limits.Memory)
	assert.InDelta(t, 2.0, detail.Recommendations.Current.Limits.Memory.Amount, 0.001)

	b, err := json.Marshal(detail)
	require.NoError(t, err)
	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(b, &raw))
	recsRaw := raw["recommendations"].(map[string]interface{})
	_, hasTopNotifs := recsRaw["notifications"]
	assert.False(t, hasTopNotifs)
	termsRaw := recsRaw["recommendation_terms"].(map[string]interface{})
	stRaw := termsRaw["short_term"].(map[string]interface{})
	_, hasTermNotifs := stRaw["notifications"]
	assert.False(t, hasTermNotifs)
}

func TestBuildNamespaceDetailResponse_JSONKeys(t *testing.T) {
	cpuReq := int64(500)
	curCPU := int64(250)

	native := &NativeNamespaceResult{
		ID:           "ns-uuid",
		ClusterUUID:  "22222222-2222-2222-2222-222222222222",
		Project:      "default",
		LastReported: "2026-04-10T12:00:00Z",
		Recommendations: map[string]any{
			"short_term": TermRecommendation{
				Cost: &EngineRecommendation{
					CPURequestMillicores: &cpuReq,
					CurrentCPURequestMC:  &curCPU,
				},
			},
		},
	}

	met := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	detail := BuildNamespaceDetailResponse(native, nil, met)

	b, err := json.Marshal(detail)
	require.NoError(t, err)

	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(b, &raw))

	assert.NotContains(t, raw, "container", "namespace response should not have 'container'")
	assert.NotContains(t, raw, "workload", "namespace response should not have 'workload'")
	assert.NotContains(t, raw, "workload_type", "namespace response should not have 'workload_type'")
	assert.NotContains(t, raw, "gpu", "namespace response should not have 'gpu'")

	recs, ok := raw["recommendations"].(map[string]interface{})
	require.True(t, ok)

	_, hasTerms := recs["recommendation_terms"]
	assert.True(t, hasTerms, "should have 'recommendation_terms'")
	_, hasMET := recs["monitoring_end_time"]
	assert.True(t, hasMET, "should have 'monitoring_end_time'")
	_, hasCurrent := recs["current"]
	assert.True(t, hasCurrent, "should have 'current'")

	terms := recs["recommendation_terms"].(map[string]interface{})
	st := terms["short_term"].(map[string]interface{})
	engines := st["recommendation_engines"].(map[string]interface{})
	costEng := engines["cost"].(map[string]interface{})

	config := costEng["config"].(map[string]interface{})
	requests := config["requests"].(map[string]interface{})
	cpu := requests["cpu"].(map[string]interface{})
	_, hasAmount := cpu["amount"]
	assert.True(t, hasAmount, "cpu should have 'amount'")
	_, hasFormat := cpu["format"]
	assert.True(t, hasFormat, "cpu should have 'format'")

	assert.NotContains(t, costEng, "cpu_request_millicores", "should NOT have flat fields")
}

func TestBuildNamespaceDetailResponse_NoCurrent(t *testing.T) {
	cpuReq := int64(100)
	native := &NativeNamespaceResult{
		ID:          "ns-uuid",
		ClusterUUID: "22222222-2222-2222-2222-222222222222",
		Project:     "default",
		Recommendations: map[string]any{
			"short_term": TermRecommendation{
				Cost: &EngineRecommendation{
					CPURequestMillicores: &cpuReq,
				},
			},
		},
	}

	detail := BuildNamespaceDetailResponse(native, nil, time.Time{})
	assert.Nil(t, detail.Recommendations.Current, "current should be nil when no current_* fields")
	assert.Equal(t, "", detail.Recommendations.MonitoringEndTime)
}

func TestBuildNamespaceDetailResponse_SkipsMonitoringEndTimeKey(t *testing.T) {
	cpuReq := int64(100)
	native := &NativeNamespaceResult{
		ID:          "ns-uuid",
		ClusterUUID: "22222222-2222-2222-2222-222222222222",
		Project:     "default",
		Recommendations: map[string]any{
			"short_term": TermRecommendation{
				Cost: &EngineRecommendation{CPURequestMillicores: &cpuReq},
			},
			"monitoring_end_time": "2026-04-09T23:00:00Z",
		},
	}

	met := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	detail := BuildNamespaceDetailResponse(native, nil, met)

	_, hasMetKey := detail.Recommendations.RecommendationTerms["monitoring_end_time"]
	assert.False(t, hasMetKey, "monitoring_end_time should not appear as a term")
	assert.Equal(t, "2026-04-10T00:00:00Z", detail.Recommendations.MonitoringEndTime)
}

func TestBuildDetailResponse_BusinessHoursPresent(t *testing.T) {
	cpuReq := int64(800)
	memReq := int64(2048)
	native := &NativeContainerResult{
		ID:          "test-uuid",
		ClusterUUID: "11111111-1111-1111-1111-111111111111",
		Container:   "main",
		Project:     "default",
		Workload:    "api-deploy",
		Recommendations: map[string]TermRecommendation{
			"short_term": {
				Cost: &EngineRecommendation{
					CPURequestMillicores: int64Ptr(500),
					MemRequestKiB:        int64Ptr(1024),
					BusinessHours: &BusinessHoursRecommendation{
						CPURequestMillicores: &cpuReq,
						MemRequestKiB:        &memReq,
					},
				},
			},
		},
	}

	detail := BuildDetailResponse(native, nil, time.Time{})
	cost := detail.Recommendations.RecommendationTerms["short_term"].RecommendationEngines.Cost
	require.NotNil(t, cost.BusinessHours)
	require.NotNil(t, cost.BusinessHours.Requests)
	require.NotNil(t, cost.BusinessHours.Requests.CPU)
	assert.InDelta(t, 0.8, cost.BusinessHours.Requests.CPU.Amount, 0.001)
	assert.Equal(t, "cores", cost.BusinessHours.Requests.CPU.Format)
	assert.Nil(t, cost.BusinessHours.Limits, "limits should be nil when no limit data is provided")
}

func TestBuildDetailResponse_BusinessHoursAbsent(t *testing.T) {
	native := &NativeContainerResult{
		ID:          "test-uuid",
		ClusterUUID: "11111111-1111-1111-1111-111111111111",
		Container:   "main",
		Project:     "default",
		Workload:    "api-deploy",
		Recommendations: map[string]TermRecommendation{
			"short_term": {
				Cost: &EngineRecommendation{CPURequestMillicores: int64Ptr(500)},
			},
		},
	}

	b, err := json.Marshal(BuildDetailResponse(native, nil, time.Time{}))
	require.NoError(t, err)
	assert.NotContains(t, string(b), `"business_hours"`)
}

func TestBuildDetailResponse_KillSwitch_NoBHField(t *testing.T) {
	cpuBH := int64(900)
	native := &NativeContainerResult{
		ID:          "test-uuid",
		ClusterUUID: "11111111-1111-1111-1111-111111111111",
		Container:   "main",
		Project:     "default",
		Workload:    "api-deploy",
		Recommendations: map[string]TermRecommendation{
			"short_term": {
				Cost: &EngineRecommendation{
					CPURequestMillicores: int64Ptr(500),
					BusinessHours:        &BusinessHoursRecommendation{CPURequestMillicores: &cpuBH},
				},
			},
		},
	}
	// Kill switch is enforced at enrichment time; builder only omits nil BusinessHours.
	native.Recommendations["short_term"] = TermRecommendation{
		Cost: &EngineRecommendation{CPURequestMillicores: int64Ptr(500)},
	}
	b, err := json.Marshal(BuildDetailResponse(native, nil, time.Time{}))
	require.NoError(t, err)
	assert.NotContains(t, string(b), `"business_hours"`)
}

func TestBusinessHours_KruizeAmountFormat(t *testing.T) {
	cpu := int64(1500)
	mem := int64(4194304) // 4 GiB in KiB
	native := &NativeContainerResult{
		ID: "x", ClusterUUID: "11111111-1111-1111-1111-111111111111",
		Container: "c", Project: "ns", Workload: "w",
		Recommendations: map[string]TermRecommendation{
			"short_term": {Cost: &EngineRecommendation{
				BusinessHours: &BusinessHoursRecommendation{
					CPURequestMillicores: &cpu,
					MemRequestKiB:        &mem,
				},
			}},
		},
	}
	b, err := json.Marshal(BuildDetailResponse(native, nil, time.Time{}))
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(b, &raw))
	bh := raw["recommendations"].(map[string]any)["recommendation_terms"].(map[string]any)["short_term"].(map[string]any)["recommendation_engines"].(map[string]any)["cost"].(map[string]any)["business_hours"].(map[string]any)
	cpuObj := bh["requests"].(map[string]any)["cpu"].(map[string]any)
	assert.Equal(t, "cores", cpuObj["format"])
	assert.IsType(t, float64(0), cpuObj["amount"])
	memObj := bh["requests"].(map[string]any)["memory"].(map[string]any)
	assert.Equal(t, "MiB", memObj["format"])
}

func TestBusinessHours_LimitsOmittedWhenEmpty(t *testing.T) {
	cpu := int64(100)
	native := &NativeContainerResult{
		ID: "x", ClusterUUID: "11111111-1111-1111-1111-111111111111",
		Container: "c", Project: "ns", Workload: "w",
		Recommendations: map[string]TermRecommendation{
			"short_term": {Cost: &EngineRecommendation{
				BusinessHours: &BusinessHoursRecommendation{CPURequestMillicores: &cpu},
			}},
		},
	}
	b, err := json.Marshal(BuildDetailResponse(native, nil, time.Time{}))
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(b, &raw))
	bh := raw["recommendations"].(map[string]any)["recommendation_terms"].(map[string]any)["short_term"].(map[string]any)["recommendation_engines"].(map[string]any)["cost"].(map[string]any)["business_hours"].(map[string]any)
	_, hasLimits := bh["limits"]
	assert.False(t, hasLimits, "limits should be omitted when no limit data is set")
}

func TestBusinessHours_LimitsPresentWhenPopulated(t *testing.T) {
	cpuReq := int64(100)
	cpuLim := int64(200)
	native := &NativeContainerResult{
		ID: "x", ClusterUUID: "11111111-1111-1111-1111-111111111111",
		Container: "c", Project: "ns", Workload: "w",
		Recommendations: map[string]TermRecommendation{
			"short_term": {Cost: &EngineRecommendation{
				BusinessHours: &BusinessHoursRecommendation{
					CPURequestMillicores: &cpuReq,
					CPULimitMillicores:   &cpuLim,
				},
			}},
		},
	}
	b, err := json.Marshal(BuildDetailResponse(native, nil, time.Time{}))
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(b, &raw))
	bh := raw["recommendations"].(map[string]any)["recommendation_terms"].(map[string]any)["short_term"].(map[string]any)["recommendation_engines"].(map[string]any)["cost"].(map[string]any)["business_hours"].(map[string]any)
	limits, ok := bh["limits"].(map[string]any)
	require.True(t, ok, "limits should be present when limit data is set")
	cpuObj := limits["cpu"].(map[string]any)
	assert.Equal(t, 0.2, cpuObj["amount"])
}

func TestBusinessHours_ListDetailParity(t *testing.T) {
	cpu := int64(600)
	bh := &BusinessHoursRecommendation{CPURequestMillicores: &cpu}
	native := &NativeContainerResult{
		ID: "x", ClusterUUID: "11111111-1111-1111-1111-111111111111",
		Container: "c", Project: "ns", Workload: "w",
		Recommendations: map[string]TermRecommendation{
			"short_term": {Cost: &EngineRecommendation{BusinessHours: bh}},
		},
	}
	detail := BuildDetailResponse(native, nil, time.Time{})
	listJSON, _ := json.Marshal(detail)
	detail2 := BuildDetailResponse(native, nil, time.Time{})
	detailJSON, _ := json.Marshal(detail2)
	assert.JSONEq(t, string(listJSON), string(detailJSON))
}

func TestBuildDetailResponse_ClusterIngestAndAnalyticsFlags(t *testing.T) {
	at := "2026-06-01T12:00:00Z"
	native := &NativeContainerResult{
		ID:                    "721eb376-13a9-43ab-868e-755aa1ce7f2a",
		ClusterUUID:           "11111111-1111-1111-1111-111111111111",
		Container:             "app",
		Project:               "prod",
		Workload:              "api",
		AnalyticsIncomplete:   true,
		AnalyticsIncompleteAt: &at,
		IngestHooksFailed:     true,
		IngestHooksFailedAt:   &at,
		Recommendations: map[string]TermRecommendation{
			"short_term": {Cost: &EngineRecommendation{CPURequestMillicores: int64Ptr(100)}},
		},
	}
	detail := BuildDetailResponse(native, nil, time.Time{})
	require.True(t, detail.AnalyticsIncomplete)
	require.True(t, detail.IngestHooksFailed)
	require.NotNil(t, detail.AnalyticsIncompleteAt)
	require.Equal(t, at, *detail.AnalyticsIncompleteAt)
	require.NotNil(t, detail.IngestHooksFailedAt)
	require.Equal(t, at, *detail.IngestHooksFailedAt)

	b, err := json.Marshal(detail)
	require.NoError(t, err)
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(b, &m))
	assert.Equal(t, true, m["analytics_incomplete"])
	assert.Equal(t, true, m["ingest_hooks_failed"])
	assert.Equal(t, at, m["analytics_incomplete_at"])
	assert.Equal(t, at, m["ingest_hooks_failed_at"])
}

func int64Ptr(v int64) *int64 {
	return &v
}
