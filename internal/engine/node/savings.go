package node

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
)

// ApplyNodeSavings computes EstimatedMonthlySavingsCents for each node recommendation
// using configured rates from Koku. If costData is nil, savings remain 0 and
// NotifNoCostData is appended. Use savings recalculation (POST /internal/recalculate-savings)
// to refresh persisted rows after upstream cost model changes without re-ingestion.
func ApplyNodeSavings(recs []Rec, costData *costdata.ClusterCostData) {
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
		savingsMicroCents := computeNodeSavingsMicroCents(&recs[i], cpuRate, memRate, nodeRate)
		recs[i].EstimatedMonthlySavingsCents = core.MicroCentsToCents(savingsMicroCents)
	}
}

func computeNodeSavingsMicroCents(rec *Rec, cpuRate, memRate, nodeRate int64) int64 {
	cpuDelta := rec.CurrentCPUMC - rec.RecommendedCPUMC
	memDelta := rec.CurrentMemKiB - rec.RecommendedMemKiB

	cpuSavings := core.CPUSavingsMicroCents(cpuDelta, cpuRate, core.HoursPerMonthInt, 1)
	memSavings := core.MemSavingsMicroCentsFromKiB(memDelta, memRate, core.HoursPerMonthInt, 1)
	nodeSavings := core.MonthlyFlatSavingsMicroCents(int64(rec.NodeCountReduction), nodeRate)

	return cpuSavings + memSavings + nodeSavings
}

// computeNodeSavings returns monthly savings in USD for tests and backward compatibility.
func computeNodeSavings(rec *Rec, cpuRate, memRate, nodeRate float64) float64 {
	cpuRateMC := core.RateMicroCentsPerMCHour(cpuRate)
	memRateGiB := core.RateMicroCentsPerGiBHour(memRate)
	nodeRateMonthly := core.RateMicroCentsPerDollarMonth(nodeRate)
	return core.MicroCentsToDollars(computeNodeSavingsMicroCents(rec, cpuRateMC, memRateGiB, nodeRateMonthly))
}
