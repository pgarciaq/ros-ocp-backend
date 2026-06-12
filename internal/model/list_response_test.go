package model

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	require.NotNil(t, list.Recommendations.Notifications)
	assert.Contains(t, list.Recommendations.Notifications, "1")
	assert.Contains(t, list.Recommendations.Notifications, "5")
	assert.Contains(t, list.Recommendations.Notifications, "7")
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
}
