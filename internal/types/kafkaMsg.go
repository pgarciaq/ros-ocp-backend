package types

import "time"

type PayloadType string

const (
	PayloadTypeContainer PayloadType = "container"
	PayloadTypeNamespace PayloadType = "namespace"
)

// KafkaCustomTimeframes is optional embedded settings (metadata.custom_timeframes).
type KafkaCustomTimeframes struct {
	Terms         []KafkaTermConfig      `json:"terms,omitempty"`
	BusinessHours KafkaBusinessHoursLite `json:"business_hours,omitempty"`
}

// KafkaTermConfig is one term in a Kafka metadata payload.
type KafkaTermConfig struct {
	Name         string `json:"name"`
	DurationDays int    `json:"duration_days"`
}

// KafkaBusinessHoursLite mirrors business_hours in Kafka JSON (subset).
type KafkaBusinessHoursLite struct {
	Enabled bool `json:"enabled"`
}

// KafkaMsgMetadata is the metadata object in hccm.ros.events payloads.
type KafkaMsgMetadata struct {
	Account          string                 `json:"account,omitempty"`
	Org_id           string                 `json:"org_id" validate:"required"`
	Source_id        string                 `json:"source_id" validate:"required"`
	Cluster_uuid     string                 `json:"cluster_uuid" validate:"required,uuid"`
	Cluster_alias    string                 `json:"cluster_alias" validate:"required"`
	CustomTimeframes *KafkaCustomTimeframes `json:"custom_timeframes,omitempty"`
}

type KafkaMsg struct {
	Request_id   string           `json:"request_id" validate:"required"`
	B64_identity string           `json:"b64_identity" validate:"required"`
	Metadata     KafkaMsgMetadata `json:"metadata" validate:"required"`
	Files        []string         `json:"files" validate:"required"`
}

type RecommendationMetadata struct {
	Org_id             string      `validate:"required"`
	Workload_id        uint        `validate:"required"`
	Experiment_name    string      `validate:"required"`
	Max_endtime_report time.Time   `validate:"required"`
	ExperimentType     PayloadType `validate:"required"`
}

type RecommendationKafkaMsg struct {
	Request_id string                 `validate:"required"`
	Metadata   RecommendationMetadata `validate:"required"`
}
