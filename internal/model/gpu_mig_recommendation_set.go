package model

import (
	"encoding/json"
	"time"
)

// GPUMIGRecommendationSet maps to one row in the gpu_mig_recommendation_sets table.
type GPUMIGRecommendationSet struct {
	ID                   int64     `json:"-"`
	OrgID                string    `json:"-"`
	ClusterUUID          string    `json:"cluster_uuid"`
	Namespace            string    `json:"namespace"`
	Workload             string    `json:"workload"`
	WorkloadType         string    `json:"workload_type,omitempty"`
	ContainerName        string    `json:"container_name"`
	NodeName             string    `json:"node_name,omitempty"`
	GPUModelName         string    `json:"gpu_model_name"`
	Term                 string    `json:"term"`
	RecommendedGPUProfile string  `json:"recommended_gpu_profile"`
	CurrentGPUProfile    string    `json:"current_gpu_profile,omitempty"`
	GPUClassification    string    `json:"gpu_classification"`
	Confidence           float32   `json:"confidence"`
	FBUsageMaxMiB        float32   `json:"fb_usage_max_mib"`
	TotalFBMiB           *int64    `json:"total_fb_mib,omitempty"`
	GPUIdleState         string    `json:"gpu_idle_state"`
	GPUIdleSince         *time.Time `json:"gpu_idle_since,omitempty"`
	GPUIdleDurationDays  int       `json:"gpu_idle_duration_days,omitempty"`
	SavingsMicroCents    int64     `json:"savings_micro_cents,omitempty"`
	WasteMicroCents      int64     `json:"waste_micro_cents,omitempty"`
	Category             string    `json:"category,omitempty"`
	IdleState            string    `json:"idle_state,omitempty"`
	NotificationCodes    []int16   `json:"notification_codes,omitempty"`
	LastReported         time.Time `json:"last_reported"`
	CreatedAt            time.Time `json:"-"`
	UpdatedAt            time.Time `json:"-"`
}

// GPUMIGRecommendationSetRow is returned by the list query,
// projected for the API response (includes cluster_alias).
type GPUMIGRecommendationSetRow struct {
	ClusterUUID           string  `json:"cluster_uuid"`
	ClusterAlias          string  `json:"cluster_alias,omitempty"`
	Namespace             string  `json:"namespace"`
	Workload              string  `json:"workload"`
	WorkloadType          string  `json:"workload_type,omitempty"`
	Container             string  `json:"container"`
	NodeName              string  `json:"node_name,omitempty"`
	GPUModel              string  `json:"gpu_model"`
	Term                  string  `json:"term"`
	RecommendedGPUProfile string  `json:"recommended_gpu_profile"`
	CurrentGPUProfile     string  `json:"current_gpu_profile,omitempty"`
	Classification        string  `json:"gpu_classification"`
	Confidence            float32 `json:"confidence"`
	ConfidenceLevel       float32 `json:"confidence_level"`
	FBUsageMaxMiB         float32 `json:"fb_usage_max_mib"`
	TotalFBMiB            *int64  `json:"total_fb_mib,omitempty"`
	GPUIdleState          string  `json:"gpu_idle_state,omitempty"`

	SortKey json.RawMessage `json:"-"`
}

// GPUMIGGroupedRow is a single group-by aggregate row.
type GPUMIGGroupedRow struct {
	GroupKey string `json:"group_key"`
	Count   int    `json:"count"`
}
