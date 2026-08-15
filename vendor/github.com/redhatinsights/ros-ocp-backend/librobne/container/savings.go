package container

import (
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// replicaCountForSavings returns the best available replica count for savings
// multiplication.
func replicaCountForSavings(rec *types.ContainerRec) float64 {
	return float64(types.ReplicaCountInt(rec))
}

// ApplySavingsEstimates computes EstimatedSavingsCents for each recommendation
// from a deposited RateCard. hoursPerMonth is calendar hours (HoursInMonth),
// not milli-hours. Nil/empty card: savings stay nil and NotifNoCostData is appended.
//
// When Namespaces is non-nil (Tier B / Koku), effective rate = spend / request-hours
// for that namespace; missing namespace → NotifNoCostData (no Tier A fallback).
// When Namespaces is nil, Tier A unit prices are used (markup applied if deposited).
func ApplySavingsEstimates(recs []types.ContainerRec, card *types.RateCard, hoursPerMonth int64) {
	if card.IsEmpty() {
		for i := range recs {
			recs[i].NotificationCodes = types.AppendUnique(recs[i].NotificationCodes, types.NotifNoCostData)
		}
		return
	}

	distType := card.Distribution
	if distType == "" {
		distType = "cpu"
	}
	useB := card.HasTierB()
	var aCPU, aMem int64
	if !useB {
		aCPU = card.CPURateMicroCentsPerMCHour()
		aMem = card.MemRateMicroCentsPerGiBHour()
	}

	for i := range recs {
		var modelCPURate, modelMemRate, infraCPURate, infraMemRate int64
		if useB {
			ns, ok := card.Namespaces[recs[i].Namespace]
			if !ok {
				recs[i].NotificationCodes = types.AppendUnique(recs[i].NotificationCodes, types.NotifNoCostData)
				continue
			}
			modelCPURate = types.EffectiveRateFromCPUTotals(ns.CostModelCPUMicroCents, ns.CPURequestMilliHours)
			modelMemRate = types.EffectiveRateFromMemTotals(ns.CostModelMemMicroCents, ns.MemRequestMilliHours)
			infraTotal := ns.InfraMicroCents + ns.DistributedMicroCents
			if distType == "memory" {
				infraMemRate = types.EffectiveRateFromMemTotals(infraTotal, ns.MemRequestMilliHours)
			} else {
				infraCPURate = types.EffectiveRateFromCPUTotals(infraTotal, ns.CPURequestMilliHours)
			}
		} else {
			modelCPURate = aCPU
			modelMemRate = aMem
		}

		if recs[i].IdleState.IsIdleOrZombie() {
			cpuMicro, memMicro := computeIdleSavingsBreakdownMicroCents(
				&recs[i], modelCPURate, modelMemRate, infraCPURate, infraMemRate, distType, hoursPerMonth,
			)
			total := types.MicroCentsToCents(cpuMicro + memMicro)
			cpuCents := types.MicroCentsToCents(cpuMicro)
			memCents := types.MicroCentsToCents(memMicro)
			recs[i].EstimatedSavingsCents = &total
			recs[i].EstimatedWasteCents = &total
			recs[i].EstimatedCPUSavingsCents = &cpuCents
			recs[i].EstimatedMemSavingsCents = &memCents
			continue
		}

		cpuMicro, memMicro := computeSavingsBreakdownMicroCents(
			&recs[i], modelCPURate, modelMemRate, infraCPURate, infraMemRate, distType, hoursPerMonth,
		)
		total := types.MicroCentsToCents(cpuMicro + memMicro)
		cpuCents := types.MicroCentsToCents(cpuMicro)
		memCents := types.MicroCentsToCents(memMicro)
		recs[i].EstimatedSavingsCents = &total
		recs[i].EstimatedCPUSavingsCents = &cpuCents
		recs[i].EstimatedMemSavingsCents = &memCents
	}
}

func computeSavingsBreakdownMicroCents(
	rec *types.ContainerRec,
	modelCPURate, modelMemRate, infraCPURate, infraMemRate int64,
	distType string,
	hoursPerMonth int64,
) (cpuMicro, memMicro int64) {
	cpuDeltaMC := rec.CurrentCPURequestMC - rec.RecCPURequestMC
	memDeltaKiB := rec.CurrentMemRequestKiB - rec.RecMemRequestKiB
	replicas := types.ReplicaCountForSavingsApply(rec)

	savingsReplicas := replicas
	if rec.RecommendedReplicas > 0 && rec.RecommendedReplicas < replicas {
		savingsReplicas = rec.RecommendedReplicas
	}

	cpuMicro = types.CPUSavingsMicroCents(cpuDeltaMC, modelCPURate, hoursPerMonth, savingsReplicas)
	memMicro = types.MemSavingsMicroCentsFromKiB(memDeltaKiB, modelMemRate, hoursPerMonth, savingsReplicas)

	if distType == "memory" {
		memMicro += types.MemSavingsMicroCentsFromKiB(memDeltaKiB, infraMemRate, hoursPerMonth, savingsReplicas)
	} else {
		cpuMicro += types.CPUSavingsMicroCents(cpuDeltaMC, infraCPURate, hoursPerMonth, savingsReplicas)
	}

	if rec.RecommendedReplicas > 0 && rec.RecommendedReplicas < replicas {
		replicaCPU, replicaMem := ReplicaReductionSavingsMicroCents(rec, modelCPURate, modelMemRate, hoursPerMonth)
		cpuMicro += replicaCPU
		memMicro += replicaMem
	}

	return cpuMicro, memMicro
}

func computeIdleSavingsBreakdownMicroCents(
	rec *types.ContainerRec,
	modelCPURate, modelMemRate, infraCPURate, infraMemRate int64,
	distType string,
	hoursPerMonth int64,
) (cpuMicro, memMicro int64) {
	replicas := types.ReplicaCountForSavingsApply(rec)

	cpuMicro = types.CPUSavingsMicroCents(rec.CurrentCPURequestMC, modelCPURate, hoursPerMonth, replicas)
	memMicro = types.MemSavingsMicroCentsFromKiB(rec.CurrentMemRequestKiB, modelMemRate, hoursPerMonth, replicas)

	if distType == "memory" {
		memMicro += types.MemSavingsMicroCentsFromKiB(rec.CurrentMemRequestKiB, infraMemRate, hoursPerMonth, replicas)
	} else {
		cpuMicro += types.CPUSavingsMicroCents(rec.CurrentCPURequestMC, infraCPURate, hoursPerMonth, replicas)
	}

	return cpuMicro, memMicro
}
