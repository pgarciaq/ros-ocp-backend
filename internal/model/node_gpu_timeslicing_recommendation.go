package model

import (
	"time"

	"github.com/google/uuid"

	"github.com/redhatinsights/ros-ocp-backend/internal/model/types"
)

// Re-export types from the lightweight types sub-package.
type NodeGPUTimeslicingRecommendationHistory = types.NodeGPUTimeslicingRecommendationHistory
type NodeContainerRef = types.NodeContainerRef
type NodeContainerRefList = types.NodeContainerRefList

// NodeGPUTimeslicingRecommendation is a persisted node-level GPU time-slicing recommendation.
// Kept in model because it references SmallintArray (defined in recommendation_set_native.go).
type NodeGPUTimeslicingRecommendation struct {
	OrgID                  string               `db:"org_id" json:"org_id"`
	ClusterUUID            uuid.UUID            `db:"cluster_uuid" json:"cluster_uuid"`
	NodeName               string               `db:"node_name" json:"node_name"`
	GPUModel               string               `db:"gpu_model" json:"gpu_model"`
	Term                   string               `db:"term" json:"term"`
	RecommendedReplicas    int32                `db:"recommended_replicas" json:"recommended_replicas"`
	Confidence             float32              `db:"confidence" json:"confidence"`
	ConfidenceLevel        float32              `db:"confidence_level" json:"confidence_level"`
	CandidateCount         int32                `db:"candidate_count" json:"candidate_count"`
	ImpactedCount          int32                `db:"impacted_count" json:"impacted_count"`
	CandidateContainers    NodeContainerRefList `db:"candidate_containers" json:"candidate_containers"`
	ImpactedContainers     NodeContainerRefList `db:"impacted_containers" json:"impacted_containers"`
	NotificationCodes      SmallintArray        `db:"notification_codes" json:"notification_codes"`
	EstimatedSavingsCents  *int64               `db:"estimated_savings_cents" json:"estimated_savings_cents,omitempty"`
	SavingsPerGPUCents     *int64               `db:"savings_per_gpu_cents" json:"savings_per_gpu_cents,omitempty"`
	LastSeenAt             *time.Time           `db:"last_seen_at" json:"last_seen_at,omitempty"`
	UpdatedAt              time.Time            `db:"updated_at" json:"updated_at"`
	ExplDataDays           *int                 `db:"expl_data_days" json:"expl_data_days,omitempty"`
	ExplCandidateCount     *int                 `db:"expl_candidate_count" json:"expl_candidate_count,omitempty"`
	ExplImpactedCount      *int                 `db:"expl_impacted_count" json:"expl_impacted_count,omitempty"`
	ExplClassificationRule *string              `db:"expl_classification_rule" json:"expl_classification_rule,omitempty"`
}
