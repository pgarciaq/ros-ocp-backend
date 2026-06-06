package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

func TestNodeGPURecommendation_JSONRoundtrip(t *testing.T) {
	savings := money.FormatUSDToSavings(225.0, "USD")
	rec := NodeGPURecommendation{
		NodeName:            "gpu-worker-1",
		ClusterUUID:         "abc-123",
		RecommendationType:  "gpu_time_slicing",
		GPUModel:            "T4",
		RecommendedReplicas: 4,
		SavingsPerGPU:       &savings,
		Confidence:          0.65,
		CandidateContainers: []NodeContainerRef{
			{Namespace: "ml", Workload: "embed", Container: "srv", SMActiveAvg: 0.12, Classification: "underutilized"},
		},
		NotificationCodes: []int16{29},
	}
	data, err := json.Marshal(rec)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"node_name":"gpu-worker-1"`)
	assert.Contains(t, string(data), `"recommended_replicas":4`)
	assert.Contains(t, string(data), `"gpu_model":"T4"`)
	assert.Contains(t, string(data), `"savings_per_gpu"`)

	var decoded NodeGPURecommendation
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, rec.NodeName, decoded.NodeName)
	assert.Equal(t, rec.RecommendedReplicas, decoded.RecommendedReplicas)
}

func TestNodeRecommendationListResponse_EmptyData(t *testing.T) {
	resp := NodeRecommendationListResponse{
		Meta:  NodeRecommendationMeta{Count: 0, Limit: 10, Offset: 0, Currency: "USD"},
		Data:  []NodeGPURecommendation{},
		Links: NodeRecommendationLinks{First: "/nodes?limit=10&offset=0"},
	}
	data, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"count":0`)
	assert.Contains(t, string(data), `"limit":10`)
	assert.Contains(t, string(data), `"offset":0`)
	assert.Contains(t, string(data), `"currency":"USD"`)
	assert.Contains(t, string(data), `"data":[]`)
	assert.Contains(t, string(data), `"links"`)
}

func TestNodeRecommendationMeta_WithPagination(t *testing.T) {
	totalSavings := money.FormatUSDToSavings(500.0, "USD")
	meta := NodeRecommendationMeta{
		Count:        42,
		Limit:        10,
		Offset:       20,
		Currency:     "USD",
		TotalSavings: &totalSavings,
	}
	data, err := json.Marshal(meta)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"count":42`)
	assert.Contains(t, string(data), `"limit":10`)
	assert.Contains(t, string(data), `"offset":20`)
	assert.Contains(t, string(data), `"currency":"USD"`)
	assert.Contains(t, string(data), `"total_savings"`)
}
