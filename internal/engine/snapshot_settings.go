package engine

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

// SnapshotSettingsDefaults are the compiled-in defaults.
var SnapshotSettingsDefaults = SnapshotSettings{
	OrphanAgeDays:      7,
	NeverRestoredDays:  30,
	StaleDays:          90,
	RedundantThreshold: 3,
	CostPerGiBMonth:    0.05,
}

// SnapshotSettingsRow is the DB shape for the snapshot_settings table.
type SnapshotSettingsRow struct {
	OrphanAgeDays      int     `json:"orphan_age_days"`
	NeverRestoredDays  int     `json:"never_restored_days"`
	StaleDays          int     `json:"stale_days"`
	RedundantThreshold int     `json:"redundant_threshold"`
	CostPerGiBMonthUSD float64 `json:"cost_per_gib_month_usd"`
}

// SnapshotSettingsResponse is the API GET response.
type SnapshotSettingsResponse struct {
	OrphanAgeDays      int      `json:"orphan_age_days"`
	NeverRestoredDays  int      `json:"never_restored_days"`
	StaleDays          int      `json:"stale_days"`
	RedundantThreshold int      `json:"redundant_threshold"`
	CostPerGiBMonthUSD float64  `json:"cost_per_gib_month_usd"`
	LockedFields       []string `json:"locked_fields"`
}

// SnapshotSettingsUpdate is the API PUT request body.
type SnapshotSettingsUpdate struct {
	OrphanAgeDays      *int     `json:"orphan_age_days"`
	NeverRestoredDays  *int     `json:"never_restored_days"`
	StaleDays          *int     `json:"stale_days"`
	RedundantThreshold *int     `json:"redundant_threshold"`
	CostPerGiBMonthUSD *float64 `json:"cost_per_gib_month_usd"`
}

// envLockMap maps env variable names to JSON field names.
var envLockMap = map[string]string{
	"ROS_SNAPSHOT_ORPHAN_AGE_DAYS":      "orphan_age_days",
	"ROS_SNAPSHOT_NEVER_RESTORED_DAYS":  "never_restored_days",
	"ROS_SNAPSHOT_STALE_DAYS":           "stale_days",
	"ROS_SNAPSHOT_REDUNDANT_THRESHOLD":  "redundant_threshold",
	"ROS_SNAPSHOT_COST_PER_GIB_MONTH":   "cost_per_gib_month_usd",
}

// GetLockedFields returns the list of fields locked by environment variables.
func GetLockedFields() []string {
	var locked []string
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
	} {
		if envLockMap[envKey] == field {
			if _, ok := os.LookupEnv(envKey); ok {
				return true
			}
		}
	}
	return false
}

// ResolveSnapshotSettings resolves settings in priority order:
// 1. Env variable (locked) → 2. DB stored value → 3. Compiled-in default
func ResolveSnapshotSettings(ctx context.Context, pool *pgxpool.Pool, orgID string) (SnapshotSettings, error) {
	cfg := config.GetConfig()

	// Start with compiled-in defaults
	result := SnapshotSettingsDefaults

	// Override with DB values if a row exists
	var dbRow SnapshotSettingsRow
	err := pool.QueryRow(ctx, `
		SELECT orphan_age_days, never_restored_days, stale_days,
			redundant_threshold, cost_per_gib_month_usd
		FROM snapshot_settings WHERE org_id = $1`, orgID,
	).Scan(&dbRow.OrphanAgeDays, &dbRow.NeverRestoredDays, &dbRow.StaleDays,
		&dbRow.RedundantThreshold, &dbRow.CostPerGiBMonthUSD)

	if err == nil {
		result.OrphanAgeDays = dbRow.OrphanAgeDays
		result.NeverRestoredDays = dbRow.NeverRestoredDays
		result.StaleDays = dbRow.StaleDays
		result.RedundantThreshold = dbRow.RedundantThreshold
		result.CostPerGiBMonth = dbRow.CostPerGiBMonthUSD
	} else if err != pgx.ErrNoRows {
		return result, fmt.Errorf("querying snapshot settings: %w", err)
	}

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
	if cfg.SnapshotCostPerGiBMonth != 0 {
		if _, ok := os.LookupEnv("ROS_SNAPSHOT_COST_PER_GIB_MONTH"); ok {
			result.CostPerGiBMonth = cfg.SnapshotCostPerGiBMonth
		}
	}

	return result, nil
}

// GetSnapshotSettingsForAPI resolves settings and adds locked_fields for the GET response.
func GetSnapshotSettingsForAPI(ctx context.Context, pool *pgxpool.Pool, orgID string) (SnapshotSettingsResponse, error) {
	settings, err := ResolveSnapshotSettings(ctx, pool, orgID)
	if err != nil {
		return SnapshotSettingsResponse{}, err
	}
	return SnapshotSettingsResponse{
		OrphanAgeDays:      settings.OrphanAgeDays,
		NeverRestoredDays:  settings.NeverRestoredDays,
		StaleDays:          settings.StaleDays,
		RedundantThreshold: settings.RedundantThreshold,
		CostPerGiBMonthUSD: settings.CostPerGiBMonth,
		LockedFields:       GetLockedFields(),
	}, nil
}

// UpdateSnapshotSettings applies a partial update to the org's settings.
// Returns an error if any requested field is locked by an env variable.
func UpdateSnapshotSettings(ctx context.Context, pool *pgxpool.Pool, orgID string, update SnapshotSettingsUpdate) error {
	// Check for locked fields
	lockedAttempts := lockedFieldsInUpdate(update)
	if len(lockedAttempts) > 0 {
		return fmt.Errorf("%w: %v", ErrFieldsLocked, lockedAttempts)
	}

	// Resolve current settings to fill in missing values
	current, err := ResolveSnapshotSettings(ctx, pool, orgID)
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

	_, err = pool.Exec(ctx, `
		INSERT INTO snapshot_settings (
			org_id, orphan_age_days, never_restored_days, stale_days,
			redundant_threshold, cost_per_gib_month_usd, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (org_id)
		DO UPDATE SET
			orphan_age_days = EXCLUDED.orphan_age_days,
			never_restored_days = EXCLUDED.never_restored_days,
			stale_days = EXCLUDED.stale_days,
			redundant_threshold = EXCLUDED.redundant_threshold,
			cost_per_gib_month_usd = EXCLUDED.cost_per_gib_month_usd,
			updated_at = NOW()`,
		orgID, current.OrphanAgeDays, current.NeverRestoredDays, current.StaleDays,
		current.RedundantThreshold, current.CostPerGiBMonth,
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
	return locked
}

