package api

import (
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

const snapshotRecommendationSelectSQL = `
	SELECT cluster_uuid, namespace, snapshot_name, source_pvc_name,
		volume_snapshot_class, storageclass, creation_timestamp,
		restore_size_bytes, age_days, source_pvc_exists, restored_pvc_count,
		managed_by, recommendation_type, estimated_cost_cents,
		notification_codes, updated_at`

func scanSnapshotRecommendationRow(row pgx.Row) (SnapshotRecommendationResponse, error) {
	var r SnapshotRecommendationResponse
	var codes []int16
	var costCents sql.NullInt64
	var creationTS time.Time
	var updatedAt time.Time
	if err := row.Scan(
		&r.ClusterUUID, &r.Namespace, &r.SnapshotName, &r.SourcePVCName,
		&r.VolumeSnapshotClass, &r.StorageClass, &creationTS,
		&r.RestoreSizeBytes, &r.AgeDays, &r.SourcePVCExists, &r.RestoredPVCCount,
		&r.ManagedBy, &r.RecommendationType, &costCents,
		&codes, &updatedAt,
	); err != nil {
		return r, err
	}
	r.CreationTimestamp = creationTS.UTC().Format(time.RFC3339)
	r.LastReported = updatedAt.UTC().Format(time.RFC3339)
	if costCents.Valid {
		r.EstimatedMonthlyCost = money.FormatCentsToAmountPtr(&costCents.Int64, money.DefaultCurrency)
	}
	r.Notifications = notifications.MapToKruizeFormat(codes)
	return r, nil
}
