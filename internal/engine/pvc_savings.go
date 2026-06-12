package engine

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
)

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

	storageRate := RateMicroCentsPerGiBMonth(StorageRequestPerMonth(costData))

	for i := range recs {
		savingsMicroCents := computePVCSavingsMicroCents(&recs[i], storageRate)
		recs[i].EstimatedMonthlySavingsCents = MicroCentsToCents(savingsMicroCents)
	}
}

func computePVCSavingsMicroCents(rec *PVCRec, storageRateMicroCentsPerGiBMonth int64) int64 {
	if storageRateMicroCentsPerGiBMonth == 0 {
		return 0
	}

	currentBytes := rec.RequestBytes
	if currentBytes == 0 {
		currentBytes = rec.CapacityBytes
	}
	if currentBytes == 0 {
		return 0
	}

	// Full monthly storage cost is recoverable when deleting an orphaned PVC.
	if rec.RecommendationType == PVCRecTypeOrphaned {
		return StorageSavingsMicroCentsFromBytes(currentBytes, storageRateMicroCentsPerGiBMonth)
	}

	if rec.RecommendedBytes == nil {
		return 0
	}

	deltaBytes := currentBytes - *rec.RecommendedBytes
	return StorageSavingsMicroCentsFromBytes(deltaBytes, storageRateMicroCentsPerGiBMonth)
}
