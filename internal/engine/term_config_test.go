package engine

import (
	"context"
	"os"
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
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	terms, err := LoadTermConfig(ctx, pool, "org-no-custom-terms", "container")
	require.NoError(t, err)
	require.Len(t, terms, 3)
	assert.Equal(t, "short", terms[0].Name)
	assert.Equal(t, 1, terms[0].WindowDays)
}

func TestLoadTermConfig_ReturnsCustomTerms_WhenOverridesExist(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx,
		`INSERT INTO org_recommendation_terms (org_id, recommendation_type, term_ord, window_days, min_data_days, decay_halflife_hours)
		 VALUES ('org-custom', 'container', 1, 3, 1, 48),
		        ('org-custom', 'container', 2, 14, 7, 336),
		        ('org-custom', 'container', 3, 30, 15, 720)`)
	require.NoError(t, err)

	terms, err := LoadTermConfig(ctx, pool, "org-custom", "container")
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

func TestLoadTermConfig_PerPluginIsolation(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	_, err := pool.Exec(ctx,
		`INSERT INTO org_recommendation_terms (org_id, recommendation_type, term_ord, window_days, min_data_days, decay_halflife_hours)
		 VALUES ('org-iso', 'pvc', 1, 30, 14, 0),
		        ('org-iso', 'pvc', 2, 60, 30, 0),
		        ('org-iso', 'pvc', 3, 90, 45, 0)`)
	require.NoError(t, err)

	// PVC should see overrides.
	pvcTerms, err := LoadTermConfig(ctx, pool, "org-iso", "pvc")
	require.NoError(t, err)
	assert.Equal(t, 30, pvcTerms[0].WindowDays)

	// Container should see defaults (no overrides for container).
	containerTerms, err := LoadTermConfig(ctx, pool, "org-iso", "container")
	require.NoError(t, err)
	assert.Equal(t, 1, containerTerms[0].WindowDays)
}

func TestLoadTermConfig_EnvVarOverridesDBAndLocks(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// Set env var for container long term.
	t.Setenv("ROS_TERMS_CONTAINER_LONG_WINDOW_DAYS", "45")
	t.Setenv("ROS_TERMS_CONTAINER_LONG_MIN_DATA_DAYS", "20")
	t.Setenv("ROS_TERMS_CONTAINER_LONG_DECAY_HALFLIFE_HOURS", "500")

	// Insert DB override for long term (should be ignored since env wins).
	_, err := pool.Exec(ctx,
		`INSERT INTO org_recommendation_terms (org_id, recommendation_type, term_ord, window_days, min_data_days, decay_halflife_hours)
		 VALUES ('org-env-test', 'container', 3, 30, 15, 720)`)
	require.NoError(t, err)

	terms, err := LoadTermConfig(ctx, pool, "org-env-test", "container")
	require.NoError(t, err)
	require.Len(t, terms, 3)

	// Long term should reflect env vars, not DB.
	assert.Equal(t, "long", terms[2].Name)
	assert.Equal(t, 45, terms[2].WindowDays)
	assert.Equal(t, 20, terms[2].MinDataDays)
	assert.InDelta(t, 500.0, terms[2].DecayHalfLifeHours, 0.001)

	// Verify it's locked.
	assert.True(t, IsTermLocked("container", "long"))
	assert.False(t, IsTermLocked("container", "short"))
}

func TestIsTermLocked(t *testing.T) {
	// Clean state — nothing locked.
	assert.False(t, IsTermLocked("pvc", "short"))

	t.Setenv("ROS_TERMS_PVC_SHORT_WINDOW_DAYS", "14")
	assert.True(t, IsTermLocked("pvc", "short"))
	assert.False(t, IsTermLocked("pvc", "medium"))
	assert.False(t, IsTermLocked("node", "short"))
}

func TestLoadEnvTerm(t *testing.T) {
	fallback := TermConfig{Name: "medium", WindowDays: 7, MinDataDays: 3, DecayHalfLifeHours: 168}

	t.Run("no env vars returns not set", func(t *testing.T) {
		os.Unsetenv("ROS_TERMS_NODE_MEDIUM_WINDOW_DAYS")
		os.Unsetenv("ROS_TERMS_NODE_MEDIUM_MIN_DATA_DAYS")
		os.Unsetenv("ROS_TERMS_NODE_MEDIUM_DECAY_HALFLIFE_HOURS")
		_, ok := loadEnvTerm("node", "medium", fallback)
		assert.False(t, ok)
	})

	t.Run("partial env var sets only specified fields", func(t *testing.T) {
		t.Setenv("ROS_TERMS_NODE_MEDIUM_WINDOW_DAYS", "21")
		tc, ok := loadEnvTerm("node", "medium", fallback)
		assert.True(t, ok)
		assert.Equal(t, 21, tc.WindowDays)
		assert.Equal(t, 10, tc.MinDataDays) // auto-computed: 21/2 = 10
		assert.InDelta(t, 168.0, tc.DecayHalfLifeHours, 0.001) // from fallback
	})
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

func TestComputeMinDataDays(t *testing.T) {
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
			got := ComputeMinDataDays(tt.windowDays)
			assert.Equal(t, tt.wantMinData, got)
		})
	}
}

func TestPluginMaxWindowDays(t *testing.T) {
	tests := []struct {
		recType string
		want    int
	}{
		{"container", 90},
		{"namespace", 90},
		{"node", 90},
		{"gpu", 90},
		{"pvc", 365},
		{"unknown_plugin", 365}, // fallback
	}
	for _, tt := range tests {
		t.Run(tt.recType, func(t *testing.T) {
			assert.Equal(t, tt.want, PluginMaxWindowDays(tt.recType))
		})
	}
}

func TestInvalidateTermCache(t *testing.T) {
	// Populate cache manually then invalidate.
	key := "test-org-invalidate"
	recType := "container"

	// Prime the cache via LoadTermConfigCached (pool=nil → defaults).
	terms, err := LoadTermConfigCached(context.Background(), nil, key, recType)
	require.NoError(t, err)
	assert.Len(t, terms, 3)

	// Invalidate and verify no panic.
	InvalidateTermCache(key, recType)

	// Calling again should still return defaults (no panic, no stale data issue).
	terms2, err := LoadTermConfigCached(context.Background(), nil, key, recType)
	require.NoError(t, err)
	assert.Len(t, terms2, 3)
}
