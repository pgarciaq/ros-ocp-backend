package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/librobne/bhschedule"
	"github.com/redhatinsights/ros-ocp-backend/librobne/engine"
	"github.com/redhatinsights/ros-ocp-backend/librobne/types"
	"gopkg.in/yaml.v3"
)

var allowedYAMLKeys = map[string]struct{}{
	"org_id": {}, "cluster_uuid": {}, "now": {}, "plugins": {},
	"terms": {}, "sizing": {}, "idle": {}, "staleness_hours": {},
	"business_hours": {}, "node": {}, "gpu": {}, "pvc": {}, "vm": {}, "quota": {}, "cluster_quota": {}, "snapshot": {},
}

var reservedYAMLKeys = []string{"node", "gpu", "pvc", "vm", "quota", "cluster_quota", "snapshot"}

var enabledPlugins = map[string]struct{}{
	"container": {}, "namespace": {}, "node": {}, "gpu": {}, "pvc": {}, "vm": {}, "quota": {}, "cluster_quota": {}, "snapshot": {},
}

var knownPlugins = map[string]struct{}{
	"container": {}, "node": {}, "namespace": {}, "gpu": {},
	"pvc": {}, "vm": {}, "quota": {}, "cluster_quota": {}, "snapshot": {},
}

type fileConfig struct {
	OrgID          string             `yaml:"org_id,omitempty"`
	ClusterUUID    string             `yaml:"cluster_uuid,omitempty"`
	Now            *string            `yaml:"now,omitempty"`
	Plugins        []string           `yaml:"plugins,omitempty"`
	Terms          []termYAML         `yaml:"terms,omitempty"`
	Sizing         sizingYAML         `yaml:"sizing,omitempty"`
	Idle           idleYAML           `yaml:"idle,omitempty"`
	StalenessHours *float64           `yaml:"staleness_hours,omitempty"`
	BusinessHours  *businessHoursYAML `yaml:"business_hours,omitempty"`
	Node           map[string]any     `yaml:"node,omitempty"`
	GPU            map[string]any     `yaml:"gpu,omitempty"`
	PVC            map[string]any     `yaml:"pvc,omitempty"`
	VM             map[string]any     `yaml:"vm,omitempty"`
	Quota          map[string]any     `yaml:"quota,omitempty"`
	ClusterQuota   map[string]any     `yaml:"cluster_quota,omitempty"`
	Snapshot       map[string]any     `yaml:"snapshot,omitempty"`
}

type termYAML struct {
	Name                        string  `yaml:"name"`
	WindowDays                  int     `yaml:"window_days"`
	MinDataDays                 int     `yaml:"min_data_days"`
	DecayHalfLifeHours          float64 `yaml:"decay_half_life_hours"`
	ReplicaTargetUtilizationPct int     `yaml:"replica_target_utilization_pct"`
}

type sizingYAML struct {
	CPUCostPercentile      float64 `yaml:"cpu_cost_percentile"`
	CPUPerfPercentile      float64 `yaml:"cpu_perf_percentile"`
	MemCostPercentile      float64 `yaml:"mem_cost_percentile"`
	MemPerfPercentile      float64 `yaml:"mem_perf_percentile"`
	MinMargin              float64 `yaml:"min_margin"`
	MaxMargin              float64 `yaml:"max_margin"`
	LimitMultiplier        float64 `yaml:"limit_multiplier"`
	CPUFloorMC             int64   `yaml:"cpu_floor_mc"`
	MemFloorKiB            int64   `yaml:"mem_floor_kib"`
	IdleCPUThresholdMC     int64   `yaml:"idle_cpu_threshold_mc"`
	IdleMemThresholdKiB    int64   `yaml:"idle_mem_threshold_kib"`
	MemTrendSlopeThreshold float64 `yaml:"mem_trend_slope_threshold"`
	LowConfidenceThreshold float32 `yaml:"low_confidence_threshold"`
	SparseDataThreshold    int     `yaml:"sparse_data_threshold"`
}

type idleYAML struct {
	Enabled              bool     `yaml:"enabled"`
	ZombieCPUP95MC       int64    `yaml:"zombie_cpu_p95_mc"`
	ZombieCPUPeakMC      int64    `yaml:"zombie_cpu_peak_mc"`
	IdleCPUUtilPct       int64    `yaml:"idle_cpu_util_pct"`
	IdleMemUtilPct       int64    `yaml:"idle_mem_util_pct"`
	BurstRatio           int64    `yaml:"burst_ratio"`
	MinObservationDays   int      `yaml:"min_observation_days"`
	ExcludeNamespaces    []string `yaml:"exclude_namespaces"`
	ExcludeWorkloadTypes []string `yaml:"exclude_workload_types"`
}

// businessHoursYAML is the cluster-wide CLI schedule (not Settings JSON nesting).
type businessHoursYAML struct {
	Enabled        *bool    `yaml:"enabled"`
	Timezone       string   `yaml:"timezone,omitempty"`
	Days           []string `yaml:"days,omitempty"`
	StartTime      string   `yaml:"start_time,omitempty"`
	EndTime        string   `yaml:"end_time,omitempty"`
	OffHoursWeight *float64 `yaml:"off_hours_weight,omitempty"`
}

var (
	hhmmRE      = regexp.MustCompile(`^([01][0-9]|2[0-3]):[0-5][0-9]$`)
	weekdayYAML = map[string]struct{}{
		"sunday": {}, "monday": {}, "tuesday": {}, "wednesday": {},
		"thursday": {}, "friday": {}, "saturday": {},
	}
)

func loadFileConfig(env overlayEnv, configFlag string) (fileConfig, error) {
	base, err := compiledDefaultMap()
	if err != nil {
		return fileConfig{}, err
	}
	termsPresent := false
	pluginsPresent := false
	if user := firstExistingUserFile(env, "robne.yaml", ".robne.yaml"); user != "" {
		if err := overlayYAMLFile(base, user, &termsPresent, &pluginsPresent); err != nil {
			return fileConfig{}, err
		}
	}
	if configFlag != "" {
		if err := overlayYAMLFile(base, configFlag, &termsPresent, &pluginsPresent); err != nil {
			return fileConfig{}, err
		}
	} else {
		cwd := filepath.Join(env.Cwd, "robne.yaml")
		if fileExists(cwd) {
			if err := overlayYAMLFile(base, cwd, &termsPresent, &pluginsPresent); err != nil {
				return fileConfig{}, err
			}
		}
	}
	if err := rejectEnabledReserved(base); err != nil {
		return fileConfig{}, err
	}
	cfg, err := mapToFileConfig(base)
	if err != nil {
		return fileConfig{}, err
	}
	if termsPresent && len(cfg.Terms) == 0 {
		return fileConfig{}, fmt.Errorf("terms: is empty; omit the key to use compiled defaults")
	}
	if pluginsPresent && len(cfg.Plugins) == 0 {
		return fileConfig{}, fmt.Errorf("plugins: is empty; omit the key to enable all shipped plugins")
	}
	if err := validateFileConfig(cfg); err != nil {
		return fileConfig{}, err
	}
	return cfg, nil
}

func compiledDefaultMap() (map[string]any, error) {
	def := engine.DefaultEngineConfig("", "", time.Time{})
	hours := def.StalenessThreshold.Hours()
	cfg := fileConfig{
		Terms:          termsFromEngine(def.Terms),
		Sizing:         sizingFromEngine(def.Sizing),
		Idle:           idleFromEngine(def.Idle),
		StalenessHours: &hours,
	}
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := yaml.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func overlayYAMLFile(base map[string]any, path string, termsPresent, pluginsPresent *bool) error {
	raw, err := os.ReadFile(filepath.Clean(path)) //nolint:gosec // G304: explicit CLI/config overlay path
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil
	}
	if err := rejectUnknownYAMLKeys(raw, path); err != nil {
		return err
	}
	var overlay map[string]any
	if err := yaml.Unmarshal(raw, &overlay); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if overlay == nil {
		return nil
	}
	if sz, ok := overlay["sizing"]; ok {
		if err := requireCompleteSizing(sz, path); err != nil {
			return err
		}
	}
	if _, ok := overlay["terms"]; ok {
		*termsPresent = true
	}
	if _, ok := overlay["plugins"]; ok {
		*pluginsPresent = true
	}
	for k, v := range overlay {
		base[k] = v
	}
	return nil
}

// requiredSizingKeys is every field on sizingYAML. A later file's sizing: replaces
// the whole map, so a partial block would zero omitted knobs (min_margin, floors, …).
var requiredSizingKeys = []string{
	"cpu_cost_percentile", "cpu_perf_percentile", "mem_cost_percentile", "mem_perf_percentile",
	"min_margin", "max_margin", "limit_multiplier", "cpu_floor_mc", "mem_floor_kib",
	"idle_cpu_threshold_mc", "idle_mem_threshold_kib", "mem_trend_slope_threshold",
	"low_confidence_threshold", "sparse_data_threshold",
}

func requireCompleteSizing(v any, path string) error {
	m, ok := asStringMap(v)
	if !ok {
		return fmt.Errorf("%s: sizing: must be a mapping (copy cmd/robne/robne.yaml.sample or omit the key)", path)
	}
	var missing []string
	for _, k := range requiredSizingKeys {
		if _, ok := m[k]; !ok {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s: sizing: replaces the whole key; missing %s (copy cmd/robne/robne.yaml.sample or omit sizing:)", path, strings.Join(missing, ", "))
	}
	return nil
}

func asStringMap(v any) (map[string]any, bool) {
	switch m := v.(type) {
	case map[string]any:
		return m, true
	case map[any]any:
		out := make(map[string]any, len(m))
		for k, val := range m {
			ks, ok := k.(string)
			if !ok {
				return nil, false
			}
			out[ks] = val
		}
		return out, true
	default:
		return nil, false
	}
}

func rejectUnknownYAMLKeys(raw []byte, path string) error {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var cfg fileConfig
	if err := dec.Decode(&cfg); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	var probe map[string]any
	if err := yaml.Unmarshal(raw, &probe); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	for k := range probe {
		if _, ok := allowedYAMLKeys[k]; !ok {
			return fmt.Errorf("%s: unknown YAML key %q", path, k)
		}
	}
	return nil
}

func rejectEnabledReserved(m map[string]any) error {
	for _, key := range reservedYAMLKeys {
		v, ok := m[key]
		if !ok || v == nil {
			continue
		}
		nested, ok := v.(map[string]any)
		if !ok {
			continue
		}
		en, ok := nested["enabled"]
		if !ok {
			continue
		}
		if b, ok := en.(bool); ok && b {
			return fmt.Errorf("%s is not supported in Phase 1 (set enabled: false or omit the key)", key)
		}
	}
	return nil
}

func mapToFileConfig(m map[string]any) (fileConfig, error) {
	b, err := yaml.Marshal(m)
	if err != nil {
		return fileConfig{}, err
	}
	var cfg fileConfig
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return fileConfig{}, err
	}
	return cfg, nil
}

func validateFileConfig(cfg fileConfig) error {
	for _, p := range []struct {
		name string
		v    float64
	}{
		{"sizing.cpu_cost_percentile", cfg.Sizing.CPUCostPercentile},
		{"sizing.cpu_perf_percentile", cfg.Sizing.CPUPerfPercentile},
		{"sizing.mem_cost_percentile", cfg.Sizing.MemCostPercentile},
		{"sizing.mem_perf_percentile", cfg.Sizing.MemPerfPercentile},
	} {
		if p.v <= 0 || p.v > 1 {
			return fmt.Errorf("%s must be in (0, 1], got %v", p.name, p.v)
		}
	}
	for _, name := range cfg.Plugins {
		if _, ok := knownPlugins[name]; !ok {
			return fmt.Errorf("unknown plugin %q", name)
		}
	}
	return validateBusinessHours(cfg)
}

func validatePlugins(cfg fileConfig, flag string) error {
	list := resolvedPlugins(cfg, flag)
	for _, p := range list {
		if _, ok := knownPlugins[p]; !ok {
			return fmt.Errorf("unknown plugin %q", p)
		}
		if _, ok := enabledPlugins[p]; !ok {
			return fmt.Errorf("plugin %q is not supported in Phase 1", p)
		}
	}
	return rejectExplicitUnsupportedBHPlugins(cfg, flag)
}

// rejectExplicitUnsupportedBHPlugins hard-errors YAML business_hours with an
// explicit --plugins / YAML plugins: list that includes node, gpu, or vm.
// Implicit default-all still runs those plugins on all_hours. CLI JSON BH
// siblings for those entities are #487; product BH for node/GPU/VM is shipped.
func rejectExplicitUnsupportedBHPlugins(cfg fileConfig, flag string) error {
	if !businessHoursEnabled(cfg) || !pluginsExplicit(cfg, flag) {
		return nil
	}
	for _, p := range resolvedPlugins(cfg, flag) {
		switch p {
		case "node", "gpu", "vm":
			return fmt.Errorf("YAML business_hours with --plugins %s is not supported (node/GPU/VM BH CLI JSON is #487)", p)
		}
	}
	return nil
}

func allShippedPlugins() []string {
	return []string{"container", "namespace", "node", "gpu", "pvc", "vm", "quota", "cluster_quota", "snapshot"}
}

func pluginsExplicit(cfg fileConfig, flag string) bool {
	return strings.TrimSpace(flag) != "" || len(cfg.Plugins) > 0
}

func resolvedPlugins(cfg fileConfig, flag string) []string {
	var list []string
	if flag != "" {
		for _, p := range strings.Split(flag, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			list = append(list, p)
		}
	} else {
		list = append(list, cfg.Plugins...)
	}
	if len(list) == 0 {
		return allShippedPlugins()
	}
	return list
}

func pluginEnabled(plugins []string, name string) bool {
	for _, p := range plugins {
		if p == name {
			return true
		}
	}
	return false
}

func engineConfigFromFile(cfg fileConfig, orgID, clusterUUID string, now time.Time) types.EngineConfig {
	ec := engine.DefaultEngineConfig(orgID, clusterUUID, now)
	if len(cfg.Terms) > 0 {
		ec.Terms = termsToEngine(cfg.Terms)
	}
	ec.Sizing = sizingToEngine(cfg.Sizing)
	ec.Idle = idleToEngine(cfg.Idle)
	if cfg.StalenessHours != nil {
		ec.StalenessThreshold = time.Duration(*cfg.StalenessHours * float64(time.Hour))
	}
	return ec
}

func parseNow(flag string, cfg fileConfig, maxEnd time.Time) (time.Time, error) {
	if flag != "" {
		t, err := time.Parse(time.RFC3339, flag)
		if err != nil {
			t, err = time.Parse(time.RFC3339Nano, flag)
			if err != nil {
				return time.Time{}, fmt.Errorf("--now: %w", err)
			}
		}
		return t.UTC(), nil
	}
	if cfg.Now != nil && strings.TrimSpace(*cfg.Now) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(*cfg.Now))
		if err != nil {
			t, err = time.Parse(time.RFC3339Nano, strings.TrimSpace(*cfg.Now))
			if err != nil {
				return time.Time{}, fmt.Errorf("yaml now: %w", err)
			}
		}
		return t.UTC(), nil
	}
	if maxEnd.IsZero() {
		return time.Time{}, fmt.Errorf("cannot resolve engine clock: pass --now, set YAML now, or provide rows with interval_end")
	}
	return maxEnd.UTC(), nil
}

func termsFromEngine(terms []types.TermConfig) []termYAML {
	out := make([]termYAML, 0, len(terms))
	for _, t := range terms {
		out = append(out, termYAML{
			Name:                        t.Name,
			WindowDays:                  t.WindowDays,
			MinDataDays:                 t.MinDataDays,
			DecayHalfLifeHours:          t.DecayHalfLifeHours,
			ReplicaTargetUtilizationPct: t.ReplicaTargetUtilizationPct,
		})
	}
	return out
}

func termsToEngine(terms []termYAML) []types.TermConfig {
	out := make([]types.TermConfig, 0, len(terms))
	for _, t := range terms {
		out = append(out, types.TermConfig{
			Name:                        t.Name,
			WindowDays:                  t.WindowDays,
			MinDataDays:                 t.MinDataDays,
			DecayHalfLifeHours:          t.DecayHalfLifeHours,
			ReplicaTargetUtilizationPct: t.ReplicaTargetUtilizationPct,
		})
	}
	return out
}

func sizingFromEngine(s types.SizingThresholdSettings) sizingYAML {
	return sizingYAML{
		CPUCostPercentile:      s.CPUCostPercentile,
		CPUPerfPercentile:      s.CPUPerfPercentile,
		MemCostPercentile:      s.MemCostPercentile,
		MemPerfPercentile:      s.MemPerfPercentile,
		MinMargin:              s.MinMargin,
		MaxMargin:              s.MaxMargin,
		LimitMultiplier:        s.LimitMultiplier,
		CPUFloorMC:             s.CPUFloorMC,
		MemFloorKiB:            s.MemFloorKiB,
		IdleCPUThresholdMC:     s.IdleCPUThresholdMC,
		IdleMemThresholdKiB:    s.IdleMemThresholdKiB,
		MemTrendSlopeThreshold: s.MemTrendSlopeThreshold,
		LowConfidenceThreshold: s.LowConfidenceThreshold,
		SparseDataThreshold:    s.SparseDataThreshold,
	}
}

func sizingToEngine(s sizingYAML) types.SizingThresholdSettings {
	return types.SizingThresholdSettings{
		CPUCostPercentile:      s.CPUCostPercentile,
		CPUPerfPercentile:      s.CPUPerfPercentile,
		MemCostPercentile:      s.MemCostPercentile,
		MemPerfPercentile:      s.MemPerfPercentile,
		MinMargin:              s.MinMargin,
		MaxMargin:              s.MaxMargin,
		LimitMultiplier:        s.LimitMultiplier,
		CPUFloorMC:             s.CPUFloorMC,
		MemFloorKiB:            s.MemFloorKiB,
		IdleCPUThresholdMC:     s.IdleCPUThresholdMC,
		IdleMemThresholdKiB:    s.IdleMemThresholdKiB,
		MemTrendSlopeThreshold: s.MemTrendSlopeThreshold,
		LowConfidenceThreshold: s.LowConfidenceThreshold,
		SparseDataThreshold:    s.SparseDataThreshold,
	}
}

func idleFromEngine(i types.IdleConfig) idleYAML {
	return idleYAML{
		Enabled:              i.Enabled,
		ZombieCPUP95MC:       i.ZombieCPUP95MC,
		ZombieCPUPeakMC:      i.ZombieCPUPeakMC,
		IdleCPUUtilPct:       i.IdleCPUUtilPct,
		IdleMemUtilPct:       i.IdleMemUtilPct,
		BurstRatio:           i.BurstRatio,
		MinObservationDays:   i.MinObservationDays,
		ExcludeNamespaces:    i.ExcludeNamespaces,
		ExcludeWorkloadTypes: i.ExcludeWorkloadTypes,
	}
}

func idleToEngine(i idleYAML) types.IdleConfig {
	return types.IdleConfig{
		Enabled:              i.Enabled,
		ZombieCPUP95MC:       i.ZombieCPUP95MC,
		ZombieCPUPeakMC:      i.ZombieCPUPeakMC,
		IdleCPUUtilPct:       i.IdleCPUUtilPct,
		IdleMemUtilPct:       i.IdleMemUtilPct,
		BurstRatio:           i.BurstRatio,
		MinObservationDays:   i.MinObservationDays,
		ExcludeNamespaces:    i.ExcludeNamespaces,
		ExcludeWorkloadTypes: i.ExcludeWorkloadTypes,
	}
}

func validateBusinessHours(cfg fileConfig) error {
	bh := cfg.BusinessHours
	if bh == nil {
		return nil
	}
	if bh.Enabled == nil {
		return fmt.Errorf("business_hours.enabled is required when business_hours: is set")
	}
	if bh.OffHoursWeight != nil {
		w := *bh.OffHoursWeight
		if w < 0 || w > 1 {
			return fmt.Errorf("business_hours.off_hours_weight must be in [0, 1], got %v", w)
		}
	}
	if !*bh.Enabled {
		return nil
	}
	if strings.TrimSpace(bh.Timezone) == "" {
		return fmt.Errorf("business_hours.timezone is required when enabled")
	}
	if _, err := time.LoadLocation(bh.Timezone); err != nil {
		return fmt.Errorf("business_hours.timezone: invalid IANA timezone %q", bh.Timezone)
	}
	if len(bh.Days) == 0 {
		return fmt.Errorf("business_hours.days is required when enabled")
	}
	for _, d := range bh.Days {
		if _, ok := weekdayYAML[d]; !ok {
			return fmt.Errorf("business_hours.days: %q is not a lowercase weekday (monday–sunday)", d)
		}
	}
	if err := validateHHMM("business_hours.start_time", bh.StartTime); err != nil {
		return err
	}
	if err := validateHHMM("business_hours.end_time", bh.EndTime); err != nil {
		return err
	}
	if bh.StartTime == bh.EndTime {
		return fmt.Errorf("business_hours: start_time and end_time must differ (zero-width window)")
	}
	s := scheduleFromYAML(bh)
	if err := s.InitLocation(); err != nil {
		return fmt.Errorf("business_hours: %w", err)
	}
	return nil
}

func validateHHMM(field, v string) error {
	if !hhmmRE.MatchString(v) {
		return fmt.Errorf("%s must be HH:MM (00:00–23:59), got %q", field, v)
	}
	return nil
}

func businessHoursEnabled(cfg fileConfig) bool {
	return cfg.BusinessHours != nil && cfg.BusinessHours.Enabled != nil && *cfg.BusinessHours.Enabled
}

func scheduleFromYAML(bh *businessHoursYAML) bhschedule.Schedule {
	if bh == nil || bh.Enabled == nil {
		return bhschedule.AllHoursSchedule()
	}
	w := 0.0
	if bh.OffHoursWeight != nil {
		w = *bh.OffHoursWeight
	}
	return bhschedule.Schedule{
		Enabled:        *bh.Enabled,
		Timezone:       bh.Timezone,
		Days:           bh.Days,
		StartTime:      bh.StartTime,
		EndTime:        bh.EndTime,
		OffHoursWeight: w,
	}
}
