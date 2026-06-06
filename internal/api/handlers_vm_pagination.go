package api

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
)

func vmSortValue(rec model.VMRecommendation, orderBy string) interface{} {
	switch orderBy {
	case "namespace":
		return rec.Namespace
	case "current_vcpu":
		return rec.CurrentVCPU
	case "current_memory_gib":
		return rec.CurrentMemoryGiB
	case "guest_os":
		return rec.GuestOS
	case "recommended_vcpu":
		return rec.RecommendedVCPU
	case "recommended_memory_gib":
		return rec.RecommendedMemoryGiB
	case "is_idle":
		return rec.IsIdle
	case "is_abandoned":
		return rec.IsAbandoned
	case "is_oversized":
		return rec.IsOversized
	case "confidence":
		return rec.Confidence
	case "last_recommended_at":
		return rec.LastRecommendedAt
	case "savings", "savings_amount":
		if rec.EstimatedSavingsCents != nil {
			return float64(*rec.EstimatedSavingsCents) / 100.0
		}
		return nil
	default:
		return rec.VMName
	}
}

func vmNextCursor(orderBy string, last model.VMRecommendation) string {
	return EncodeVMCursor(VMCursor{
		ClusterUUID: last.ClusterUUID.String(),
		VMName:      last.VMName,
		Namespace:   last.Namespace,
		Term:        last.Term,
		Engine:      last.Engine,
		SortValue:   model.PaginationSortValueJSON(vmSortValue(last, orderBy)),
	})
}
