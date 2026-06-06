package model

import "github.com/redhatinsights/ros-ocp-backend/internal/money"

// NodeGPURecommendation represents a GPU time-slicing recommendation for a node.
type NodeGPURecommendation struct {
	NodeName            string               `json:"node_name"`
	ClusterUUID         string               `json:"cluster_uuid"`
	Term                string               `json:"term"`
	RecommendationType  string               `json:"recommendation_type"`
	GPUModel            string               `json:"gpu_model"`
	RecommendedReplicas int                  `json:"recommended_replicas"`
	SavingsPerGPU       *money.MoneyAmount `json:"savings_per_gpu,omitempty"`
	TotalNodeSavings    *money.MoneyAmount `json:"total_node_savings,omitempty"`
	Confidence          float32              `json:"confidence"`
	ConfidenceLevel     float32            `json:"confidence_level"`
	CandidateContainers []NodeContainerRef `json:"candidate_containers"`
	ImpactedContainers  []NodeContainerRef `json:"impacted_containers"`
	NotificationCodes   []int16            `json:"notification_codes"`
}

// NodeContainerRef identifies a container within a node-level recommendation.
type NodeContainerRef struct {
	Namespace      string  `json:"namespace"`
	Workload       string  `json:"workload"`
	Container      string  `json:"container"`
	SMActiveAvg    float32 `json:"sm_active_avg"`
	Classification string  `json:"classification"`
}

// NodeRecommendationListResponse is the envelope for the GPU time-slicing recommendations endpoint.
type NodeRecommendationListResponse struct {
	Meta     NodeRecommendationMeta  `json:"meta"`
	Data     []NodeGPURecommendation `json:"data"`
	Links    PaginationLinks         `json:"links"`
	Warnings []string                `json:"warnings,omitempty"`
}

// NodeRecommendationMeta holds metadata for the node recommendations response.
type NodeRecommendationMeta struct {
	Count         int                  `json:"count"`
	Limit         int                  `json:"limit"`
	Offset        int                  `json:"offset"`
	HasNext       bool                 `json:"has_next"`
	NextCursor    string               `json:"next_cursor,omitempty"`
	Currency      string               `json:"currency"`
	TotalSavings  *money.MoneyAmount `json:"total_savings,omitempty"`
}

// NodeRecommendationLinks is an alias for backward compatibility.
type NodeRecommendationLinks = PaginationLinks
