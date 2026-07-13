package vm

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

// ComputeVMSavings estimates monthly savings (USD) for a VM recommendation using
// configured Koku effective rates. Returns nil when cost data is unavailable or
// no rates are configured for the cluster.
func ComputeVMSavings(rec *model.VMRecommendation, costData *costdata.ClusterCostData) *float64 {
	microCents := vmSavingsMicroCents(rec, costData)
	if microCents == nil {
		return nil
	}
	total := engine.MicroCentsToDollars(*microCents)
	return &total
}

// ApplyVMSavings sets estimated_savings_cents and savings_currency on each recommendation when
// savings estimates are enabled and Koku rates are available.
func ApplyVMSavings(recs []model.VMRecommendation, costData *costdata.ClusterCostData, savingsEnabled bool) {
	if !savingsEnabled {
		return
	}
	currency := costdata.ResolveCurrency(costData)
	for i := range recs {
		microCents := vmSavingsMicroCents(&recs[i], costData)
		if microCents == nil {
			recs[i].EstimatedSavingsCents = nil
			recs[i].SavingsCurrency = nil
			continue
		}
		cents := engine.MicroCentsToCents(*microCents)
		recs[i].EstimatedSavingsCents = &cents
		recs[i].SavingsCurrency = &currency
	}
}

func vmSavingsMicroCents(rec *model.VMRecommendation, costData *costdata.ClusterCostData) *int64 {
	if rec == nil || costData == nil {
		return nil
	}
	cpuRate := engine.RateMicroCentsPerMCHour(engine.EffectiveCPUCoreHourlyRate(costData))
	memRate := engine.RateMicroCentsPerGiBHour(engine.EffectiveMemoryGBHourlyRate(costData))
	gpuRate := engine.RateMicroCentsPerDollarMonth(engine.GPUMonthlyRate(costData))
	vmMonthlyRate := engine.RateMicroCentsPerDollarMonth(engine.VMCostPerMonth(costData))
	if cpuRate == 0 && memRate == 0 && gpuRate == 0 && vmMonthlyRate == 0 {
		return nil
	}

	var total int64
	switch {
	case rec.IsAbandoned, rec.IsIdle:
		total = vmIdleOrAbandonedSavingsMicroCents(rec, cpuRate, memRate, gpuRate, vmMonthlyRate)
	case rec.IsPowerOffCandidate:
		total = vmPowerOffScheduleSavingsMicroCents(rec, cpuRate, memRate, gpuRate, vmMonthlyRate)
	default:
		total = vmDownsizeSavingsMicroCents(rec, cpuRate, memRate)
		total += vmGPUReductionSavingsMicroCents(rec, gpuRate)
	}
	return &total
}

func vmDownsizeSavingsMicroCents(rec *model.VMRecommendation, cpuRate, memRate int64) int64 {
	cpuDelta := int64(rec.CurrentVCPU - rec.RecommendedVCPU)
	if cpuDelta < 0 {
		cpuDelta = 0
	}
	memDelta := int64(rec.CurrentMemoryGiB - rec.RecommendedMemoryGiB)
	if memDelta < 0 {
		memDelta = 0
	}
	return engine.VCPUSavingsMicroCents(cpuDelta, cpuRate, engine.HoursPerMonthInt) +
		engine.GiBSavingsMicroCents(memDelta, memRate, engine.HoursPerMonthInt)
}

func vmPowerOffScheduleSavingsMicroCents(rec *model.VMRecommendation, cpuRate, memRate, gpuRate, vmMonthlyRate int64) int64 {
	base := vmIdleOrAbandonedSavingsMicroCents(rec, cpuRate, memRate, gpuRate, vmMonthlyRate)
	if rec.PowerOffIdleRatio == nil || *rec.PowerOffIdleRatio <= 0 {
		return base
	}
	return engine.ScaleMicroCentsByBasisPoints(base, int64(*rec.PowerOffIdleRatio))
}

func vmIdleOrAbandonedSavingsMicroCents(rec *model.VMRecommendation, cpuRate, memRate, gpuRate, vmMonthlyRate int64) int64 {
	base := engine.VCPUSavingsMicroCents(int64(rec.CurrentVCPU), cpuRate, engine.HoursPerMonthInt) +
		engine.GiBSavingsMicroCents(int64(rec.CurrentMemoryGiB), memRate, engine.HoursPerMonthInt)
	if vmMonthlyRate > 0 {
		base += vmMonthlyRate
	}
	if rec.GPUCount > 0 && gpuRate > 0 {
		base += engine.MonthlyFlatSavingsMicroCents(int64(rec.GPUCount), gpuRate)
	}
	return base
}

func vmGPUReductionSavingsMicroCents(rec *model.VMRecommendation, gpuMonthlyRate int64) int64 {
	if rec.GPUCount <= 0 || gpuMonthlyRate == 0 {
		return 0
	}

	switch rec.RecommendedGPUAction {
	case vmGPUActionRemoveGPU:
		return engine.MonthlyFlatSavingsMicroCents(int64(rec.GPUCount), gpuMonthlyRate)
	case vmGPUActionSmallerMIGProfile, vmGPUActionUseMIGProfile:
		if rec.RecommendedGPUProfile == "" || rec.RecommendedGPUProfile == "full_gpu" {
			return 0
		}
		spec := engine.MatchGPUModel(rec.GPUModel)
		if spec == nil {
			return 0
		}
		totalSlices := int64(engine.MigTotalSlices(spec))
		recSlices := int64(engine.MigProfileSlices(spec, rec.RecommendedGPUProfile))
		if totalSlices <= 0 || recSlices <= 0 {
			return 0
		}
		perGPU := engine.MIGFractionSavingsMicroCents(gpuMonthlyRate, totalSlices, recSlices)
		return perGPU * int64(rec.GPUCount)
	default:
		return 0
	}
}
