package types

import "time"

type PayloadType string

const (
	PayloadTypeContainer    PayloadType = "container"
	PayloadTypeNamespace    PayloadType = "namespace"
	PayloadTypeStorage      PayloadType = "storage"
	PayloadTypeSnapshot     PayloadType = "snapshot"
	PayloadTypeClusterQuota PayloadType = "cluster-quota"
	PayloadTypeVM           PayloadType = "vm"
	PayloadTypeVMGPU        PayloadType = "vm-gpu"
	PayloadTypeUnknown      PayloadType = "unknown"
)

type KafkaMsg struct {
	Request_id   string `validate:"required"`
	B64_identity string `validate:"required"`
	Metadata     struct {
		Account        string
		Org_id         string `validate:"required"`
		Source_id      string `validate:"required"`
		Cluster_uuid   string `validate:"required,uuid"`
		Cluster_alias  string `validate:"required"`
		Manifest_id    string `json:"manifest_id,omitempty"`
		Expected_files []string `json:"expected_files,omitempty"`
	} `validate:"required"`
	Files       []string `validate:"required,max=1000"`
	Object_keys []string `json:"object_keys,omitempty" validate:"max=1000"`
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
