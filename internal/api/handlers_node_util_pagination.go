package api

import "github.com/redhatinsights/ros-ocp-backend/internal/model"

func nodeUtilSortValue(rec model.NodeUtilizationRec, orderByKey string) interface{} {
	switch orderByKey {
	case "node":
		return rec.Node
	default:
		for _, termRec := range rec.RecommendationTerms {
			if termRec.RecommendationEngines == nil {
				continue
			}
			if eng := termRec.RecommendationEngines.Cost; eng != nil && eng.EstimatedMonthlySavings != nil {
				return eng.EstimatedMonthlySavings.Value
			}
		}
		return nil
	}
}
