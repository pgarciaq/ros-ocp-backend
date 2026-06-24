package api

import (
	"database/sql"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/model"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

func scanPVCRecommendationRow(row pgx.Row, includeExplanation bool) (PVCRecommendationResponse, error) {
	var r PVCRecommendationResponse
	var codes []int16
	var growth sql.NullInt64
	var savings sql.NullInt64
	var idleSince sql.NullTime
	var idleDays sql.NullInt32
	var explDataDays sql.NullInt32
	var explOversized, explNearFull, explMultiplier, explMinGiB sql.NullInt32
	var explReason sql.NullString
	var updatedAt sql.NullTime
	if err := row.Scan(
		&r.ClusterUUID, &r.Namespace, &r.PersistentVolumeClaim, &r.MountedBy, &r.VMName, &r.PersistentVolume,
		&r.StorageClass, &r.CapacityBytes, &r.UsageBytesMax, &r.UsageRatio,
		&r.RecommendationType, &r.RecommendedBytes, &r.DaysToFull,
		&growth, &codes, &r.DataDays, &r.Term,
		&savings, &idleSince, &idleDays,
		&explDataDays, &explOversized, &explNearFull, &explMultiplier, &explMinGiB, &explReason,
		&updatedAt,
	); err != nil {
		return r, err
	}
	if growth.Valid {
		v := growth.Int64
		r.GrowthBytesPerDay = &v
	}
	if savings.Valid {
		r.EstimatedMonthlySavings = money.FormatCentsToAmountPtr(&savings.Int64, money.DefaultCurrency)
	}
	if idleSince.Valid {
		s := idleSince.Time.UTC().Format("2006-01-02")
		r.IdleSince = &s
	}
	if idleDays.Valid && idleDays.Int32 > 0 {
		d := int(idleDays.Int32)
		r.IdleDurationDays = &d
	}
	if updatedAt.Valid {
		r.LastReported = updatedAt.Time.UTC().Format(time.RFC3339)
	}
	r.Notifications = notifications.MapToKruizeFormat(codes)
	if includeExplanation {
		var dataDays *int
		if explDataDays.Valid {
			v := int(explDataDays.Int32)
			dataDays = &v
		} else if r.DataDays > 0 {
			dataDays = &r.DataDays
		}
		usageRatio := r.UsageRatio
		r.Explanation = model.BuildPVCExplanationAPI(
			nil, dataDays, &usageRatio, r.GrowthBytesPerDay,
			nullInt32Ptr(explOversized), nullInt32Ptr(explNearFull), nullInt32Ptr(explMultiplier), nullInt32Ptr(explMinGiB),
			nullStringPtr(explReason),
		)
	}
	switch r.RecommendationType {
	case "oversized":
		r.ResizeNote = "Kubernetes does not support in-place PVC shrinking. Reducing this PVC requires creating a smaller volume, migrating data, and deleting the original."
	case "orphaned":
		r.ResizeNote = "This PVC has zero usage. If the data is no longer needed, deleting the PVC will reclaim the backing storage volume."
	}
	if r.Count == 0 && r.ClusterUUID != "" && r.Namespace != "" && r.PersistentVolumeClaim != "" {
		r.ID = model.NativePvcID(r.ClusterUUID, r.Namespace, r.PersistentVolumeClaim)
	}
	return r, nil
}

const pvcRecommendationSelectSQL = `
	SELECT cluster_uuid, namespace, persistentvolumeclaim, last_seen_pod, vm_name, persistentvolume,
		storageclass, capacity_bytes, usage_bytes_max, usage_ratio,
		recommendation_type, recommended_bytes, days_to_full,
		growth_bytes_per_day, notification_codes, data_days, term,
		estimated_savings_cents, idle_since, idle_duration_days,
		expl_data_days, expl_oversized_threshold_bp, expl_near_full_threshold_bp,
		expl_recommended_size_multiplier, expl_min_recommended_gib, expl_classification_reason,
		updated_at`
