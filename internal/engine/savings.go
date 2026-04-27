package engine

import (
	"math"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
)

const hoursPerMonth = 730.0

// ApplySavingsEstimates computes EstimatedSavingsUSD for each recommendation
// using cost data from Koku. If costData is nil (Koku unavailable or not
// configured), all savings remain 0 and NotifNoCostData is appended.
func ApplySavingsEstimates(recs []ContainerRec, costData *costdata.ClusterCostData) {
	if costData == nil {
		for i := range recs {
			recs[i].NotificationCodes = appendUnique(recs[i].NotificationCodes, NotifNoCostData)
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
			recs[i].NotificationCodes = appendUnique(recs[i].NotificationCodes, NotifNoCostData)
			continue
		}

		savings := computeSavings(&recs[i], &ns, distType)
		recs[i].EstimatedSavingsUSD = float32(savings)
	}
}

func computeSavings(rec *ContainerRec, ns *costdata.NamespaceCosts, distType string) float64 {
	// Resource deltas: current - recommended (positive = saving, workload over-provisioned)
	cpuDeltaCores := float64(rec.CurrentCPURequestMC-rec.RecCPURequestMC) / 1000.0
	memDeltaGiB := float64(rec.CurrentMemRequestKiB-rec.RecMemRequestKiB) / (1024.0 * 1024.0)

	podCountAvg := float64(rec.PodCountAvg)
	if podCountAvg < 1.0 {
		podCountAvg = 1.0
	}

	// Cost model savings: derive effective per-unit rate from summary aggregates
	modelCPURate := safeDiv(ns.CostModelCPUCost, ns.CPURequestHours)
	modelMemRate := safeDiv(ns.CostModelMemCost, ns.MemRequestHours)

	modelSavings := (cpuDeltaCores*modelCPURate + memDeltaGiB*modelMemRate) * hoursPerMonth * podCountAvg

	// Infrastructure savings: apportion by distribution type
	var infraSavings float64
	if distType == "memory" {
		infraRate := safeDiv(ns.InfraCost, ns.MemRequestHours)
		infraSavings = memDeltaGiB * infraRate * hoursPerMonth * podCountAvg
	} else {
		infraRate := safeDiv(ns.InfraCost, ns.CPURequestHours)
		infraSavings = cpuDeltaCores * infraRate * hoursPerMonth * podCountAvg
	}

	total := modelSavings + infraSavings

	// Round to 2 decimal places
	return math.Round(total*100) / 100
}

func safeDiv(numerator, denominator float64) float64 {
	if denominator == 0 {
		return 0
	}
	return numerator / denominator
}

func appendUnique(codes []int16, code int16) []int16 {
	for _, c := range codes {
		if c == code {
			return codes
		}
	}
	return append(codes, code)
}
