package gpu

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
)

// PersistGPUMIGRecommendationSets denormalizes the per-container MIG
// recommendation view and writes it to gpu_mig_recommendation_sets. This runs
// in the background engine cycle after StoreGPUClassifications.
func PersistGPUMIGRecommendationSets(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	terms []core.TermConfig,
	costData *costdata.ClusterCostData,
) error {
	log := logging.GetLogger()

	now := time.Now().UTC()
	start := now.AddDate(0, 0, -core.MaxWindowDays(terms, 30))

	gpuRecs, nodeMap, _, err := QueryGPURecommendations(ctx, pool, orgID, clusterUUID, start, now, terms, nil)
	if err != nil {
		return fmt.Errorf("PersistGPUMIGRecommendationSets: query recs: %w", err)
	}
	if len(gpuRecs) == 0 {
		return nil
	}

	gpuMonthlyRate := GPUMonthlyRate(costData)

	persistedSavings, loadErr := LoadPersistedGPUSavings(ctx, pool, orgID, clusterUUID)
	if loadErr != nil {
		log.Warnf("PersistGPUMIGRecommendationSets: load savings: %v", loadErr)
	}
	crossRefs, crossErr := LoadPersistedGPUTimeslicingCrossRefs(ctx, pool, orgID, clusterUUID)
	if crossErr != nil {
		log.Warnf("PersistGPUMIGRecommendationSets: load cross-refs: %v", crossErr)
	}

	writes := make([]pgrec.GPUMIGRow, 0, len(gpuRecs)*3)
	for key, recs := range gpuRecs {
		ns, wl, cn := key.Namespace, key.Workload, key.ContainerName
		nodeName := nodeMap[key]

		for _, rec := range recs {
			if rec == nil || !rec.HasMIGRecommendation() {
				continue
			}

			gpuIdle := string(rec.GPUIdleState)
			if gpuIdle == "" {
				gpuIdle = "active"
			}
			classification := string(rec.Classification)
			if classification == "" {
				classification = string(GPUClassNoProfiling)
			}

			var totalFB *int64
			if spec := MatchGPUModel(rec.GPUModelName); spec != nil {
				v := int64(spec.TotalFBMiB)
				totalFB = &v
			}

			savingsCents := int64(0)
			lookup := GPUSavingsLookupKey(ns, wl, cn, rec.Term)
			if cents, ok := persistedSavings[lookup]; ok && cents != nil {
				savingsCents = *cents
			} else if computed := ComputeGPUSavingsCents(rec, costData); computed != nil {
				savingsCents = *computed
			}

			wasteCents := int64(0)
			if rec.GPUIdleState != core.IdleStateActive && gpuMonthlyRate > 0 {
				wasteCents = money.USDToCents(gpuMonthlyRate)
			}

			notifCodes := rec.NotificationCodes
			if _, ok := crossRefs[lookup]; ok {
				notifCodes = appendUniqueInt16Slice(notifCodes, NotifGPUTimeSharingCandidate)
			}

			writes = append(writes, pgrec.GPUMIGRow{
				OrgID:               orgID,
				ClusterUUID:         clusterUUID,
				Namespace:           ns,
				Workload:            wl,
				ContainerName:       cn,
				NodeName:            nodeName,
				GPUModelName:        rec.GPUModelName,
				Term:                rec.Term,
				RecommendedProfile:  rec.RecommendedGPUProfile,
				CurrentProfile:      rec.CurrentGPUProfile,
				Classification:      classification,
				Confidence:          rec.Confidence,
				FBUsageMaxMiB:       rec.FBUsageMaxMiB,
				TotalFBMiB:          totalFB,
				GPUIdleState:        gpuIdle,
				GPUIdleSince:        rec.GPUIdleSince,
				GPUIdleDurationDays: rec.GPUIdleDurationDays,
				SavingsMicroCents:   savingsCents,
				WasteMicroCents:     wasteCents,
				NotificationCodes:   notifCodes,
				LastReported:        now,
			})
		}
	}

	if err := pgrec.WriteGPUMIGRecommendationSets(ctx, pool, writes); err != nil {
		return err
	}
	if len(writes) == 0 {
		return nil
	}
	log.Infof("PersistGPUMIGRecommendationSets: upserted %d rows for cluster %s", len(writes), clusterUUID)
	return nil
}

func appendUniqueInt16Slice(codes []int16, code int16) []int16 {
	for _, c := range codes {
		if c == code {
			return codes
		}
	}
	return append(codes, code)
}
