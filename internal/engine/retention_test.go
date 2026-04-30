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

func TestRunRetentionSweep_DropsOldNamespaceSamplePartitions(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// Create a partition 8 months in the past
	old := time.Now().UTC().AddDate(0, -8, 0)
	monthStart := time.Date(old.Year(), old.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	partName := fmt.Sprintf("namespace_usage_samples_%s", monthStart.Format("200601"))
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF namespace_usage_samples FOR VALUES FROM ('%s') TO ('%s')`,
		partName, monthStart.Format("2006-01-02"), monthEnd.Format("2006-01-02"),
	)
	_, err := pool.Exec(ctx, sql)
	require.NoError(t, err)

	// Verify partition exists
	partitions, err := listPartitions(ctx, pool, "namespace_usage_samples")
	require.NoError(t, err)
	found := false
	for _, p := range partitions {
		if p == partName {
			found = true
			break
		}
	}
	require.True(t, found, "old partition should exist before sweep")

	// Run retention with 6-month window
	RunRetentionSweep(ctx, pool, 6)

	// Verify partition was dropped
	partitions, err = listPartitions(ctx, pool, "namespace_usage_samples")
	require.NoError(t, err)
	for _, p := range partitions {
		assert.NotEqual(t, partName, p, "old partition should have been dropped")
	}
}

func TestRunRetentionSweep_KeepsRecentNamespaceSamplePartitions(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// Create a partition for the current month
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	partName := fmt.Sprintf("namespace_usage_samples_%s", monthStart.Format("200601"))
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF namespace_usage_samples FOR VALUES FROM ('%s') TO ('%s')`,
		partName, monthStart.Format("2006-01-02"), monthEnd.Format("2006-01-02"),
	)
	_, err := pool.Exec(ctx, sql)
	require.NoError(t, err)

	RunRetentionSweep(ctx, pool, 6)

	// Verify partition was NOT dropped
	partitions, err := listPartitions(ctx, pool, "namespace_usage_samples")
	require.NoError(t, err)
	found := false
	for _, p := range partitions {
		if p == partName {
			found = true
			break
		}
	}
	assert.True(t, found, "current month partition should be kept")
}

func TestRunRetentionSweep_DropsOldHistoryPartitions(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	// Create a partition 4 months in the past (within 6-month general retention
	// but outside 90-day history retention)
	old := time.Now().UTC().AddDate(0, -4, 0)
	monthStart := time.Date(old.Year(), old.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	partName := fmt.Sprintf("recommendation_history_%s", monthStart.Format("200601"))
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF recommendation_history FOR VALUES FROM ('%s') TO ('%s')`,
		partName, monthStart.Format("2006-01-02"), monthEnd.Format("2006-01-02"),
	)
	_, err := pool.Exec(ctx, sql)
	require.NoError(t, err)

	RunRetentionSweep(ctx, pool, 6)

	partitions, err := listPartitions(ctx, pool, "recommendation_history")
	require.NoError(t, err)
	for _, p := range partitions {
		assert.NotEqual(t, partName, p, "4-month-old history partition should have been dropped (90-day retention)")
	}
}

func TestRunRetentionSweep_DropsOldGPUDigestPartitions(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	old := time.Now().UTC().AddDate(0, -8, 0)
	monthStart := time.Date(old.Year(), old.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	partName := fmt.Sprintf("gpu_container_digests_%s", monthStart.Format("200601"))
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF gpu_container_digests FOR VALUES FROM ('%s') TO ('%s')`,
		partName, monthStart.Format("2006-01-02"), monthEnd.Format("2006-01-02"),
	)
	_, err := pool.Exec(ctx, sql)
	require.NoError(t, err)

	partitions, err := listPartitions(ctx, pool, "gpu_container_digests")
	require.NoError(t, err)
	found := false
	for _, p := range partitions {
		if p == partName {
			found = true
			break
		}
	}
	require.True(t, found, "old GPU digest partition should exist before sweep")

	RunRetentionSweep(ctx, pool, 6)

	partitions, err = listPartitions(ctx, pool, "gpu_container_digests")
	require.NoError(t, err)
	for _, p := range partitions {
		assert.NotEqual(t, partName, p, "old GPU digest partition should have been dropped")
	}
}

func TestRunRetentionSweep_DropsOldDailyContainerDigestPartitions(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	old := time.Now().UTC().AddDate(0, -8, 0)
	monthStart := time.Date(old.Year(), old.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	partName := fmt.Sprintf("daily_container_digests_%s", monthStart.Format("200601"))
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF daily_container_digests FOR VALUES FROM ('%s') TO ('%s')`,
		partName, monthStart.Format("2006-01-02"), monthEnd.Format("2006-01-02"),
	)
	_, err := pool.Exec(ctx, sql)
	require.NoError(t, err)

	partitions, err := listPartitions(ctx, pool, "daily_container_digests")
	require.NoError(t, err)
	found := false
	for _, p := range partitions {
		if p == partName {
			found = true
			break
		}
	}
	require.True(t, found, "old daily container digest partition should exist before sweep")

	RunRetentionSweep(ctx, pool, 6)

	partitions, err = listPartitions(ctx, pool, "daily_container_digests")
	require.NoError(t, err)
	for _, p := range partitions {
		assert.NotEqual(t, partName, p, "old daily container digest partition should have been dropped")
	}
}

func TestExtractYearMonth(t *testing.T) {
	tests := []struct {
		partName    string
		parentTable string
		expected    string
	}{
		{"namespace_usage_samples_202603", "namespace_usage_samples", "202603"},
		{"container_usage_samples_202601", "container_usage_samples", "202601"},
		{"daily_namespace_digests_202605", "daily_namespace_digests", "202605"},
		{"daily_container_digests_202604", "daily_container_digests", "202604"},
		{"gpu_container_digests_202607", "gpu_container_digests", "202607"},
		{"unrelated_table_202603", "namespace_usage_samples", ""},
		{"namespace_usage_samples_2026", "namespace_usage_samples", ""},
	}

	for _, tt := range tests {
		t.Run(tt.partName, func(t *testing.T) {
			got := extractYearMonth(tt.partName, tt.parentTable)
			assert.Equal(t, tt.expected, got)
		})
	}
}
