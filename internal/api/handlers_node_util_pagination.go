package api

import (
	"strconv"

	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

func nodeUtilSortValue(rec model.NodeUtilizationRec, orderByKey string) interface{} {
	switch orderByKey {
	case "node":
		return rec.Node
	case "cpu_util_p95":
		return rec.Metrics.CPUUtilP95
	case "mem_util_p95":
		return rec.Metrics.MemUtilP95
	case "pod_count":
		return rec.PodCount
	default:
		// SQL sorts on estimated_savings_cents (bigint), so the cursor
		// must store cents, not the formatted USD string.
		for _, termRec := range rec.RecommendationTerms {
			if termRec.RecommendationEngines == nil {
				continue
			}
			if eng := termRec.RecommendationEngines.Cost; eng != nil && eng.EstimatedMonthlySavings != nil {
				usd, err := strconv.ParseFloat(eng.EstimatedMonthlySavings.Value, 64)
				if err != nil {
					return nil
				}
				return money.USDToCents(usd)
			}
		}
		return nil
	}
}
