package quota

import "github.com/redhatinsights/ros-ocp-backend/librobne/types"

// RecommendClusterQuotas produces per-CRQ recommendations from deposited snapshots.
// nsAggs is keyed by ClusterQuotaName. Query/persist stay in the product.
// ApplyClusterQuotaSavings is a separate call after this returns.
func RecommendClusterQuotas(
	snapshots []ClusterQuotaSnapshot,
	nsAggs map[string]NamespaceQuotaClusterAggregate,
	orgID, clusterUUID string,
	cfg QuotaRecConfig,
) []ClusterQuotaRec {
	var recs []ClusterQuotaRec
	for _, snap := range snapshots {
		if !snap.hasHardLimits() {
			continue
		}
		recs = append(recs, ComputeClusterQuotaRecommendation(orgID, clusterUUID, snap, nsAggs[snap.ClusterQuotaName], cfg))
	}
	return recs
}

// ComputeClusterQuotaRecommendation exposes cluster-quota recommendation math for API reprojection.
func ComputeClusterQuotaRecommendation(
	orgID, clusterUUID string,
	snap ClusterQuotaSnapshot,
	nsAgg NamespaceQuotaClusterAggregate,
	cfg QuotaRecConfig,
) ClusterQuotaRec {
	rec := ClusterQuotaRec{
		OrgID:            orgID,
		ClusterUUID:      clusterUUID,
		ClusterQuotaName: snap.ClusterQuotaName,
		Namespaces:       snap.Namespaces,
		Snapshot:         snap,
	}

	baseRecommended := QuotaResourceBundle{
		CPURequestMillicores: maxInt64(snap.CPURequestUsedMC, nsAgg.CPURequestRecommendedMC),
		CPULimitMillicores:   maxInt64(snap.CPULimitUsedMC, nsAgg.CPULimitRecommendedMC),
		MemoryRequestBytes:   maxInt64(snap.MemoryRequestUsedBytes, nsAgg.MemoryRequestRecommendedBytes),
		MemoryLimitBytes:     maxInt64(snap.MemoryLimitUsedBytes, nsAgg.MemoryLimitRecommendedBytes),
	}
	rec.Recommended = QuotaResourceBundle{
		CPURequestMillicores: applyHeadroom(baseRecommended.CPURequestMillicores, cfg.HeadroomBasisPoints),
		CPULimitMillicores:   applyHeadroom(baseRecommended.CPULimitMillicores, cfg.HeadroomBasisPoints),
		MemoryRequestBytes:   applyHeadroom(baseRecommended.MemoryRequestBytes, cfg.HeadroomBasisPoints),
		MemoryLimitBytes:     applyHeadroom(baseRecommended.MemoryLimitBytes, cfg.HeadroomBasisPoints),
	}
	rec.StorageRecommendedBytes = applyHeadroom(snap.StorageRequestUsedBytes, cfg.HeadroomBasisPoints)
	rec.PodsRecommended = applyHeadroom(snap.PodsUsed, cfg.HeadroomBasisPoints)

	util := clusterQuotaUtilizationBP(snap, baseRecommended, rec.StorageRecommendedBytes, rec.PodsRecommended)
	rec.UtilizationCPURequestPercent = bpToPercentInt(util.CPURequestBP)
	rec.UtilizationMemoryRequestPercent = bpToPercentInt(util.MemoryRequestBP)
	rec.UtilizationStorageRequestPercent = bpToPercentInt(util.StorageRequestBP)
	rec.UtilizationPodsPercent = bpToPercentInt(util.PodsBP)
	rec.RiskLevel = classifyClusterQuotaRisk(util, cfg)
	rec.RecommendationType, rec.CapacityFreed = classifyClusterQuotaRecommendation(snap, rec.Recommended, rec.StorageRecommendedBytes, rec.PodsRecommended, util, cfg)
	rec.NotificationCodes = ClusterQuotaNotificationCodes(rec)

	rec.Expl = types.ClusterQuotaExplanationFactors{
		HeadroomBP:           int32(cfg.HeadroomBasisPoints),
		NSQuotaCPUSumMC:      nsAgg.CPURequestRecommendedMC,
		NSQuotaMemSumBytes:   nsAgg.MemoryRequestRecommendedBytes,
		BaseCPUMC:            baseRecommended.CPURequestMillicores,
		MaxUtilizationBP:     int32(maxClusterQuotaUtilizationBP(util)),
		RecommendationReason: rec.RecommendationType,
	}

	return rec
}

func BPToPercentInt(bp *int) *int {
	if bp == nil {
		return nil
	}
	pct := *bp / 100
	return &pct
}

func bpToPercentInt(bp *int) *int {
	return BPToPercentInt(bp)
}

type clusterQuotaUtilization struct {
	CPURequestBP     *int
	MemoryRequestBP  *int
	StorageRequestBP *int
	PodsBP           *int
	ObjectCountBP    *int
}

func clusterQuotaUtilizationBP(
	snap ClusterQuotaSnapshot,
	base QuotaResourceBundle,
	storageRecommended, podsRecommended int64,
) clusterQuotaUtilization {
	return clusterQuotaUtilization{
		CPURequestBP: UtilizationBP(maxInt64(snap.CPURequestUsedMC, base.CPURequestMillicores), snap.CPURequestHardMC),
		MemoryRequestBP: UtilizationBP(
			maxInt64(snap.MemoryRequestUsedBytes, base.MemoryRequestBytes), snap.MemoryRequestHardBytes),
		StorageRequestBP: UtilizationBP(maxInt64(snap.StorageRequestUsedBytes, storageRecommended), snap.StorageRequestHardBytes),
		PodsBP:           UtilizationBP(maxInt64(snap.PodsUsed, podsRecommended), snap.PodsHard),
		ObjectCountBP:    UtilizationBP(snap.ObjectCountUsed, snap.ObjectCountHard),
	}
}

func maxClusterQuotaUtilizationBP(util clusterQuotaUtilization) int {
	maxBP := 0
	for _, bp := range []*int{
		util.CPURequestBP, util.MemoryRequestBP, util.StorageRequestBP, util.PodsBP, util.ObjectCountBP,
	} {
		if bp != nil && *bp > maxBP {
			maxBP = *bp
		}
	}
	return maxBP
}

func classifyClusterQuotaRisk(util clusterQuotaUtilization, cfg QuotaRecConfig) string {
	maxBP := maxClusterQuotaUtilizationBP(util)
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

func classifyClusterQuotaRecommendation(
	snap ClusterQuotaSnapshot,
	recommended QuotaResourceBundle,
	storageRecommended, podsRecommended int64,
	util clusterQuotaUtilization,
	cfg QuotaRecConfig,
) (string, QuotaCapacityFreed) {
	freed := QuotaCapacityFreed{}
	needsRaise := maxClusterQuotaUtilizationBP(util) >= cfg.HighRiskThresholdBP

	cpuTighten := snap.CPURequestHardMC > 0 && recommended.CPURequestMillicores > 0 &&
		recommended.CPURequestMillicores < snap.CPURequestHardMC
	memTighten := snap.MemoryRequestHardBytes > 0 && recommended.MemoryRequestBytes > 0 &&
		recommended.MemoryRequestBytes < snap.MemoryRequestHardBytes
	storageTighten := snap.StorageRequestHardBytes > 0 && storageRecommended > 0 &&
		storageRecommended < snap.StorageRequestHardBytes
	podsTighten := snap.PodsHard > 0 && podsRecommended > 0 && podsRecommended < snap.PodsHard

	if cpuTighten {
		freed.CPUMillicores = snap.CPURequestHardMC - recommended.CPURequestMillicores
	}
	if memTighten {
		freed.MemoryBytes = snap.MemoryRequestHardBytes - recommended.MemoryRequestBytes
	}
	if storageTighten {
		freed.StorageBytes = snap.StorageRequestHardBytes - storageRecommended
	}
	if podsTighten {
		freed.PodsFreed = snap.PodsHard - podsRecommended
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
