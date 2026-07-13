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

const clusterQuotaRecommendationType = "cluster-quota"

const (
	clusterQuotaDefaultHeadroomPercent            = 10
	clusterQuotaDefaultHighRiskThresholdPercent   = 90
	clusterQuotaDefaultMediumRiskThresholdPercent = 70
)

// ClusterQuotaSettings are tenant-configurable cluster-quota thresholds (percent).
type ClusterQuotaSettings struct {
	HeadroomPercent            int `json:"headroom_percent"`
	HighRiskThresholdPercent   int `json:"high_risk_threshold_percent"`
	MediumRiskThresholdPercent int `json:"medium_risk_threshold_percent"`
}

// ClusterQuotaSettingsResponse is the API GET/PUT/DELETE response.
type ClusterQuotaSettingsResponse struct {
	HeadroomPercent            int      `json:"headroom_percent"`
	HighRiskThresholdPercent   int      `json:"high_risk_threshold_percent"`
	MediumRiskThresholdPercent int      `json:"medium_risk_threshold_percent"`
	LockedFields               []string `json:"locked_fields"`
	SettingsLocked             bool     `json:"settings_locked,omitempty"`
}

type clusterQuotaSettingsStored struct {
	HeadroomPercent            *int `json:"headroom_percent,omitempty"`
	HighRiskThresholdPercent   *int `json:"high_risk_threshold_percent,omitempty"`
	MediumRiskThresholdPercent *int `json:"medium_risk_threshold_percent,omitempty"`
}

func clusterQuotaEnvLockMap() map[string]string {
	return map[string]string{
		"ROS_CLUSTER_QUOTA_HEADROOM_PERCENT":              "headroom_percent",
		"ROS_CLUSTER_QUOTA_HIGH_RISK_THRESHOLD_PERCENT":   "high_risk_threshold_percent",
		"ROS_CLUSTER_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT": "medium_risk_threshold_percent",
	}
}

func lockedClusterQuotaFieldsFromEnv() []string {
	return lockedFieldsFromEnvMap(clusterQuotaEnvLockMap())
}

func defaultClusterQuotaSettings() ClusterQuotaSettings {
	return ClusterQuotaSettings{
		HeadroomPercent:            clusterQuotaDefaultHeadroomPercent,
		HighRiskThresholdPercent:   clusterQuotaDefaultHighRiskThresholdPercent,
		MediumRiskThresholdPercent: clusterQuotaDefaultMediumRiskThresholdPercent,
	}
}

func clusterQuotaSettingsFromConfig(cfg *config.Config) ClusterQuotaSettings {
	result := defaultClusterQuotaSettings()
	if cfg == nil {
		return result
	}
	if cfg.ClusterQuotaHeadroomPercent >= 0 {
		result.HeadroomPercent = cfg.ClusterQuotaHeadroomPercent
	}
	if cfg.ClusterQuotaHighRiskThresholdPercent > 0 {
		result.HighRiskThresholdPercent = cfg.ClusterQuotaHighRiskThresholdPercent
	}
	if cfg.ClusterQuotaMediumRiskThresholdPercent > 0 {
		result.MediumRiskThresholdPercent = cfg.ClusterQuotaMediumRiskThresholdPercent
	}
	return result
}

func clusterQuotaRecConfigFromSettings(s ClusterQuotaSettings) QuotaRecConfig {
	headroomBP := 10000 + s.HeadroomPercent*100
	if headroomBP < 10000 {
		headroomBP = 10000
	}
	highBP := s.HighRiskThresholdPercent * 100
	mediumBP := s.MediumRiskThresholdPercent * 100
	if highBP <= 0 {
		highBP = clusterQuotaDefaultHighRiskThresholdPercent * 100
	}
	if mediumBP <= 0 {
		mediumBP = clusterQuotaDefaultMediumRiskThresholdPercent * 100
	}
	return QuotaRecConfig{
		HeadroomBasisPoints:   headroomBP,
		HighRiskThresholdBP:   highBP,
		MediumRiskThresholdBP: mediumBP,
	}
}

// ResolveClusterQuotaSettings resolves thresholds: env defaults, then per-org DB overrides.
func ResolveClusterQuotaSettings(ctx context.Context, pool *pgxpool.Pool, orgID string) (ClusterQuotaSettings, error) {
	return resolveThresholdCached(ctx, pool, orgID, clusterQuotaRecommendationType, resolveClusterQuotaSettingsUncached)
}

func resolveClusterQuotaSettingsUncached(ctx context.Context, pool *pgxpool.Pool, orgID string) (ClusterQuotaSettings, error) {
	result := clusterQuotaSettingsFromConfig(config.GetConfig())
	if !IsSettingsLocked(clusterQuotaRecommendationType) {
		overlay, err := loadClusterQuotaSettingsStored(ctx, pool, orgID)
		if err != nil {
			return result, err
		}
		applyClusterQuotaStoredOverlay(&result, overlay)
	}
	result = applyClusterQuotaEnvLocks(result, config.GetConfig())
	return result, nil
}

func applyClusterQuotaEnvLocks(base ClusterQuotaSettings, cfg *config.Config) ClusterQuotaSettings {
	if cfg == nil {
		return base
	}
	if _, ok := os.LookupEnv("ROS_CLUSTER_QUOTA_HEADROOM_PERCENT"); ok {
		if cfg.ClusterQuotaHeadroomPercent >= 0 {
			base.HeadroomPercent = cfg.ClusterQuotaHeadroomPercent
		}
	}
	if _, ok := os.LookupEnv("ROS_CLUSTER_QUOTA_HIGH_RISK_THRESHOLD_PERCENT"); ok {
		if cfg.ClusterQuotaHighRiskThresholdPercent > 0 {
			base.HighRiskThresholdPercent = cfg.ClusterQuotaHighRiskThresholdPercent
		}
	}
	if _, ok := os.LookupEnv("ROS_CLUSTER_QUOTA_MEDIUM_RISK_THRESHOLD_PERCENT"); ok {
		if cfg.ClusterQuotaMediumRiskThresholdPercent > 0 {
			base.MediumRiskThresholdPercent = cfg.ClusterQuotaMediumRiskThresholdPercent
		}
	}
	return base
}

// ResolveClusterQuotaRecConfig resolves tenant settings into engine basis-point config.
func ResolveClusterQuotaRecConfig(ctx context.Context, pool *pgxpool.Pool, orgID string) (QuotaRecConfig, error) {
	settings, err := ResolveClusterQuotaSettings(ctx, pool, orgID)
	if err != nil {
		return QuotaRecConfig{}, err
	}
	return clusterQuotaRecConfigFromSettings(settings), nil
}

// GetClusterQuotaSettingsForAPI returns merged settings for GET.
func GetClusterQuotaSettingsForAPI(ctx context.Context, pool *pgxpool.Pool, orgID string) (ClusterQuotaSettingsResponse, error) {
	settings, err := ResolveClusterQuotaSettings(ctx, pool, orgID)
	if err != nil {
		return ClusterQuotaSettingsResponse{}, err
	}
	return ClusterQuotaSettingsResponse{
		HeadroomPercent:            settings.HeadroomPercent,
		HighRiskThresholdPercent:   settings.HighRiskThresholdPercent,
		MediumRiskThresholdPercent: settings.MediumRiskThresholdPercent,
		LockedFields:               LockedFieldsForAPI(clusterQuotaRecommendationType, lockedClusterQuotaFieldsFromEnv()),
		SettingsLocked:             IsSettingsLocked(clusterQuotaRecommendationType),
	}, nil
}

func loadClusterQuotaSettingsStored(ctx context.Context, pool *pgxpool.Pool, orgID string) (*clusterQuotaSettingsStored, error) {
	if pool == nil {
		return nil, nil
	}
	var raw []byte
	err := pool.QueryRow(ctx, `
		SELECT thresholds FROM recommendation_thresholds
		WHERE org_id = $1 AND recommendation_type = $2`, orgID, clusterQuotaRecommendationType,
	).Scan(&raw)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query cluster-quota settings: %w", err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var stored clusterQuotaSettingsStored
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, fmt.Errorf("decode cluster-quota settings: %w", err)
	}
	return &stored, nil
}

func applyClusterQuotaStoredOverlay(dest *ClusterQuotaSettings, stored *clusterQuotaSettingsStored) {
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

// UpdateClusterQuotaSettings validates and persists tenant overrides.
func UpdateClusterQuotaSettings(ctx context.Context, pool *pgxpool.Pool, orgID string, rawUpdate json.RawMessage) error {
	if err := validateClusterQuotaSettingsUpdate(rawUpdate); err != nil {
		return err
	}
	var update ClusterQuotaSettings
	if err := json.Unmarshal(rawUpdate, &update); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	if locked := lockedClusterQuotaFieldsInUpdate(rawUpdate); len(locked) > 0 {
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
	if err := upsertThresholdOverrides(ctx, pool, orgID, clusterQuotaRecommendationType, overrides); err != nil {
		return err
	}
	InvalidateThresholdCache(orgID, clusterQuotaRecommendationType)
	return nil
}

func lockedClusterQuotaFieldsInUpdate(rawUpdate json.RawMessage) []string {
	var update map[string]json.RawMessage
	if err := json.Unmarshal(rawUpdate, &update); err != nil {
		return nil
	}
	lockMap := clusterQuotaEnvLockMap()
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

func validateClusterQuotaSettingsUpdate(rawUpdate json.RawMessage) error {
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
	v := &FieldValidator{}
	for key := range top {
		if _, ok := allowed[key]; !ok {
			v.AddConstraint("body", fmt.Sprintf("unknown field %q", key))
		}
	}
	validateClusterQuotaSettingsFields(v, top)
	return v.Result()
}

func validateClusterQuotaSettingsFields(v *FieldValidator, fields map[string]json.RawMessage) {
	var headroom, high, medium *int
	if raw, ok := fields["headroom_percent"]; ok {
		var n int
		if json.Unmarshal(raw, &n) != nil {
			v.AddConstraint("headroom_percent", "must be an integer")
		} else {
			headroom = &n
			v.AddRangeInt("headroom_percent", n, 0, 100)
		}
	} else {
		v.AddConstraint("headroom_percent", "is required")
	}
	if raw, ok := fields["high_risk_threshold_percent"]; ok {
		var n int
		if json.Unmarshal(raw, &n) != nil {
			v.AddConstraint("high_risk_threshold_percent", "must be an integer")
		} else {
			high = &n
			v.AddRangeInt("high_risk_threshold_percent", n, 1, 100)
		}
	} else {
		v.AddConstraint("high_risk_threshold_percent", "is required")
	}
	if raw, ok := fields["medium_risk_threshold_percent"]; ok {
		var n int
		if json.Unmarshal(raw, &n) != nil {
			v.AddConstraint("medium_risk_threshold_percent", "must be an integer")
		} else {
			medium = &n
			v.AddRangeInt("medium_risk_threshold_percent", n, 1, 99)
		}
	} else {
		v.AddConstraint("medium_risk_threshold_percent", "is required")
	}
	if high != nil && medium != nil && *high <= *medium {
		v.AddConstraint("high_risk_threshold_percent", "must be greater than medium_risk_threshold_percent")
	}
	_ = headroom
}

// DeleteClusterQuotaSettings removes tenant cluster-quota overrides.
func DeleteClusterQuotaSettings(ctx context.Context, pool *pgxpool.Pool, orgID string) error {
	_, err := pool.Exec(ctx, `
		DELETE FROM recommendation_thresholds
		WHERE org_id = $1 AND recommendation_type = $2`, orgID, clusterQuotaRecommendationType)
	if err != nil {
		return fmt.Errorf("delete cluster-quota settings: %w", err)
	}
	InvalidateThresholdCache(orgID, clusterQuotaRecommendationType)
	return nil
}
