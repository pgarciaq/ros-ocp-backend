package model

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

// NodeUtilizationClassification holds shared utilization flags for a node.
type NodeUtilizationClassification struct {
	IsUnderutilized  bool    `json:"is_underutilized"`
	IsOvercommitted  bool    `json:"is_overcommitted"`
	StrandedResource *string `json:"stranded_resource"`
}

// NodeUtilizationMetrics holds shared utilization percentiles for a node.
type NodeUtilizationMetrics struct {
	CPUUtilP50 float32 `json:"cpu_util_p50"`
	CPUUtilP95 float32 `json:"cpu_util_p95"`
	MemUtilP50 float32 `json:"mem_util_p50"`
	MemUtilP95 float32 `json:"mem_util_p95"`
}

// NodeUtilizationEngineRec holds engine-specific sizing and savings for a node term.
type NodeUtilizationEngineRec struct {
	RecommendedCPUCores        float32                                    `json:"recommended_cpu_cores,omitempty"`
	RecommendedMemoryGiB       float32                                    `json:"recommended_memory_gib,omitempty"`
	NodeCountReduction         int                                        `json:"node_count_reduction"`
	EstimatedMonthlySavings *money.SavingsObject                       `json:"estimated_monthly_savings,omitempty"`
	Notifications              map[string]notifications.NotificationEntry `json:"notifications,omitempty"`
	UpdatedAt                  string                                     `json:"updated_at,omitempty"`
}

// NodeUtilizationEngines groups cost and performance engine recommendations.
type NodeUtilizationEngines struct {
	Cost        *NodeUtilizationEngineRec `json:"cost,omitempty"`
	Performance *NodeUtilizationEngineRec `json:"performance,omitempty"`
}

// NodeUtilizationTermRec holds recommendation engines for a single term window.
type NodeUtilizationTermRec struct {
	RecommendationEngines *NodeUtilizationEngines `json:"recommendation_engines,omitempty"`
}

// NodeUtilizationRec is the API response DTO for a node CPU/memory utilization recommendation.
// Each node appears once with nested recommendation_terms and recommendation_engines.
type NodeUtilizationRec struct {
	Node                string                            `json:"node"`
	ClusterUUID         string                            `json:"cluster_uuid"`
	RecommendationType  string                            `json:"recommendation_type"`
	Classification      NodeUtilizationClassification     `json:"classification"`
	Metrics             NodeUtilizationMetrics            `json:"metrics"`
	PodCount            int64                             `json:"pod_count"`
	CPUOvercommitRatio  float32                           `json:"cpu_overcommit_ratio"`
	TrendSlope          float32                           `json:"trend_slope"`
	RecommendationTerms map[string]NodeUtilizationTermRec `json:"recommendation_terms"`
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
	Count    int      `json:"count"`
	Limit    int      `json:"limit"`
	Offset   int      `json:"offset"`
	Currency string   `json:"currency"`
	Warnings []string `json:"warnings,omitempty"`
}

// NodeUtilTermAPIKey returns the API term key (e.g. "medium_term") for a DB term name.
func NodeUtilTermAPIKey(dbTerm string) string {
	return dbTerm + "_term"
}
