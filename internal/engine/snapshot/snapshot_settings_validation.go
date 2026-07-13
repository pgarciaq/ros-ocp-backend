package snapshot

import "github.com/redhatinsights/ros-ocp-backend/internal/engine"

// ValidateSnapshotSettingsUpdate checks incoming PUT fields against allowed ranges.
func ValidateSnapshotSettingsUpdate(update SnapshotSettingsUpdate) error {
	v := engine.FieldValidator{}

	if update.OrphanAgeDays != nil {
		v.AddRangeInt("orphan_age_days", *update.OrphanAgeDays, 1, 3650)
	}
	if update.NeverRestoredDays != nil {
		v.AddRangeInt("never_restored_days", *update.NeverRestoredDays, 1, 3650)
	}
	if update.StaleDays != nil {
		v.AddRangeInt("stale_days", *update.StaleDays, 1, 3650)
	}
	if update.RedundantThreshold != nil {
		v.AddRangeInt("redundant_threshold", *update.RedundantThreshold, 1, 100)
	}
	if update.CostPerGiBMonthUSD != nil {
		v.AddRangeFloat("cost_per_gib_month_usd", *update.CostPerGiBMonthUSD, 0, 1000)
	}
	if update.InventoryFreshHours != nil {
		v.AddRangeInt("inventory_fresh_hours", *update.InventoryFreshHours, 1, 168)
	}

	return v.Result()
}
