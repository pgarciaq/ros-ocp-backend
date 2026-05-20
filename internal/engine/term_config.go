package engine

import (
	"context"
	"database/sql"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const termConfigCacheTTL = 60 * time.Second

type termConfigCacheEntry struct {
	terms []TermConfig
	until time.Time
}

var (
	termConfigMu    sync.RWMutex
	termConfigByOrg = map[string]termConfigCacheEntry{}
)

// LoadTermConfigCached returns term configurations for an org, caching the
// result for termConfigCacheTTL (60s) to avoid repeated DB queries on hot paths.
func LoadTermConfigCached(ctx context.Context, pool *pgxpool.Pool, orgID string) ([]TermConfig, error) {
	if pool == nil {
		return DefaultTerms(), nil
	}
	now := time.Now().UTC()
	termConfigMu.RLock()
	e, ok := termConfigByOrg[orgID]
	termConfigMu.RUnlock()
	if ok && now.Before(e.until) {
		return e.terms, nil
	}

	terms, err := LoadTermConfig(ctx, pool, orgID)
	if err != nil {
		return nil, err
	}
	termConfigMu.Lock()
	termConfigByOrg[orgID] = termConfigCacheEntry{terms: terms, until: now.Add(termConfigCacheTTL)}
	termConfigMu.Unlock()
	return terms, nil
}

var termNames = [3]string{"short", "medium", "long"}

// DefaultTerms returns the hardcoded term configurations used when
// a customer has no overrides in org_recommendation_terms.
func DefaultTerms() []TermConfig {
	return []TermConfig{
		{Name: "short", WindowDays: 1, MinDataDays: 1, DecayHalfLifeHours: 0},
		{Name: "medium", WindowDays: 7, MinDataDays: 3, DecayHalfLifeHours: 168},
		{Name: "long", WindowDays: 15, MinDataDays: 7, DecayHalfLifeHours: 360},
	}
}

// LoadTermConfig loads term configurations for an org.
// If the org has custom overrides in org_recommendation_terms, those are used;
// otherwise DefaultTerms() is returned.
func LoadTermConfig(ctx context.Context, pool *pgxpool.Pool, orgID string) ([]TermConfig, error) {
	rows, err := pool.Query(ctx,
		`SELECT term_ord, window_days, decay_halflife_hours
		 FROM org_recommendation_terms
		 WHERE org_id = $1
		 ORDER BY term_ord`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	defaults := DefaultTerms()
	var customs []TermConfig
	for rows.Next() {
		var ord int
		var windowDays int
		var decayHL sql.NullFloat64
		if err := rows.Scan(&ord, &windowDays, &decayHL); err != nil {
			return nil, err
		}
		decay := defaults[ord-1].DecayHalfLifeHours
		if decayHL.Valid {
			decay = decayHL.Float64
		}
		customs = append(customs, TermConfig{
			Name:               termNames[ord-1],
			WindowDays:         windowDays,
			MinDataDays:        computeMinDataDays(windowDays),
			DecayHalfLifeHours: decay,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(customs) == 0 {
		return DefaultTerms(), nil
	}
	return customs, nil
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

// computeMinDataDays returns the minimum data days required for a given window.
// Rule: half the window, rounded down, but at least 1.
func computeMinDataDays(windowDays int) int {
	min := windowDays / 2
	if min < 1 {
		return 1
	}
	return min
}
