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
	var idleSince sql.NullTime
	var idleDays sql.NullInt32
	if err := row.Scan(
		&r.ClusterUUID, &r.Namespace, &r.PersistentVolumeClaim, &r.MountedBy, &r.VMName, &r.PersistentVolume,
		&r.StorageClass, &r.CapacityBytes, &r.UsageBytesMax, &r.UsageRatio,
		&r.RecommendationType, &r.RecommendedBytes, &r.DaysToFull,
		&growth, &codes, &r.DataDays, &r.Term,
		&savings, &idleSince, &idleDays,
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
	SELECT cluster_uuid, namespace, persistentvolumeclaim, last_seen_pod, vm_name, persistentvolume,
		storageclass, capacity_bytes, usage_bytes_max, usage_ratio,
		recommendation_type, recommended_bytes, days_to_full,
		growth_bytes_per_day, notification_codes, data_days, term,
		estimated_savings_cents, idle_since, idle_duration_days`
