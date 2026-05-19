package model

import "github.com/redhatinsights/ros-ocp-backend/internal/notifications"

// NodeUtilizationRec is the API response DTO for a node CPU/memory utilization recommendation.
type NodeUtilizationRec struct {
	Node               string                                     `json:"node"`
	ClusterUUID        string                                     `json:"cluster_uuid"`
	Term               string                                     `json:"term"`
	CPUUtilP50         float32                                    `json:"cpu_utilization_p50"`
	CPUUtilP95         float32                                    `json:"cpu_utilization_p95"`
	MemUtilP50         float32                                    `json:"memory_utilization_p50"`
	MemUtilP95         float32                                    `json:"memory_utilization_p95"`
	CPUOvercommitRatio float32                                    `json:"cpu_overcommit_ratio"`
	IsUnderutilized    bool                                       `json:"is_underutilized"`
	IsOvercommitted    bool                                       `json:"is_overcommitted"`
	StrandedResource   *string                                    `json:"stranded_resource"`
	PodCount           int64                                      `json:"pod_count"`
	TrendSlope         float32                                    `json:"trend_slope"`
	RecommendationType string                                     `json:"recommendation_type"`
	Notifications      map[string]notifications.NotificationEntry `json:"notifications,omitempty"`
	UpdatedAt          string                                     `json:"updated_at"`
}

// PaginationLinks holds pagination link URLs for list responses.
type PaginationLinks struct {
	First    string `json:"first"`
	Previous string `json:"previous,omitempty"`
	Next     string `json:"next,omitempty"`
	Last     string `json:"last"`
}

// NodeUtilizationListResponse is the paginated list response for node utilization recs.
type NodeUtilizationListResponse struct {
	Meta     NodeUtilizationMeta  `json:"meta"`
	Data     []NodeUtilizationRec `json:"data"`
	Links    PaginationLinks      `json:"links"`
	Warnings []string             `json:"warnings,omitempty"`
}

// NodeUtilizationMeta holds pagination metadata for node utilization responses.
type NodeUtilizationMeta struct {
	Count  int `json:"count"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}
