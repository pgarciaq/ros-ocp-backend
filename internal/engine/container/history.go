package container

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/librobne/pgrec"
)

// resolveHistoryHCMap loads the window association map for one org+cluster.
// Unreadable state degrades to empty (all rows unassociated ''), never an error.
func resolveHistoryHCMap(ctx context.Context, pool *pgxpool.Pool, orgID, clusterUUID string) map[string]string {
	maxLookbackDays := config.GetConfig().MaxLookbackDays
	if maxLookbackDays <= 0 {
		maxLookbackDays = 14
	}
	snaps, err := pgrec.LoadHCPSnapshotsForRun(ctx, pool, orgID, clusterUUID, time.Now().UTC().AddDate(0, 0, -maxLookbackDays))
	if err != nil {
		logging.GetLogger().Warnf("history: unable to load hcp snapshots (all rows unassociated): %v", err)
		return map[string]string{}
	}
	return pgrec.ResolveHCAssociation(snaps)
}

// WriteRecommendationHistory batch-inserts recommendation snapshots into
// recommendation_history for trend analysis and audit. Each ContainerRec
// produces one row keyed by (container, term, engine, recorded_at).
func WriteRecommendationHistory(ctx context.Context, pool *pgxpool.Pool, recs []core.ContainerRec, sourceBinary string) error {
	if len(recs) == 0 {
		return nil
	}

	nowClock := time.Now().UTC()
	recordedAt := time.Date(nowClock.Year(), nowClock.Month(), nowClock.Day(), 0, 0, 0, 0, time.UTC)

	// Freeze the HC ID per row (#623): resolve once per org+cluster, never
	// per row. Batches are single-cluster by construction (one recommend run
	// per cluster); grouping keeps a mixed batch from misattributing.
	hcByCluster := make(map[[2]string]map[string]string)
	hcFor := func(orgID, clusterUUID, namespace string) string {
		key := [2]string{orgID, clusterUUID}
		m, ok := hcByCluster[key]
		if !ok {
			m = resolveHistoryHCMap(ctx, pool, orgID, clusterUUID)
			hcByCluster[key] = m
		}
		return m[namespace]
	}

	for chunkStart := 0; chunkStart < len(recs); chunkStart += db.MaxPgxBatchQueue {
		if err := ctx.Err(); err != nil {
			return err
		}
		chunkEnd := min(chunkStart+db.MaxPgxBatchQueue, len(recs))
		chunk := recs[chunkStart:chunkEnd]
		batch := &pgx.Batch{}

		for _, r := range chunk {
			batch.Queue(`
			INSERT INTO recommendation_history (
				recorded_at, org_id, cluster_uuid, namespace, workload, workload_type, container_name,
				term, engine,
				rec_cpu_request_millicores, rec_cpu_limit_millicores,
				rec_memory_request_kib, rec_memory_limit_kib,
				notification_codes, confidence_level,
				estimated_savings_cents, source_binary, hosted_cluster_id,`+core.ContainerExplSQLColumns+`
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,`+core.ContainerExplValuePlaceholders(19)+`)
			ON CONFLICT (org_id, cluster_uuid, namespace, workload, workload_type, container_name, term, engine, recorded_at, hosted_cluster_id)
			DO UPDATE SET
				rec_cpu_request_millicores = EXCLUDED.rec_cpu_request_millicores,
				rec_cpu_limit_millicores = EXCLUDED.rec_cpu_limit_millicores,
				rec_memory_request_kib = EXCLUDED.rec_memory_request_kib,
				rec_memory_limit_kib = EXCLUDED.rec_memory_limit_kib,
				notification_codes = EXCLUDED.notification_codes,
				confidence_level = EXCLUDED.confidence_level,
				estimated_savings_cents = EXCLUDED.estimated_savings_cents,
				source_binary = EXCLUDED.source_binary,`+core.ContainerExplUpdateSet+``,
				core.AppendContainerExplArgs([]any{
					recordedAt, r.OrgID, r.ClusterUUID, r.Namespace, r.Workload, r.WorkloadType, r.ContainerName,
					r.Term, r.Engine,
					r.RecCPURequestMC, r.RecCPULimitMC,
					r.RecMemRequestKiB, r.RecMemLimitKiB,
					r.NotificationCodes, r.ConfidenceLevel,
					r.EstimatedSavingsCents, sourceBinary, hcFor(r.OrgID, r.ClusterUUID, r.Namespace),
				}, r.Expl)...,
			)
		}

		br := pool.SendBatch(ctx, batch)
		for range chunk {
			if _, err := br.Exec(); err != nil {
				br.Close()
				if pgrec.IsUndefinedTable(err) {
					// Pre-migration database (code ahead of 000200):
					// degrade to unwritten history, never fail the run.
					logging.GetLogger().Warnf("history: hosted_cluster_id unavailable (pre-migration, skipping): %v", err)
					return nil
				}
				return fmt.Errorf("WriteRecommendationHistory batch exec: %w", err)
			}
		}
		if err := br.Close(); err != nil {
			return fmt.Errorf("WriteRecommendationHistory batch close: %w", err)
		}
	}
	return nil
}
