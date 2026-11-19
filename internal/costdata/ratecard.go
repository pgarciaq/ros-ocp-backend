package costdata

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
)

// ClusterCostDataToRateCard maps a Koku effective-rates payload onto RateCard
// once per cluster. Apply* must not call this. Money is already integer
// micro-cents and hours are already milli-hours (#461), so this copies — it
// never converts. Nil in → nil out.
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
				CostModelCPUMicroCents: nonNegative(ns.CostModelCPUCost),
				CostModelMemMicroCents: nonNegative(ns.CostModelMemCost),
				InfraMicroCents:        nonNegative(ns.InfraCost),
				DistributedMicroCents:  nonNegative(ns.DistributedCost),
				CPURequestMilliHours:   nonNegative(ns.CPURequestHours),
				MemRequestMilliHours:   nonNegative(ns.MemRequestHours),
			}
		}
	}
	return card
}

// nonNegative keeps the pre-#461 mapper's clamp: a negative aggregate is not a
// spend, and letting it through would let one namespace reduce the savings
// computed from another.
func nonNegative(v int64) int64 {
	if v < 0 {
		return 0
	}
	return v
}
