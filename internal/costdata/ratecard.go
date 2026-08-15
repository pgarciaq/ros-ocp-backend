package costdata

import (
	"math"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
)

// ClusterCostDataToRateCard maps a Koku effective-rates payload onto RateCard
// once per cluster. Apply* must not call this. Floats are quantized here only
// (until #461 ships integer JSON). Nil in → nil out.
//
// Koku container savings is Tier B (namespace spend / request-hours). This
// mapper does not copy ConfiguredRates into Tier A and does not copy MarkupPct
// (aggregates are already priced; applying markup again would double-count).
// Currency is copied as-is; empty stays unset (never "USD").
func ClusterCostDataToRateCard(cd *ClusterCostData) *core.RateCard {
	if cd == nil {
		return nil
	}
	card := &core.RateCard{
		Currency:     cd.Currency,
		Distribution: cd.DistributionType,
	}
	if cd.Namespaces != nil {
		card.Namespaces = make(map[string]core.NamespaceSpend, len(cd.Namespaces))
		for name, ns := range cd.Namespaces {
			card.Namespaces[name] = core.NamespaceSpend{
				CostModelCPUMicroCents: dollarsToMicroCents(ns.CostModelCPUCost),
				CostModelMemMicroCents: dollarsToMicroCents(ns.CostModelMemCost),
				InfraMicroCents:        dollarsToMicroCents(ns.InfraCost),
				DistributedMicroCents:  dollarsToMicroCents(ns.DistributedCost),
				CPURequestMilliHours:   hoursToMilliHours(ns.CPURequestHours),
				MemRequestMilliHours:   hoursToMilliHours(ns.MemRequestHours),
			}
		}
	}
	return card
}

func dollarsToMicroCents(usd float64) int64 {
	if usd <= 0 {
		return 0
	}
	return int64(math.Round(usd * float64(core.MicroCentsPerDollar)))
}

func hoursToMilliHours(hours float64) int64 {
	if hours <= 0 {
		return 0
	}
	return int64(math.Round(hours * 1000))
}
