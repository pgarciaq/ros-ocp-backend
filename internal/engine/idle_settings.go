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

const idleDetectionRecommendationType = "idle_detection"

var validIdleWorkloadTypes = map[string]struct{}{
	"Deployment":       {},
	"StatefulSet":      {},
	"DaemonSet":        {},
	"Job":              {},
	"CronJob":          {},
	"DeploymentConfig": {},
}

// IdleDetectionThresholds are tenant-configurable classification thresholds.
type IdleDetectionThresholds struct {
	CPUUtilizationPercent    int64 `json:"cpu_utilization_percent"`
	MemoryUtilizationPercent int64 `json:"memory_utilization_percent"`
	BurstRatio               int64 `json:"burst_ratio"`
	MinimumObservationDays   int   `json:"minimum_observation_days"`
	GPUSMActiveBasisPoints   int64 `json:"gpu_sm_active_basis_points"`
	GPUDRAMActiveBasisPoints int64 `json:"gpu_dram_active_basis_points"`
}

// IdleDetectionExclusions lists namespaces and workload types never flagged idle.
type IdleDetectionExclusions struct {
	Namespaces    []string `json:"namespaces"`
	WorkloadTypes []string `json:"workload_types"`
}

// IdleDetectionSettings is the idle_detection block in the Settings API.
type IdleDetectionSettings struct {
	Enabled    bool                    `json:"enabled"`
	Thresholds IdleDetectionThresholds `json:"thresholds"`
	Exclusions IdleDetectionExclusions `json:"exclusions"`
}

// IdleDetectionSettingsResponse wraps settings with locked_fields for GET.
type IdleDetectionSettingsResponse struct {
	IdleDetection  IdleDetectionSettings `json:"idle_detection"`
	LockedFields   []string              `json:"locked_fields"`
	SettingsLocked bool                  `json:"settings_locked,omitempty"`
}

// idleDetectionStored is the JSON document in recommendation_thresholds.
type idleDetectionStored struct {
	Enabled                  *bool    `json:"enabled,omitempty"`
	CPUUtilizationPercent    *int64   `json:"cpu_utilization_percent,omitempty"`
	MemoryUtilizationPercent *int64   `json:"memory_utilization_percent,omitempty"`
	BurstRatio               *int64   `json:"burst_ratio,omitempty"`
	MinimumObservationDays   *int     `json:"minimum_observation_days,omitempty"`
	GPUSMActiveBasisPoints   *int64   `json:"gpu_sm_active_basis_points,omitempty"`
	GPUDRAMActiveBasisPoints *int64   `json:"gpu_dram_active_basis_points,omitempty"`
	ExcludeNamespaces        []string `json:"exclude_namespaces,omitempty"`
	ExcludeWorkloadTypes     []string `json:"exclude_workload_types,omitempty"`
}

func idleEnvLockMap() map[string]string {
	return map[string]string{
		"ROS_IDLE_DETECTION_ENABLED":      "enabled",
		"ROS_IDLE_ZOMBIE_CPU_MILLICORES":  "zombie_cpu_millicores",
		"ROS_IDLE_ZOMBIE_PEAK_MILLICORES": "zombie_peak_millicores",
		"ROS_IDLE_CPU_UTILIZATION_PCT":    "cpu_utilization_percent",
		"ROS_IDLE_MEMORY_UTILIZATION_PCT": "memory_utilization_percent",
		"ROS_IDLE_BURST_RATIO":            "burst_ratio",
		"ROS_IDLE_MIN_OBSERVATION_DAYS":   "minimum_observation_days",
		"ROS_IDLE_EXCLUDE_NAMESPACES":     "exclude_namespaces",
		"ROS_IDLE_EXCLUDE_WORKLOAD_TYPES": "exclude_workload_types",
		"ROS_IDLE_GPU_SM_ACTIVE_BP":       "gpu_sm_active_basis_points",
		"ROS_IDLE_GPU_DRAM_ACTIVE_BP":     "gpu_dram_active_basis_points",
	}
}

func lockedIdleFieldsFromEnv() []string {
	return lockedFieldsFromEnvMap(idleEnvLockMap())
}

func defaultIdleDetectionSettings() IdleDetectionSettings {
	cfg := DefaultIdleConfig()
	gpuDef := GPUIdleConfig{
		Enabled:          true,
		IdleSMActiveBP:   500,
		IdleDRAMActiveBP: 500,
		MinObservationDays: 7,
	}
	if c := config.GetConfig(); c != nil {
		gpuDef.Enabled = c.IdleDetectionEnabled
		if c.IdleGPUSMActiveBP > 0 {
			gpuDef.IdleSMActiveBP = c.IdleGPUSMActiveBP
		}
		if c.IdleGPUDRAMActiveBP > 0 {
			gpuDef.IdleDRAMActiveBP = c.IdleGPUDRAMActiveBP
		}
	}
	return IdleDetectionSettings{
		Enabled: cfg.Enabled,
		Thresholds: IdleDetectionThresholds{
			CPUUtilizationPercent:    cfg.IdleCPUUtilPct,
			MemoryUtilizationPercent: cfg.IdleMemUtilPct,
			BurstRatio:               cfg.BurstRatio,
			MinimumObservationDays:   cfg.MinObservationDays,
			GPUSMActiveBasisPoints:   gpuDef.IdleSMActiveBP,
			GPUDRAMActiveBasisPoints: gpuDef.IdleDRAMActiveBP,
		},
		Exclusions: IdleDetectionExclusions{
			Namespaces:    append([]string(nil), cfg.ExcludeNamespaces...),
			WorkloadTypes: append([]string(nil), cfg.ExcludeWorkloadTypes...),
		},
	}
}

func idleConfigFromSettings(s IdleDetectionSettings) IdleConfig {
	def := DefaultIdleConfig()
	out := IdleConfig{
		Enabled:              s.Enabled,
		ZombieCPUP95MC:       def.ZombieCPUP95MC,
		ZombieCPUPeakMC:      def.ZombieCPUPeakMC,
		IdleCPUUtilPct:       s.Thresholds.CPUUtilizationPercent,
		IdleMemUtilPct:       s.Thresholds.MemoryUtilizationPercent,
		BurstRatio:           s.Thresholds.BurstRatio,
		MinObservationDays:   s.Thresholds.MinimumObservationDays,
		ExcludeNamespaces:    append([]string(nil), s.Exclusions.Namespaces...),
		ExcludeWorkloadTypes: append([]string(nil), s.Exclusions.WorkloadTypes...),
	}
	cfg := config.GetConfig()
	if cfg != nil {
		if cfg.IdleZombieCPUMillicores > 0 {
			out.ZombieCPUP95MC = cfg.IdleZombieCPUMillicores
		}
		if cfg.IdleZombiePeakMillicores > 0 {
			out.ZombieCPUPeakMC = cfg.IdleZombiePeakMillicores
		}
	}
	return out
}

func gpuIdleConfigFromSettings(s IdleDetectionSettings) GPUIdleConfig {
	gpu := GPUIdleConfig{
		Enabled:            s.Enabled,
		IdleSMActiveBP:     500,
		IdleDRAMActiveBP:   500,
		ZombieSMActiveBP:   gpuZombieThresholdBP,
		ZombieDRAMActiveBP: gpuZombieThresholdBP,
		MinObservationDays: 7,
	}
	if s.Thresholds.GPUSMActiveBasisPoints > 0 {
		gpu.IdleSMActiveBP = s.Thresholds.GPUSMActiveBasisPoints
	}
	if s.Thresholds.GPUDRAMActiveBasisPoints > 0 {
		gpu.IdleDRAMActiveBP = s.Thresholds.GPUDRAMActiveBasisPoints
	}
	if s.Thresholds.MinimumObservationDays > 0 {
		gpu.MinObservationDays = s.Thresholds.MinimumObservationDays
	}
	return gpu
}

// GetIdleDetectionSettingsForAPI returns merged idle detection settings for GET.
func GetIdleDetectionSettingsForAPI(ctx context.Context, pool *pgxpool.Pool, orgID string) (IdleDetectionSettingsResponse, error) {
	settings, err := resolveIdleDetectionSettings(ctx, pool, orgID)
	if err != nil {
		return IdleDetectionSettingsResponse{}, err
	}
	return IdleDetectionSettingsResponse{
		IdleDetection:  settings,
		LockedFields:   LockedFieldsForAPI(idleDetectionRecommendationType, lockedIdleFieldsFromEnv()),
		SettingsLocked: IsSettingsLocked(idleDetectionRecommendationType),
	}, nil
}

func resolveIdleDetectionSettings(ctx context.Context, pool *pgxpool.Pool, orgID string) (IdleDetectionSettings, error) {
	return resolveThresholdCached(ctx, pool, orgID, idleDetectionRecommendationType, resolveIdleDetectionSettingsUncached)
}

func resolveIdleDetectionSettingsUncached(ctx context.Context, pool *pgxpool.Pool, orgID string) (IdleDetectionSettings, error) {
	result := defaultIdleDetectionSettings()
	if !IsSettingsLocked(idleDetectionRecommendationType) {
		overlay, err := loadIdleDetectionStored(ctx, pool, orgID)
		if err != nil {
			return result, err
		}
		applyIdleStoredOverlay(&result, overlay)
	}
	result = applyIdleEnvLocks(result, config.GetConfig())
	return result, nil
}

func applyIdleEnvLocks(base IdleDetectionSettings, cfg *config.Config) IdleDetectionSettings {
	if cfg == nil {
		return base
	}
	if _, ok := os.LookupEnv("ROS_IDLE_DETECTION_ENABLED"); ok {
		base.Enabled = cfg.IdleDetectionEnabled
	}
	if _, ok := os.LookupEnv("ROS_IDLE_CPU_UTILIZATION_PCT"); ok {
		if cfg.IdleCPUUtilizationPct > 0 {
			base.Thresholds.CPUUtilizationPercent = cfg.IdleCPUUtilizationPct
		}
	}
	if _, ok := os.LookupEnv("ROS_IDLE_MEMORY_UTILIZATION_PCT"); ok {
		if cfg.IdleMemUtilizationPct > 0 {
			base.Thresholds.MemoryUtilizationPercent = cfg.IdleMemUtilizationPct
		}
	}
	if _, ok := os.LookupEnv("ROS_IDLE_BURST_RATIO"); ok {
		if cfg.IdleBurstRatio > 0 {
			base.Thresholds.BurstRatio = cfg.IdleBurstRatio
		}
	}
	if _, ok := os.LookupEnv("ROS_IDLE_MIN_OBSERVATION_DAYS"); ok {
		if cfg.IdleMinObservationDays > 0 {
			base.Thresholds.MinimumObservationDays = cfg.IdleMinObservationDays
		}
	}
	if _, ok := os.LookupEnv("ROS_IDLE_GPU_SM_ACTIVE_BP"); ok {
		if cfg.IdleGPUSMActiveBP > 0 {
			base.Thresholds.GPUSMActiveBasisPoints = cfg.IdleGPUSMActiveBP
		}
	}
	if _, ok := os.LookupEnv("ROS_IDLE_GPU_DRAM_ACTIVE_BP"); ok {
		if cfg.IdleGPUDRAMActiveBP > 0 {
			base.Thresholds.GPUDRAMActiveBasisPoints = cfg.IdleGPUDRAMActiveBP
		}
	}
	if _, ok := os.LookupEnv("ROS_IDLE_EXCLUDE_NAMESPACES"); ok {
		base.Exclusions.Namespaces = splitCSVList(cfg.IdleExcludeNamespaces)
	}
	if _, ok := os.LookupEnv("ROS_IDLE_EXCLUDE_WORKLOAD_TYPES"); ok {
		base.Exclusions.WorkloadTypes = splitCSVList(cfg.IdleExcludeWorkloadTypes)
	}
	return base
}

func loadIdleDetectionStored(ctx context.Context, pool *pgxpool.Pool, orgID string) (*idleDetectionStored, error) {
	if pool == nil {
		return nil, nil
	}
	var raw []byte
	err := pool.QueryRow(ctx, `
		SELECT thresholds FROM recommendation_thresholds
		WHERE org_id = $1 AND recommendation_type = $2`, orgID, idleDetectionRecommendationType,
	).Scan(&raw)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query idle detection settings: %w", err)
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var stored idleDetectionStored
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil, fmt.Errorf("decode idle detection settings: %w", err)
	}
	return &stored, nil
}

func applyIdleStoredOverlay(dest *IdleDetectionSettings, stored *idleDetectionStored) {
	if stored == nil {
		return
	}
	if stored.Enabled != nil {
		dest.Enabled = *stored.Enabled
	}
	if stored.CPUUtilizationPercent != nil {
		dest.Thresholds.CPUUtilizationPercent = *stored.CPUUtilizationPercent
	}
	if stored.MemoryUtilizationPercent != nil {
		dest.Thresholds.MemoryUtilizationPercent = *stored.MemoryUtilizationPercent
	}
	if stored.BurstRatio != nil {
		dest.Thresholds.BurstRatio = *stored.BurstRatio
	}
	if stored.MinimumObservationDays != nil {
		dest.Thresholds.MinimumObservationDays = *stored.MinimumObservationDays
	}
	if stored.GPUSMActiveBasisPoints != nil {
		dest.Thresholds.GPUSMActiveBasisPoints = *stored.GPUSMActiveBasisPoints
	}
	if stored.GPUDRAMActiveBasisPoints != nil {
		dest.Thresholds.GPUDRAMActiveBasisPoints = *stored.GPUDRAMActiveBasisPoints
	}
	if len(stored.ExcludeNamespaces) > 0 {
		dest.Exclusions.Namespaces = append(dest.Exclusions.Namespaces, stored.ExcludeNamespaces...)
	}
	if len(stored.ExcludeWorkloadTypes) > 0 {
		dest.Exclusions.WorkloadTypes = append(dest.Exclusions.WorkloadTypes, stored.ExcludeWorkloadTypes...)
	}
}

// UpdateIdleDetectionSettings validates and persists tenant idle detection overrides.
func UpdateIdleDetectionSettings(ctx context.Context, pool *pgxpool.Pool, orgID string, rawUpdate json.RawMessage) error {
	if err := validateIdleDetectionUpdate(rawUpdate); err != nil {
		return err
	}
	var body struct {
		IdleDetection json.RawMessage `json:"idle_detection"`
	}
	if err := json.Unmarshal(rawUpdate, &body); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	if len(body.IdleDetection) == 0 {
		return &ThresholdValidationError{Errors: []string{"idle_detection is required"}}
	}
	var update map[string]json.RawMessage
	if err := json.Unmarshal(body.IdleDetection, &update); err != nil {
		return fmt.Errorf("invalid idle_detection: %w", err)
	}
	if locked := lockedIdleFieldsInUpdate(update); len(locked) > 0 {
		return fmt.Errorf("%w: %v", ErrFieldsLocked, locked)
	}

	overrides, err := loadIdleDetectionOverridesMap(ctx, pool, orgID)
	if err != nil {
		return err
	}
	if overrides == nil {
		overrides = map[string]json.RawMessage{}
	}
	for key, val := range update {
		if key == "thresholds" {
			var th map[string]json.RawMessage
			if err := json.Unmarshal(val, &th); err == nil {
				for tk, tv := range th {
					overrides[tk] = tv
				}
				continue
			}
		}
		if key == "exclusions" {
			var ex struct {
				Namespaces    []string `json:"namespaces"`
				WorkloadTypes []string `json:"workload_types"`
			}
			if err := json.Unmarshal(val, &ex); err == nil {
				if b, err := json.Marshal(ex.Namespaces); err == nil {
					overrides["exclude_namespaces"] = b
				}
				if b, err := json.Marshal(ex.WorkloadTypes); err == nil {
					overrides["exclude_workload_types"] = b
				}
				continue
			}
		}
		overrides[key] = val
	}
	if err := upsertThresholdOverrides(ctx, pool, orgID, idleDetectionRecommendationType, overrides); err != nil {
		return err
	}
	InvalidateThresholdCache(orgID, idleDetectionRecommendationType)
	return nil
}

func loadIdleDetectionOverridesMap(ctx context.Context, pool *pgxpool.Pool, orgID string) (map[string]json.RawMessage, error) {
	return loadThresholdOverrides(ctx, pool, orgID, idleDetectionRecommendationType)
}

var idleUpdateKeyToEnv = map[string]string{
	"enabled":                      "ROS_IDLE_DETECTION_ENABLED",
	"cpu_utilization_percent":      "ROS_IDLE_CPU_UTILIZATION_PCT",
	"memory_utilization_percent":   "ROS_IDLE_MEMORY_UTILIZATION_PCT",
	"burst_ratio":                  "ROS_IDLE_BURST_RATIO",
	"minimum_observation_days":     "ROS_IDLE_MIN_OBSERVATION_DAYS",
	"gpu_sm_active_basis_points":   "ROS_IDLE_GPU_SM_ACTIVE_BP",
	"gpu_dram_active_basis_points": "ROS_IDLE_GPU_DRAM_ACTIVE_BP",
	"exclude_namespaces":           "ROS_IDLE_EXCLUDE_NAMESPACES",
	"exclude_workload_types":       "ROS_IDLE_EXCLUDE_WORKLOAD_TYPES",
}

func lockedIdleFieldsInUpdate(update map[string]json.RawMessage) []string {
	var locked []string
	for key := range update {
		if key == "thresholds" || key == "exclusions" {
			continue
		}
		if envKey, ok := idleUpdateKeyToEnv[key]; ok {
			if _, set := os.LookupEnv(envKey); set {
				locked = append(locked, key)
			}
		}
	}
	return locked
}

func validateIdleDetectionUpdate(rawUpdate json.RawMessage) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(rawUpdate, &top); err != nil {
		return fmt.Errorf("invalid request body: %w", err)
	}
	allowedTop := map[string]struct{}{
		"idle_detection": {},
		"locked_fields":  {},
	}
	v := &fieldValidator{}
	for key := range top {
		if _, ok := allowedTop[key]; !ok {
			v.addConstraint("body", fmt.Sprintf("unknown field %q", key))
		}
	}
	idleRaw, ok := top["idle_detection"]
	if !ok {
		v.addConstraint("idle_detection", "is required")
		return v.result()
	}
	var idle map[string]json.RawMessage
	if err := json.Unmarshal(idleRaw, &idle); err != nil {
		return fmt.Errorf("invalid idle_detection: %w", err)
	}
	allowedIdle := map[string]struct{}{
		"enabled":                    {},
		"thresholds":               {},
		"exclusions":               {},
		"cpu_utilization_percent":    {},
		"memory_utilization_percent": {},
		"burst_ratio":                {},
		"minimum_observation_days":   {},
		"gpu_sm_active_basis_points": {},
		"gpu_dram_active_basis_points": {},
		"exclude_namespaces":         {},
		"exclude_workload_types":     {},
	}
	for key, val := range idle {
		if _, ok := allowedIdle[key]; !ok {
			v.addConstraint("idle_detection", fmt.Sprintf("unknown field %q", key))
			continue
		}
		validateIdleField(v, key, val)
	}
	return v.result()
}

func validateIdleField(v *fieldValidator, key string, val json.RawMessage) {
	switch key {
	case "enabled":
		var b bool
		if json.Unmarshal(val, &b) != nil {
			v.addConstraint(key, "must be a boolean")
		}
	case "thresholds":
		var th map[string]json.RawMessage
		if json.Unmarshal(val, &th) != nil {
			v.addConstraint("thresholds", "must be an object")
			return
		}
		for tk, tv := range th {
			validateIdleThresholdKey(v, "thresholds."+tk, tk, tv)
		}
	case "exclusions":
		var ex struct {
			Namespaces    []string `json:"namespaces"`
			WorkloadTypes []string `json:"workload_types"`
		}
		if json.Unmarshal(val, &ex) != nil {
			v.addConstraint("exclusions", "must be an object")
			return
		}
		if len(ex.Namespaces) > 50 {
			v.addConstraint("exclusions.namespaces", "must have at most 50 entries")
		}
		for _, wt := range ex.WorkloadTypes {
			if _, ok := validIdleWorkloadTypes[wt]; !ok {
				v.addConstraint("exclusions.workload_types", fmt.Sprintf("invalid workload type %q", wt))
			}
		}
	default:
		validateIdleThresholdKey(v, key, key, val)
	}
}

func validateIdleThresholdKey(v *fieldValidator, path, key string, val json.RawMessage) {
	switch key {
	case "cpu_utilization_percent":
		var n int64
		if json.Unmarshal(val, &n) != nil {
			v.addConstraint(path, "must be an integer")
			return
		}
		v.addRangeInt64(path, n, 1, 50)
	case "memory_utilization_percent":
		var n int64
		if json.Unmarshal(val, &n) != nil {
			v.addConstraint(path, "must be an integer")
			return
		}
		v.addRangeInt64(path, n, 1, 50)
	case "burst_ratio":
		var n int64
		if json.Unmarshal(val, &n) != nil {
			v.addConstraint(path, "must be an integer")
			return
		}
		v.addRangeInt64(path, n, 2, 100)
	case "minimum_observation_days":
		var n int
		if json.Unmarshal(val, &n) != nil {
			v.addConstraint(path, "must be an integer")
			return
		}
		v.addRangeInt(path, n, 3, 90)
	case "gpu_sm_active_basis_points", "gpu_dram_active_basis_points":
		var n int64
		if json.Unmarshal(val, &n) != nil {
			v.addConstraint(path, "must be an integer")
			return
		}
		v.addRangeInt64(path, n, 100, 5000)
	}
}

// DeleteIdleDetectionSettings removes tenant idle detection overrides.
func DeleteIdleDetectionSettings(ctx context.Context, pool *pgxpool.Pool, orgID string) error {
	_, err := pool.Exec(ctx, `
		DELETE FROM recommendation_thresholds
		WHERE org_id = $1 AND recommendation_type = $2`, orgID, idleDetectionRecommendationType)
	if err != nil {
		return fmt.Errorf("delete idle detection settings: %w", err)
	}
	InvalidateThresholdCache(orgID, idleDetectionRecommendationType)
	return nil
}
