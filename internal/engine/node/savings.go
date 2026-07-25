package node

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
)

// ApplyNodeSavings computes EstimatedMonthlySavingsCents for each node recommendation
// using configured rates from Koku. hoursPerMonth should be HoursInMonth(year, month)
// for the target calendar month. If costData is nil, savings remain 0 and
// NotifNoCostData is appended.
func ApplyNodeSavings(recs []Rec, costData *costdata.ClusterCostData, hoursPerMonth int64) {
	if costData == nil {
		for i := range recs {
			recs[i].NotificationCodes = core.AppendUnique(recs[i].NotificationCodes, core.NotifNoCostData)
		}
		return
	}

	cpuRate := core.RateMicroCentsPerMCHour(core.CPUCoreHourlyRate(costData))
	memRate := core.RateMicroCentsPerGiBHour(core.MemoryGBHourlyRate(costData))
	nodeRate := core.RateMicroCentsPerDollarMonth(core.NodeCostPerMonth(costData))

	for i := range recs {
		savingsMicroCents := computeNodeSavingsMicroCents(&recs[i], cpuRate, memRate, nodeRate, hoursPerMonth)
		recs[i].EstimatedMonthlySavingsCents = core.MicroCentsToCents(savingsMicroCents)
	}
}

func computeNodeSavingsMicroCents(rec *Rec, cpuRate, memRate, nodeRate, hoursPerMonth int64) int64 {
	cpuDelta := rec.CurrentCPUMC - rec.RecommendedCPUMC
	memDelta := rec.CurrentMemKiB - rec.RecommendedMemKiB

	cpuSavings := core.CPUSavingsMicroCents(cpuDelta, cpuRate, hoursPerMonth, 1)
	memSavings := core.MemSavingsMicroCentsFromKiB(memDelta, memRate, hoursPerMonth, 1)
	nodeSavings := core.MonthlyFlatSavingsMicroCents(int64(rec.NodeCountReduction), nodeRate)

	return cpuSavings + memSavings + nodeSavings
}

// computeNodeSavings returns monthly savings in USD for tests and backward compatibility.
func computeNodeSavings(rec *Rec, cpuRate, memRate, nodeRate float64, hoursPerMonth int64) float64 {
	cpuRateMC := core.RateMicroCentsPerMCHour(cpuRate)
	memRateGiB := core.RateMicroCentsPerGiBHour(memRate)
	nodeRateMonthly := core.RateMicroCentsPerDollarMonth(nodeRate)
	return core.MicroCentsToDollars(computeNodeSavingsMicroCents(rec, cpuRateMC, memRateGiB, nodeRateMonthly, hoursPerMonth))
}
