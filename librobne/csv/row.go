package csv

import "time"

// Row is one parsed ROS container CSV line (integer millicores / KiB).
type Row struct {
	IntervalStart  time.Time
	IntervalEnd    time.Time
	Namespace      string
	WorkloadName   string
	WorkloadType   string
	ContainerName  string
	Pod            string
	Node           string
	ClusterID      string
	InstanceType   string
	Arch           string
	GPUModel       string
	GPUProfile     string
	GPUUUID        string
	MachineSetName string

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

	NodeCapacityCPUMC       int64
	NodeCapacityMemKiB      int64
	NodeAllocatableCPUMC    int64
	NodeAllocatableMemKiB   int64
	NodeAllocatableGPUCount int64
	NodePodCapacity         int64

	FBUsageMinMiB float64
	FBUsageMaxMiB float64
	FBUsageAvgMiB float64
	TensorPipeMin float64
	TensorPipeMax float64
	TensorPipeAvg float64
	DRAMActiveMin float64
	DRAMActiveMax float64
	DRAMActiveAvg float64
	SMActiveMin   float64
	SMActiveMax   float64
	SMActiveAvg   float64
}

// HasGPU reports whether this row carries GPU identity (DCGM optional).
func (r Row) HasGPU() bool {
	return r.GPUModel != ""
}

// RowMeta is the latest-row lookup used to resolve a per-container rate card.
type RowMeta struct {
	InstanceType string
	Arch         string
	GPUModel     string
	ClusterID    string
}

// NamespaceRow is one parsed ROS namespace CSV line (integer millicores / KiB).
type NamespaceRow struct {
	IntervalStart time.Time
	IntervalEnd   time.Time
	Namespace     string
	ClusterID     string

	CPURequestMC   int64
	CPULimitMC     int64
	CPUUsageMC     int64
	CPUUsageMaxMC  int64
	MemRequestKiB  int64
	MemLimitKiB    int64
	MemUsageKiB    int64
	MemUsageMaxKiB int64
	MemRSSKiB      int64

	QuotaName               string
	CPURequestUsedMC        int64
	CPULimitUsedMC          int64
	MemoryRequestUsedBytes  int64
	MemoryLimitUsedBytes    int64
	StorageRequestHardBytes int64
	StorageRequestUsedBytes int64
	PodsHard                int64
	PodsUsed                int64
	ObjectCountHard         int64
	ObjectCountUsed         int64
}
