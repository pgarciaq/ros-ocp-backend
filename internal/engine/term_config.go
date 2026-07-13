package engine

import (
	"context"
	"database/sql"
	"strconv"
	"sync"
	"time"

	"github.com/hashicorp/golang-lru/v2/expirable"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/engine/core"
	"github.com/redhatinsights/ros-ocp-backend/internal/logging"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

const termConfigCacheTTL = 60 * time.Second

type termConfigCacheKey struct {
	orgID              string
	recommendationType string
}

var (
	termCache     *expirable.LRU[termConfigCacheKey, []TermConfig]
	termCacheOnce sync.Once
)

var (
	termConfigCacheSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "rosocp_term_config_cache_size",
		Help: "Current number of entries in the term config LRU cache",
	})
	termConfigCacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_term_config_cache_hits_total",
		Help: "Term config cache lookups that returned a valid cached entry",
	})
	termConfigCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_term_config_cache_misses_total",
		Help: "Term config cache lookups that missed or found an expired entry",
	})
	termConfigCacheEvictions = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_term_config_cache_evictions_total",
		Help: "Term config cache entries evicted due to LRU capacity",
	})
)

var termNames = [3]string{"short", "medium", "long"}

// InitTermConfigCache initializes the bounded LRU cache for term configurations.
// Must be called once at startup (before any LoadTermConfigCached call).
func InitTermConfigCache(cfg *config.Config) {
	maxEntries := 1000
	if cfg != nil {
		maxEntries = cfg.EffectiveTermConfigCacheMaxEntries()
	}
	initTermConfigCacheWithSize(maxEntries)
}

func initTermConfigCacheWithSize(maxEntries int) {
	termCacheOnce.Do(func() {
		termCache = expirable.NewLRU[termConfigCacheKey, []TermConfig](
			maxEntries,
			func(_ termConfigCacheKey, _ []TermConfig) {
				termConfigCacheEvictions.Inc()
			},
			termConfigCacheTTL,
		)
	})
}

// ensureTermCache lazily initializes the cache with default settings if InitTermConfigCache
// was not called (e.g. in tests). Production code should always call InitTermConfigCache.
func ensureTermCache() {
	if termCache == nil {
		initTermConfigCacheWithSize(1000)
	}
}

// ResetTermCacheForTest replaces the term cache with a fresh instance for test isolation.
// Not safe for concurrent use; call only from test setup.
func ResetTermCacheForTest(maxEntries int) {
	termCacheOnce = sync.Once{}
	termCache = expirable.NewLRU[termConfigCacheKey, []TermConfig](
		maxEntries,
		func(_ termConfigCacheKey, _ []TermConfig) {
			termConfigCacheEvictions.Inc()
		},
		termConfigCacheTTL,
	)
}

// InvalidateTermCache removes cached term entries for an org + recommendation type,
// ensuring subsequent calls to LoadTermConfigCached will re-read from DB.
func InvalidateTermCache(orgID, recommendationType string) {
	ensureTermCache()
	termCache.Remove(termConfigCacheKey{orgID: orgID, recommendationType: recommendationType})
	termConfigCacheSize.Set(float64(termCache.Len()))
}

// LoadTermConfigCached returns term configurations for an org and recommendation type,
// applying the precedence: admin env var > tenant DB override > plugin default.
// Results are cached for termConfigCacheTTL (60s) per org+type combination with LRU eviction.
func LoadTermConfigCached(ctx context.Context, pool *pgxpool.Pool, orgID, recommendationType string) ([]TermConfig, error) {
	if pool == nil {
		return DefaultTermsForPlugin(recommendationType), nil
	}
	ensureTermCache()
	key := termConfigCacheKey{orgID: orgID, recommendationType: recommendationType}

	if terms, ok := termCache.Get(key); ok {
		termConfigCacheHits.Inc()
		return terms, nil
	}
	termConfigCacheMisses.Inc()

	terms, err := LoadTermConfig(ctx, pool, orgID, recommendationType)
	if err != nil {
		return nil, err
	}
	termCache.Add(key, terms)
	termConfigCacheSize.Set(float64(termCache.Len()))
	return terms, nil
}

// DefaultTerms returns the legacy hardcoded defaults (backward compat for callers
// that don't yet specify a recommendation type).
func DefaultTerms() []TermConfig {
	replicaPct := DefaultReplicaTargetUtilizationPctFromConfig()
	return []TermConfig{
		{Name: "short", WindowDays: 1, MinDataDays: 1, DecayHalfLifeHours: 0, ReplicaTargetUtilizationPct: replicaPct},
		{Name: "medium", WindowDays: 7, MinDataDays: 3, DecayHalfLifeHours: 168, ReplicaTargetUtilizationPct: replicaPct},
		{Name: "long", WindowDays: 15, MinDataDays: 7, DecayHalfLifeHours: 360, ReplicaTargetUtilizationPct: replicaPct},
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
	replicaTargetPct := DefaultReplicaTargetUtilizationPctFromConfig()
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

		result[i].ReplicaTargetUtilizationPct = replicaTargetPct
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

// MinDataDaysForTerm returns the MinDataDays value for the specified term name.
// If termName is empty, it uses the default term ("medium" by convention).
// Falls back to the maximum MinDataDays across all terms if no match is found.
func MinDataDaysForTerm(terms []TermConfig, termName string) int {
	if termName == "" {
		termName = "medium"
	}
	for _, tc := range terms {
		if tc.Name == termName {
			return tc.MinDataDays
		}
	}
	max := 0
	for _, tc := range terms {
		if tc.MinDataDays > max {
			max = tc.MinDataDays
		}
	}
	return max
}

// MaxWindowDays delegates to core.MaxWindowDays.
var MaxWindowDays = core.MaxWindowDays

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
	replicaPct := DefaultReplicaTargetUtilizationPctFromConfig()
	out := make([]TermConfig, len(pts))
	for i, pt := range pts {
		out[i] = TermConfig{
			Name:                        pt.Name,
			WindowDays:                  pt.WindowDays,
			MinDataDays:                 pt.MinDataDays,
			DecayHalfLifeHours:          pt.DecayHalfLifeHours,
			ReplicaTargetUtilizationPct: replicaPct,
		}
	}
	return out
}
