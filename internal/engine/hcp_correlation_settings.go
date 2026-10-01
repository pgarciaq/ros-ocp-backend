package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
)

const hcpCorrelationRecommendationType = "hcp-correlation"

// Compiled defaults mirror correlate.DefaultPolicy (#625 close draft, #628
// threshold table). Drift between the two is guarded by
// TestHCPCorrelationSettings_MatchCorrelatorDefaults in correlate (which may
// import engine; the reverse would cycle).
const (
	hcpDefaultHP99ThresholdS  = 0.30
	hcpDefaultHBaselineMult   = 3.0
	hcpDefaultCCPUPct         = 80.0
	hcpDefaultCEtcdP99S       = 0.01
	hcpDefaultWindowHours     = 1
	hcpDefaultSkewMinutes     = 5
	hcpDefaultFreshnessHours  = 2
	hcpDefaultAdvisoryExpHrs  = 24
)

// HCPCorrelationSettings are tenant-configurable correlator policy values.
type HCPCorrelationSettings struct {
	HP99ThresholdS   float64 `json:"h_p99_threshold_s"`
	HBaselineMultiple float64 `json:"h_baseline_multiple"`
	CCPUPct          float64 `json:"c_cpu_pct"`
	CEtcdP99S        float64 `json:"c_etcd_p99_s"`
	WindowHours      int     `json:"window_h"`
	SkewMinutes      int     `json:"skew_m"`
	FreshnessHours   int     `json:"freshness_h"`
	ExpiryHours      int     `json:"expiry_h"`
}

// HCPCorrelationSettingsResponse is the API GET/PUT/DELETE response.
type HCPCorrelationSettingsResponse struct {
	HP99ThresholdS    float64  `json:"h_p99_threshold_s"`
	HBaselineMultiple float64  `json:"h_baseline_multiple"`
	CCPUPct           float64  `json:"c_cpu_pct"`
	CEtcdP99S         float64  `json:"c_etcd_p99_s"`
	WindowHours       int      `json:"window_h"`
	SkewMinutes       int      `json:"skew_m"`
	FreshnessHours    int      `json:"freshness_h"`
	ExpiryHours       int      `json:"expiry_h"`
	LockedFields      []string `json:"locked_fields"`
	SettingsLocked    bool     `json:"settings_locked,omitempty"`
}

// hcpCorrelationSettingsStored is the JSON document in recommendation_thresholds.
type hcpCorrelationSettingsStored struct {
	HP99ThresholdS    *float64 `json:"h_p99_threshold_s,omitempty"`
	HBaselineMultiple *float64 `json:"h_baseline_multiple,omitempty"`
	CCPUPct           *float64 `json:"c_cpu_pct,omitempty"`
	CEtcdP99S         *float64 `json:"c_etcd_p99_s,omitempty"`
	WindowHours       *int     `json:"window_h,omitempty"`
	SkewMinutes       *int     `json:"skew_m,omitempty"`
	FreshnessHours    *int     `json:"freshness_h,omitempty"`
	ExpiryHours       *int     `json:"expiry_h,omitempty"`
}

func hcpCorrelationEnvLockMap() map[string]string {
	return map[string]string{
		"ROS_HCP_H_P99_THRESHOLD_S": "h_p99_threshold_s",
		"ROS_HCP_H_BASELINE_MULTIPLE": "h_baseline_multiple",
		"ROS_HCP_C_CPU_PCT":           "c_cpu_pct",
		"ROS_HCP_C_ETCD_P99_S":        "c_etcd_p99_s",
		"ROS_HCP_WINDOW_HOURS":        "window_h",
		"ROS_HCP_SKEW_MINUTES":        "skew_m",
		"ROS_HCP_FRESHNESS_HOURS":     "freshness_h",
		"ROS_HCP_EXPIRY_HOURS":        "expiry_h",
	}
}

func lockedHCPFieldsFromEnv() []string {
	return LockedFieldsFromEnvMap(hcpCorrelationEnvLockMap())
}

func defaultHCPCorrelationSettings() HCPCorrelationSettings {
	return HCPCorrelationSettings{
		HP99ThresholdS:    hcpDefaultHP99ThresholdS,
		HBaselineMultiple: hcpDefaultHBaselineMult,
		CCPUPct:           hcpDefaultCCPUPct,
		CEtcdP99S:         hcpDefaultCEtcdP99S,
		WindowHours:       hcpDefaultWindowHours,
		SkewMinutes:       hcpDefaultSkewMinutes,
		FreshnessHours:    hcpDefaultFreshnessHours,
		ExpiryHours:       hcpDefaultAdvisoryExpHrs,
	}
}

func hcpCorrelationSettingsFromConfig(cfg *config.Config) HCPCorrelationSettings {
	result := defaultHCPCorrelationSettings()
	if cfg == nil {
		return result
	}
	if cfg.HCPHP99ThresholdS > 0 {
		result.HP99ThresholdS = cfg.HCPHP99ThresholdS
	}
	if cfg.HCPHBaselineMultiple > 0 {
		result.HBaselineMultiple = cfg.HCPHBaselineMultiple
	}
	if cfg.HCPCCPUPct > 0 {
		result.CCPUPct = cfg.HCPCCPUPct
	}
	if cfg.HCPCEtcdP99S > 0 {
		result.CEtcdP99S = cfg.HCPCEtcdP99S
	}
	if cfg.HCPWindowHours > 0 {
		result.WindowHours = cfg.HCPWindowHours
	}
	if cfg.HCPClockSkewMinutes > 0 {
		result.SkewMinutes = cfg.HCPClockSkewMinutes
	}
	if cfg.HCPFreshnessHours > 0 {
		result.FreshnessHours = cfg.HCPFreshnessHours
	}
	if cfg.HCPAdvisoryExpiryHrs > 0 {
		result.ExpiryHours = cfg.HCPAdvisoryExpiryHrs
	}
	return result
}

// ResolveHCPCorrelationSettings resolves policy: config/env defaults, then
// per-org DB overrides (unless locked), then admin env-var locks re-applied.
func ResolveHCPCorrelationSettings(ctx context.Context, pool *pgxpool.Pool, orgID string) (HCPCorrelationSettings, error) {
	return ResolveThresholdCached(ctx, pool, orgID, hcpCorrelationRecommendationType, resolveHCPCorrelationSettingsUncached)
}

func loadHCPCorrelationSettingsStored(ctx context.Context, pool *pgxpool.Pool, orgID string) (hcpCorrelationSettingsStored, error) {
	var overlay hcpCorrelationSettingsStored
	if err := overlayThresholdJSON(ctx, pool, orgID, hcpCorrelationRecommendationType, &overlay); err != nil {
		return overlay, err
	}
	return overlay, nil
}

func applyHCPStoredOverlay(result *HCPCorrelationSettings, overlay hcpCorrelationSettingsStored) {
	if overlay.HP99ThresholdS != nil {
		result.HP99ThresholdS = *overlay.HP99ThresholdS
	}
	if overlay.HBaselineMultiple != nil {
		result.HBaselineMultiple = *overlay.HBaselineMultiple
	}
	if overlay.CCPUPct != nil {
		result.CCPUPct = *overlay.CCPUPct
	}
	if overlay.CEtcdP99S != nil {
		result.CEtcdP99S = *overlay.CEtcdP99S
	}
	if overlay.WindowHours != nil {
		result.WindowHours = *overlay.WindowHours
	}
	if overlay.SkewMinutes != nil {
		result.SkewMinutes = *overlay.SkewMinutes
	}
	if overlay.FreshnessHours != nil {
		result.FreshnessHours = *overlay.FreshnessHours
	}
	if overlay.ExpiryHours != nil {
		result.ExpiryHours = *overlay.ExpiryHours
	}
}

// HCPCorrelationSettingsToResponse renders the API response with lock state.
func HCPCorrelationSettingsToResponse(s HCPCorrelationSettings) HCPCorrelationSettingsResponse {
	return HCPCorrelationSettingsResponse{
		HP99ThresholdS: s.HP99ThresholdS, HBaselineMultiple: s.HBaselineMultiple,
		CCPUPct: s.CCPUPct, CEtcdP99S: s.CEtcdP99S,
		WindowHours: s.WindowHours, SkewMinutes: s.SkewMinutes,
		FreshnessHours: s.FreshnessHours, ExpiryHours: s.ExpiryHours,
		LockedFields:   LockedFieldsForAPI(hcpCorrelationRecommendationType, lockedHCPFieldsFromEnv()),
		SettingsLocked: IsSettingsLocked(hcpCorrelationRecommendationType),
	}
}

// GetHCPCorrelationSettingsForAPI resolves effective settings for GET responses.
func GetHCPCorrelationSettingsForAPI(ctx context.Context, pool *pgxpool.Pool, orgID string) (HCPCorrelationSettingsResponse, error) {
	s, err := ResolveHCPCorrelationSettings(ctx, pool, orgID)
	if err != nil {
		return HCPCorrelationSettingsResponse{}, err
	}
	return HCPCorrelationSettingsToResponse(s), nil
}

// validateHCPCorrelationSettingsUpdate enforces field allowlist, ranges, and
// cross-field rules (freshness must cover the correlation window; expiry must
// be positive). PUT replaces the whole domain, so all fields are required.
func validateHCPCorrelationSettingsUpdate(rawUpdate json.RawMessage) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(rawUpdate, &top); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	allowed := map[string]struct{}{
		"h_p99_threshold_s": {}, "h_baseline_multiple": {},
		"c_cpu_pct": {}, "c_etcd_p99_s": {},
		"window_h": {}, "skew_m": {}, "freshness_h": {}, "expiry_h": {},
		"locked_fields": {},
	}
	v := &FieldValidator{}
	for key := range top {
		if _, ok := allowed[key]; !ok {
			v.AddConstraint("body", fmt.Sprintf("unknown field %q", key))
		}
	}
	getFloat := func(key string, min, max float64) *float64 {
		raw, ok := top[key]
		if !ok {
			v.AddConstraint(key, "is required")
			return nil
		}
		var f float64
		if json.Unmarshal(raw, &f) != nil {
			v.AddConstraint(key, "must be a number")
			return nil
		}
		v.AddRangeFloat(key, f, min, max)
		return &f
	}
	getInt := func(key string, min, max int) *int {
		raw, ok := top[key]
		if !ok {
			v.AddConstraint(key, "is required")
			return nil
		}
		var n int
		if json.Unmarshal(raw, &n) != nil {
			v.AddConstraint(key, "must be an integer")
			return nil
		}
		v.AddRangeInt(key, n, min, max)
		return &n
	}
	getFloat("h_p99_threshold_s", 0.001, 3600)
	getFloat("h_baseline_multiple", 1, 100)
	getFloat("c_cpu_pct", 1, 100)
	getFloat("c_etcd_p99_s", 0.001, 60)
	window := getInt("window_h", 1, 168)
	getInt("skew_m", 0, 60)
	freshness := getInt("freshness_h", 1, 720)
	getInt("expiry_h", 1, 720)
	if window != nil && freshness != nil && *freshness < *window {
		v.AddConstraint("freshness_h", "must cover the correlation window (freshness_h >= window_h)")
	}
	return v.Result()
}

// UpdateHCPCorrelationSettings validates, rejects locked fields, and stores
// tenant overrides. No recalculation is triggered: the hourly correlator
// picks new values up within one cycle (documented delay, #645).
func UpdateHCPCorrelationSettings(ctx context.Context, pool *pgxpool.Pool, orgID string, rawUpdate json.RawMessage) error {
	if err := validateHCPCorrelationSettingsUpdate(rawUpdate); err != nil {
		return err
	}
	var update HCPCorrelationSettings
	if err := json.Unmarshal(rawUpdate, &update); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	if locked := lockedHCPFieldsInUpdate(rawUpdate); len(locked) > 0 {
		return fmt.Errorf("%w: %v", ErrFieldsLocked, locked)
	}
	overrides := map[string]json.RawMessage{}
	put := func(key string, val any) {
		if b, err := json.Marshal(val); err == nil {
			overrides[key] = b
		}
	}
	put("h_p99_threshold_s", update.HP99ThresholdS)
	put("h_baseline_multiple", update.HBaselineMultiple)
	put("c_cpu_pct", update.CCPUPct)
	put("c_etcd_p99_s", update.CEtcdP99S)
	put("window_h", update.WindowHours)
	put("skew_m", update.SkewMinutes)
	put("freshness_h", update.FreshnessHours)
	put("expiry_h", update.ExpiryHours)
	if err := UpsertThresholdOverrides(ctx, pool, orgID, hcpCorrelationRecommendationType, overrides); err != nil {
		return err
	}
	InvalidateThresholdCache(orgID, hcpCorrelationRecommendationType)
	return nil
}

func lockedHCPFieldsInUpdate(rawUpdate json.RawMessage) []string {
	var update map[string]json.RawMessage
	if err := json.Unmarshal(rawUpdate, &update); err != nil {
		return nil
	}
	lockMap := hcpCorrelationEnvLockMap()
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

// DeleteHCPCorrelationSettings removes tenant overrides (revert to defaults).
func DeleteHCPCorrelationSettings(ctx context.Context, pool *pgxpool.Pool, orgID string) error {
	_, err := pool.Exec(ctx, `
		DELETE FROM recommendation_thresholds
		WHERE org_id = $1 AND recommendation_type = $2`, orgID, hcpCorrelationRecommendationType)
	if err != nil {
		return fmt.Errorf("delete hcp correlation settings: %w", err)
	}
	InvalidateThresholdCache(orgID, hcpCorrelationRecommendationType)
	return nil
}

func resolveHCPCorrelationSettingsUncached(ctx context.Context, pool *pgxpool.Pool, orgID string) (HCPCorrelationSettings, error) {
	result := hcpCorrelationSettingsFromConfig(config.GetConfig())
	if !IsSettingsLocked(hcpCorrelationRecommendationType) {
		overlay, err := loadHCPCorrelationSettingsStored(ctx, pool, orgID)
		if err != nil {
			return result, err
		}
		applyHCPStoredOverlay(&result, overlay)
	}
	result = applyHCPEnvLocks(result, config.GetConfig())
	return result, nil
}

func applyHCPEnvLocks(base HCPCorrelationSettings, cfg *config.Config) HCPCorrelationSettings {
	if cfg == nil {
		return base
	}
	if _, ok := os.LookupEnv("ROS_HCP_H_P99_THRESHOLD_S"); ok && cfg.HCPHP99ThresholdS > 0 {
		base.HP99ThresholdS = cfg.HCPHP99ThresholdS
	}
	if _, ok := os.LookupEnv("ROS_HCP_H_BASELINE_MULTIPLE"); ok && cfg.HCPHBaselineMultiple > 0 {
		base.HBaselineMultiple = cfg.HCPHBaselineMultiple
	}
	if _, ok := os.LookupEnv("ROS_HCP_C_CPU_PCT"); ok && cfg.HCPCCPUPct > 0 {
		base.CCPUPct = cfg.HCPCCPUPct
	}
	if _, ok := os.LookupEnv("ROS_HCP_C_ETCD_P99_S"); ok && cfg.HCPCEtcdP99S > 0 {
		base.CEtcdP99S = cfg.HCPCEtcdP99S
	}
	if _, ok := os.LookupEnv("ROS_HCP_WINDOW_HOURS"); ok && cfg.HCPWindowHours > 0 {
		base.WindowHours = cfg.HCPWindowHours
	}
	if _, ok := os.LookupEnv("ROS_HCP_SKEW_MINUTES"); ok && cfg.HCPClockSkewMinutes > 0 {
		base.SkewMinutes = cfg.HCPClockSkewMinutes
	}
	if _, ok := os.LookupEnv("ROS_HCP_FRESHNESS_HOURS"); ok && cfg.HCPFreshnessHours > 0 {
		base.FreshnessHours = cfg.HCPFreshnessHours
	}
	if _, ok := os.LookupEnv("ROS_HCP_EXPIRY_HOURS"); ok && cfg.HCPAdvisoryExpiryHrs > 0 {
		base.ExpiryHours = cfg.HCPAdvisoryExpiryHrs
	}
	return base
}
