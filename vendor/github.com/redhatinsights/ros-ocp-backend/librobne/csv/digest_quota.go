package csv

import (
	"cmp"
	"slices"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/quota"
)

type quotaDigestKey struct {
	Namespace string
	QuotaName string
	Date      time.Time
}

type quotaDigestAgg struct {
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
}

func (a *quotaDigestAgg) snapshot(key quotaDigestKey) quota.NamespaceQuotaSnapshot {
	return quota.NamespaceQuotaSnapshot{
		Namespace:               key.Namespace,
		QuotaName:               key.QuotaName,
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
		LastObservedAt:          key.Date,
	}
}

func aggregateNamespaceQuotaDays(rows []NamespaceRow) map[quotaDigestKey]*quotaDigestAgg {
	daily := make(map[quotaDigestKey]*quotaDigestAgg)
	for _, row := range rows {
		if row.QuotaName == "" {
			continue
		}
		end := row.IntervalEnd
		if end.IsZero() {
			end = row.IntervalStart
		}
		date := time.Date(end.Year(), end.Month(), end.Day(), 0, 0, 0, 0, time.UTC)
		key := quotaDigestKey{Namespace: row.Namespace, QuotaName: row.QuotaName, Date: date}
		agg, ok := daily[key]
		if !ok {
			agg = &quotaDigestAgg{}
			daily[key] = agg
		}
		agg.cpuRequestHard = max(agg.cpuRequestHard, row.CPURequestMC)
		agg.cpuLimitHard = max(agg.cpuLimitHard, row.CPULimitMC)
		agg.memoryRequestHard = max(agg.memoryRequestHard, row.MemRequestKiB*1024)
		agg.memoryLimitHard = max(agg.memoryLimitHard, row.MemLimitKiB*1024)
		agg.cpuRequestUsed = max(agg.cpuRequestUsed, row.CPURequestUsedMC)
		agg.cpuLimitUsed = max(agg.cpuLimitUsed, row.CPULimitUsedMC)
		agg.memoryRequestUsed = max(agg.memoryRequestUsed, row.MemoryRequestUsedBytes)
		agg.memoryLimitUsed = max(agg.memoryLimitUsed, row.MemoryLimitUsedBytes)
		agg.storageRequestHard = max(agg.storageRequestHard, row.StorageRequestHardBytes)
		agg.storageRequestUsed = max(agg.storageRequestUsed, row.StorageRequestUsedBytes)
		agg.podsHard = max(agg.podsHard, row.PodsHard)
		agg.podsUsed = max(agg.podsUsed, row.PodsUsed)
		agg.objectCountHard = max(agg.objectCountHard, row.ObjectCountHard)
		agg.objectCountUsed = max(agg.objectCountUsed, row.ObjectCountUsed)
	}
	return daily
}

// DailyNamespaceQuotaDigests aggregates named ResourceQuota rows by day
// (max of each hard/used field, same as ingest). One row per
// namespace×quota_name×day. LastObservedAt is that day's date (interval end).
// Rows with empty quota_name are skipped.
func DailyNamespaceQuotaDigests(rows []NamespaceRow) []quota.NamespaceQuotaSnapshot {
	daily := aggregateNamespaceQuotaDays(rows)
	out := make([]quota.NamespaceQuotaSnapshot, 0, len(daily))
	for key, agg := range daily {
		out = append(out, agg.snapshot(key))
	}
	slices.SortFunc(out, func(a, b quota.NamespaceQuotaSnapshot) int {
		if n := cmp.Compare(a.Namespace, b.Namespace); n != 0 {
			return n
		}
		if n := cmp.Compare(a.QuotaName, b.QuotaName); n != 0 {
			return n
		}
		return a.LastObservedAt.Compare(b.LastObservedAt)
	})
	return out
}

// LatestNamespaceQuotaSnapshots keeps the latest day per namespace×quota_name
// from DailyNamespaceQuotaDigests.
func LatestNamespaceQuotaSnapshots(rows []NamespaceRow) []quota.NamespaceQuotaSnapshot {
	return LatestNamespaceQuotaFromDaily(DailyNamespaceQuotaDigests(rows))
}

// LatestNamespaceQuotaFromDaily keeps the latest day per namespace×quota_name.
func LatestNamespaceQuotaFromDaily(daily []quota.NamespaceQuotaSnapshot) []quota.NamespaceQuotaSnapshot {
	latest := make(map[quotaIdentity]quota.NamespaceQuotaSnapshot)
	for _, snap := range daily {
		id := quotaIdentity{Namespace: snap.Namespace, QuotaName: snap.QuotaName}
		prev, ok := latest[id]
		if ok && !snap.LastObservedAt.After(prev.LastObservedAt) {
			continue
		}
		latest[id] = snap
	}
	out := make([]quota.NamespaceQuotaSnapshot, 0, len(latest))
	for _, snap := range latest {
		out = append(out, snap)
	}
	slices.SortFunc(out, func(a, b quota.NamespaceQuotaSnapshot) int {
		if n := cmp.Compare(a.Namespace, b.Namespace); n != 0 {
			return n
		}
		return cmp.Compare(a.QuotaName, b.QuotaName)
	})
	return out
}

type quotaIdentity struct {
	Namespace string
	QuotaName string
}
