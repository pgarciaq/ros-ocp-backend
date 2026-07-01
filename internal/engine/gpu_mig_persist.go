package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

const gpuMIGRecSetUpsertSQL = `
INSERT INTO gpu_mig_recommendation_sets (
	org_id, cluster_uuid, namespace, workload, workload_type,
	container_name, node_name, gpu_model_name, term,
	recommended_gpu_profile, current_gpu_profile,
	gpu_classification, confidence, fb_usage_max_mib, total_fb_mib,
	gpu_idle_state, gpu_idle_since, gpu_idle_duration_days,
	savings_micro_cents, waste_micro_cents,
	category, idle_state, notification_codes,
	last_reported, updated_at
) VALUES (
	$1, $2::uuid, $3, $4, $5,
	$6, $7, $8, $9,
	$10, $11,
	$12, $13, $14, $15,
	$16, $17, $18,
	$19, $20,
	$21, $22, $23,
	$24, $24
)
ON CONFLICT (org_id, cluster_uuid, namespace, workload, container_name, term)
DO UPDATE SET
	workload_type           = EXCLUDED.workload_type,
	node_name               = EXCLUDED.node_name,
	gpu_model_name          = EXCLUDED.gpu_model_name,
	recommended_gpu_profile = EXCLUDED.recommended_gpu_profile,
	current_gpu_profile     = EXCLUDED.current_gpu_profile,
	gpu_classification      = EXCLUDED.gpu_classification,
	confidence              = EXCLUDED.confidence,
	fb_usage_max_mib        = EXCLUDED.fb_usage_max_mib,
	total_fb_mib            = EXCLUDED.total_fb_mib,
	gpu_idle_state          = EXCLUDED.gpu_idle_state,
	gpu_idle_since          = EXCLUDED.gpu_idle_since,
	gpu_idle_duration_days  = EXCLUDED.gpu_idle_duration_days,
	savings_micro_cents     = EXCLUDED.savings_micro_cents,
	waste_micro_cents       = EXCLUDED.waste_micro_cents,
	category                = EXCLUDED.category,
	idle_state              = EXCLUDED.idle_state,
	notification_codes      = EXCLUDED.notification_codes,
	last_reported           = EXCLUDED.last_reported,
	updated_at              = EXCLUDED.updated_at
`

type gpuMIGRecSetWrite struct {
	orgID                string
	clusterUUID          string
	namespace            string
	workload             string
	workloadType         string
	containerName        string
	nodeName             string
	gpuModelName         string
	term                 string
	recommendedProfile   string
	currentProfile       string
	classification       string
	confidence           float32
	fbUsageMaxMiB        float32
	totalFBMiB           *int64
	gpuIdleState         string
	gpuIdleSince         *time.Time
	gpuIdleDurationDays  int
	savingsMicroCents    int64
	wasteMicroCents      int64
	category             string
	idleState            string
	notificationCodes    []int16
	lastReported         time.Time
}

// PersistGPUMIGRecommendationSets denormalizes the per-container MIG
// recommendation view and writes it to gpu_mig_recommendation_sets. This runs
// in the background engine cycle after StoreGPUClassifications.
func PersistGPUMIGRecommendationSets(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID, clusterUUID string,
	terms []TermConfig,
	costData *costdata.ClusterCostData,
) error {
	log := logging.GetLogger()

	now := time.Now().UTC()
	start := now.AddDate(0, 0, -MaxWindowDays(terms, 30))

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

	var writes []gpuMIGRecSetWrite
	for key, recs := range gpuRecs {
		parts := strings.SplitN(key, "/", 3)
		if len(parts) != 3 {
			continue
		}
		ns, wl, cn := parts[0], parts[1], parts[2]
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
			if rec.GPUIdleState != IdleStateActive && gpuMonthlyRate > 0 {
				wasteCents = money.USDToCents(gpuMonthlyRate)
			}

			notifCodes := rec.NotificationCodes
			if ref, ok := crossRefs[lookup]; ok {
				_ = ref
				notifCodes = appendUniqueInt16Slice(notifCodes, NotifGPUTimeSharingCandidate)
			}

			writes = append(writes, gpuMIGRecSetWrite{
				orgID:               orgID,
				clusterUUID:         clusterUUID,
				namespace:           ns,
				workload:            wl,
				containerName:       cn,
				nodeName:            nodeName,
				gpuModelName:        rec.GPUModelName,
				term:                rec.Term,
				recommendedProfile:  rec.RecommendedGPUProfile,
				currentProfile:      rec.CurrentGPUProfile,
				classification:      classification,
				confidence:          rec.Confidence,
				fbUsageMaxMiB:       rec.FBUsageMaxMiB,
				totalFBMiB:          totalFB,
				gpuIdleState:        gpuIdle,
				gpuIdleSince:        rec.GPUIdleSince,
				gpuIdleDurationDays: rec.GPUIdleDurationDays,
				savingsMicroCents:   savingsCents,
				wasteMicroCents:     wasteCents,
				notificationCodes:   notifCodes,
				lastReported:        now,
			})
		}
	}

	if len(writes) == 0 {
		return nil
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("PersistGPUMIGRecommendationSets: begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	for chunkStart := 0; chunkStart < len(writes); chunkStart += maxPgxBatchQueue {
		chunkEnd := chunkStart + maxPgxBatchQueue
		if chunkEnd > len(writes) {
			chunkEnd = len(writes)
		}
		chunk := writes[chunkStart:chunkEnd]
		batch := &pgx.Batch{}
		for _, w := range chunk {
			batch.Queue(gpuMIGRecSetUpsertSQL,
				w.orgID, w.clusterUUID, w.namespace, w.workload, w.workloadType,
				w.containerName, w.nodeName, w.gpuModelName, w.term,
				w.recommendedProfile, w.currentProfile,
				w.classification, w.confidence, w.fbUsageMaxMiB, w.totalFBMiB,
				w.gpuIdleState, w.gpuIdleSince, w.gpuIdleDurationDays,
				w.savingsMicroCents, w.wasteMicroCents,
				w.category, w.idleState, w.notificationCodes,
				w.lastReported,
			)
		}
		br := tx.SendBatch(ctx, batch)
		for i := 0; i < batch.Len(); i++ {
			if _, err := br.Exec(); err != nil {
				br.Close()
				return fmt.Errorf("PersistGPUMIGRecommendationSets batch row %d: %w", chunkStart+i, err)
			}
		}
		br.Close()
	}

	log.Infof("PersistGPUMIGRecommendationSets: upserted %d rows for cluster %s", len(writes), clusterUUID)
	return tx.Commit(ctx)
}

func appendUniqueInt16Slice(codes []int16, code int16) []int16 {
	for _, c := range codes {
		if c == code {
			return codes
		}
	}
	return append(codes, code)
}
