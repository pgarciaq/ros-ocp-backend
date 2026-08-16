package csv

import "time"

// Row is one parsed ROS container CSV line (integer millicores / KiB).
type Row struct {
	IntervalStart time.Time
	IntervalEnd   time.Time
	Namespace     string
	WorkloadName  string
	WorkloadType  string
	ContainerName string
	Pod           string
	Node          string
	ClusterID     string
	InstanceType  string
	Arch          string
	GPUModel      string

	CPURequestMC      int64
	CPULimitMC        int64
	CPUUsageMC        int64
	CPUThrottleMC     int64
	MemRequestKiB     int64
	MemLimitKiB       int64
	MemUsageKiB       int64
	MemRSSKiB         int64
	OOMCount          int64
	WorkloadPodCount  int64
	DesiredReplicas   int64
	AvailableReplicas int64
}

// RowMeta is the latest-row lookup used to resolve a per-container rate card.
type RowMeta struct {
	InstanceType string
	Arch         string
	GPUModel     string
	ClusterID    string
}
