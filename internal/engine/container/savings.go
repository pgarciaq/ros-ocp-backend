package container

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
)

// replicaCountForSavings returns the best available replica count for savings
// multiplication.
func replicaCountForSavings(rec *core.ContainerRec) float64 {
	return float64(core.ReplicaCountInt(rec))
}

// ApplySavingsEstimates computes EstimatedSavingsCents for each recommendation
// using cost data from Koku. hoursPerMonth should be HoursInMonth(year, month)
// for the target calendar month. If costData is nil (Koku unavailable or not
// configured), all savings fields remain nil and NotifNoCostData is appended.
func ApplySavingsEstimates(recs []core.ContainerRec, costData *costdata.ClusterCostData, hoursPerMonth int64) {
	if costData == nil {
		for i := range recs {
			recs[i].NotificationCodes = core.AppendUnique(recs[i].NotificationCodes, core.NotifNoCostData)
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
			recs[i].NotificationCodes = core.AppendUnique(recs[i].NotificationCodes, core.NotifNoCostData)
			continue
		}

		if recs[i].IdleState.IsIdleOrZombie() {
			idleMicroCents := computeIdleSavingsMicroCents(&recs[i], &ns, distType, hoursPerMonth)
			total := core.MicroCentsToCents(idleMicroCents)
			recs[i].EstimatedSavingsCents = &total
			recs[i].EstimatedWasteCents = &total
			cpuSavings, memSavings := computeIdleSavingsBreakdownMicroCents(&recs[i], &ns, distType, hoursPerMonth)
			cpuCents := core.MicroCentsToCents(cpuSavings)
			memCents := core.MicroCentsToCents(memSavings)
			recs[i].EstimatedCPUSavingsCents = &cpuCents
			recs[i].EstimatedMemSavingsCents = &memCents
			continue
		}

		cpuMicro, memMicro := computeSavingsBreakdownMicroCents(&recs[i], &ns, distType, hoursPerMonth)
		total := core.MicroCentsToCents(cpuMicro + memMicro)
		cpuCents := core.MicroCentsToCents(cpuMicro)
		memCents := core.MicroCentsToCents(memMicro)
		recs[i].EstimatedSavingsCents = &total
		recs[i].EstimatedCPUSavingsCents = &cpuCents
		recs[i].EstimatedMemSavingsCents = &memCents
	}
}

func computeSavingsBreakdownMicroCents(rec *core.ContainerRec, ns *costdata.NamespaceCosts, distType string, hoursPerMonth int64) (cpuMicro, memMicro int64) {
	cpuDeltaMC := rec.CurrentCPURequestMC - rec.RecCPURequestMC
	memDeltaKiB := rec.CurrentMemRequestKiB - rec.RecMemRequestKiB
	replicas := core.ReplicaCountForSavingsApply(rec)

	savingsReplicas := replicas
	if rec.RecommendedReplicas > 0 && rec.RecommendedReplicas < replicas {
		savingsReplicas = rec.RecommendedReplicas
	}

	modelCPURate := core.EffectiveRateMicroCentsPerMCHour(ns.CostModelCPUCost, ns.CPURequestHours)
	modelMemRate := core.EffectiveRateMicroCentsPerGiBHour(ns.CostModelMemCost, ns.MemRequestHours)

	cpuMicro = core.CPUSavingsMicroCents(cpuDeltaMC, modelCPURate, hoursPerMonth, savingsReplicas)
	memMicro = core.MemSavingsMicroCentsFromKiB(memDeltaKiB, modelMemRate, hoursPerMonth, savingsReplicas)

	totalInfraUSD := core.ClampNonNegativeUSD(ns.InfraCost + ns.DistributedCost)
	if distType == "memory" {
		infraRate := core.EffectiveRateMicroCentsPerGiBHour(totalInfraUSD, ns.MemRequestHours)
		memMicro += core.MemSavingsMicroCentsFromKiB(memDeltaKiB, infraRate, hoursPerMonth, savingsReplicas)
	} else {
		infraRate := core.EffectiveRateMicroCentsPerMCHour(totalInfraUSD, ns.CPURequestHours)
		cpuMicro += core.CPUSavingsMicroCents(cpuDeltaMC, infraRate, hoursPerMonth, savingsReplicas)
	}

	if rec.RecommendedReplicas > 0 && rec.RecommendedReplicas < replicas {
		replicaCPU, replicaMem := ReplicaReductionSavingsMicroCents(rec, modelCPURate, modelMemRate, hoursPerMonth)
		cpuMicro += replicaCPU
		memMicro += replicaMem
	}

	return cpuMicro, memMicro
}

func computeIdleSavingsMicroCents(rec *core.ContainerRec, ns *costdata.NamespaceCosts, distType string, hoursPerMonth int64) int64 {
	cpu, mem := computeIdleSavingsBreakdownMicroCents(rec, ns, distType, hoursPerMonth)
	return cpu + mem
}

func computeIdleSavingsBreakdownMicroCents(rec *core.ContainerRec, ns *costdata.NamespaceCosts, distType string, hoursPerMonth int64) (cpuMicro, memMicro int64) {
	replicas := core.ReplicaCountForSavingsApply(rec)

	modelCPURate := core.EffectiveRateMicroCentsPerMCHour(ns.CostModelCPUCost, ns.CPURequestHours)
	modelMemRate := core.EffectiveRateMicroCentsPerGiBHour(ns.CostModelMemCost, ns.MemRequestHours)

	cpuMicro = core.CPUSavingsMicroCents(rec.CurrentCPURequestMC, modelCPURate, hoursPerMonth, replicas)
	memMicro = core.MemSavingsMicroCentsFromKiB(rec.CurrentMemRequestKiB, modelMemRate, hoursPerMonth, replicas)

	totalInfraUSD := core.ClampNonNegativeUSD(ns.InfraCost + ns.DistributedCost)
	if distType == "memory" {
		infraRate := core.EffectiveRateMicroCentsPerGiBHour(totalInfraUSD, ns.MemRequestHours)
		memMicro += core.MemSavingsMicroCentsFromKiB(rec.CurrentMemRequestKiB, infraRate, hoursPerMonth, replicas)
	} else {
		infraRate := core.EffectiveRateMicroCentsPerMCHour(totalInfraUSD, ns.CPURequestHours)
		cpuMicro += core.CPUSavingsMicroCents(rec.CurrentCPURequestMC, infraRate, hoursPerMonth, replicas)
	}

	return cpuMicro, memMicro
}
