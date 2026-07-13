package core

import (
	"math"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
)

// CombinedConfiguredRate returns infrastructure + supplementary for a metric.
func CombinedConfiguredRate(costData *costdata.ClusterCostData, metric string) float64 {
	if costData == nil || costData.ConfiguredRates == nil {
		return 0
	}
	rp, ok := costData.ConfiguredRates[metric]
	if !ok {
		return 0
	}
	return rp.Infrastructure + rp.Supplementary
}

// CombinedConfiguredRateWithFallbacks tries each metric name in order,
// returning the first non-zero combined rate.
func CombinedConfiguredRateWithFallbacks(costData *costdata.ClusterCostData, metrics ...string) float64 {
	for _, m := range metrics {
		if r := CombinedConfiguredRate(costData, m); r > 0 {
			return r
		}
	}
	return 0
}

// CPUCoreHourlyRate returns the combined cpu_core_usage_per_hour rate,
// falling back to cpu_core_effective_usage_per_hour.
func CPUCoreHourlyRate(costData *costdata.ClusterCostData) float64 {
	return CombinedConfiguredRateWithFallbacks(costData,
		"cpu_core_usage_per_hour",
		"cpu_core_effective_usage_per_hour",
	)
}

// MemoryGBHourlyRate returns the combined memory_gb_usage_per_hour rate,
// falling back to memory_gb_effective_usage_per_hour.
func MemoryGBHourlyRate(costData *costdata.ClusterCostData) float64 {
	return CombinedConfiguredRateWithFallbacks(costData,
		"memory_gb_usage_per_hour",
		"memory_gb_effective_usage_per_hour",
	)
}

// NodeCostPerMonth returns the combined node_cost_per_month rate,
// falling back to node_core_cost_per_month.
func NodeCostPerMonth(costData *costdata.ClusterCostData) float64 {
	return CombinedConfiguredRateWithFallbacks(costData,
		"node_cost_per_month",
		"node_core_cost_per_month",
	)
}

// VMCostPerMonth returns the combined vm_cost_per_month rate (flat monthly VM charge).
func VMCostPerMonth(costData *costdata.ClusterCostData) float64 {
	return CombinedConfiguredRate(costData, "vm_cost_per_month")
}

// EffectiveCPUCoreHourlyRate returns max(request, usage) combined rates for CPU.
func EffectiveCPUCoreHourlyRate(costData *costdata.ClusterCostData) float64 {
	return effectiveConfiguredRate(costData, "cpu_core_request_per_hour", "cpu_core_usage_per_hour")
}

// EffectiveMemoryGBHourlyRate returns max(request, usage) combined rates for memory.
func EffectiveMemoryGBHourlyRate(costData *costdata.ClusterCostData) float64 {
	return effectiveConfiguredRate(costData, "memory_gb_request_per_hour", "memory_gb_usage_per_hour")
}

func effectiveConfiguredRate(costData *costdata.ClusterCostData, requestMetric, usageMetric string) float64 {
	return math.Max(CombinedConfiguredRate(costData, requestMetric), CombinedConfiguredRate(costData, usageMetric))
}

// StorageRequestPerMonth returns storage_gb_request_per_month, falling back to
// storage_gb_usage_per_month when the request rate is zero or missing.
func StorageRequestPerMonth(costData *costdata.ClusterCostData) float64 {
	rate := CombinedConfiguredRate(costData, "storage_gb_request_per_month")
	if rate > 0 {
		return rate
	}
	return CombinedConfiguredRate(costData, "storage_gb_usage_per_month")
}
