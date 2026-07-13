package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
)

// OldSnapshotRecommendation holds previous recommendation state read from
// snapshot_recommendation_sets before reconciliation removes adopted rows.
type OldSnapshotRecommendation struct {
	SnapshotName       string
	RecommendationType string
	UpdatedAt          time.Time
}

// ReadClusterOldSnapshotRecommendations fetches existing
// snapshot_recommendation_sets rows for a cluster before reconciliation.
func ReadClusterOldSnapshotRecommendations(
	ctx context.Context, pool *pgxpool.Pool,
	orgID, clusterUUID string,
) ([]OldSnapshotRecommendation, error) {
	rows, err := pool.Query(ctx, `
		SELECT snapshot_name, recommendation_type, updated_at
		FROM snapshot_recommendation_sets
		WHERE org_id = $1 AND cluster_uuid = $2`,
		orgID, clusterUUID)
	if err != nil {
		return nil, fmt.Errorf("ReadClusterOldSnapshotRecommendations: %w", err)
	}
	defer rows.Close()

	var result []OldSnapshotRecommendation
	for rows.Next() {
		var old OldSnapshotRecommendation
		if err := rows.Scan(&old.SnapshotName, &old.RecommendationType, &old.UpdatedAt); err != nil {
			return nil, fmt.Errorf("ReadClusterOldSnapshotRecommendations scan: %w", err)
		}
		result = append(result, old)
	}
	return result, rows.Err()
}

// ReadCurrentSnapshotInventoryNames reads the set of snapshot names currently
// present in snapshot_inventory for a cluster.
func ReadCurrentSnapshotInventoryNames(
	ctx context.Context, pool *pgxpool.Pool,
	orgID, clusterUUID string,
	freshHours int,
) (map[string]bool, error) {
	if freshHours <= 0 {
		freshHours = 48
	}
	rows, err := pool.Query(ctx, `
		SELECT DISTINCT snapshot_name
		FROM snapshot_inventory
		WHERE org_id = $1 AND cluster_uuid = $2
			AND ingested_at >= NOW() - ($3 * INTERVAL '1 hour')`,
		orgID, clusterUUID, freshHours)
	if err != nil {
		return nil, fmt.Errorf("ReadCurrentSnapshotInventoryNames: %w", err)
	}
	defer rows.Close()

	result := make(map[string]bool, 64)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("ReadCurrentSnapshotInventoryNames scan: %w", err)
		}
		result[name] = true
	}
	return result, rows.Err()
}

// DetectSnapshotAdoption determines which snapshots have been adopted (deleted
// by the user following a recommendation). A snapshot is considered adopted if
// it had a recommendation but no longer appears in current inventory.
func DetectSnapshotAdoption(currentInventory map[string]bool, oldRecs []OldSnapshotRecommendation) map[string]bool {
	adopted := make(map[string]bool)
	for _, rec := range oldRecs {
		if rec.RecommendationType == "active" {
			continue
		}
		if !currentInventory[rec.SnapshotName] {
			adopted[rec.SnapshotName] = true
		}
	}
	return adopted
}

// SnapshotQualityRow represents a row to be inserted into snapshot_recommendation_quality.
type SnapshotQualityRow struct {
	MeasuredAt           time.Time
	OrgID                string
	ClusterUUID          string
	SnapshotName         string
	AdoptionDetected     bool
	RecommendationAgeHrs int64
}

// WriteSnapshotQuality batch-inserts quality metrics into snapshot_recommendation_quality.
func WriteSnapshotQuality(ctx context.Context, pool *pgxpool.Pool, qualityRows []SnapshotQualityRow) error {
	if len(qualityRows) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, r := range qualityRows {
		batch.Queue(`
			INSERT INTO snapshot_recommendation_quality (
				measured_at, org_id, cluster_uuid, snapshot_name,
				adoption_detected, recommendation_age_hours
			) VALUES ($1, $2, $3, $4, $5, $6)
			ON CONFLICT (org_id, cluster_uuid, snapshot_name, measured_at)
			DO UPDATE SET
				adoption_detected = EXCLUDED.adoption_detected,
				recommendation_age_hours = EXCLUDED.recommendation_age_hours`,
			r.MeasuredAt, r.OrgID, r.ClusterUUID, r.SnapshotName,
			r.AdoptionDetected, r.RecommendationAgeHrs,
		)
	}

	br := pool.SendBatch(ctx, batch)
	defer br.Close()

	for i := 0; i < batch.Len(); i++ {
		if _, err := br.Exec(); err != nil {
			if IsPartitionMissing(err) {
				logging.GetLogger().Errorf("WriteSnapshotQuality: missing partition for snapshot_recommendation_quality: %v", err)
				return fmt.Errorf("partition missing for snapshot_recommendation_quality: %w", err)
			}
			return fmt.Errorf("WriteSnapshotQuality batch exec: %w", err)
		}
	}
	return nil
}

// BuildSnapshotQualityRows constructs quality rows for all snapshots that have
// recommendations. Snapshots that disappeared from inventory are marked adopted.
func BuildSnapshotQualityRows(
	orgID, clusterUUID string,
	oldRecs []OldSnapshotRecommendation,
	currentInventory map[string]bool,
) []SnapshotQualityRow {
	if len(oldRecs) == 0 {
		return nil
	}

	nowClock := time.Now().UTC()
	measuredAt := time.Date(nowClock.Year(), nowClock.Month(), nowClock.Day(), 0, 0, 0, 0, time.UTC)
	adopted := DetectSnapshotAdoption(currentInventory, oldRecs)

	seen := map[string]bool{}
	var rows []SnapshotQualityRow

	for _, rec := range oldRecs {
		if rec.RecommendationType == "active" {
			continue
		}
		if seen[rec.SnapshotName] {
			continue
		}
		seen[rec.SnapshotName] = true

		ageHours := ComputeRecommendationAgeHours(rec.UpdatedAt, nowClock)

		rows = append(rows, SnapshotQualityRow{
			MeasuredAt:           measuredAt,
			OrgID:                orgID,
			ClusterUUID:          clusterUUID,
			SnapshotName:         rec.SnapshotName,
			AdoptionDetected:     adopted[rec.SnapshotName],
			RecommendationAgeHrs: ageHours,
		})
	}
	return rows
}
