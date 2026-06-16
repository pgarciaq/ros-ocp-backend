package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// NodeGPUTimeslicingRecommendation is a persisted node-level GPU time-slicing recommendation.
type NodeGPUTimeslicingRecommendation struct {
	OrgID                 string               `db:"org_id" json:"org_id"`
	ClusterUUID           uuid.UUID            `db:"cluster_uuid" json:"cluster_uuid"`
	NodeName              string               `db:"node_name" json:"node_name"`
	GPUModel              string               `db:"gpu_model" json:"gpu_model"`
	Term                  string               `db:"term" json:"term"`
	RecommendedReplicas   int32                `db:"recommended_replicas" json:"recommended_replicas"`
	Confidence            float32              `db:"confidence" json:"confidence"`
	ConfidenceLevel       float32              `db:"confidence_level" json:"confidence_level"`
	CandidateCount        int32                `db:"candidate_count" json:"candidate_count"`
	ImpactedCount         int32                `db:"impacted_count" json:"impacted_count"`
	CandidateContainers   NodeContainerRefList `db:"candidate_containers" json:"candidate_containers"`
	ImpactedContainers    NodeContainerRefList `db:"impacted_containers" json:"impacted_containers"`
	NotificationCodes     SmallintArray        `db:"notification_codes" json:"notification_codes"`
	EstimatedSavingsCents *int64               `db:"estimated_savings_cents" json:"estimated_savings_cents,omitempty"`
	SavingsPerGPUCents    *int64               `db:"savings_per_gpu_cents" json:"savings_per_gpu_cents,omitempty"`
	LastSeenAt            *time.Time           `db:"last_seen_at" json:"last_seen_at,omitempty"`
	UpdatedAt             time.Time            `db:"updated_at" json:"updated_at"`
}

// NodeGPUTimeslicingRecommendationHistory is an append-only history snapshot.
type NodeGPUTimeslicingRecommendationHistory struct {
	ID                    int64     `db:"id" json:"id"`
	OrgID                 string    `db:"org_id" json:"org_id"`
	ClusterUUID           uuid.UUID `db:"cluster_uuid" json:"cluster_uuid"`
	NodeName              string    `db:"node_name" json:"node_name"`
	GPUModel              string    `db:"gpu_model" json:"gpu_model"`
	Term                  string    `db:"term" json:"term"`
	RecommendedReplicas   int32     `db:"recommended_replicas" json:"recommended_replicas"`
	Confidence            float32   `db:"confidence" json:"confidence"`
	CandidateCount        int32     `db:"candidate_count" json:"candidate_count"`
	ImpactedCount         int32     `db:"impacted_count" json:"impacted_count"`
	EstimatedSavingsCents *int64    `db:"estimated_savings_cents" json:"estimated_savings_cents,omitempty"`
	RecordedAt            time.Time `db:"recorded_at" json:"recorded_at"`
}

// NodeContainerRefList is a JSONB-backed slice of NodeContainerRef for PostgreSQL.
type NodeContainerRefList []NodeContainerRef

// Scan implements sql.Scanner for PostgreSQL JSONB columns.
func (l *NodeContainerRefList) Scan(src interface{}) error {
	if src == nil {
		*l = NodeContainerRefList{}
		return nil
	}
	var data []byte
	switch v := src.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return fmt.Errorf("NodeContainerRefList.Scan: unsupported type %T", src)
	}
	if len(data) == 0 || string(data) == "null" {
		*l = NodeContainerRefList{}
		return nil
	}
	return json.Unmarshal(data, l)
}

// Value implements driver.Valuer for PostgreSQL JSONB columns.
func (l NodeContainerRefList) Value() (driver.Value, error) {
	if l == nil {
		return "[]", nil
	}
	b, err := json.Marshal(l)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}
