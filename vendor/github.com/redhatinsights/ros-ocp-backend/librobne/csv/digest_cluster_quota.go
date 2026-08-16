package csv

import (
	"cmp"
	"slices"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/quota"
)

type clusterQuotaDigestKey struct {
	Name string
	Date time.Time
}

type clusterQuotaDigestAgg struct {
	cpuRequestHard     int64
	cpuRequestUsed     int64
	cpuLimitHard       int64
	cpuLimitUsed       int64
	memoryRequestHard  int64
	memoryRequestUsed  int64
	memoryLimitHard    int64
	memoryLimitUsed    int64
	storageRequestHard int64
	storageRequestUsed int64
	podsHard           int64
	podsUsed           int64
	objectCountHard    int64
	objectCountUsed    int64
	namespaces         string
}

func (a clusterQuotaDigestAgg) snapshot(name string, date time.Time) quota.ClusterQuotaSnapshot {
	return quota.ClusterQuotaSnapshot{
		ClusterQuotaName:        name,
		Namespaces:              a.namespaces,
		CPURequestHardMC:        a.cpuRequestHard,
		CPULimitHardMC:          a.cpuLimitHard,
		MemoryRequestHardBytes:  a.memoryRequestHard,
		MemoryLimitHardBytes:    a.memoryLimitHard,
		CPURequestUsedMC:        a.cpuRequestUsed,
		CPULimitUsedMC:          a.cpuLimitUsed,
		MemoryRequestUsedBytes:  a.memoryRequestUsed,
		MemoryLimitUsedBytes:    a.memoryLimitUsed,
		StorageRequestHardBytes: a.storageRequestHard,
		StorageRequestUsedBytes: a.storageRequestUsed,
		PodsHard:                a.podsHard,
		PodsUsed:                a.podsUsed,
		ObjectCountHard:         a.objectCountHard,
		ObjectCountUsed:         a.objectCountUsed,
		LastObservedAt:          date,
	}
}

func aggregateClusterQuotaDays(rows []ClusterQuotaRow) map[clusterQuotaDigestKey]*clusterQuotaDigestAgg {
	daily := make(map[clusterQuotaDigestKey]*clusterQuotaDigestAgg)
	for _, row := range rows {
		if row.ClusterQuotaName == "" {
			continue
		}
		end := row.IntervalEnd
		if end.IsZero() {
			end = row.IntervalStart
		}
		date := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
		key := clusterQuotaDigestKey{Name: row.ClusterQuotaName, Date: date}
		agg, ok := daily[key]
		if !ok {
			agg = &clusterQuotaDigestAgg{}
			daily[key] = agg
		}
		agg.cpuRequestHard = max(agg.cpuRequestHard, row.CPURequestHardMC)
		agg.cpuRequestUsed = max(agg.cpuRequestUsed, row.CPURequestUsedMC)
		agg.cpuLimitHard = max(agg.cpuLimitHard, row.CPULimitHardMC)
		agg.cpuLimitUsed = max(agg.cpuLimitUsed, row.CPULimitUsedMC)
		agg.memoryRequestHard = max(agg.memoryRequestHard, row.MemoryRequestHardBytes)
		agg.memoryRequestUsed = max(agg.memoryRequestUsed, row.MemoryRequestUsedBytes)
		agg.memoryLimitHard = max(agg.memoryLimitHard, row.MemoryLimitHardBytes)
		agg.memoryLimitUsed = max(agg.memoryLimitUsed, row.MemoryLimitUsedBytes)
		agg.storageRequestHard = max(agg.storageRequestHard, row.StorageRequestHardBytes)
		agg.storageRequestUsed = max(agg.storageRequestUsed, row.StorageRequestUsedBytes)
		agg.podsHard = max(agg.podsHard, row.PodsHard)
		agg.podsUsed = max(agg.podsUsed, row.PodsUsed)
		agg.objectCountHard = max(agg.objectCountHard, row.ObjectCountHard)
		agg.objectCountUsed = max(agg.objectCountUsed, row.ObjectCountUsed)
		if row.Namespaces != "" {
			agg.namespaces = row.Namespaces
		}
	}
	return daily
}

// DailyClusterQuotaDigests aggregates CRQ rows by day (max of each hard/used
// field, last non-empty namespaces). One row per cluster_quota_name×day,
// including days with no hard limits. LastObservedAt is that day's date
// (interval end).
func DailyClusterQuotaDigests(rows []ClusterQuotaRow) []quota.ClusterQuotaSnapshot {
	daily := aggregateClusterQuotaDays(rows)
	out := make([]quota.ClusterQuotaSnapshot, 0, len(daily))
	for key, agg := range daily {
		out = append(out, agg.snapshot(key.Name, key.Date))
	}
	slices.SortFunc(out, func(a, b quota.ClusterQuotaSnapshot) int {
		if n := cmp.Compare(a.ClusterQuotaName, b.ClusterQuotaName); n != 0 {
			return n
		}
		return a.LastObservedAt.Compare(b.LastObservedAt)
	})
	return out
}

// LatestClusterQuotaSnapshots keeps the latest day that has hard limits per
// cluster_quota_name from DailyClusterQuotaDigests.
func LatestClusterQuotaSnapshots(rows []ClusterQuotaRow) []quota.ClusterQuotaSnapshot {
	return LatestClusterQuotaFromDaily(DailyClusterQuotaDigests(rows))
}

// LatestClusterQuotaFromDaily keeps the latest day that has hard limits per
// cluster_quota_name.
func LatestClusterQuotaFromDaily(daily []quota.ClusterQuotaSnapshot) []quota.ClusterQuotaSnapshot {
	latest := make(map[string]quota.ClusterQuotaSnapshot)
	for _, snap := range daily {
		if !snap.HasHardLimits() {
			continue
		}
		prev, ok := latest[snap.ClusterQuotaName]
		if ok && !snap.LastObservedAt.After(prev.LastObservedAt) {
			continue
		}
		latest[snap.ClusterQuotaName] = snap
	}
	out := make([]quota.ClusterQuotaSnapshot, 0, len(latest))
	for _, snap := range latest {
		out = append(out, snap)
	}
	slices.SortFunc(out, func(a, b quota.ClusterQuotaSnapshot) int {
		return cmp.Compare(a.ClusterQuotaName, b.ClusterQuotaName)
	})
	return out
}
