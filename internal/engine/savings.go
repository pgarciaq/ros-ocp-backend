package engine

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
)

// replicaCountForSavings returns the best available replica count for savings
// multiplication. Prefers authoritative desired_replicas from kube-state-metrics
// when available, falling back to pod_count_avg (derived from workload_pod_count
// or distinct pod names).
func replicaCountForSavings(rec *ContainerRec) float64 {
	return float64(replicaCountInt(rec))
}

// ApplySavingsEstimates computes EstimatedSavingsCents for each recommendation
// using cost data from Koku. If costData is nil (Koku unavailable or not
// configured), all savings remain 0 and NotifNoCostData is appended.
//
// Stored USD values reflect rates from the last successful cost fetch during
// report processing. When Koku cost models change, POST /internal/recalculate-savings
// (or TriggerSavingsRecalculationAsync) refreshes savings without re-ingestion.
func ApplySavingsEstimates(recs []ContainerRec, costData *costdata.ClusterCostData) {
	if costData == nil {
		for i := range recs {
			recs[i].NotificationCodes = appendUnique(recs[i].NotificationCodes, NotifNoCostData)
		}
		return
	}

	distType := costData.DistributionType
	if distType == "" {
		distType = "cpu"
	}

	for i := range recs {
		ns, ok := costData.Namespaces[recs[i].Namespace]
		if !ok {
			recs[i].NotificationCodes = appendUnique(recs[i].NotificationCodes, NotifNoCostData)
			continue
		}

		// Idle or abandoned workloads: 100% of current resource cost is recoverable.
		// Only explicit idle/zombie state counts — zero-value IdleState must not trigger this path.
		if recs[i].IsIdle || recs[i].IsAbandoned ||
			recs[i].IdleState == IdleStateIdle || recs[i].IdleState == IdleStateZombie {
			idleMicroCents := computeIdleSavingsMicroCents(&recs[i], &ns, distType)
			recs[i].EstimatedSavingsCents = MicroCentsToCents(idleMicroCents)
			if recs[i].IdleState == IdleStateIdle || recs[i].IdleState == IdleStateZombie {
				recs[i].EstimatedWasteCents = recs[i].EstimatedSavingsCents
			}
			continue
		}

		savingsMicroCents := computeSavingsMicroCents(&recs[i], &ns, distType)
		recs[i].EstimatedSavingsCents = MicroCentsToCents(savingsMicroCents)
	}
}

func computeSavingsMicroCents(rec *ContainerRec, ns *costdata.NamespaceCosts, distType string) int64 {
	cpuDeltaMC := rec.CurrentCPURequestMC - rec.RecCPURequestMC
	memDeltaKiB := rec.CurrentMemRequestKiB - rec.RecMemRequestKiB
	replicas := replicaCountForSavingsApply(rec)

	modelCPURate := EffectiveRateMicroCentsPerMCHour(ns.CostModelCPUCost, ns.CPURequestHours)
	modelMemRate := EffectiveRateMicroCentsPerGiBHour(ns.CostModelMemCost, ns.MemRequestHours)

	modelSavings := CPUSavingsMicroCents(cpuDeltaMC, modelCPURate, HoursPerMonthInt, replicas) +
		MemSavingsMicroCentsFromKiB(memDeltaKiB, modelMemRate, HoursPerMonthInt, replicas)

	totalInfraUSD := clampNonNegativeUSD(ns.InfraCost + ns.DistributedCost)
	var infraSavings int64
	if distType == "memory" {
		infraRate := EffectiveRateMicroCentsPerGiBHour(totalInfraUSD, ns.MemRequestHours)
		infraSavings = MemSavingsMicroCentsFromKiB(memDeltaKiB, infraRate, HoursPerMonthInt, replicas)
	} else {
		infraRate := EffectiveRateMicroCentsPerMCHour(totalInfraUSD, ns.CPURequestHours)
		infraSavings = CPUSavingsMicroCents(cpuDeltaMC, infraRate, HoursPerMonthInt, replicas)
	}

	return modelSavings + infraSavings
}

// computeIdleSavingsMicroCents estimates the full cost of an idle/abandoned workload's
// current resource allocation, since 100% is recoverable by scaling down.
func computeIdleSavingsMicroCents(rec *ContainerRec, ns *costdata.NamespaceCosts, distType string) int64 {
	replicas := replicaCountForSavingsApply(rec)

	modelCPURate := EffectiveRateMicroCentsPerMCHour(ns.CostModelCPUCost, ns.CPURequestHours)
	modelMemRate := EffectiveRateMicroCentsPerGiBHour(ns.CostModelMemCost, ns.MemRequestHours)

	modelCost := CPUSavingsMicroCents(rec.CurrentCPURequestMC, modelCPURate, HoursPerMonthInt, replicas) +
		MemSavingsMicroCentsFromKiB(rec.CurrentMemRequestKiB, modelMemRate, HoursPerMonthInt, replicas)

	totalInfraUSD := clampNonNegativeUSD(ns.InfraCost + ns.DistributedCost)
	var infraCost int64
	if distType == "memory" {
		infraRate := EffectiveRateMicroCentsPerGiBHour(totalInfraUSD, ns.MemRequestHours)
		infraCost = MemSavingsMicroCentsFromKiB(rec.CurrentMemRequestKiB, infraRate, HoursPerMonthInt, replicas)
	} else {
		infraRate := EffectiveRateMicroCentsPerMCHour(totalInfraUSD, ns.CPURequestHours)
		infraCost = CPUSavingsMicroCents(rec.CurrentCPURequestMC, infraRate, HoursPerMonthInt, replicas)
	}

	return modelCost + infraCost
}

func safeDiv(numerator, denominator float64) float64 {
	if denominator == 0 {
		return 0
	}
	return numerator / denominator
}
