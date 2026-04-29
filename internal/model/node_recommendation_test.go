package model

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNodeGPURecommendation_JSONRoundtrip(t *testing.T) {
	savings := float32(225.0)
	rec := NodeGPURecommendation{
		NodeName:            "gpu-worker-1",
		ClusterUUID:         "abc-123",
		RecommendationType:  "gpu_time_slicing",
		GPUModel:            "T4",
		RecommendedReplicas: 4,
		SavingsPerGPUUSD:    &savings,
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

	var decoded NodeGPURecommendation
	require.NoError(t, json.Unmarshal(data, &decoded))
	assert.Equal(t, rec.NodeName, decoded.NodeName)
	assert.Equal(t, rec.RecommendedReplicas, decoded.RecommendedReplicas)
}

func TestNodeRecommendationListResponse_EmptyData(t *testing.T) {
	resp := NodeRecommendationListResponse{
		Meta:  NodeRecommendationMeta{Count: 0, Limit: 10, Offset: 0},
		Data:  []NodeGPURecommendation{},
		Links: NodeRecommendationLinks{First: "/nodes?limit=10&offset=0"},
	}
	data, err := json.Marshal(resp)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"count":0`)
	assert.Contains(t, string(data), `"limit":10`)
	assert.Contains(t, string(data), `"offset":0`)
	assert.Contains(t, string(data), `"data":[]`)
	assert.Contains(t, string(data), `"links"`)
}

func TestNodeRecommendationMeta_WithPagination(t *testing.T) {
	savings := float32(500.0)
	meta := NodeRecommendationMeta{
		Count:           42,
		Limit:           10,
		Offset:          20,
		TotalSavingsUSD: &savings,
	}
	data, err := json.Marshal(meta)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"count":42`)
	assert.Contains(t, string(data), `"limit":10`)
	assert.Contains(t, string(data), `"offset":20`)
	assert.Contains(t, string(data), `"total_savings_usd":500`)
}
