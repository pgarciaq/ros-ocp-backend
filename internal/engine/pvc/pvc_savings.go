package pvc

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
)

// ApplyPVCSavings computes EstimatedMonthlySavingsCents for each PVC recommendation
// using configured storage rates from Koku. If costData is nil, savings remain 0
// and NotifNoCostData is appended.
func ApplyPVCSavings(recs []PVCRec, costData *costdata.ClusterCostData) {
	if costData == nil {
		for i := range recs {
			recs[i].NotificationCodes = core.AppendUnique(recs[i].NotificationCodes, core.NotifNoCostData)
		}
		return
	}

	storageRate := core.RateMicroCentsPerGiBMonth(core.StorageRequestPerMonth(costData))

	for i := range recs {
		savingsMicroCents := computePVCSavingsMicroCents(&recs[i], storageRate)
		recs[i].EstimatedMonthlySavingsCents = core.MicroCentsToCents(savingsMicroCents)
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

	if rec.RecommendationType == PVCRecTypeOrphaned {
		return core.StorageSavingsMicroCentsFromBytes(currentBytes, storageRateMicroCentsPerGiBMonth)
	}

	if rec.RecommendedBytes == nil {
		return 0
	}

	deltaBytes := currentBytes - *rec.RecommendedBytes
	return core.StorageSavingsMicroCentsFromBytes(deltaBytes, storageRateMicroCentsPerGiBMonth)
}
