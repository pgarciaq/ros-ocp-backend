package ingestion

import (
	"slices"
	"time"

	libcsv "github.com/redhatinsights/ros-ocp-backend/librobne/csv"
	libdigest "github.com/redhatinsights/ros-ocp-backend/librobne/digest"
)

// MetricRow is a parsed ROS container CSV row (librobne/csv.Row).
// GPU identity is GPUModel / GPUProfile; framebuffer is FBUsage*MiB;
// tensor pipe is TensorPipeMin/Max/Avg. Extra ClusterID / Arch fields
// exist on the shared type; ingest may ignore them.
type MetricRow = libcsv.Row

// metricSample holds per-sample measurements retained between digest group flushes.
// Container/workload metadata lives in DigestKey; convert from MetricRow once at append.
type metricSample struct {
	IntervalStart     time.Time
	Pod               string
	CPURequestMC      int64
	CPUUsageMC        int64
	CPUThrottleMC     int64
	MemRequestKiB     int64
	MemUsageKiB       int64
	MemRSSKiB         int64
	OOMCount          int64
	WorkloadPodCount  int64
	DesiredReplicas   int64
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

// Digest is the canonical in-memory percentile aggregate (librobne/digest).
type Digest = libdigest.Digest
