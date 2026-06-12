package engine

import (
	"context"
	"database/sql"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

const termConfigCacheTTL = 60 * time.Second

type termConfigCacheEntry struct {
	terms []TermConfig
	until time.Time
}

type termConfigCacheKey struct {
	orgID              string
	recommendationType string
}

var (
	termConfigMu    sync.RWMutex
	termConfigCache = map[termConfigCacheKey]termConfigCacheEntry{}
)

var termNames = [3]string{"short", "medium", "long"}

// InvalidateTermCache removes cached term entries for an org + recommendation type,
// ensuring subsequent calls to LoadTermConfigCached will re-read from DB.
func InvalidateTermCache(orgID, recommendationType string) {
	termConfigMu.Lock()
	delete(termConfigCache, termConfigCacheKey{orgID: orgID, recommendationType: recommendationType})
	termConfigMu.Unlock()
}

// LoadTermConfigCached returns term configurations for an org and recommendation type,
// applying the precedence: admin env var > tenant DB override > plugin default.
// Results are cached for termConfigCacheTTL (60s) per org+type combination.
func LoadTermConfigCached(ctx context.Context, pool *pgxpool.Pool, orgID, recommendationType string) ([]TermConfig, error) {
	if pool == nil {
		return DefaultTermsForPlugin(recommendationType), nil
	}
	now := time.Now().UTC()
	key := termConfigCacheKey{orgID: orgID, recommendationType: recommendationType}

	termConfigMu.RLock()
	e, ok := termConfigCache[key]
	termConfigMu.RUnlock()
	if ok && now.Before(e.until) {
		return e.terms, nil
	}

	terms, err := LoadTermConfig(ctx, pool, orgID, recommendationType)
	if err != nil {
		return nil, err
	}
	termConfigMu.Lock()
	termConfigCache[key] = termConfigCacheEntry{terms: terms, until: now.Add(termConfigCacheTTL)}
	termConfigMu.Unlock()
	return terms, nil
}

// DefaultTerms returns the legacy hardcoded defaults (backward compat for callers
// that don't yet specify a recommendation type).
func DefaultTerms() []TermConfig {
	return []TermConfig{
		{Name: "short", WindowDays: 1, MinDataDays: 1, DecayHalfLifeHours: 0},
		{Name: "medium", WindowDays: 7, MinDataDays: 3, DecayHalfLifeHours: 168},
		{Name: "long", WindowDays: 15, MinDataDays: 7, DecayHalfLifeHours: 360},
	}
}

// DefaultTermsForPlugin returns plugin-specific defaults from the TermProvider trait,
// falling back to the legacy global defaults if the plugin doesn't implement TermProvider.
func DefaultTermsForPlugin(recommendationType string) []TermConfig {
	for _, tp := range plugin.ByTrait[plugin.TermProvider]() {
		if tp.Name() == recommendationType {
			pTerms := tp.DefaultTerms()
			return pluginTermsToEngine(pTerms)
		}
	}
	return DefaultTerms()
}

// LoadTermConfig resolves effective terms for an org + recommendation type.
// Precedence per term: admin env var (locked) > tenant DB > plugin default.
func LoadTermConfig(ctx context.Context, pool *pgxpool.Pool, orgID, recommendationType string) ([]TermConfig, error) {
	defaults := DefaultTermsForPlugin(recommendationType)

	// Load tenant overrides from DB (skipped when platform settings lock is active).
	var dbTerms map[int]TermConfig
	if !ShouldSkipTermTenantOverrides(recommendationType) {
		var err error
		dbTerms, err = loadDBTerms(ctx, pool, orgID, recommendationType)
		if err != nil {
			return nil, err
		}
	}

	// Build effective terms: for each position, apply precedence.
	result := make([]TermConfig, 3)
	for i, name := range termNames {
		// Start with plugin default.
		result[i] = defaults[i]

		// Apply DB override (if exists and not locked).
		if dbTerm, ok := dbTerms[i]; ok {
			if !IsTermLocked(recommendationType, name) {
				result[i] = dbTerm
			}
		}

		// Apply env var override (always wins if set).
		if envTerm, ok := loadEnvTerm(recommendationType, name, defaults[i]); ok {
			result[i] = envTerm
		}
	}

	return result, nil
}

// loadDBTerms reads tenant-specific overrides from org_recommendation_terms.
func loadDBTerms(ctx context.Context, pool *pgxpool.Pool, orgID, recommendationType string) (map[int]TermConfig, error) {
	rows, err := pool.Query(ctx,
		`SELECT term_ord, window_days, min_data_days, decay_halflife_hours
		 FROM org_recommendation_terms
		 WHERE org_id = $1 AND recommendation_type = $2
		 ORDER BY term_ord`, orgID, recommendationType)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int]TermConfig)
	for rows.Next() {
		var ord int
		var windowDays, minDataDays int
		var decayHL sql.NullFloat64
		if err := rows.Scan(&ord, &windowDays, &minDataDays, &decayHL); err != nil {
			return nil, err
		}
		tc := TermConfig{
			Name:        termNames[ord-1],
			WindowDays:  windowDays,
			MinDataDays: minDataDays,
		}
		if decayHL.Valid {
			tc.DecayHalfLifeHours = decayHL.Float64
		} else {
			// Tenant customized the window but left decay_halflife_hours NULL:
			// scale decay shape with the window instead of plugin defaults.
			tc.DecayHalfLifeHours = DeriveDecayHalfLifeHours(windowDays)
		}
		result[ord-1] = tc
	}
	return result, rows.Err()
}

// loadEnvTerm checks if admin env vars override a specific term for a recommendation type.
// Env var format: ROS_TERMS_<PLUGIN>_<TERM>_WINDOW_DAYS, etc.
// Returns (TermConfig, true) if any env var is set for this term.
func loadEnvTerm(recommendationType, termName string, fallback TermConfig) (TermConfig, bool) {
	prefix := config.TermEnvPrefix(recommendationType, termName)
	tc := fallback
	tc.Name = termName
	anySet := false
	windowOverridden := false
	minDataExplicit := false
	maxWin := PluginMaxWindowDays(recommendationType)

	if v := config.EnvString(prefix + "WINDOW_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= maxWin {
			tc.WindowDays = n
			anySet = true
			windowOverridden = true
		} else {
			logging.GetLogger().Warnf("term_config: invalid env %sWINDOW_DAYS=%q (must be 1-%d), ignoring", prefix, v, maxWin)
		}
	}
	if v := config.EnvString(prefix + "MIN_DATA_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 {
			tc.MinDataDays = n
			anySet = true
			minDataExplicit = true
		} else {
			logging.GetLogger().Warnf("term_config: invalid env %sMIN_DATA_DAYS=%q, ignoring", prefix, v)
		}
	}
	if v := config.EnvString(prefix + "DECAY_HALFLIFE_HOURS"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			tc.DecayHalfLifeHours = f
			anySet = true
		} else {
			logging.GetLogger().Warnf("term_config: invalid env %sDECAY_HALFLIFE_HOURS=%q, ignoring", prefix, v)
		}
	}

	// Auto-derive MinDataDays from the new window if window changed but min wasn't explicitly set.
	if windowOverridden && !minDataExplicit {
		tc.MinDataDays = ComputeMinDataDays(tc.WindowDays)
	}

	// Validate: min_data_days must not exceed window_days.
	if anySet && tc.MinDataDays > tc.WindowDays {
		logging.GetLogger().Warnf(
			"term_config: env %sMIN_DATA_DAYS=%d exceeds WINDOW_DAYS=%d, clamping to window_days",
			prefix, tc.MinDataDays, tc.WindowDays)
		tc.MinDataDays = tc.WindowDays
	}
	return tc, anySet
}

// IsTermLocked reports whether a specific term for a recommendation type is
// locked by admin environment variables (tenant cannot modify).
func IsTermLocked(recommendationType, termName string) bool {
	prefix := config.TermEnvPrefix(recommendationType, termName)
	return config.EnvString(prefix+"WINDOW_DAYS") != "" ||
		config.EnvString(prefix+"MIN_DATA_DAYS") != "" ||
		config.EnvString(prefix+"DECAY_HALFLIFE_HOURS") != ""
}

// MaxWindowDays returns the largest WindowDays across the given terms,
// with a floor of minFloor (use 0 for no floor).
func MaxWindowDays(terms []TermConfig, minFloor int) int {
	max := minFloor
	for _, tc := range terms {
		if tc.WindowDays > max {
			max = tc.WindowDays
		}
	}
	return max
}

// ComputeMinDataDays returns the minimum data days required for a given window.
// Rule: half the window, rounded down, but at least 1.
func ComputeMinDataDays(windowDays int) int {
	min := windowDays / 2
	if min < 1 {
		return 1
	}
	return min
}

// PluginMaxWindowDays returns the maximum allowed window_days for a given recommendation type.
// Falls back to 365 if the plugin is not found or doesn't implement TermProvider.
func PluginMaxWindowDays(recommendationType string) int {
	for _, tp := range plugin.ByTrait[plugin.TermProvider]() {
		if tp.Name() == recommendationType {
			return tp.MaxWindowDays()
		}
	}
	return 365
}

func pluginTermsToEngine(pts []plugin.TermConfig) []TermConfig {
	out := make([]TermConfig, len(pts))
	for i, pt := range pts {
		out[i] = TermConfig{
			Name:               pt.Name,
			WindowDays:         pt.WindowDays,
			MinDataDays:        pt.MinDataDays,
			DecayHalfLifeHours: pt.DecayHalfLifeHours,
		}
	}
	return out
}
