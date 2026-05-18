package engine_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/redhatinsights/ros-ocp-backend/internal/engine"
	_ "github.com/redhatinsights/ros-ocp-backend/internal/plugins"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func listChildPartitions(t *testing.T, ctx context.Context, pool *pgxpool.Pool, parentTable string) []string {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT c.relname
		FROM pg_class c
		JOIN pg_inherits i ON i.inhrelid = c.oid
		JOIN pg_class p ON p.oid = i.inhparent
		WHERE p.relname = $1 AND c.relkind = 'r'
		ORDER BY c.relname`, parentTable)
	require.NoError(t, err)
	defer rows.Close()

	var names []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		names = append(names, name)
	}
	require.NoError(t, rows.Err())
	return names
}

func TestRunRetentionSweep_DropsOldPVCDigestPartitions_WithRetentionPluginsLoaded(t *testing.T) {
	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	old := time.Now().UTC().AddDate(0, -8, 0)
	monthStart := time.Date(old.Year(), old.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := monthStart.AddDate(0, 1, 0)
	partName := fmt.Sprintf("daily_pvc_digests_%s", monthStart.Format("200601"))
	sql := fmt.Sprintf(
		`CREATE TABLE IF NOT EXISTS %s PARTITION OF daily_pvc_digests FOR VALUES FROM ('%s') TO ('%s')`,
		partName, monthStart.Format("2006-01-02"), monthEnd.Format("2006-01-02"),
	)
	_, err := pool.Exec(ctx, sql)
	require.NoError(t, err)

	partitions := listChildPartitions(t, ctx, pool, "daily_pvc_digests")
	found := false
	for _, p := range partitions {
		if p == partName {
			found = true
			break
		}
	}
	require.True(t, found, "old PVC digest partition should exist before sweep")

	require.NoError(t, engine.RunRetentionSweep(ctx, pool, 6))

	partitions = listChildPartitions(t, ctx, pool, "daily_pvc_digests")
	for _, p := range partitions {
		assert.NotEqual(t, partName, p, "old PVC digest partition should have been dropped by pvc RetentionProvider sweep")
	}
}
