package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

const quotaRecommendationType = "quota"

const (
	quotaDefaultHeadroomPercent            = 10
	quotaDefaultHighRiskThresholdPercent   = 90
	quotaDefaultMediumRiskThresholdPercent = 70
)

// QuotaSettings are tenant-configurable quota recommendation thresholds (percent).
type QuotaSettings struct {
	HeadroomPercent            int `json:"headroom_percent"`
	HighRiskThresholdPercent   int `json:"high_risk_threshold_percent"`
	MediumRiskThresholdPercent int `json:"medium_risk_threshold_percent"`
}

// QuotaSettingsResponse is the API GET/PUT/DELETE response (flat fields + locked_fields).
type QuotaSettingsResponse struct {
	HeadroomPercent            int      `json:"headroom_percent"`
	HighRiskThresholdPercent   int      `json:"high_risk_threshold_percent"`
	MediumRiskThresholdPercent int      `json:"medium_risk_threshold_percent"`
	LockedFields               []string `json:"locked_fields"`
}

// quotaSettingsStored is the JSON document in recommendation_thresholds.
type quotaSettingsStored struct {
	HeadroomPercent            *int `json:"headroom_percent,omitempty"`
	HighRiskThresholdPercent   *int `json:"high_risk_threshold_percent,omitempty"`
	MediumRiskThresholdPercent *int `json:"medium_risk_threshold_percent,omitempty"`
}

func quotaEnvLockMap() map[string]string {
	return map[string]string{
		"ROS_QUOTA_HEADROOM_PERCENT":              "headroom_percent",
		"ROS_QUOTA_HIGH_RISK_THRESHOLD_PERCENT":   "high_risk_threshold_percent",
		"ROS_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT": "medium_risk_threshold_percent",
	}
}

func lockedQuotaFieldsFromEnv() []string {
	return lockedFieldsFromEnvMap(quotaEnvLockMap())
}

func defaultQuotaSettings() QuotaSettings {
	return QuotaSettings{
		HeadroomPercent:            quotaDefaultHeadroomPercent,
		HighRiskThresholdPercent:   quotaDefaultHighRiskThresholdPercent,
		MediumRiskThresholdPercent: quotaDefaultMediumRiskThresholdPercent,
	}
}

func quotaSettingsFromConfig(cfg *config.Config) QuotaSettings {
	result := defaultQuotaSettings()
	if cfg == nil {
		return result
	}
	if cfg.QuotaHeadroomPercent >= 0 {
		result.HeadroomPercent = cfg.QuotaHeadroomPercent
	}
	if cfg.QuotaHighRiskThresholdPercent > 0 {
		result.HighRiskThresholdPercent = cfg.QuotaHighRiskThresholdPercent
	}
	if cfg.QuotaMediumRiskThresholdPercent > 0 {
		result.MediumRiskThresholdPercent = cfg.QuotaMediumRiskThresholdPercent
	}
	return result
}

func quotaRecConfigFromSettings(s QuotaSettings) QuotaRecConfig {
	headroomBP := 10000 + s.HeadroomPercent*100
	if headroomBP < 10000 {
		headroomBP = 10000
	}
	highBP := s.HighRiskThresholdPercent * 100
	mediumBP := s.MediumRiskThresholdPercent * 100
	if highBP <= 0 {
		highBP = quotaDefaultHighRiskThresholdPercent * 100
	}
	if mediumBP <= 0 {
		mediumBP = quotaDefaultMediumRiskThresholdPercent * 100
	}
	return QuotaRecConfig{
		HeadroomBasisPoints:   headroomBP,
		HighRiskThresholdBP:   highBP,
		MediumRiskThresholdBP: mediumBP,
	}
}

// ResolveQuotaSettings resolves quota thresholds: config/env defaults, then per-org DB overrides.
func ResolveQuotaSettings(ctx context.Context, pool *pgxpool.Pool, orgID string) (QuotaSettings, error) {
	result := quotaSettingsFromConfig(config.GetConfig())
	overlay, err := loadQuotaSettingsStored(ctx, pool, orgID)
	if err != nil {
		return result, err
	}
	applyQuotaStoredOverlay(&result, overlay)
	result = applyQuotaEnvLocks(result, config.GetConfig())
	return result, nil
}

func applyQuotaEnvLocks(base QuotaSettings, cfg *config.Config) QuotaSettings {
	if cfg == nil {
		return base
	}
	if _, ok := os.LookupEnv("ROS_QUOTA_HEADROOM_PERCENT"); ok {
		if cfg.QuotaHeadroomPercent >= 0 {
			base.HeadroomPercent = cfg.QuotaHeadroomPercent
		}
	}
	if _, ok := os.LookupEnv("ROS_QUOTA_HIGH_RISK_THRESHOLD_PERCENT"); ok {
		if cfg.QuotaHighRiskThresholdPercent > 0 {
			base.HighRiskThresholdPercent = cfg.QuotaHighRiskThresholdPercent
		}
	}
	if _, ok := os.LookupEnv("ROS_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT"); ok {
		if cfg.QuotaMediumRiskThresholdPercent > 0 {
			base.MediumRiskThresholdPercent = cfg.QuotaMediumRiskThresholdPercent
		}
	}
	return base
}

// ResolveQuotaRecConfig resolves tenant quota settings into engine basis-point config.
func ResolveQuotaRecConfig(ctx context.Context, pool *pgxpool.Pool, orgID string) (QuotaRecConfig, error) {
	settings, err := ResolveQuotaSettings(ctx, pool, orgID)
	if err != nil {
		return QuotaRecConfig{}, err
	}
	return quotaRecConfigFromSettings(settings), nil
}

// GetQuotaSettingsForAPI returns merged quota settings for GET.
func GetQuotaSettingsForAPI(ctx context.Context, pool *pgxpool.Pool, orgID string) (QuotaSettingsResponse, error) {
	settings, err := ResolveQuotaSettings(ctx, pool, orgID)
	if err != nil {
		return QuotaSettingsResponse{}, err
	}
	return QuotaSettingsResponse{
		HeadroomPercent:            settings.HeadroomPercent,
		HighRiskThresholdPercent:   settings.HighRiskThresholdPercent,
		MediumRiskThresholdPercent: settings.MediumRiskThresholdPercent,
		LockedFields:               lockedQuotaFieldsFromEnv(),
	}, nil
}

func loadQuotaSettingsStored(ctx context.Context, pool *pgxpool.Pool, orgID string) (*quotaSettingsStored, error) {
	if pool == nil {
		return nil, nil
	}
	var raw []byte
	err := pool.QueryRow(ctx, `
		SELECT thresholds FROM recommendation_thresholds
		WHERE org_id = $1 AND recommendation_type = $2`, orgID, quotaRecommendationType,
	).Scan(&raw)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query quota settings: %w", err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var stored quotaSettingsStored
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, fmt.Errorf("decode quota settings: %w", err)
	}
	return &stored, nil
}

func applyQuotaStoredOverlay(dest *QuotaSettings, stored *quotaSettingsStored) {
	if stored == nil {
		return
	}
	if stored.HeadroomPercent != nil {
		dest.HeadroomPercent = *stored.HeadroomPercent
	}
	if stored.HighRiskThresholdPercent != nil {
		dest.HighRiskThresholdPercent = *stored.HighRiskThresholdPercent
	}
	if stored.MediumRiskThresholdPercent != nil {
		dest.MediumRiskThresholdPercent = *stored.MediumRiskThresholdPercent
	}
}

// UpdateQuotaSettings validates and persists tenant quota overrides.
func UpdateQuotaSettings(ctx context.Context, pool *pgxpool.Pool, orgID string, rawUpdate json.RawMessage) error {
	if err := validateQuotaSettingsUpdate(rawUpdate); err != nil {
		return err
	}
	var update QuotaSettings
	if err := json.Unmarshal(rawUpdate, &update); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	if locked := lockedQuotaFieldsInUpdate(rawUpdate); len(locked) > 0 {
		return fmt.Errorf("%w: %v", ErrFieldsLocked, locked)
	}

	overrides := map[string]json.RawMessage{}
	if b, err := json.Marshal(update.HeadroomPercent); err == nil {
		overrides["headroom_percent"] = b
	}
	if b, err := json.Marshal(update.HighRiskThresholdPercent); err == nil {
		overrides["high_risk_threshold_percent"] = b
	}
	if b, err := json.Marshal(update.MediumRiskThresholdPercent); err == nil {
		overrides["medium_risk_threshold_percent"] = b
	}
	return upsertThresholdOverrides(ctx, pool, orgID, quotaRecommendationType, overrides)
}

func lockedQuotaFieldsInUpdate(rawUpdate json.RawMessage) []string {
	var update map[string]json.RawMessage
	if err := json.Unmarshal(rawUpdate, &update); err != nil {
		return nil
	}
	lockMap := quotaEnvLockMap()
	var locked []string
	for envKey, field := range lockMap {
		if _, set := os.LookupEnv(envKey); !set {
			continue
		}
		if _, ok := update[field]; ok {
			locked = append(locked, field)
		}
	}
	return locked
}

func validateQuotaSettingsUpdate(rawUpdate json.RawMessage) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(rawUpdate, &top); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	allowed := map[string]struct{}{
		"headroom_percent":              {},
		"high_risk_threshold_percent":   {},
		"medium_risk_threshold_percent": {},
		"locked_fields":                 {},
	}
	v := &fieldValidator{}
	for key := range top {
		if _, ok := allowed[key]; !ok {
			v.addConstraint("body", fmt.Sprintf("unknown field %q", key))
		}
	}
	validateQuotaSettingsFields(v, top)
	return v.result()
}

func validateQuotaSettingsFields(v *fieldValidator, fields map[string]json.RawMessage) {
	var headroom, high, medium *int
	if raw, ok := fields["headroom_percent"]; ok {
		var n int
		if json.Unmarshal(raw, &n) != nil {
			v.addConstraint("headroom_percent", "must be an integer")
		} else {
			headroom = &n
			v.addRangeInt("headroom_percent", n, 0, 100)
		}
	} else {
		v.addConstraint("headroom_percent", "is required")
	}
	if raw, ok := fields["high_risk_threshold_percent"]; ok {
		var n int
		if json.Unmarshal(raw, &n) != nil {
			v.addConstraint("high_risk_threshold_percent", "must be an integer")
		} else {
			high = &n
			v.addRangeInt("high_risk_threshold_percent", n, 1, 100)
		}
	} else {
		v.addConstraint("high_risk_threshold_percent", "is required")
	}
	if raw, ok := fields["medium_risk_threshold_percent"]; ok {
		var n int
		if json.Unmarshal(raw, &n) != nil {
			v.addConstraint("medium_risk_threshold_percent", "must be an integer")
		} else {
			medium = &n
			v.addRangeInt("medium_risk_threshold_percent", n, 1, 99)
		}
	} else {
		v.addConstraint("medium_risk_threshold_percent", "is required")
	}
	if high != nil && medium != nil && *high <= *medium {
		v.addConstraint("high_risk_threshold_percent", "must be greater than medium_risk_threshold_percent")
	}
	_ = headroom
}

// DeleteQuotaSettings removes tenant quota overrides.
func DeleteQuotaSettings(ctx context.Context, pool *pgxpool.Pool, orgID string) error {
	_, err := pool.Exec(ctx, `
		DELETE FROM recommendation_thresholds
		WHERE org_id = $1 AND recommendation_type = $2`, orgID, quotaRecommendationType)
	if err != nil {
		return fmt.Errorf("delete quota settings: %w", err)
	}
	return nil
}
