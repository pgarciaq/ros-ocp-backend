package quota

import "github.com/redhatinsights/ros-ocp-backend/librobne/types"

// RecommendQuotas produces per-namespace quota recommendations from deposited snapshots.
// Query/persist stay in the product. ApplyQuotaSavings is a separate call after this returns.
func RecommendQuotas(
	snapshots []NamespaceQuotaSnapshot,
	aggregates map[string]ContainerQuotaAggregate,
	orgID, clusterUUID string,
	cfg QuotaRecConfig,
) []QuotaRec {
	var recs []QuotaRec
	for _, snap := range snapshots {
		if !snap.hasHardLimits() {
			continue
		}
		agg := aggregates[snap.Namespace]
		recs = append(recs, ComputeQuotaRecommendation(orgID, clusterUUID, snap, agg, cfg))
	}
	return recs
}

// ComputeQuotaRecommendation exposes quota recommendation math for API reprojection.
func ComputeQuotaRecommendation(orgID, clusterUUID string, snap NamespaceQuotaSnapshot, agg ContainerQuotaAggregate, cfg QuotaRecConfig) QuotaRec {
	rec := QuotaRec{
		OrgID:       orgID,
		ClusterUUID: clusterUUID,
		Namespace:   snap.Namespace,
		QuotaName:   snap.QuotaName,
		Snapshot:    snap,
		HeadroomBP:  cfg.HeadroomBasisPoints,
		Currency:    cfg.Currency,
	}

	storageRecommended := applyHeadroom(snap.StorageRequestUsedBytes, cfg.HeadroomBasisPoints)
	podsRecommended := applyHeadroom(snap.PodsUsed, cfg.HeadroomBasisPoints)

	rec.Recommended = QuotaResourceBundle{
		CPURequestMillicores: applyHeadroom(agg.CPURequestSumMC, cfg.HeadroomBasisPoints),
		CPULimitMillicores:   applyHeadroom(agg.CPULimitSumMC, cfg.HeadroomBasisPoints),
		MemoryRequestBytes:   applyHeadroom(agg.MemoryRequestSumBytes, cfg.HeadroomBasisPoints),
		MemoryLimitBytes:     applyHeadroom(agg.MemoryLimitSumBytes, cfg.HeadroomBasisPoints),
		StorageRequestBytes:  storageRecommended,
		Pods:                 podsRecommended,
	}

	// Signal C: utilization vs hard uses the greater of quota "used" and container rec sums.
	rec.Utilization = QuotaUtilizationBP{
		CPURequestBP: UtilizationBP(maxInt64(snap.CPURequestUsedMC, agg.CPURequestSumMC), snap.CPURequestHardMC),
		CPULimitBP:   UtilizationBP(maxInt64(snap.CPULimitUsedMC, agg.CPULimitSumMC), snap.CPULimitHardMC),
		MemoryRequestBP: UtilizationBP(
			maxInt64(snap.MemoryRequestUsedBytes, agg.MemoryRequestSumBytes), snap.MemoryRequestHardBytes),
		MemoryLimitBP: UtilizationBP(
			maxInt64(snap.MemoryLimitUsedBytes, agg.MemoryLimitSumBytes), snap.MemoryLimitHardBytes),
		StorageRequestBP: UtilizationBP(maxInt64(snap.StorageRequestUsedBytes, storageRecommended), snap.StorageRequestHardBytes),
		PodsBP:           UtilizationBP(maxInt64(snap.PodsUsed, podsRecommended), snap.PodsHard),
		ObjectCountBP:    UtilizationBP(snap.ObjectCountUsed, snap.ObjectCountHard),
	}

	rec.RiskLevel = classifyQuotaRisk(rec.Utilization, cfg)
	rec.RecommendationType, rec.CapacityFreed = classifyQuotaRecommendation(snap, rec.Recommended, rec.Utilization, cfg)
	rec.NotificationCodes = QuotaNotificationCodes(snap, rec)

	signalCPU := maxInt64(snap.CPURequestUsedMC, agg.CPURequestSumMC)
	rec.Expl = types.QuotaExplanationFactors{
		HeadroomBP:           int32(cfg.HeadroomBasisPoints),
		ContainerCPUSumMC:    agg.CPURequestSumMC,
		ContainerMemSumBytes: agg.MemoryRequestSumBytes,
		SignalCCPUUsedMC:     signalCPU,
		MaxUtilizationBP:     int32(maxUtilizationBP(rec.Utilization)),
		RiskLevel:            rec.RiskLevel,
		RecommendationReason: rec.RecommendationType,
	}

	return rec
}

// applyHeadroom scales value by headroomBP basis points (e.g., 11000 = 110%).
// Integer division truncates fractional results, so very small values (e.g., 1 mc
// at 10% headroom) receive zero additional headroom. This is acceptable because
// production quotas are in the hundreds-to-thousands range.
func applyHeadroom(value int64, headroomBP int) int64 {
	if value <= 0 {
		return 0
	}
	return (value * int64(headroomBP)) / 10000
}

// UtilizationBP returns used/hard as basis points, or nil when hard is unset.
func UtilizationBP(used, hard int64) *int {
	if hard <= 0 {
		return nil
	}
	bp := int((used * 10000) / hard)
	return &bp
}

func utilizationBP(used, hard int64) *int {
	return UtilizationBP(used, hard)
}

func maxUtilizationBP(util QuotaUtilizationBP) int {
	max := 0
	for _, v := range []*int{
		util.CPURequestBP, util.CPULimitBP, util.MemoryRequestBP, util.MemoryLimitBP,
		util.StorageRequestBP, util.PodsBP, util.ObjectCountBP,
	} {
		if v != nil && *v > max {
			max = *v
		}
	}
	return max
}

func classifyQuotaRisk(util QuotaUtilizationBP, cfg QuotaRecConfig) string {
	maxBP := maxUtilizationBP(util)
	switch {
	case maxBP >= cfg.HighRiskThresholdBP:
		return QuotaRiskHigh
	case maxBP >= cfg.MediumRiskThresholdBP:
		return QuotaRiskMedium
	case maxBP > 0:
		return QuotaRiskLow
	default:
		return QuotaRiskNone
	}
}

func classifyQuotaRecommendation(snap NamespaceQuotaSnapshot, recommended QuotaResourceBundle, util QuotaUtilizationBP, cfg QuotaRecConfig) (string, QuotaCapacityFreed) {
	freed := QuotaCapacityFreed{}
	needsRaise := maxUtilizationBP(util) >= cfg.HighRiskThresholdBP

	cpuTighten := snap.CPURequestHardMC > 0 && recommended.CPURequestMillicores > 0 && recommended.CPURequestMillicores < snap.CPURequestHardMC
	memTighten := snap.MemoryRequestHardBytes > 0 && recommended.MemoryRequestBytes > 0 && recommended.MemoryRequestBytes < snap.MemoryRequestHardBytes
	storageTighten := snap.StorageRequestHardBytes > 0 && recommended.StorageRequestBytes > 0 &&
		recommended.StorageRequestBytes < snap.StorageRequestHardBytes
	podsTighten := snap.PodsHard > 0 && recommended.Pods > 0 && recommended.Pods < snap.PodsHard

	if cpuTighten {
		freed.CPUMillicores = snap.CPURequestHardMC - recommended.CPURequestMillicores
	}
	if memTighten {
		freed.MemoryBytes = snap.MemoryRequestHardBytes - recommended.MemoryRequestBytes
	}
	if storageTighten {
		freed.StorageBytes = snap.StorageRequestHardBytes - recommended.StorageRequestBytes
	}
	if podsTighten {
		freed.PodsFreed = snap.PodsHard - recommended.Pods
	}

	if needsRaise {
		return QuotaRecTypeRaise, freed
	}
	if cpuTighten || memTighten || storageTighten || podsTighten {
		return QuotaRecTypeTighten, freed
	}
	if snap.hasHardLimits() {
		return QuotaRecTypeOptimal, freed
	}
	return QuotaRecTypeNone, freed
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
