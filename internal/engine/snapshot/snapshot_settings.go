package snapshot

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/costdata"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
)

func init() {
	engine.RegisterSettingsResolver("snapshot", func(ctx context.Context, pool *pgxpool.Pool, orgID string) (any, error) {
		return ResolveSnapshotSettings(ctx, pool, orgID, nil)
	})
}

const StorageGBUsagePerMonthMetric = "storage_gb_usage_per_month"

// SnapshotSettingsDefaults are the compiled-in defaults.
var SnapshotSettingsDefaults = SnapshotSettings{
	OrphanAgeDays:       7,
	NeverRestoredDays:   30,
	StaleDays:           90,
	RedundantThreshold:  3,
	CostPerGiBMonth:     0.05,
	InventoryFreshHours: 6,
}

// SnapshotSettingsRow is the DB shape for the snapshot_settings table.
type SnapshotSettingsRow struct {
	OrphanAgeDays       int     `json:"orphan_age_days"`
	NeverRestoredDays   int     `json:"never_restored_days"`
	StaleDays           int     `json:"stale_days"`
	RedundantThreshold  int     `json:"redundant_threshold"`
	CostPerGiBMonthUSD  float64 `json:"cost_per_gib_month_usd"`
	InventoryFreshHours int     `json:"inventory_fresh_hours"`
}

// SnapshotSettingsResponse is the API GET response.
type SnapshotSettingsResponse struct {
	OrphanAgeDays       int      `json:"orphan_age_days"`
	NeverRestoredDays   int      `json:"never_restored_days"`
	StaleDays           int      `json:"stale_days"`
	RedundantThreshold  int      `json:"redundant_threshold"`
	CostPerGiBMonthUSD  float64  `json:"cost_per_gib_month_usd"`
	InventoryFreshHours int      `json:"inventory_fresh_hours"`
	LockedFields        []string `json:"locked_fields"`
	SettingsLocked      bool     `json:"settings_locked,omitempty"`
}

// SnapshotSettingsUpdate is the API PUT request body.
type SnapshotSettingsUpdate struct {
	OrphanAgeDays       *int     `json:"orphan_age_days"`
	NeverRestoredDays   *int     `json:"never_restored_days"`
	StaleDays           *int     `json:"stale_days"`
	RedundantThreshold  *int     `json:"redundant_threshold"`
	CostPerGiBMonthUSD  *float64 `json:"cost_per_gib_month_usd"`
	InventoryFreshHours *int     `json:"inventory_fresh_hours"`
}

// envLockMap maps env variable names to JSON field names.
var envLockMap = map[string]string{
	"ROS_SNAPSHOT_ORPHAN_AGE_DAYS":          "orphan_age_days",
	"ROS_SNAPSHOT_NEVER_RESTORED_DAYS":        "never_restored_days",
	"ROS_SNAPSHOT_STALE_DAYS":                 "stale_days",
	"ROS_SNAPSHOT_REDUNDANT_THRESHOLD":      "redundant_threshold",
	"ROS_SNAPSHOT_COST_PER_GIB_MONTH":       "cost_per_gib_month_usd",
	"ROS_SNAPSHOT_INVENTORY_FRESH_HOURS":    "inventory_fresh_hours",
}

// GetLockedFields returns the list of fields locked by environment variables.
func GetLockedFields() []string {
	locked := make([]string, 0)
	for envKey, fieldName := range envLockMap {
		if _, ok := os.LookupEnv(envKey); ok {
			locked = append(locked, fieldName)
		}
	}
	return locked
}

// IsFieldLocked returns true if the given JSON field name is locked by an env var.
func IsFieldLocked(field string) bool {
	for _, envKey := range []string{
		"ROS_SNAPSHOT_ORPHAN_AGE_DAYS",
		"ROS_SNAPSHOT_NEVER_RESTORED_DAYS",
		"ROS_SNAPSHOT_STALE_DAYS",
		"ROS_SNAPSHOT_REDUNDANT_THRESHOLD",
		"ROS_SNAPSHOT_COST_PER_GIB_MONTH",
		"ROS_SNAPSHOT_INVENTORY_FRESH_HOURS",
	} {
		if envLockMap[envKey] == field {
			if _, ok := os.LookupEnv(envKey); ok {
				return true
			}
		}
	}
	return false
}

// ResolveSnapshotSettings resolves snapshot classification settings.
//
// Threshold fields (orphan/stale/etc.) use: env variable (locked) → DB → compiled default.
//
// CostPerGiBMonth uses a separate priority when costData is provided (ingestion path):
//  1. Per-org DB setting (user explicitly configured via Settings API)
//  2. ROS_SNAPSHOT_COST_PER_GIB_MONTH env var (admin override)
//  3. storage_gb_usage_per_month from effective-rates (infra + supplementary sum)
//  4. Compiled default ($0.05/GiB/month)
//
// When costData is nil (Settings API GET/PUT), step 3 is skipped.
func ResolveSnapshotSettings(
	ctx context.Context,
	pool *pgxpool.Pool,
	orgID string,
	costData *costdata.ClusterCostData,
) (SnapshotSettings, error) {
	cfg := config.GetConfig()

	// Start with compiled-in defaults
	result := SnapshotSettingsDefaults

	// Override with DB values if a row exists (skipped when platform settings lock is active).
	var dbRow SnapshotSettingsRow
	dbHasRow := false
	if !engine.IsSettingsLocked("snapshot") {
		err := pool.QueryRow(ctx, `
			SELECT orphan_age_days, never_restored_days, stale_days,
				redundant_threshold, cost_per_gib_month_usd, inventory_fresh_hours
			FROM snapshot_settings WHERE org_id = $1`, orgID,
		).Scan(&dbRow.OrphanAgeDays, &dbRow.NeverRestoredDays, &dbRow.StaleDays,
			&dbRow.RedundantThreshold, &dbRow.CostPerGiBMonthUSD, &dbRow.InventoryFreshHours)

		if err == nil {
			dbHasRow = true
			result.OrphanAgeDays = dbRow.OrphanAgeDays
			result.NeverRestoredDays = dbRow.NeverRestoredDays
			result.StaleDays = dbRow.StaleDays
			result.RedundantThreshold = dbRow.RedundantThreshold
			result.InventoryFreshHours = dbRow.InventoryFreshHours
		} else if err != pgx.ErrNoRows {
			return result, fmt.Errorf("querying snapshot settings: %w", err)
		}
	}

	result.CostPerGiBMonth = ResolveCostPerGiBMonth(cfg, dbHasRow, dbRow.CostPerGiBMonthUSD, costData)

	// Override with env variables (highest priority — locked)
	if cfg.SnapshotOrphanAgeDays != 0 {
		if _, ok := os.LookupEnv("ROS_SNAPSHOT_ORPHAN_AGE_DAYS"); ok {
			result.OrphanAgeDays = cfg.SnapshotOrphanAgeDays
		}
	}
	if cfg.SnapshotNeverRestoredDays != 0 {
		if _, ok := os.LookupEnv("ROS_SNAPSHOT_NEVER_RESTORED_DAYS"); ok {
			result.NeverRestoredDays = cfg.SnapshotNeverRestoredDays
		}
	}
	if cfg.SnapshotStaleDays != 0 {
		if _, ok := os.LookupEnv("ROS_SNAPSHOT_STALE_DAYS"); ok {
			result.StaleDays = cfg.SnapshotStaleDays
		}
	}
	if cfg.SnapshotRedundantThreshold != 0 {
		if _, ok := os.LookupEnv("ROS_SNAPSHOT_REDUNDANT_THRESHOLD"); ok {
			result.RedundantThreshold = cfg.SnapshotRedundantThreshold
		}
	}
	if cfg.SnapshotInventoryFreshHours > 0 {
		if _, ok := os.LookupEnv("ROS_SNAPSHOT_INVENTORY_FRESH_HOURS"); ok {
			result.InventoryFreshHours = cfg.SnapshotInventoryFreshHours
		}
	}

	return result, nil
}

// ResolveCostPerGiBMonth resolves the snapshot cost rate for ingestion/classification.
// See ResolveSnapshotSettings for the priority chain.
func ResolveCostPerGiBMonth(
	cfg *config.Config,
	dbHasRow bool,
	dbValue float64,
	costData *costdata.ClusterCostData,
) float64 {
	if dbHasRow {
		return dbValue
	}
	if _, ok := os.LookupEnv("ROS_SNAPSHOT_COST_PER_GIB_MONTH"); ok {
		return cfg.SnapshotCostPerGiBMonth
	}
	if rate, ok := StorageGBUsageRateFromCostData(costData); ok {
		return rate
	}
	return SnapshotSettingsDefaults.CostPerGiBMonth
}

// StorageGBUsageRateFromCostData returns the sum of infrastructure and supplementary
// rates for storage_gb_usage_per_month from Koku effective-rates configured_rates.
func StorageGBUsageRateFromCostData(costData *costdata.ClusterCostData) (float64, bool) {
	if costData == nil || costData.ConfiguredRates == nil {
		return 0, false
	}
	pair, ok := costData.ConfiguredRates[StorageGBUsagePerMonthMetric]
	if !ok {
		return 0, false
	}
	sum := pair.Infrastructure + pair.Supplementary
	if sum <= 0 {
		return 0, false
	}
	return sum, true
}

// GetSnapshotSettingsForAPI resolves settings and adds locked_fields for the GET response.
func GetSnapshotSettingsForAPI(ctx context.Context, pool *pgxpool.Pool, orgID string) (SnapshotSettingsResponse, error) {
	settings, err := ResolveSnapshotSettings(ctx, pool, orgID, nil)
	if err != nil {
		return SnapshotSettingsResponse{}, err
	}
	return SnapshotSettingsResponse{
		OrphanAgeDays:       settings.OrphanAgeDays,
		NeverRestoredDays:   settings.NeverRestoredDays,
		StaleDays:           settings.StaleDays,
		RedundantThreshold:  settings.RedundantThreshold,
		CostPerGiBMonthUSD:  settings.CostPerGiBMonth,
		InventoryFreshHours: settings.InventoryFreshHours,
		LockedFields:        engine.LockedFieldsForAPI("snapshot", GetLockedFields()),
		SettingsLocked:      engine.IsSettingsLocked("snapshot"),
	}, nil
}

// DeleteSnapshotSettings removes tenant snapshot settings overrides.
func DeleteSnapshotSettings(ctx context.Context, pool *pgxpool.Pool, orgID string) error {
	_, err := pool.Exec(ctx, `DELETE FROM snapshot_settings WHERE org_id = $1`, orgID)
	if err != nil {
		return fmt.Errorf("delete snapshot settings: %w", err)
	}
	return nil
}

// UpdateSnapshotSettings applies a partial update to the org's settings.
// Returns an error if any requested field is locked by an env variable.
func UpdateSnapshotSettings(ctx context.Context, pool *pgxpool.Pool, orgID string, update SnapshotSettingsUpdate) error {
	if err := ValidateSnapshotSettingsUpdate(update); err != nil {
		return err
	}

	// Check for locked fields
	lockedAttempts := lockedFieldsInUpdate(update)
	if len(lockedAttempts) > 0 {
		return fmt.Errorf("%w: %v", engine.ErrFieldsLocked, lockedAttempts)
	}

	// Resolve current settings to fill in missing values
	current, err := ResolveSnapshotSettings(ctx, pool, orgID, nil)
	if err != nil {
		return err
	}

	// Apply partial update
	if update.OrphanAgeDays != nil {
		current.OrphanAgeDays = *update.OrphanAgeDays
	}
	if update.NeverRestoredDays != nil {
		current.NeverRestoredDays = *update.NeverRestoredDays
	}
	if update.StaleDays != nil {
		current.StaleDays = *update.StaleDays
	}
	if update.RedundantThreshold != nil {
		current.RedundantThreshold = *update.RedundantThreshold
	}
	if update.CostPerGiBMonthUSD != nil {
		current.CostPerGiBMonth = *update.CostPerGiBMonthUSD
	}
	if update.InventoryFreshHours != nil {
		current.InventoryFreshHours = *update.InventoryFreshHours
	}

	_, err = pool.Exec(ctx, `
		INSERT INTO snapshot_settings (
			org_id, orphan_age_days, never_restored_days, stale_days,
			redundant_threshold, cost_per_gib_month_usd, inventory_fresh_hours, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (org_id)
		DO UPDATE SET
			orphan_age_days = EXCLUDED.orphan_age_days,
			never_restored_days = EXCLUDED.never_restored_days,
			stale_days = EXCLUDED.stale_days,
			redundant_threshold = EXCLUDED.redundant_threshold,
			cost_per_gib_month_usd = EXCLUDED.cost_per_gib_month_usd,
			inventory_fresh_hours = EXCLUDED.inventory_fresh_hours,
			updated_at = NOW()`,
		orgID, current.OrphanAgeDays, current.NeverRestoredDays, current.StaleDays,
		current.RedundantThreshold, current.CostPerGiBMonth, current.InventoryFreshHours,
	)
	if err != nil {
		return fmt.Errorf("upserting snapshot settings: %w", err)
	}
	return nil
}

func lockedFieldsInUpdate(update SnapshotSettingsUpdate) []string {
	var locked []string
	if update.OrphanAgeDays != nil && IsFieldLocked("orphan_age_days") {
		locked = append(locked, "orphan_age_days")
	}
	if update.NeverRestoredDays != nil && IsFieldLocked("never_restored_days") {
		locked = append(locked, "never_restored_days")
	}
	if update.StaleDays != nil && IsFieldLocked("stale_days") {
		locked = append(locked, "stale_days")
	}
	if update.RedundantThreshold != nil && IsFieldLocked("redundant_threshold") {
		locked = append(locked, "redundant_threshold")
	}
	if update.CostPerGiBMonthUSD != nil && IsFieldLocked("cost_per_gib_month_usd") {
		locked = append(locked, "cost_per_gib_month_usd")
	}
	if update.InventoryFreshHours != nil && IsFieldLocked("inventory_fresh_hours") {
		locked = append(locked, "inventory_fresh_hours")
	}
	return locked
}
