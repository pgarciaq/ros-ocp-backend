package engine

import (
	"math"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

// ApplyNodeSavings computes EstimatedMonthlySavingsCents for each node recommendation
// using configured rates from Koku. If costData is nil, savings remain 0 and
// NotifNoCostData is appended.
func ApplyNodeSavings(recs []NodeRec, costData *costdata.ClusterCostData) {
	if costData == nil {
		for i := range recs {
			recs[i].NotificationCodes = appendUnique(recs[i].NotificationCodes, NotifNoCostData)
		}
		return
	}

	cpuRate := CPUCoreHourlyRate(costData)
	memRate := MemoryGBHourlyRate(costData)
	nodeRate := NodeCostPerMonth(costData)

	for i := range recs {
		savings := computeNodeSavings(&recs[i], cpuRate, memRate, nodeRate)
		recs[i].EstimatedMonthlySavingsCents = money.USDToCents(savings)
	}
}

func computeNodeSavings(rec *NodeRec, cpuRate, memRate, nodeRate float64) float64 {
	cpuDelta := float64(rec.CurrentCPUMC-rec.RecommendedCPUMC) / 1000.0
	memDelta := float64(rec.CurrentMemKiB-rec.RecommendedMemKiB) / (1024.0 * 1024.0)

	cpuSavings := cpuDelta * cpuRate * hoursPerMonth
	memSavings := memDelta * memRate * hoursPerMonth
	nodeSavings := float64(rec.NodeCountReduction) * nodeRate

	total := cpuSavings + memSavings + nodeSavings
	return math.Round(total*100) / 100
}
