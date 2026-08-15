package quota

import "github.com/redhatinsights/ros-ocp-backend/librobne/types"

const (
	NotifQuotaNearCapacity      = types.NotifQuotaNearCapacity
	NotifQuotaOversized         = types.NotifQuotaOversized
	NotifQuotaBlocking          = types.NotifQuotaBlocking
	NotifClusterQuotaAtCapacity = types.NotifClusterQuotaAtCapacity
)

// QuotaNotificationCodes derives notification codes for a namespace quota recommendation.
func QuotaNotificationCodes(snap NamespaceQuotaSnapshot, rec QuotaRec) []int16 {
	codes := []int16{}
	if quotaResourceBlocking(snap) {
		codes = append(codes, NotifQuotaBlocking)
	}
	if rec.RiskLevel == QuotaRiskHigh {
		codes = append(codes, NotifQuotaNearCapacity)
	}
	if rec.RecommendationType == QuotaRecTypeTighten {
		codes = append(codes, NotifQuotaOversized)
	}
	return codes
}

// ClusterQuotaNotificationCodes derives notification codes for a ClusterResourceQuota recommendation.
func ClusterQuotaNotificationCodes(rec ClusterQuotaRec) []int16 {
	codes := []int16{}
	if clusterQuotaResourceBlocking(rec.Snapshot) {
		codes = append(codes, NotifQuotaBlocking)
	}
	if rec.RiskLevel == QuotaRiskHigh {
		codes = append(codes, NotifQuotaNearCapacity, NotifClusterQuotaAtCapacity)
	}
	if rec.RecommendationType == QuotaRecTypeTighten {
		codes = append(codes, NotifQuotaOversized)
	}
	return codes
}

func clusterQuotaResourceBlocking(snap ClusterQuotaSnapshot) bool {
	return quotaUsedAtHard(snap.CPURequestUsedMC, snap.CPURequestHardMC) ||
		quotaUsedAtHard(snap.CPULimitUsedMC, snap.CPULimitHardMC) ||
		quotaUsedAtHard(snap.MemoryRequestUsedBytes, snap.MemoryRequestHardBytes) ||
		quotaUsedAtHard(snap.MemoryLimitUsedBytes, snap.MemoryLimitHardBytes) ||
		quotaUsedAtHard(snap.StorageRequestUsedBytes, snap.StorageRequestHardBytes) ||
		quotaUsedAtHard(snap.PodsUsed, snap.PodsHard) ||
		quotaUsedAtHard(snap.ObjectCountUsed, snap.ObjectCountHard)
}

func quotaResourceBlocking(snap NamespaceQuotaSnapshot) bool {
	return quotaUsedAtHard(snap.CPURequestUsedMC, snap.CPURequestHardMC) ||
		quotaUsedAtHard(snap.CPULimitUsedMC, snap.CPULimitHardMC) ||
		quotaUsedAtHard(snap.MemoryRequestUsedBytes, snap.MemoryRequestHardBytes) ||
		quotaUsedAtHard(snap.MemoryLimitUsedBytes, snap.MemoryLimitHardBytes) ||
		quotaUsedAtHard(snap.StorageRequestUsedBytes, snap.StorageRequestHardBytes) ||
		quotaUsedAtHard(snap.PodsUsed, snap.PodsHard) ||
		quotaUsedAtHard(snap.ObjectCountUsed, snap.ObjectCountHard)
}

func quotaUsedAtHard(used, hard int64) bool {
	return hard > 0 && used >= hard
}
