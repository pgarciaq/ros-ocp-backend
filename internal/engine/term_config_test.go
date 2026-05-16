package engine

import (
	"context"
	"testing"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultTerms(t *testing.T) {
	terms := DefaultTerms()
	require.Len(t, terms, 3)

	assert.Equal(t, "short", terms[0].Name)
	assert.Equal(t, 1, terms[0].WindowDays)
	assert.Equal(t, 1, terms[0].MinDataDays)
	assert.InDelta(t, 0.0, terms[0].DecayHalfLifeHours, 0.001)

	assert.Equal(t, "medium", terms[1].Name)
	assert.Equal(t, 7, terms[1].WindowDays)
	assert.Equal(t, 3, terms[1].MinDataDays)
	assert.InDelta(t, 168.0, terms[1].DecayHalfLifeHours, 0.001)

	assert.Equal(t, "long", terms[2].Name)
	assert.Equal(t, 15, terms[2].WindowDays)
	assert.Equal(t, 7, terms[2].MinDataDays)
	assert.InDelta(t, 360.0, terms[2].DecayHalfLifeHours, 0.001)
}

func TestLoadTermConfig_ReturnsDefaults_WhenNoOverrides(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	terms, err := LoadTermConfig(ctx, pool, "org-no-custom-terms")
	require.NoError(t, err)
	require.Len(t, terms, 3)
	assert.Equal(t, "short", terms[0].Name)
	assert.Equal(t, 1, terms[0].WindowDays)
}

func TestLoadTermConfig_ReturnsCustomTerms_WhenOverridesExist(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO org_recommendation_terms (org_id, term_ord, window_days, decay_halflife_hours)
		VALUES ('org-custom', 1, 3, 48), ('org-custom', 2, 14, 336), ('org-custom', 3, 30, 720)`)
	require.NoError(t, err)

	terms, err := LoadTermConfig(ctx, pool, "org-custom")
	require.NoError(t, err)
	require.Len(t, terms, 3)

	assert.Equal(t, "short", terms[0].Name)
	assert.Equal(t, 3, terms[0].WindowDays)
	assert.Equal(t, 1, terms[0].MinDataDays)
	assert.InDelta(t, 48.0, terms[0].DecayHalfLifeHours, 0.001)

	assert.Equal(t, "medium", terms[1].Name)
	assert.Equal(t, 14, terms[1].WindowDays)
	assert.Equal(t, 7, terms[1].MinDataDays)
	assert.InDelta(t, 336.0, terms[1].DecayHalfLifeHours, 0.001)

	assert.Equal(t, "long", terms[2].Name)
	assert.Equal(t, 30, terms[2].WindowDays)
	assert.Equal(t, 15, terms[2].MinDataDays)
	assert.InDelta(t, 720.0, terms[2].DecayHalfLifeHours, 0.001)
}

func TestLoadTermConfig_NULLDecayUsesDefaultHalfLifeForTermOrd(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx, `INSERT INTO org_recommendation_terms (org_id, term_ord, window_days, decay_halflife_hours)
		VALUES ('org-null-decay', 1, 5, NULL), ('org-null-decay', 2, 20, NULL)`)
	require.NoError(t, err)

	terms, err := LoadTermConfig(ctx, pool, "org-null-decay")
	require.NoError(t, err)
	require.Len(t, terms, 2)

	def := DefaultTerms()
	assert.InDelta(t, def[0].DecayHalfLifeHours, terms[0].DecayHalfLifeHours, 0.001,
		"NULL decay for short term should use default short half-life")
	assert.InDelta(t, def[1].DecayHalfLifeHours, terms[1].DecayHalfLifeHours, 0.001,
		"NULL decay for medium term should use default medium half-life")
	assert.Equal(t, 5, terms[0].WindowDays)
	assert.Equal(t, 20, terms[1].WindowDays)
}

func TestMaxWindowDays(t *testing.T) {
	tests := []struct {
		name     string
		terms    []TermConfig
		minFloor int
		want     int
	}{
		{"defaults with floor 30", DefaultTerms(), 30, 30},
		{"defaults with floor 0", DefaultTerms(), 0, 15},
		{"custom large window", []TermConfig{{WindowDays: 90}, {WindowDays: 7}}, 30, 90},
		{"empty terms returns floor", []TermConfig{}, 30, 30},
		{"nil terms returns floor", nil, 30, 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, MaxWindowDays(tt.terms, tt.minFloor))
		})
	}
}

func TestLoadTermConfig_MinDataDaysScaling(t *testing.T) {
	tests := []struct {
		name        string
		windowDays  int
		wantMinData int
	}{
		{"1-day window", 1, 1},
		{"2-day window", 2, 1},
		{"3-day window", 3, 1},
		{"7-day window", 7, 3},
		{"14-day window", 14, 7},
		{"15-day window", 15, 7},
		{"30-day window", 30, 15},
		{"90-day window", 90, 45},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeMinDataDays(tt.windowDays)
			assert.Equal(t, tt.wantMinData, got)
		})
	}
}
