package model

import (
	"time"

	"github.com/google/uuid"
)

// VMRecommendation holds the current vs recommended sizing for a VM.
type VMRecommendation struct {
	ID          int64     `db:"id"`
	OrgID       string    `db:"org_id"`
	ClusterUUID uuid.UUID `db:"cluster_uuid"`
	VMName      string    `db:"vm_name"`
	Namespace   string    `db:"namespace"`
	GuestOS     string    `db:"guest_os"`

	// Current allocation
	CurrentVCPU         int32   `db:"current_vcpu"`
	CurrentMemoryGiB    int32   `db:"current_memory_gib"`
	CurrentDiskGiB      *int32  `db:"current_disk_gib"`
	CurrentInstanceType *string `db:"current_instance_type"`

	// Recommended
	RecommendedVCPU         int32   `db:"recommended_vcpu"`
	RecommendedMemoryGiB    int32   `db:"recommended_memory_gib"`
	RecommendedDiskGiB      *int32  `db:"recommended_disk_gib"`
	RecommendedInstanceType *string `db:"recommended_instance_type"`
	RecommendedSeries       *string `db:"recommended_series"`

	// Metadata
	GuestAgentDetected bool   `db:"guest_agent_detected"`
	Confidence         string `db:"confidence"` // "high" or "moderate"
	Term               string `db:"term"`       // short_term, medium_term, long_term
	Engine             string `db:"engine"`     // cost, performance

	// Status flags
	IsIdle       bool `db:"is_idle"`
	IsAbandoned  bool `db:"is_abandoned"`
	IsPowerOffCandidate bool   `db:"is_power_off_candidate"`
	PowerOffIdleRatio   *int32 `db:"power_off_idle_ratio"`
	IsOversized     bool `db:"is_oversized"`
	IsNetworkBound       bool `db:"is_network_bound"`
	IsRedundantPlacement bool `db:"is_redundant_placement"`
	HasSharedStorage     bool `db:"has_shared_storage"`
	NUMAOversized        bool `db:"numa_oversized"`

	// I/O profile (nullable JSON or individual columns)
	IOReadIOPSP95  *int64  `db:"io_read_iops_p95"`
	IOWriteIOPSP95 *int64  `db:"io_write_iops_p95"`
	IOReadBPS95    *int64  `db:"io_read_bps_p95"`
	IOWriteBPS95   *int64  `db:"io_write_bps_p95"`
	IOHint         *string `db:"io_hint"`
	IOPattern      string  `db:"io_pattern"` // sequential, random, mixed, low-io, or empty

	// Disk projection
	DiskDaysUntilFull        *int32   `db:"disk_days_until_full"`
	DiskGrowthGiBPerDay      *float64 `db:"disk_growth_gib_per_day"`
	DiskRecommendedExpandGiB *int32   `db:"disk_recommended_expand_gib"`

	// Notifications (stored as JSONB)
	Notifications []byte `db:"notifications"`

	// GPU recommendation (empty when VM has no GPU)
	GPUCount              int32  `db:"gpu_count" json:"gpu_count"`
	GPUModel              string `db:"gpu_model" json:"gpu_model"`
	GPUClassification     string `db:"gpu_classification" json:"gpu_classification"`
	RecommendedGPUAction  string `db:"recommended_gpu_action" json:"recommended_gpu_action"`
	RecommendedGPUProfile      string `db:"recommended_gpu_profile" json:"recommended_gpu_profile"`
	RecommendedTimeSliceCount  int32  `db:"recommended_time_slice_count" json:"recommended_time_slice_count"`
	GPUTimeSliceConfidence   string `db:"gpu_timeslice_confidence" json:"gpu_timeslice_confidence"`
	GPUTimeSliceRationale    string `db:"gpu_timeslice_rationale" json:"gpu_timeslice_rationale"`
	RecommendedVGPUProfile   string `db:"recommended_vgpu_profile" json:"recommended_vgpu_profile"`
	GPUUtilizationAvgBP      int32  `db:"gpu_utilization_avg_bp" json:"gpu_utilization_avg_bp"`

	// Monthly savings estimate in cents (nullable; populated when ROS_SAVINGS_ESTIMATES_ENABLED and Koku rates exist)
	EstimatedSavingsCents *int64  `db:"estimated_savings_cents"`
	SavingsCurrency       *string `db:"savings_currency"`

	// Timestamps
	LastRecommendedAt time.Time `db:"last_recommended_at"`
	CreatedAt         time.Time `db:"created_at"`
	UpdatedAt         time.Time `db:"updated_at"`
}

// VMRecommendationStatus represents the recommendation status for filtering.
type VMRecommendationStatus string

const (
	VMStatusActive    VMRecommendationStatus = "active"
	VMStatusIdle      VMRecommendationStatus = "idle"
	VMStatusOversized VMRecommendationStatus = "oversized"
)
