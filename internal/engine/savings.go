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
// configured), all savings fields remain nil and NotifNoCostData is appended.
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
			total := MicroCentsToCents(idleMicroCents)
			recs[i].EstimatedSavingsCents = &total
			if recs[i].IdleState == IdleStateIdle || recs[i].IdleState == IdleStateZombie {
				recs[i].EstimatedWasteCents = &total
			}
			cpuSavings, memSavings := computeIdleSavingsBreakdownMicroCents(&recs[i], &ns, distType)
			cpuCents := MicroCentsToCents(cpuSavings)
			memCents := MicroCentsToCents(memSavings)
			recs[i].EstimatedCPUSavingsCents = &cpuCents
			recs[i].EstimatedMemSavingsCents = &memCents
			continue
		}

		cpuMicro, memMicro := computeSavingsBreakdownMicroCents(&recs[i], &ns, distType)
		total := MicroCentsToCents(cpuMicro + memMicro)
		cpuCents := MicroCentsToCents(cpuMicro)
		memCents := MicroCentsToCents(memMicro)
		recs[i].EstimatedSavingsCents = &total
		recs[i].EstimatedCPUSavingsCents = &cpuCents
		recs[i].EstimatedMemSavingsCents = &memCents
	}
}

func computeSavingsMicroCents(rec *ContainerRec, ns *costdata.NamespaceCosts, distType string) int64 {
	cpu, mem := computeSavingsBreakdownMicroCents(rec, ns, distType)
	return cpu + mem
}

func computeSavingsBreakdownMicroCents(rec *ContainerRec, ns *costdata.NamespaceCosts, distType string) (cpuMicro, memMicro int64) {
	cpuDeltaMC := rec.CurrentCPURequestMC - rec.RecCPURequestMC
	memDeltaKiB := rec.CurrentMemRequestKiB - rec.RecMemRequestKiB
	replicas := replicaCountForSavingsApply(rec)

	modelCPURate := EffectiveRateMicroCentsPerMCHour(ns.CostModelCPUCost, ns.CPURequestHours)
	modelMemRate := EffectiveRateMicroCentsPerGiBHour(ns.CostModelMemCost, ns.MemRequestHours)

	cpuMicro = CPUSavingsMicroCents(cpuDeltaMC, modelCPURate, HoursPerMonthInt, replicas)
	memMicro = MemSavingsMicroCentsFromKiB(memDeltaKiB, modelMemRate, HoursPerMonthInt, replicas)

	totalInfraUSD := clampNonNegativeUSD(ns.InfraCost + ns.DistributedCost)
	if distType == "memory" {
		infraRate := EffectiveRateMicroCentsPerGiBHour(totalInfraUSD, ns.MemRequestHours)
		memMicro += MemSavingsMicroCentsFromKiB(memDeltaKiB, infraRate, HoursPerMonthInt, replicas)
	} else {
		infraRate := EffectiveRateMicroCentsPerMCHour(totalInfraUSD, ns.CPURequestHours)
		cpuMicro += CPUSavingsMicroCents(cpuDeltaMC, infraRate, HoursPerMonthInt, replicas)
	}

	return cpuMicro, memMicro
}

// computeIdleSavingsMicroCents estimates the full cost of an idle/abandoned workload's
// current resource allocation, since 100% is recoverable by scaling down.
func computeIdleSavingsMicroCents(rec *ContainerRec, ns *costdata.NamespaceCosts, distType string) int64 {
	cpu, mem := computeIdleSavingsBreakdownMicroCents(rec, ns, distType)
	return cpu + mem
}

func computeIdleSavingsBreakdownMicroCents(rec *ContainerRec, ns *costdata.NamespaceCosts, distType string) (cpuMicro, memMicro int64) {
	replicas := replicaCountForSavingsApply(rec)

	modelCPURate := EffectiveRateMicroCentsPerMCHour(ns.CostModelCPUCost, ns.CPURequestHours)
	modelMemRate := EffectiveRateMicroCentsPerGiBHour(ns.CostModelMemCost, ns.MemRequestHours)

	cpuMicro = CPUSavingsMicroCents(rec.CurrentCPURequestMC, modelCPURate, HoursPerMonthInt, replicas)
	memMicro = MemSavingsMicroCentsFromKiB(rec.CurrentMemRequestKiB, modelMemRate, HoursPerMonthInt, replicas)

	totalInfraUSD := clampNonNegativeUSD(ns.InfraCost + ns.DistributedCost)
	if distType == "memory" {
		infraRate := EffectiveRateMicroCentsPerGiBHour(totalInfraUSD, ns.MemRequestHours)
		memMicro += MemSavingsMicroCentsFromKiB(rec.CurrentMemRequestKiB, infraRate, HoursPerMonthInt, replicas)
	} else {
		infraRate := EffectiveRateMicroCentsPerMCHour(totalInfraUSD, ns.CPURequestHours)
		cpuMicro += CPUSavingsMicroCents(rec.CurrentCPURequestMC, infraRate, HoursPerMonthInt, replicas)
	}

	return cpuMicro, memMicro
}

func safeDiv(numerator, denominator float64) float64 {
	if denominator == 0 {
		return 0
	}
	return numerator / denominator
}
