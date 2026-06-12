package engine

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
)

// ApplyNodeSavings computes EstimatedMonthlySavingsCents for each node recommendation
// using configured rates from Koku. If costData is nil, savings remain 0 and
// NotifNoCostData is appended. Use savings recalculation (POST /internal/recalculate-savings)
// to refresh persisted rows after upstream cost model changes without re-ingestion.
func ApplyNodeSavings(recs []NodeRec, costData *costdata.ClusterCostData) {
	if costData == nil {
		for i := range recs {
			recs[i].NotificationCodes = appendUnique(recs[i].NotificationCodes, NotifNoCostData)
		}
		return
	}

	cpuRate := RateMicroCentsPerMCHour(CPUCoreHourlyRate(costData))
	memRate := RateMicroCentsPerGiBHour(MemoryGBHourlyRate(costData))
	nodeRate := RateMicroCentsPerDollarMonth(NodeCostPerMonth(costData))

	for i := range recs {
		savingsMicroCents := computeNodeSavingsMicroCents(&recs[i], cpuRate, memRate, nodeRate)
		recs[i].EstimatedMonthlySavingsCents = MicroCentsToCents(savingsMicroCents)
	}
}

func computeNodeSavingsMicroCents(rec *NodeRec, cpuRate, memRate, nodeRate int64) int64 {
	cpuDelta := rec.CurrentCPUMC - rec.RecommendedCPUMC
	memDelta := rec.CurrentMemKiB - rec.RecommendedMemKiB

	cpuSavings := CPUSavingsMicroCents(cpuDelta, cpuRate, HoursPerMonthInt, 1)
	memSavings := MemSavingsMicroCentsFromKiB(memDelta, memRate, HoursPerMonthInt, 1)
	nodeSavings := MonthlyFlatSavingsMicroCents(int64(rec.NodeCountReduction), nodeRate)

	return cpuSavings + memSavings + nodeSavings
}

// computeNodeSavings returns monthly savings in USD for tests and backward compatibility.
func computeNodeSavings(rec *NodeRec, cpuRate, memRate, nodeRate float64) float64 {
	cpuRateMC := RateMicroCentsPerMCHour(cpuRate)
	memRateGiB := RateMicroCentsPerGiBHour(memRate)
	nodeRateMonthly := RateMicroCentsPerDollarMonth(nodeRate)
	return MicroCentsToDollars(computeNodeSavingsMicroCents(rec, cpuRateMC, memRateGiB, nodeRateMonthly))
}
