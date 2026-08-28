package model

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

func TestBuildListResponse_DefaultIncludesShortTermCostOnly(t *testing.T) {
	cpuReq := int64(500)
	varCPUReq := int32(-15)
	curCPUReq := int64(250)

	native := &NativeContainerResult{
		ID:          "test-uuid",
		ClusterUUID: "11111111-1111-1111-1111-111111111111",
		Container:   "main",
		Project:     "default",
		Recommendations: map[string]TermRecommendation{
			"short_term": {
				Cost: &EngineRecommendation{
					CPURequestMillicores:   &cpuReq,
					CurrentCPURequestMC:    &curCPUReq,
					VariationCPURequestPct: &varCPUReq,
					NotificationCodes:      SmallintArray{1, 7},
				},
				Performance: &EngineRecommendation{
					CPURequestMillicores: &cpuReq,
					NotificationCodes:    SmallintArray{5},
				},
			},
			"medium_term": {
				Cost: &EngineRecommendation{
					CPURequestMillicores: &cpuReq,
					NotificationCodes:    SmallintArray{9},
				},
			},
		},
	}

	list := BuildListResponse(native, time.Time{}, ListResponseOptions{})

	require.NotNil(t, list.Recommendations.Current)
	require.Contains(t, list.Recommendations.RecommendationTerms, "short_term")
	assert.NotContains(t, list.Recommendations.RecommendationTerms, "medium_term")

	shortTerm := list.Recommendations.RecommendationTerms["short_term"]
	require.NotNil(t, shortTerm.RecommendationEngines)
	require.NotNil(t, shortTerm.RecommendationEngines.Cost)
	assert.Nil(t, shortTerm.RecommendationEngines.Performance)
	require.NotNil(t, shortTerm.RecommendationEngines.Cost.Variation)

	require.NotNil(t, list.Recommendations.NotificationCodes)
	assert.Contains(t, list.Recommendations.NotificationCodes, int16(1))
	assert.Contains(t, list.Recommendations.NotificationCodes, int16(5))
	assert.Contains(t, list.Recommendations.NotificationCodes, int16(7))
	assert.Nil(t, shortTerm.RecommendationEngines.Cost.Notifications)
}

func TestBuildListResponse_SingleTermFilterIncludesAllEngines(t *testing.T) {
	cpuReq := int64(500)

	native := &NativeContainerResult{
		Recommendations: map[string]TermRecommendation{
			"medium_term": {
				Cost: &EngineRecommendation{
					CPURequestMillicores: &cpuReq,
				},
				Performance: &EngineRecommendation{
					CPURequestMillicores: &cpuReq,
				},
			},
		},
	}

	list := BuildListResponse(native, time.Time{}, ListResponseOptions{})
	medium := list.Recommendations.RecommendationTerms["medium_term"]
	require.NotNil(t, medium.RecommendationEngines)
	assert.NotNil(t, medium.RecommendationEngines.Cost)
	assert.NotNil(t, medium.RecommendationEngines.Performance)
}

func TestBuildListResponse_EngineFilterIncludesAllTerms(t *testing.T) {
	cpuReq := int64(500)

	native := &NativeContainerResult{
		Recommendations: map[string]TermRecommendation{
			"short_term": {
				Performance: &EngineRecommendation{CPURequestMillicores: &cpuReq},
			},
			"medium_term": {
				Performance: &EngineRecommendation{CPURequestMillicores: &cpuReq},
			},
		},
	}

	list := BuildListResponse(native, time.Time{}, ListResponseOptions{})
	require.Contains(t, list.Recommendations.RecommendationTerms, "short_term")
	require.Contains(t, list.Recommendations.RecommendationTerms, "medium_term")
	assert.Nil(t, list.Recommendations.RecommendationTerms["short_term"].RecommendationEngines.Cost)
	assert.NotNil(t, list.Recommendations.RecommendationTerms["short_term"].RecommendationEngines.Performance)
}

func TestBuildListResponse_EngineFilterOption(t *testing.T) {
	cpuReq := int64(500)

	native := &NativeContainerResult{
		Recommendations: map[string]TermRecommendation{
			"short_term": {
				Cost:        &EngineRecommendation{CPURequestMillicores: &cpuReq},
				Performance: &EngineRecommendation{CPURequestMillicores: &cpuReq},
			},
		},
	}

	list := BuildListResponse(native, time.Time{}, ListResponseOptions{EngineFilter: "performance"})
	shortTerm := list.Recommendations.RecommendationTerms["short_term"]
	require.NotNil(t, shortTerm.RecommendationEngines)
	assert.Nil(t, shortTerm.RecommendationEngines.Cost)
	assert.NotNil(t, shortTerm.RecommendationEngines.Performance)
}

func TestBuildListResponse_JSONOmitsPlotsAndDuration(t *testing.T) {
	cpuReq := int64(500)
	native := &NativeContainerResult{
		IdleState: "active",
		Recommendations: map[string]TermRecommendation{
			"short_term": {
				Cost: &EngineRecommendation{CPURequestMillicores: &cpuReq},
			},
		},
	}

	raw, err := json.Marshal(BuildListResponse(native, time.Time{}, ListResponseOptions{}))
	require.NoError(t, err)

	body := string(raw)
	assert.NotContains(t, body, "plots")
	assert.NotContains(t, body, "duration_in_hours")
	assert.NotContains(t, body, "business_hours")
	assert.NotContains(t, body, `"notifications"`)
}

func TestBuildListResponse_OmitsGPUBusinessHours(t *testing.T) {
	t.Parallel()
	profile := "1g.10gb"
	native := &NativeContainerResult{
		IdleState: "active",
		GPU: map[string]*GPURecommendation{
			"medium": {
				CurrentGPUModel: "A100",
				BusinessHours: &GPUBHRecommendation{
					GPUClassification:     "well_utilized",
					RecommendedGPUProfile: &profile,
					Notifications: map[string]notifications.NotificationEntry{
						"80": {Code: 80, Type: "WARNING", Message: "office window"},
					},
				},
			},
		},
	}
	list := BuildListResponse(native, time.Time{}, ListResponseOptions{})
	require.NotNil(t, list.GPU["medium"])
	assert.Nil(t, list.GPU["medium"].BusinessHours)
	require.NotNil(t, native.GPU["medium"].BusinessHours, "list copy must not mutate native GPU")

	raw, err := json.Marshal(list)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "business_hours")
	assert.NotContains(t, string(raw), "GPU_BH_OFFICE_WINDOW")
}

func TestBuildNamespaceListResponse_DefaultIncludesShortTermCostOnly(t *testing.T) {
	cpuReq := int64(500)
	varCPUReq := int32(-15)
	curCPUReq := int64(250)

	native := &NativeNamespaceResult{
		ID:           "ns-uuid",
		ClusterUUID:  "11111111-1111-1111-1111-111111111111",
		Project:      "default",
		LastReported: "2026-06-01T00:00:00Z",
		Recommendations: map[string]any{
			"monitoring_end_time": "2026-06-01T12:00:00Z",
			"short_term": TermRecommendation{
				Cost: &EngineRecommendation{
					CPURequestMillicores:   &cpuReq,
					CurrentCPURequestMC:    &curCPUReq,
					VariationCPURequestPct: &varCPUReq,
					NotificationCodes:      SmallintArray{1, 7},
				},
				Performance: &EngineRecommendation{
					CPURequestMillicores: &cpuReq,
					NotificationCodes:    SmallintArray{5},
				},
			},
			"medium_term": TermRecommendation{
				Cost: &EngineRecommendation{
					CPURequestMillicores: &cpuReq,
					NotificationCodes:    SmallintArray{9},
				},
			},
		},
	}

	list := BuildNamespaceListResponse(native, ListResponseOptions{})

	require.NotNil(t, list.Recommendations.Current)
	require.Contains(t, list.Recommendations.RecommendationTerms, "short_term")
	assert.NotContains(t, list.Recommendations.RecommendationTerms, "medium_term")
	assert.Equal(t, "2026-06-01T12:00:00Z", list.Recommendations.MonitoringEndTime)

	shortTerm := list.Recommendations.RecommendationTerms["short_term"]
	require.NotNil(t, shortTerm.RecommendationEngines)
	require.NotNil(t, shortTerm.RecommendationEngines.Cost)
	assert.Nil(t, shortTerm.RecommendationEngines.Performance)
	require.NotNil(t, shortTerm.RecommendationEngines.Cost.Variation)
	assert.Nil(t, shortTerm.RecommendationEngines.Cost.Notifications)

	require.NotNil(t, list.Recommendations.NotificationCodes)
	assert.Contains(t, list.Recommendations.NotificationCodes, int16(1))
	assert.Contains(t, list.Recommendations.NotificationCodes, int16(5))
	assert.Contains(t, list.Recommendations.NotificationCodes, int16(7))
}

func TestBuildNamespaceListResponse_TermEngineFilters(t *testing.T) {
	cpuReq := int64(500)

	native := &NativeNamespaceResult{
		Recommendations: map[string]any{
			"medium_term": TermRecommendation{
				Cost: &EngineRecommendation{CPURequestMillicores: &cpuReq},
				Performance: &EngineRecommendation{
					CPURequestMillicores: &cpuReq,
				},
			},
		},
	}

	list := BuildNamespaceListResponse(native, ListResponseOptions{
		TermFilter:   "medium_term",
		EngineFilter: "performance",
	})
	require.Len(t, list.Recommendations.RecommendationTerms, 1)
	medium := list.Recommendations.RecommendationTerms["medium_term"]
	require.NotNil(t, medium.RecommendationEngines)
	assert.Nil(t, medium.RecommendationEngines.Cost)
	assert.NotNil(t, medium.RecommendationEngines.Performance)
}

func TestBuildNamespaceListResponse_JSONOmitsPlotsAndDuration(t *testing.T) {
	cpuReq := int64(500)
	native := &NativeNamespaceResult{
		IdleState: "active",
		Recommendations: map[string]any{
			"short_term": TermRecommendation{
				Cost: &EngineRecommendation{CPURequestMillicores: &cpuReq},
			},
		},
	}

	raw, err := json.Marshal(BuildNamespaceListResponse(native, ListResponseOptions{}))
	require.NoError(t, err)

	body := string(raw)
	assert.NotContains(t, body, "plots")
	assert.NotContains(t, body, "duration_in_hours")
	assert.NotContains(t, body, "business_hours")
	assert.NotContains(t, body, `"notifications"`)
}

func TestBuildNamespaceListResponse_OmitsEngineBusinessHours(t *testing.T) {
	t.Parallel()
	cpuReq := int64(500)
	native := &NativeNamespaceResult{
		IdleState: "active",
		Recommendations: map[string]any{
			"short_term": TermRecommendation{
				Cost: &EngineRecommendation{
					CPURequestMillicores: &cpuReq,
					BusinessHours: &BusinessHoursRecommendation{
						Reason: "office window",
					},
				},
			},
		},
	}

	list := BuildNamespaceListResponse(native, ListResponseOptions{})
	short := list.Recommendations.RecommendationTerms["short_term"]
	require.NotNil(t, short.RecommendationEngines)
	require.NotNil(t, short.RecommendationEngines.Cost)
	assert.Nil(t, short.RecommendationEngines.Cost.BusinessHours)

	term := native.Recommendations["short_term"].(TermRecommendation)
	require.NotNil(t, term.Cost.BusinessHours, "slim list copy must not mutate native")

	raw, err := json.Marshal(list)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "business_hours")
}

func TestStripNamespaceDetailBusinessHours_FatListOmitsNest(t *testing.T) {
	t.Parallel()
	cpuReq := int64(500)
	native := &NativeNamespaceResult{
		IdleState: "active",
		Recommendations: map[string]any{
			"short_term": TermRecommendation{
				Cost: &EngineRecommendation{
					CPURequestMillicores: &cpuReq,
					BusinessHours: &BusinessHoursRecommendation{
						Reason: "office window",
					},
				},
				Performance: &EngineRecommendation{
					CPURequestMillicores: &cpuReq,
					BusinessHours: &BusinessHoursRecommendation{
						Reason: "office window",
					},
				},
			},
		},
	}

	detail := BuildNamespaceDetailResponse(native, nil, map[string]*NativePlot{
		"short_term": {},
	}, time.Time{}, ListResponseOptions{})
	require.NotNil(t, detail)
	short := detail.Recommendations.RecommendationTerms["short_term"]
	require.NotNil(t, short.RecommendationEngines.Cost.BusinessHours)
	require.NotNil(t, short.RecommendationEngines.Performance.BusinessHours)
	require.NotNil(t, short.BusinessHoursPlots)

	StripNamespaceDetailBusinessHours(detail)

	short = detail.Recommendations.RecommendationTerms["short_term"]
	assert.Nil(t, short.RecommendationEngines.Cost.BusinessHours)
	assert.Nil(t, short.RecommendationEngines.Performance.BusinessHours)
	assert.Nil(t, short.BusinessHoursPlots)

	term := native.Recommendations["short_term"].(TermRecommendation)
	require.NotNil(t, term.Cost.BusinessHours, "strip must not mutate native")
	require.NotNil(t, term.Performance.BusinessHours, "strip must not mutate native")

	raw, err := json.Marshal(detail)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "business_hours")
}

func TestStripNamespaceDetailBusinessHours_NilSafe(t *testing.T) {
	t.Parallel()
	StripNamespaceDetailBusinessHours(nil)
	StripNamespaceDetailBusinessHours(&NamespaceDetailResponse{})
}
