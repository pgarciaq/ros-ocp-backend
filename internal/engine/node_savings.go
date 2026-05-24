package engine

import (
	"math"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
)

// ApplyNodeSavings computes EstimatedMonthlySavingsUSD for each node recommendation
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
		recs[i].EstimatedMonthlySavingsUSD = float32(savings)
	}
}

func computeNodeSavings(rec *NodeRec, cpuRate, memRate, nodeRate float64) float64 {
	cpuDelta := rec.CurrentCPUCores - rec.RecommendedCPUCores
	memDelta := rec.CurrentMemoryGiB - rec.RecommendedMemoryGiB

	cpuSavings := cpuDelta * cpuRate * hoursPerMonth
	memSavings := memDelta * memRate * hoursPerMonth
	nodeSavings := float64(rec.NodeCountReduction) * nodeRate

	total := cpuSavings + memSavings + nodeSavings
	return math.Round(total*100) / 100
}
