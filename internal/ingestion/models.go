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
}

// DigestKey uniquely identifies a container-day combination for aggregation.
type DigestKey struct {
	OrgID         string
	ClusterUUID   string
	Namespace     string
	Workload      string
	WorkloadType  string
	ContainerName string
	BucketDate    time.Time
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
