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

		// Idle or abandoned workloads: 100% of current resource cost is recoverable
		if recs[i].IsIdle || recs[i].IsAbandoned {
			recs[i].EstimatedSavingsUSD = float32(computeIdleSavings(&recs[i], &ns, distType))
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

	// Infrastructure + distributed overhead savings: apportion by distribution type
	totalInfra := ns.InfraCost + ns.DistributedCost
	var infraSavings float64
	if distType == "memory" {
		infraRate := safeDiv(totalInfra, ns.MemRequestHours)
		infraSavings = memDeltaGiB * infraRate * hoursPerMonth * podCountAvg
	} else {
		infraRate := safeDiv(totalInfra, ns.CPURequestHours)
		infraSavings = cpuDeltaCores * infraRate * hoursPerMonth * podCountAvg
	}

	total := modelSavings + infraSavings

	// Round to 2 decimal places
	return math.Round(total*100) / 100
}

// computeIdleSavings estimates the full cost of an idle/abandoned workload's
// current resource allocation, since 100% is recoverable by scaling down.
func computeIdleSavings(rec *ContainerRec, ns *costdata.NamespaceCosts, distType string) float64 {
	cpuCores := float64(rec.CurrentCPURequestMC) / 1000.0
	memGiB := float64(rec.CurrentMemRequestKiB) / (1024.0 * 1024.0)

	podCountAvg := float64(rec.PodCountAvg)
	if podCountAvg < 1.0 {
		podCountAvg = 1.0
	}

	modelCPURate := safeDiv(ns.CostModelCPUCost, ns.CPURequestHours)
	modelMemRate := safeDiv(ns.CostModelMemCost, ns.MemRequestHours)
	modelCost := (cpuCores*modelCPURate + memGiB*modelMemRate) * hoursPerMonth * podCountAvg

	totalInfra := ns.InfraCost + ns.DistributedCost
	var infraCost float64
	if distType == "memory" {
		infraRate := safeDiv(totalInfra, ns.MemRequestHours)
		infraCost = memGiB * infraRate * hoursPerMonth * podCountAvg
	} else {
		infraRate := safeDiv(totalInfra, ns.CPURequestHours)
		infraCost = cpuCores * infraRate * hoursPerMonth * podCountAvg
	}

	total := modelCost + infraCost
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
