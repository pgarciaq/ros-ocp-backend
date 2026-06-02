package engine

// ValidateSnapshotSettingsUpdate checks incoming PUT fields against allowed ranges.
func ValidateSnapshotSettingsUpdate(update SnapshotSettingsUpdate) error {
	v := fieldValidator{}

	if update.OrphanAgeDays != nil {
		v.addRangeInt("orphan_age_days", *update.OrphanAgeDays, 1, 3650)
	}
	if update.NeverRestoredDays != nil {
		v.addRangeInt("never_restored_days", *update.NeverRestoredDays, 1, 3650)
	}
	if update.StaleDays != nil {
		v.addRangeInt("stale_days", *update.StaleDays, 1, 3650)
	}
	if update.RedundantThreshold != nil {
		v.addRangeInt("redundant_threshold", *update.RedundantThreshold, 1, 100)
	}
	if update.CostPerGiBMonthUSD != nil {
		v.addRangeFloat("cost_per_gib_month_usd", *update.CostPerGiBMonthUSD, 0, 1000)
	}
	if update.InventoryFreshHours != nil {
		v.addRangeInt("inventory_fresh_hours", *update.InventoryFreshHours, 1, 168)
	}

	return v.result()
}
