package api

import (
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

const snapshotRecommendationSelectSQL = `
	SELECT cluster_uuid, namespace, snapshot_name, source_pvc_name,
		volume_snapshot_class, storageclass, creation_timestamp,
		restore_size_bytes, age_days, source_pvc_exists, restored_pvc_count,
		managed_by, recommendation_type, estimated_cost_cents,
		notification_codes, updated_at,
		expl_threshold_used, expl_threshold_name, expl_classification_rule`

func scanSnapshotRecommendationRow(row pgx.Row, includeExplanation bool) (SnapshotRecommendationResponse, error) {
	var r SnapshotRecommendationResponse
	var codes []int16
	var costCents sql.NullInt64
	var creationTS time.Time
	var updatedAt time.Time
	var explThresholdUsed sql.NullInt32
	var explThresholdName, explClassificationRule sql.NullString
	if err := row.Scan(
		&r.ClusterUUID, &r.Namespace, &r.SnapshotName, &r.SourcePVCName,
		&r.VolumeSnapshotClass, &r.StorageClass, &creationTS,
		&r.RestoreSizeBytes, &r.AgeDays, &r.SourcePVCExists, &r.RestoredPVCCount,
		&r.ManagedBy, &r.RecommendationType, &costCents,
		&codes, &updatedAt,
		&explThresholdUsed, &explThresholdName, &explClassificationRule,
	); err != nil {
		return r, err
	}
	r.CreationTimestamp = creationTS.UTC().Format(time.RFC3339)
	r.LastReported = updatedAt.UTC().Format(time.RFC3339)
	if costCents.Valid {
		r.EstimatedMonthlyCost = money.FormatCentsToAmountPtr(&costCents.Int64, money.DefaultCurrency)
	}
	r.Notifications = notifications.MapToKruizeFormat(codes)
	if includeExplanation {
		ageDays := r.AgeDays
		restored := r.RestoredPVCCount
		sourceExists := r.SourcePVCExists
		managedBy := r.ManagedBy
		recType := r.RecommendationType
		var thresholdUsed *int
		if explThresholdUsed.Valid {
			v := int(explThresholdUsed.Int32)
			thresholdUsed = &v
		}
		var thresholdName, rule *string
		if explThresholdName.Valid {
			thresholdName = &explThresholdName.String
		}
		if explClassificationRule.Valid {
			rule = &explClassificationRule.String
		}
		var managedByPtr *string
		if managedBy != "" {
			managedByPtr = &managedBy
		}
		r.Explanation = model.BuildSnapshotExplanationAPI(
			&ageDays, &restored, thresholdUsed,
			&sourceExists, managedByPtr, &recType, thresholdName, rule,
		)
	}
	return r, nil
}
