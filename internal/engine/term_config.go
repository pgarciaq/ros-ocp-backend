package engine

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

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

	var customs []TermConfig
	for rows.Next() {
		var ord int
		var windowDays int
		var decayHL float32
		if err := rows.Scan(&ord, &windowDays, &decayHL); err != nil {
			return nil, err
		}
		customs = append(customs, TermConfig{
			Name:               termNames[ord-1],
			WindowDays:         windowDays,
			MinDataDays:        computeMinDataDays(windowDays),
			DecayHalfLifeHours: float64(decayHL),
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

// computeMinDataDays returns the minimum data days required for a given window.
// Rule: half the window, rounded down, but at least 1.
func computeMinDataDays(windowDays int) int {
	min := windowDays / 2
	if min < 1 {
		return 1
	}
	return min
}
