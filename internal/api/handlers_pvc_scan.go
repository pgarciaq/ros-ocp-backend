package api

import (
	"database/sql"

	"github.com/jackc/pgx/v5"
	"github.com/redhatinsights/ros-ocp-backend/internal/money"
	"github.com/redhatinsights/ros-ocp-backend/internal/notifications"
)

func scanPVCRecommendationRow(row pgx.Row) (PVCRecommendationResponse, error) {
	var r PVCRecommendationResponse
	var codes []int16
	var growth sql.NullInt64
	var savings sql.NullInt64
	if err := row.Scan(
		&r.ClusterUUID, &r.Namespace, &r.PersistentVolumeClaim, &r.PersistentVolume,
		&r.StorageClass, &r.CapacityBytes, &r.UsageBytesMax, &r.UsageRatio,
		&r.RecommendationType, &r.RecommendedBytes, &r.DaysToFull,
		&growth, &codes, &r.DataDays, &r.Term,
		&savings,
	); err != nil {
		return r, err
	}
	if growth.Valid {
		v := growth.Int64
		r.GrowthBytesPerDay = &v
	}
	if savings.Valid {
		r.EstimatedMonthlySavings = money.FormatCentsToSavingsPtr(&savings.Int64, money.DefaultCurrency)
	}
	r.Notifications = notifications.MapToKruizeFormat(codes)
	switch r.RecommendationType {
	case "oversized":
		r.ResizeNote = "Kubernetes does not support in-place PVC shrinking. Reducing this PVC requires creating a smaller volume, migrating data, and deleting the original."
	case "orphaned":
		r.ResizeNote = "This PVC has zero usage. If the data is no longer needed, deleting the PVC will reclaim the backing storage volume."
	}
	return r, nil
}

const pvcRecommendationSelectSQL = `
	SELECT cluster_uuid, namespace, persistentvolumeclaim, persistentvolume,
		storageclass, capacity_bytes, usage_bytes_max, usage_ratio,
		recommendation_type, recommended_bytes, days_to_full,
		growth_bytes_per_day, notification_codes, data_days, term,
		estimated_monthly_savings_usd`
