package model

import (
	"fmt"
	"strings"

	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

// NodeCategoryValues is the list of valid category values for node recommendations.
var NodeCategoryValues = []string{"idle", "overcommitted", "stranded_cpu", "stranded_memory", "underutilized", "optimized"}

// NodeUtilizationClassification holds the unified category for a node.
type NodeUtilizationClassification struct {
	Category string `json:"category"`
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
	RecommendedCPUCores     float32                                    `json:"recommended_cpu_cores,omitempty"`
	RecommendedMemoryGiB    float32                                    `json:"recommended_memory_gib,omitempty"`
	NodeCountReduction      int                                        `json:"node_count_reduction"`
	EstimatedMonthlySavings *money.MoneyAmount                         `json:"estimated_monthly_savings,omitempty"`
	Notifications           map[string]notifications.NotificationEntry `json:"notifications,omitempty"`
	UpdatedAt               string                                     `json:"updated_at,omitempty"`
	Explanation             *NodeExplanationAPI                        `json:"explanation,omitempty"`
	BusinessHours           *NodeBHRecommendation                      `json:"business_hours,omitempty"`
}

// NodeBHRecommendation is the nested business-hours perspective on a node detail engine.
// Units are cores and GiB (same as the parent engine). Notifications on this object
// are not merged into parent engine or top-level detail notifications.
type NodeBHRecommendation struct {
	RecommendedCPUCores  *float32                                   `json:"recommended_cpu_cores,omitempty"`
	RecommendedMemoryGiB *float32                                   `json:"recommended_memory_gib,omitempty"`
	Reason               string                                     `json:"reason,omitempty"`
	Notifications        map[string]notifications.NotificationEntry `json:"notifications,omitempty"`
}

// NodeUtilizationEngines groups cost and performance engine recommendations.
type NodeUtilizationEngines struct {
	Cost        *NodeUtilizationEngineRec `json:"cost,omitempty"`
	Performance *NodeUtilizationEngineRec `json:"performance,omitempty"`
}

// NodeUtilizationTermRec holds recommendation engines for a single term window.
type NodeUtilizationTermRec struct {
	ConfidenceLevel       float32                 `json:"confidence_level,omitempty"`
	DataDays              int                     `json:"data_days,omitempty"`
	RecommendationEngines *NodeUtilizationEngines `json:"recommendation_engines,omitempty"`
}

// NodeUtilizationRec is the API response DTO for a node CPU/memory utilization recommendation.
// Each node appears once with nested recommendation_terms and recommendation_engines.
type NodeUtilizationRec struct {
	ID                    string                            `json:"id,omitempty"`
	Node                  string                            `json:"node"`
	ClusterUUID           string                            `json:"cluster_uuid"`
	InstanceType          string                            `json:"instance_type,omitempty"`
	MachineSetName        string                            `json:"machineset_name,omitempty"`
	SuggestedInstanceType string                            `json:"suggested_instance_type,omitempty"`
	InstanceTypeReason    string                            `json:"instance_type_reason,omitempty"`
	RecommendationType    string                            `json:"recommendation_type"`
	Classification        NodeUtilizationClassification     `json:"classification"`
	Metrics               NodeUtilizationMetrics            `json:"metrics"`
	PodCount              int64                             `json:"pod_count"`
	PodCapacity           *int64                            `json:"pod_capacity,omitempty"`
	PodSchedulingHeadroom *float32                          `json:"pod_scheduling_headroom,omitempty"`
	CPUOvercommitRatio    float32                           `json:"cpu_overcommit_ratio"`
	NodeGPUCount          *int64                            `json:"node_gpu_count"`
	TrendSlope            float32                           `json:"trend_slope"`
	RecommendationTerms   map[string]NodeUtilizationTermRec `json:"recommendation_terms"`
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

// NodeDailyDigestItem is a single day's utilization digest for a node, returned on the detail endpoint.
type NodeDailyDigestItem struct {
	BucketDate           string `json:"bucket_date"`
	CPUUsageP50MC        int64  `json:"cpu_usage_p50_mc"`
	CPUUsageP95MC        int64  `json:"cpu_usage_p95_mc"`
	CPUUsageMaxMC        *int64 `json:"cpu_usage_max_mc,omitempty"`
	MemUsageP50KiB       int64  `json:"mem_usage_p50_kib"`
	MemUsageP95KiB       int64  `json:"mem_usage_p95_kib"`
	MemUsageMaxKiB       *int64 `json:"mem_usage_max_kib,omitempty"`
	MaxCPUAllocatableMC  int64  `json:"max_cpu_allocatable_mc"`
	MaxMemAllocatableKiB int64  `json:"max_mem_allocatable_kib"`
	MaxCPURequestsMC     int64  `json:"max_cpu_requests_mc"`
	MaxMemRequestsKiB    int64  `json:"max_mem_requests_kib"`
}

// NodeUtilizationDetailRec is the non-paginated response for a single node detail request.
type NodeUtilizationDetailRec struct {
	ID                    string                                     `json:"id,omitempty"`
	Node                  string                                     `json:"node"`
	ClusterUUID           string                                     `json:"cluster_uuid"`
	InstanceType          string                                     `json:"instance_type,omitempty"`
	MachineSetName        string                                     `json:"machineset_name,omitempty"`
	PodCount              int64                                      `json:"pod_count"`
	PodCapacity           *int64                                     `json:"pod_capacity,omitempty"`
	PodSchedulingHeadroom *float32                                   `json:"pod_scheduling_headroom,omitempty"`
	Category              string                                     `json:"category"`
	SuggestedInstanceType string                                     `json:"suggested_instance_type,omitempty"`
	InstanceTypeReason    string                                     `json:"instance_type_reason,omitempty"`
	Metrics               NodeUtilizationMetrics                     `json:"metrics"`
	CPUOvercommitRatio    float32                                    `json:"cpu_overcommit_ratio"`
	NodeGPUCount          *int64                                     `json:"node_gpu_count"`
	TrendSlope            float32                                    `json:"trend_slope"`
	RecommendationTerms   map[string]NodeUtilizationTermRec          `json:"recommendation_terms"`
	Notifications         map[string]notifications.NotificationEntry `json:"notifications,omitempty"`
	DailyDigests          []NodeDailyDigestItem                      `json:"daily_digests,omitempty"`
}

// NodeUtilizationMeta holds pagination metadata for node utilization responses.
type NodeUtilizationMeta struct {
	Count             int      `json:"count"`
	Limit             int      `json:"limit"`
	Offset            int      `json:"offset"`
	HasNext           bool     `json:"has_next"`
	NextCursor        string   `json:"next_cursor,omitempty"`
	Currency          string   `json:"currency"`
	DataDaysAvailable int      `json:"data_days_available"`
	MinDataDays       int      `json:"min_data_days"`
	Warnings          []string `json:"warnings,omitempty"`
}

// NodeUtilTermAPIKey returns the API term key (e.g. "medium_term") for a DB term name.
func NodeUtilTermAPIKey(dbTerm string) string {
	return dbTerm + "_term"
}

// NodeUtilRecommendationTermsHasData reports whether node list projection terms include an engine block.
func NodeUtilRecommendationTermsHasData(terms map[string]NodeUtilizationTermRec) bool {
	if len(terms) == 0 {
		return false
	}
	for _, term := range terms {
		if term.RecommendationEngines == nil {
			continue
		}
		if term.RecommendationEngines.Cost != nil || term.RecommendationEngines.Performance != nil {
			return true
		}
	}
	return false
}

// NodeCategoryFilterValues returns validated category values for node list filtering.
func NodeCategoryFilterValues(vals []string) ([]string, error) {
	valid := map[string]bool{
		"idle": true, "overcommitted": true,
		"stranded_cpu": true, "stranded_memory": true,
		"underutilized": true, "optimized": true,
	}
	var out []string
	for _, v := range vals {
		v = strings.TrimSpace(strings.ToLower(v))
		if v == "" {
			continue
		}
		if !valid[v] {
			return nil, fmt.Errorf("invalid node category %q; valid values: idle, overcommitted, stranded_cpu, stranded_memory, underutilized, optimized", v)
		}
		out = append(out, v)
	}
	return out, nil
}
