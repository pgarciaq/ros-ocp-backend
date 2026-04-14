package engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunRetentionSweep_DropsOldPartitions(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// Create an old partition (12 months ago) for recommendation_history
	old := time.Now().UTC().AddDate(0, -12, 0)
	oldMonth := time.Date(old.Year(), old.Month(), 1, 0, 0, 0, 0, time.UTC)
	oldSuffix := oldMonth.Format("200601")

	_, err := pool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS recommendation_history_%s PARTITION OF recommendation_history FOR VALUES FROM ('%s') TO ('%s')`,
		oldSuffix, oldMonth.Format("2006-01-02"), oldMonth.AddDate(0, 1, 0).Format("2006-01-02")))
	require.NoError(t, err)

	// Verify it exists
	var count int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM pg_class WHERE relname = $1`,
		fmt.Sprintf("recommendation_history_%s", oldSuffix)).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "old partition should exist before sweep")

	RunRetentionSweep(ctx, pool, 6)

	// Verify it was dropped
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM pg_class WHERE relname = $1`,
		fmt.Sprintf("recommendation_history_%s", oldSuffix)).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "old partition should be dropped")
}

func TestRunRetentionSweep_PreservesRecent(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// The migration already creates current + next 2 months partitions.
	// Verify they survive the sweep.
	now := time.Now().UTC()
	currentSuffix := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).Format("200601")

	RunRetentionSweep(ctx, pool, 6)

	var count int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM pg_class WHERE relname = $1`,
		fmt.Sprintf("recommendation_history_%s", currentSuffix)).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "current partition should be preserved")
}

func TestRunRetentionSweep_ConfigurableWindow(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// Create a partition 4 months ago
	old := time.Now().UTC().AddDate(0, -4, 0)
	oldMonth := time.Date(old.Year(), old.Month(), 1, 0, 0, 0, 0, time.UTC)
	oldSuffix := oldMonth.Format("200601")

	_, err := pool.Exec(ctx, fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS recommendation_history_%s PARTITION OF recommendation_history FOR VALUES FROM ('%s') TO ('%s')`,
		oldSuffix, oldMonth.Format("2006-01-02"), oldMonth.AddDate(0, 1, 0).Format("2006-01-02")))
	require.NoError(t, err)

	// With 3-month retention, the 4-month-old partition should be dropped
	RunRetentionSweep(ctx, pool, 3)

	var count int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM pg_class WHERE relname = $1`,
		fmt.Sprintf("recommendation_history_%s", oldSuffix)).Scan(&count)
	require.NoError(t, err)
	assert.Equal(t, 0, count, "4-month-old partition should be dropped with 3-month retention")
}

func TestExtractYearMonth(t *testing.T) {
	tests := []struct {
		part, parent, want string
	}{
		{"recommendation_history_202603", "recommendation_history", "202603"},
		{"container_usage_samples_202601", "container_usage_samples", "202601"},
		{"unrelated_table_202603", "recommendation_history", ""},
		{"recommendation_history_2026", "recommendation_history", ""},
	}
	for _, tt := range tests {
		got := extractYearMonth(tt.part, tt.parent)
		assert.Equal(t, tt.want, got, "extractYearMonth(%q, %q)", tt.part, tt.parent)
	}
}
