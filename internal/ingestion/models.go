package ingestion

import (
	"slices"
	"time"
)

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

	// Node capacity fields (optional; from operator ROS container CSV).
	// Zero when the column is absent from the CSV.
	NodeCapacityCPUMC  int64
	NodeCapacityMemKiB int64

	// Node allocatable fields (optional; from operator ROS container CSV).
	// Zero when the column is absent (older operators derive allocatable at flush time).
	NodeAllocatableCPUMC  int64
	NodeAllocatableMemKiB int64

	// NodeAllocatableGPUCount is the number of allocatable GPUs on the node (optional).
	// Zero when the column is absent from the CSV or the node has no GPUs.
	NodeAllocatableGPUCount int64

	// InstanceType is the cloud instance type label for the node (optional).
	// Empty when the column is absent from the CSV or the node is bare-metal.
	InstanceType string

	// NodePodCapacity is max schedulable pods on the node (optional; from node_capacity_pods or pod_capacity CSV columns).
	NodePodCapacity int64

	// MachineSetName is the OpenShift MachineSet for the node (optional).
	MachineSetName string

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

	// GPUUUID is the unique device identifier for a specific GPU (optional).
	// Empty when the column is absent or this row has no GPU data.
	GPUUUID string
}

// HasGPU returns true if this row has GPU metric data.
func (m *MetricRow) HasGPU() bool {
	return m.AcceleratorModelName != ""
}

// metricSample holds per-sample measurements retained between digest group flushes.
// Container/workload metadata lives in DigestKey; convert from MetricRow once at append.
type metricSample struct {
	IntervalStart    time.Time
	Pod              string
	CPURequestMC     int64
	CPUUsageMC       int64
	CPUThrottleMC    int64
	MemRequestKiB    int64
	MemUsageKiB      int64
	MemRSSKiB        int64
	OOMCount         int64
	WorkloadPodCount int64
	DesiredReplicas  int64
	AvailableReplicas int64
}

func metricSampleFromRow(row MetricRow) metricSample {
	return metricSample{
		IntervalStart:     row.IntervalStart,
		Pod:               row.Pod,
		CPURequestMC:      row.CPURequestMC,
		CPUUsageMC:        row.CPUUsageMC,
		CPUThrottleMC:     row.CPUThrottleMC,
		MemRequestKiB:     row.MemRequestKiB,
		MemUsageKiB:       row.MemUsageKiB,
		MemRSSKiB:         row.MemRSSKiB,
		OOMCount:          row.OOMCount,
		WorkloadPodCount:  row.WorkloadPodCount,
		DesiredReplicas:   row.DesiredReplicas,
		AvailableReplicas: row.AvailableReplicas,
	}
}

func metricSamplesFromRows(rows []MetricRow) []metricSample {
	samples := make([]metricSample, len(rows))
	for i, row := range rows {
		samples[i] = metricSampleFromRow(row)
	}
	return samples
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

// sortDigestKeys sorts keys in a deterministic order matching the unique index
// on daily_container_digests (org_id, cluster_uuid, namespace, workload,
// workload_type, container_name, bucket_date, schedule_type).
// This prevents PostgreSQL deadlocks when concurrent transactions upsert
// overlapping rows — both acquire locks in the same order.
func sortDigestKeys(keys []DigestKey) {
	slices.SortFunc(keys, func(a, b DigestKey) int {
		if c := cmpStr(a.OrgID, b.OrgID); c != 0 {
			return c
		}
		if c := cmpStr(a.ClusterUUID, b.ClusterUUID); c != 0 {
			return c
		}
		if c := cmpStr(a.Namespace, b.Namespace); c != 0 {
			return c
		}
		if c := cmpStr(a.Workload, b.Workload); c != 0 {
			return c
		}
		if c := cmpStr(a.WorkloadType, b.WorkloadType); c != 0 {
			return c
		}
		if c := cmpStr(a.ContainerName, b.ContainerName); c != 0 {
			return c
		}
		if a.BucketDate.Before(b.BucketDate) {
			return -1
		}
		if a.BucketDate.After(b.BucketDate) {
			return 1
		}
		return cmpStr(string(a.ScheduleType), string(b.ScheduleType))
	})
}

func cmpStr(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
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
