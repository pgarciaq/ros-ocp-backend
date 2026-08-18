package model

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

// NodeGPURecommendation represents a GPU time-slicing recommendation for a node.
type NodeGPURecommendation struct {
	NodeName                string                            `json:"node_name"`
	ClusterUUID             string                            `json:"cluster_uuid"`
	Term                    string                            `json:"term"`
	RecommendationType      string                            `json:"recommendation_type"`
	GPUModel                string                            `json:"gpu_model"`
	RecommendedReplicas     int                               `json:"recommended_replicas"`
	SavingsPerGPU           *money.MoneyAmount                `json:"savings_per_gpu,omitempty"`
	EstimatedMonthlySavings *money.MoneyAmount                `json:"estimated_monthly_savings,omitempty"`
	Confidence              float32                           `json:"confidence"`
	ConfidenceLevel         float32                           `json:"confidence_level"`
	Classification          string                            `json:"classification"`
	CandidateContainers     []NodeContainerRef                `json:"candidate_containers"`
	ImpactedContainers      []NodeContainerRef                `json:"impacted_containers"`
	NotificationCodes       []int16                           `json:"notification_codes"`
	Explanation             *NodeGPUTimeslicingExplanationAPI `json:"explanation,omitempty"`
	BusinessHours           *TimeslicingBHRecommendation      `json:"business_hours,omitempty"`
}

// TimeslicingBHRecommendation is the nested business-hours GPU time-slicing
// perspective on GET .../gpu/timeslicing/{node} only. List, history, and
// summary stay all-hours. Code 81 is on this object when replica sizing is
// present; reason-only insufficient-data blocks omit 81. Heterogeneous
// namespace windows omit this object entirely. Dollar savings are never nested.
type TimeslicingBHRecommendation struct {
	RecommendedReplicas *int                                       `json:"recommended_replicas,omitempty"`
	Confidence          *float32                                   `json:"confidence,omitempty"`
	CandidateCount      *int                                       `json:"candidate_count,omitempty"`
	ImpactedCount       *int                                       `json:"impacted_count,omitempty"`
	Reason              string                                     `json:"reason,omitempty"`
	Notifications       map[string]notifications.NotificationEntry `json:"notifications,omitempty"`
}

// NodeRecommendationListResponse is the envelope for the GPU time-slicing recommendations endpoint.
type NodeRecommendationListResponse struct {
	Meta     NodeRecommendationMeta  `json:"meta"`
	Data     []NodeGPURecommendation `json:"data"`
	Links    PaginationLinks         `json:"links"`
	Warnings []string                `json:"warnings,omitempty"`
}

// NodeGPUTimeslicingDetailResponse is the envelope for GET .../gpu/timeslicing/{node}.
// Same row shape as the list; business_hours is nested here only.
type NodeGPUTimeslicingDetailResponse struct {
	Data []NodeGPURecommendation `json:"data"`
}

// NodeRecommendationMeta holds metadata for the node recommendations response.
type NodeRecommendationMeta struct {
	Count                   int                `json:"count"`
	Limit                   int                `json:"limit"`
	Offset                  int                `json:"offset"`
	HasNext                 bool               `json:"has_next"`
	NextCursor              string             `json:"next_cursor,omitempty"`
	Currency                string             `json:"currency"`
	MinDataDays             int                `json:"min_data_days"`
	EstimatedMonthlySavings *money.MoneyAmount `json:"estimated_monthly_savings,omitempty"`
}

// NodeRecommendationLinks is an alias for backward compatibility.
type NodeRecommendationLinks = PaginationLinks
