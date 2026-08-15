package container

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
)

// replicaCountForSavings returns the best available replica count for savings
// multiplication.
func replicaCountForSavings(rec *core.ContainerRec) float64 {
	return float64(core.ReplicaCountInt(rec))
}

// ApplySavingsEstimates computes EstimatedSavingsCents for each recommendation
// from a deposited RateCard. hoursPerMonth is calendar hours (HoursInMonth),
// not milli-hours. Nil/empty card: savings stay nil and NotifNoCostData is appended.
//
// When Namespaces is non-nil (Tier B / Koku), effective rate = spend / request-hours
// for that namespace; missing namespace → NotifNoCostData (no Tier A fallback).
// When Namespaces is nil, Tier A unit prices are used (markup applied if deposited).
func ApplySavingsEstimates(recs []core.ContainerRec, card *core.RateCard, hoursPerMonth int64) {
	if card.IsEmpty() {
		for i := range recs {
			recs[i].NotificationCodes = core.AppendUnique(recs[i].NotificationCodes, core.NotifNoCostData)
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
				recs[i].NotificationCodes = core.AppendUnique(recs[i].NotificationCodes, core.NotifNoCostData)
				continue
			}
			modelCPURate = core.EffectiveRateFromCPUTotals(ns.CostModelCPUMicroCents, ns.CPURequestMilliHours)
			modelMemRate = core.EffectiveRateFromMemTotals(ns.CostModelMemMicroCents, ns.MemRequestMilliHours)
			infraTotal := ns.InfraMicroCents + ns.DistributedMicroCents
			if distType == "memory" {
				infraMemRate = core.EffectiveRateFromMemTotals(infraTotal, ns.MemRequestMilliHours)
			} else {
				infraCPURate = core.EffectiveRateFromCPUTotals(infraTotal, ns.CPURequestMilliHours)
			}
		} else {
			modelCPURate = aCPU
			modelMemRate = aMem
		}

		if recs[i].IdleState.IsIdleOrZombie() {
			cpuMicro, memMicro := computeIdleSavingsBreakdownMicroCents(
				&recs[i], modelCPURate, modelMemRate, infraCPURate, infraMemRate, distType, hoursPerMonth,
			)
			total := core.MicroCentsToCents(cpuMicro + memMicro)
			cpuCents := core.MicroCentsToCents(cpuMicro)
			memCents := core.MicroCentsToCents(memMicro)
			recs[i].EstimatedSavingsCents = &total
			recs[i].EstimatedWasteCents = &total
			recs[i].EstimatedCPUSavingsCents = &cpuCents
			recs[i].EstimatedMemSavingsCents = &memCents
			continue
		}

		cpuMicro, memMicro := computeSavingsBreakdownMicroCents(
			&recs[i], modelCPURate, modelMemRate, infraCPURate, infraMemRate, distType, hoursPerMonth,
		)
		total := core.MicroCentsToCents(cpuMicro + memMicro)
		cpuCents := core.MicroCentsToCents(cpuMicro)
		memCents := core.MicroCentsToCents(memMicro)
		recs[i].EstimatedSavingsCents = &total
		recs[i].EstimatedCPUSavingsCents = &cpuCents
		recs[i].EstimatedMemSavingsCents = &memCents
	}
}

func computeSavingsBreakdownMicroCents(
	rec *core.ContainerRec,
	modelCPURate, modelMemRate, infraCPURate, infraMemRate int64,
	distType string,
	hoursPerMonth int64,
) (cpuMicro, memMicro int64) {
	cpuDeltaMC := rec.CurrentCPURequestMC - rec.RecCPURequestMC
	memDeltaKiB := rec.CurrentMemRequestKiB - rec.RecMemRequestKiB
	replicas := core.ReplicaCountForSavingsApply(rec)

	savingsReplicas := replicas
	if rec.RecommendedReplicas > 0 && rec.RecommendedReplicas < replicas {
		savingsReplicas = rec.RecommendedReplicas
	}

	cpuMicro = core.CPUSavingsMicroCents(cpuDeltaMC, modelCPURate, hoursPerMonth, savingsReplicas)
	memMicro = core.MemSavingsMicroCentsFromKiB(memDeltaKiB, modelMemRate, hoursPerMonth, savingsReplicas)

	if distType == "memory" {
		memMicro += core.MemSavingsMicroCentsFromKiB(memDeltaKiB, infraMemRate, hoursPerMonth, savingsReplicas)
	} else {
		cpuMicro += core.CPUSavingsMicroCents(cpuDeltaMC, infraCPURate, hoursPerMonth, savingsReplicas)
	}

	if rec.RecommendedReplicas > 0 && rec.RecommendedReplicas < replicas {
		replicaCPU, replicaMem := ReplicaReductionSavingsMicroCents(rec, modelCPURate, modelMemRate, hoursPerMonth)
		cpuMicro += replicaCPU
		memMicro += replicaMem
	}

	return cpuMicro, memMicro
}

func computeIdleSavingsBreakdownMicroCents(
	rec *core.ContainerRec,
	modelCPURate, modelMemRate, infraCPURate, infraMemRate int64,
	distType string,
	hoursPerMonth int64,
) (cpuMicro, memMicro int64) {
	replicas := core.ReplicaCountForSavingsApply(rec)

	cpuMicro = core.CPUSavingsMicroCents(rec.CurrentCPURequestMC, modelCPURate, hoursPerMonth, replicas)
	memMicro = core.MemSavingsMicroCentsFromKiB(rec.CurrentMemRequestKiB, modelMemRate, hoursPerMonth, replicas)

	if distType == "memory" {
		memMicro += core.MemSavingsMicroCentsFromKiB(rec.CurrentMemRequestKiB, infraMemRate, hoursPerMonth, replicas)
	} else {
		cpuMicro += core.CPUSavingsMicroCents(rec.CurrentCPURequestMC, infraCPURate, hoursPerMonth, replicas)
	}

	return cpuMicro, memMicro
}
