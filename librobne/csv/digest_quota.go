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

// LatestNamespaceQuotaSnapshots aggregates named ResourceQuota rows by day
// (max of each hard/used field, same as ingest) then keeps the latest day
// per namespace×quota_name. Rows with empty quota_name are skipped.
func LatestNamespaceQuotaSnapshots(rows []NamespaceRow) []quota.NamespaceQuotaSnapshot {
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

	latest := make(map[quotaIdentity]quota.NamespaceQuotaSnapshot)
	for key, agg := range daily {
		id := quotaIdentity{Namespace: key.Namespace, QuotaName: key.QuotaName}
		prev, ok := latest[id]
		if ok && !key.Date.After(prev.LastObservedAt) {
			continue
		}
		latest[id] = quota.NamespaceQuotaSnapshot{
			Namespace:               key.Namespace,
			QuotaName:               key.QuotaName,
			CPURequestHardMC:        agg.cpuRequestHard,
			CPULimitHardMC:          agg.cpuLimitHard,
			MemoryRequestHardBytes:  agg.memoryRequestHard,
			MemoryLimitHardBytes:    agg.memoryLimitHard,
			CPURequestUsedMC:        agg.cpuRequestUsed,
			CPULimitUsedMC:          agg.cpuLimitUsed,
			MemoryRequestUsedBytes:  agg.memoryRequestUsed,
			MemoryLimitUsedBytes:    agg.memoryLimitUsed,
			StorageRequestHardBytes: agg.storageRequestHard,
			StorageRequestUsedBytes: agg.storageRequestUsed,
			PodsHard:                agg.podsHard,
			PodsUsed:                agg.podsUsed,
			ObjectCountHard:         agg.objectCountHard,
			ObjectCountUsed:         agg.objectCountUsed,
			LastObservedAt:          key.Date,
		}
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
