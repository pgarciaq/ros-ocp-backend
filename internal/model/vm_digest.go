package model

import (
	"time"

	"github.com/google/uuid"
)

// DailyVMDigest holds daily aggregated metrics for a single VM.
type DailyVMDigest struct {
	ID          int64     `db:"id"`
	OrgID       string    `db:"org_id"`
	ClusterUUID uuid.UUID `db:"cluster_uuid"`
	VMName      string    `db:"vm_name"`
	Namespace   string    `db:"namespace"`
	NodeName    string    `db:"node_name"`
	GuestOS     string    `db:"guest_os"`
	BucketDate  time.Time `db:"bucket_date"`

	// CPU (millicores)
	CPUUsageP50MC int64 `db:"cpu_usage_p50_mc"`
	CPUUsageP95MC int64 `db:"cpu_usage_p95_mc"`
	CPUUsageP99MC int64 `db:"cpu_usage_p99_mc"`
	CPUUsageMaxMC int64 `db:"cpu_usage_max_mc"`
	CPURequestMC  int64 `db:"cpu_request_mc"`
	CPULimitMC    int64 `db:"cpu_limit_mc"`

	// Memory (KiB)
	MemUsageP50KiB int64 `db:"mem_usage_p50_kib"`
	MemUsageP95KiB int64 `db:"mem_usage_p95_kib"`
	MemUsageP99KiB int64 `db:"mem_usage_p99_kib"`
	MemUsageMaxKiB int64 `db:"mem_usage_max_kib"`
	MemRequestKiB  int64 `db:"mem_request_kib"`

	// Guest agent memory (nullable)
	MemAvailableP50KiB *int64 `db:"mem_available_p50_kib"`
	MemAvailableP95KiB *int64 `db:"mem_available_p95_kib"`

	// Disk
	DiskAllocatedMaxBytes int64 `db:"disk_allocated_max_bytes"`

	// Filesystem (guest agent, nullable)
	FilesystemUsedMaxBytes  *int64 `db:"filesystem_used_max_bytes"`
	FilesystemCapacityBytes *int64 `db:"filesystem_capacity_bytes"`

	// I/O
	DiskReadIOPSP95  *int64 `db:"disk_read_iops_p95"`
	DiskWriteIOPSP95 *int64 `db:"disk_write_iops_p95"`
	DiskReadBPS95    *int64 `db:"disk_read_bps_p95"`
	DiskWriteBPS95   *int64 `db:"disk_write_bps_p95"`

	SampleCount        int32 `db:"sample_count"`
	AgentSampleCount   int32 `db:"agent_sample_count"`
	RestartCountSum    int32 `db:"restart_count_sum"`

	// GPU digest (aggregated from samples that have GPU data)
	GPUCount        int32   `db:"gpu_count"`
	GPUModel        string  `db:"gpu_model"`
	GPUUtilAvgBP    int32   `db:"gpu_util_avg_bp"`
	GPUUtilMaxBP    int32   `db:"gpu_util_max_bp"`
	GPUFBUsedAvgMiB float64 `db:"gpu_fb_used_avg_mib"`
	GPUFBUsedMaxMiB float64 `db:"gpu_fb_used_max_mib"`
	GPUSMActiveAvgBP int32  `db:"gpu_sm_active_avg_bp"`
	GPUTensorAvgBP  int32   `db:"gpu_tensor_avg_bp"`
	GPUDRAMAvgBP    int32   `db:"gpu_dram_avg_bp"`
	GPUMIGProfile   string  `db:"gpu_mig_profile"`
	GPUMaxSlices    int32   `db:"gpu_max_slices"`
	HasGPU          bool    `db:"has_gpu"`
	GPUDevices      []byte  `db:"gpu_devices"` // JSONB array of GPUDeviceDigest
}
