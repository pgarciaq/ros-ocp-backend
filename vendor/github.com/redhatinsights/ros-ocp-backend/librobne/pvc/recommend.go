package pvc

import (
	"context"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/fixedpoint"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// PVCConfidenceLevel returns 0.0–1.0 based on data coverage vs the term minimum.
func PVCConfidenceLevel(dataDays, minDataDays int) float32 {
	if dataDays <= 0 || minDataDays <= 0 {
		return 0
	}
	ratio := float32(dataDays) / float32(minDataDays)
	if ratio > 1.0 {
		return 1.0
	}
	return ratio
}

func pvcMinClassifyDays(tc types.TermConfig, settings ThresholdSettings) int {
	min := settings.MinTrendDays
	if min < 1 {
		min = 1
	}
	return min
}

// EvaluatePVCNotifications appends low-confidence and other contextual codes to PVC recommendations.
func EvaluatePVCNotifications(rec PVCRec, th types.NotificationThresholds) []int16 {
	codes := append([]int16(nil), rec.NotificationCodes...)
	if rec.ConfidenceLevel < th.LowConfidenceThreshold && rec.DataDays > 0 {
		codes = append(codes, types.NotifLowConfidence)
	}
	if rec.DataDays > 0 && rec.DataDays <= th.SparseDataThreshold {
		codes = append(codes, types.NotifSparseData)
	}
	return codes
}

// RecommendPVCs runs the PVC recommendation loop with no pool.
// grouped is PVCKey → digest rows ordered by BucketDate (same as the digest SELECT).
// ApplyPVCSavings is a separate call after this returns (product).
func RecommendPVCs(ctx context.Context, grouped map[PVCKey][]PVCDigestRow, cfg EngineConfig) ([]PVCRec, error) {
	var results []PVCRec
	for _, digests := range grouped {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for _, tc := range cfg.Terms {
			windowed := windowDigests(digests, tc.WindowDays)
			rec := ComputePVCRecommendation(windowed, cfg.OrgID, cfg.ClusterUUID, tc, cfg.Settings, cfg.NotifThresholds)
			results = append(results, rec)
		}
	}
	return results, nil
}

func windowDigests(digests []PVCDigestRow, windowDays int) []PVCDigestRow {
	if len(digests) == 0 {
		return nil
	}
	latest := digests[len(digests)-1].BucketDate
	cutoff := latest.AddDate(0, 0, -windowDays)
	var result []PVCDigestRow
	for _, d := range digests {
		if !d.BucketDate.Before(cutoff) {
			result = append(result, d)
		}
	}
	return result
}

// ComputePVCRecommendation classifies a windowed PVC digest series for one term.
func ComputePVCRecommendation(digests []PVCDigestRow, orgID, clusterUUID string, tc types.TermConfig, settings ThresholdSettings, notifThresholds types.NotificationThresholds) PVCRec {
	if len(digests) == 0 {
		return PVCRec{Term: tc.Name, OrgID: orgID, ClusterUUID: clusterUUID, RecommendationType: PVCRecTypeHealthy}
	}

	latest := digests[len(digests)-1]
	rec := PVCRec{
		OrgID:         orgID,
		ClusterUUID:   clusterUUID,
		Namespace:     latest.Namespace,
		PVC:           latest.PVC,
		LastSeenPod:   latest.LastSeenPod,
		VMName:        latest.VMName,
		PV:            latest.PV,
		StorageClass:  latest.StorageClass,
		CapacityBytes: latest.CapacityBytes,
		RequestBytes:  latest.RequestBytes,
		DataDays:      len(digests),
		Term:          tc.Name,
		Expl: types.PVCExplanationFactors{
			OversizedThresholdBP:      int32(settings.OversizedThreshold * float64(fixedpoint.BasisPointsScale)),
			NearFullThresholdBP:       int32(settings.NearFullThreshold * float64(fixedpoint.BasisPointsScale)),
			RecommendedSizeMultiplier: int32(settings.RecommendedSizeMultiplier * 100),
			MinRecommendedGiB:         int32(settings.MinRecommendedGiB),
		},
	}

	var maxUsage int64
	allZero := true
	for _, d := range digests {
		if d.UsageBytesMax > maxUsage {
			maxUsage = d.UsageBytesMax
		}
		if d.UsageBytesMax > 0 || d.UsageBytesAvg > 0 {
			allZero = false
		}
	}
	rec.UsageBytesMax = maxUsage

	if latest.CapacityBytes > 0 {
		rec.UsageRatio = float64(maxUsage) / float64(latest.CapacityBytes)
	}

	minTrend := tc.MinDataDays
	if minTrend < settings.MinTrendDays {
		minTrend = settings.MinTrendDays
	}
	if len(digests) >= minTrend {
		slope := computePVCGrowthSlope(digests, tc.DecayHalfLifeHours)
		rec.GrowthBytesPerDay = int64(slope)

		if slope > 0 && latest.CapacityBytes > 0 {
			remaining := float64(latest.CapacityBytes) - float64(maxUsage)
			if remaining > 0 {
				daysToFull := int(remaining / slope)
				rec.DaysToFull = &daysToFull
			}
		}
	}

	minClassify := pvcMinClassifyDays(tc, settings)
	switch {
	case allZero && len(digests) >= minClassify:
		rec.RecommendationType = PVCRecTypeOrphaned
		rec.Expl.ClassificationReason = PVCRecTypeOrphaned
		rec.NotificationCodes = append(rec.NotificationCodes, types.NotifPVCOrphaned)
		rec.IdleSince = findPVCOrphanedSince(digests)
		rec.IdleDurationDays = types.ComputeIdleDuration(rec.IdleSince)

	case rec.UsageRatio < settings.OversizedThreshold && len(digests) >= minClassify:
		rec.RecommendationType = PVCRecTypeOversized
		rec.Expl.ClassificationReason = PVCRecTypeOversized
		recommended := maxUsage * int64(settings.RecommendedSizeMultiplier)
		minRecommended := int64(settings.MinRecommendedGiB) << 30
		if recommended < minRecommended {
			recommended = minRecommended
		}
		if recommended < latest.CapacityBytes {
			rec.RecommendedBytes = &recommended
		}
		rec.NotificationCodes = append(rec.NotificationCodes, types.NotifPVCOversized)

	case rec.UsageRatio > settings.NearFullThreshold:
		rec.RecommendationType = PVCRecTypeNearFull
		rec.Expl.ClassificationReason = PVCRecTypeNearFull
		recommended := maxUsage * int64(settings.RecommendedSizeMultiplier)
		rec.RecommendedBytes = &recommended
		rec.NotificationCodes = append(rec.NotificationCodes, types.NotifPVCNearFull)

	default:
		rec.RecommendationType = PVCRecTypeHealthy
		rec.Expl.ClassificationReason = PVCRecTypeHealthy
	}

	rec.Expl.DataDays = rec.DataDays

	if rec.DaysToFull != nil && *rec.DaysToFull < settings.DaysToFullAlert && *rec.DaysToFull > 0 {
		rec.NotificationCodes = append(rec.NotificationCodes, types.NotifPVCNearFull)
	}

	rec.ConfidenceLevel = PVCConfidenceLevel(rec.DataDays, tc.MinDataDays)
	rec.NotificationCodes = EvaluatePVCNotifications(rec, notifThresholds)

	return rec
}

func findPVCOrphanedSince(digests []PVCDigestRow) *time.Time {
	if len(digests) == 0 {
		return nil
	}
	start := len(digests) - 1
	for start >= 0 && digests[start].UsageBytesMax == 0 && digests[start].UsageBytesAvg == 0 {
		start--
	}
	firstZero := start + 1
	if firstZero >= len(digests) {
		return nil
	}
	t := digests[firstZero].BucketDate
	return &t
}

func computePVCGrowthSlope(digests []PVCDigestRow, decayHalfLifeHours float64) float64 {
	n := len(digests)
	if n < 2 {
		return 0.0
	}

	if decayHalfLifeHours <= 0 {
		return computePVCGrowthSlopeOLS(digests)
	}
	return computePVCGrowthSlopeWLS(digests, decayHalfLifeHours)
}

func computePVCGrowthSlopeOLS(digests []PVCDigestRow) float64 {
	n := len(digests)
	var sumX, sumY, sumXY, sumX2 float64
	for i, d := range digests {
		x := float64(i)
		y := float64(d.UsageBytesAvg)
		sumX += x
		sumY += y
		sumXY += x * y
		sumX2 += x * x
	}

	nf := float64(n)
	denom := nf*sumX2 - sumX*sumX
	if denom == 0 {
		return 0.0
	}
	return (nf*sumXY - sumX*sumY) / denom
}

func computePVCGrowthSlopeWLS(digests []PVCDigestRow, halfLifeHours float64) float64 {
	n := len(digests)

	var sumW, sumWX, sumWY, sumWXY, sumWX2 float64
	for i, d := range digests {
		x := float64(i)
		y := float64(d.UsageBytesAvg)
		ageHours := float64(n-1-i) * 24.0
		w := types.DecayWeight(ageHours, halfLifeHours)
		sumW += w
		sumWX += w * x
		sumWY += w * y
		sumWXY += w * x * y
		sumWX2 += w * x * x
	}

	denom := sumW*sumWX2 - sumWX*sumWX
	if denom == 0 {
		return 0.0
	}
	return (sumW*sumWXY - sumWX*sumWY) / denom
}
