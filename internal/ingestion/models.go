package ingestion

import "time"

// MetricRow represents a single parsed row from an OCP metrics CSV file,
// with all numeric values already converted to integer types (millicores, KiB).
type MetricRow struct {
	IntervalStart time.Time
	IntervalEnd   time.Time
	Namespace     string
	WorkloadName  string
	WorkloadType  string
	ContainerName string
	Pod           string
	Node          string

	CPURequestMC     int64
	CPULimitMC       int64
	CPUUsageMC       int64
	CPUThrottleMC    int64
	MemRequestKiB    int64
	MemLimitKiB      int64
	MemUsageKiB      int64
	MemRSSKiB        int64
	OOMCount         int64
	WorkloadPodCount int64

	// Replica fields (optional; from kube-state-metrics via operator).
	// Zero when the column is absent from the CSV.
	DesiredReplicas   int64
	AvailableReplicas int64

	// Node capacity fields (optional; from cost management pod CSV).
	// Zero when the column is absent from the CSV.
	NodeCapacityCPUMC  int64
	NodeCapacityMemKiB int64

	// InstanceType is the cloud instance type label for the node (optional).
	// Empty when the column is absent from the CSV or the node is bare-metal.
	InstanceType string

	AcceleratorModelName   string
	AcceleratorProfileName string
	AcceleratorFBUsageMin  float64
	AcceleratorFBUsageMax  float64
	AcceleratorFBUsageAvg  float64
	TensorPipeActiveMin    float64
	TensorPipeActiveMax    float64
	TensorPipeActiveAvg    float64
	DRAMActiveMin          float64
	DRAMActiveMax          float64
	DRAMActiveAvg          float64
	SMActiveMin            float64
	SMActiveMax            float64
	SMActiveAvg            float64
}

// HasGPU returns true if this row has GPU metric data.
func (m *MetricRow) HasGPU() bool {
	return m.AcceleratorModelName != ""
}

// DigestKey uniquely identifies a container-day and schedule stream for aggregation.
type DigestKey struct {
	OrgID         string
	ClusterUUID   string
	Namespace     string
	Workload      string
	WorkloadType  string
	ContainerName string
	BucketDate    time.Time
	ScheduleType  ScheduleType
}

// Digest holds pre-computed percentile values for a single container-day.
type Digest struct {
	P50   int64
	P60   int64
	P95   int64
	P98   int64
	P99   int64
	Max   int64
	Mean  int64
	Sum   int64
	Count int64
}
