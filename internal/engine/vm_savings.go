package engine

import (
	"math"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

// ComputeVMSavings estimates monthly savings (USD) for a VM recommendation using
// configured Koku effective rates. Returns nil when cost data is unavailable or
// no rates are configured for the cluster.
func ComputeVMSavings(rec *model.VMRecommendation, costData *costdata.ClusterCostData) *float64 {
	if rec == nil || costData == nil {
		return nil
	}

	cpuRate := EffectiveCPUCoreHourlyRate(costData)
	memRate := EffectiveMemoryGBHourlyRate(costData)
	gpuRate := GPUMonthlyRate(costData)
	vmMonthlyRate := VMCostPerMonth(costData)
	if cpuRate == 0 && memRate == 0 && gpuRate == 0 && vmMonthlyRate == 0 {
		return nil
	}

	var total float64
	switch {
	case rec.IsAbandoned, rec.IsIdle:
		total = vmIdleOrAbandonedSavings(rec, cpuRate, memRate, gpuRate, vmMonthlyRate)
	case rec.IsPowerOffCandidate:
		total = vmPowerOffScheduleSavings(rec, cpuRate, memRate, gpuRate, vmMonthlyRate)
	default:
		total = vmDownsizeSavings(rec, cpuRate, memRate)
		total += vmGPUReductionSavings(rec, gpuRate)
	}

	total = math.Round(total*100) / 100
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
		usd := ComputeVMSavings(&recs[i], costData)
		if usd == nil {
			recs[i].EstimatedSavingsCents = nil
			recs[i].SavingsCurrency = nil
			continue
		}
		cents := money.USDToCents(*usd)
		recs[i].EstimatedSavingsCents = &cents
		recs[i].SavingsCurrency = &currency
	}
}

func vmDownsizeSavings(rec *model.VMRecommendation, cpuRate, memRate float64) float64 {
	cpuDelta := float64(rec.CurrentVCPU - rec.RecommendedVCPU)
	if cpuDelta < 0 {
		cpuDelta = 0
	}
	memDelta := float64(rec.CurrentMemoryGiB - rec.RecommendedMemoryGiB)
	if memDelta < 0 {
		memDelta = 0
	}
	return cpuDelta*cpuRate*hoursPerMonth + memDelta*memRate*hoursPerMonth
}

func vmPowerOffScheduleSavings(rec *model.VMRecommendation, cpuRate, memRate, gpuRate, vmMonthlyRate float64) float64 {
	base := vmIdleOrAbandonedSavings(rec, cpuRate, memRate, gpuRate, vmMonthlyRate)
	if rec.PowerOffIdleRatio == nil || *rec.PowerOffIdleRatio <= 0 {
		return base
	}
	multiplier := float64(*rec.PowerOffIdleRatio) / 10000.0
	return base * multiplier
}

func vmIdleOrAbandonedSavings(rec *model.VMRecommendation, cpuRate, memRate, gpuRate, vmMonthlyRate float64) float64 {
	cpu := float64(rec.CurrentVCPU)
	mem := float64(rec.CurrentMemoryGiB)
	base := cpu*cpuRate*hoursPerMonth + mem*memRate*hoursPerMonth
	if vmMonthlyRate > 0 {
		base += vmMonthlyRate
	}
	if rec.GPUCount > 0 && gpuRate > 0 {
		base += float64(rec.GPUCount) * gpuRate
	}
	return base
}

func vmGPUReductionSavings(rec *model.VMRecommendation, gpuMonthlyRate float64) float64 {
	if rec.GPUCount <= 0 || gpuMonthlyRate == 0 {
		return 0
	}

	switch rec.RecommendedGPUAction {
	case vmGPUActionRemoveGPU:
		return float64(rec.GPUCount) * gpuMonthlyRate
	case vmGPUActionSmallerMIGProfile, vmGPUActionUseMIGProfile:
		if rec.RecommendedGPUProfile == "" || rec.RecommendedGPUProfile == "full_gpu" {
			return 0
		}
		spec := MatchGPUModel(rec.GPUModel)
		if spec == nil {
			return 0
		}
		totalSlices := migTotalSlices(spec)
		recSlices := migProfileSlices(spec, rec.RecommendedGPUProfile)
		if totalSlices <= 0 || recSlices <= 0 {
			return 0
		}
		perGPU := (1.0 - float64(recSlices)/float64(totalSlices)) * gpuMonthlyRate
		return perGPU * float64(rec.GPUCount)
	default:
		return 0
	}
}
