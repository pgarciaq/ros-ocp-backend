package engine

import (
	"math"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

const bytesPerGiB = 1024.0 * 1024.0 * 1024.0

// ApplyPVCSavings computes EstimatedMonthlySavingsCents for each PVC recommendation
// using configured storage rates from Koku. If costData is nil, savings remain 0
// and NotifNoCostData is appended.
func ApplyPVCSavings(recs []PVCRec, costData *costdata.ClusterCostData) {
	if costData == nil {
		for i := range recs {
			recs[i].NotificationCodes = appendUnique(recs[i].NotificationCodes, NotifNoCostData)
		}
		return
	}

	storageRate := StorageRequestPerMonth(costData)

	for i := range recs {
		savings := computePVCSavings(&recs[i], storageRate)
		recs[i].EstimatedMonthlySavingsCents = money.USDToCents(savings)
	}
}

func computePVCSavings(rec *PVCRec, storageRatePerMonth float64) float64 {
	if storageRatePerMonth == 0 {
		return 0
	}

	currentBytes := rec.RequestBytes
	if currentBytes == 0 {
		currentBytes = rec.CapacityBytes
	}
	if currentBytes == 0 {
		return 0
	}

	currentGiB := float64(currentBytes) / bytesPerGiB

	// Full monthly storage cost is recoverable when deleting an orphaned PVC.
	if rec.RecommendationType == PVCRecTypeOrphaned {
		total := currentGiB * storageRatePerMonth
		return math.Round(total*100) / 100
	}

	if rec.RecommendedBytes == nil {
		return 0
	}

	recommendedGiB := float64(*rec.RecommendedBytes) / bytesPerGiB
	deltaGiB := currentGiB - recommendedGiB

	total := deltaGiB * storageRatePerMonth
	return math.Round(total*100) / 100
}
