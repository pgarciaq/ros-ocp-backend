package db

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestStatementTimeoutSecsFromConfig(t *testing.T) {
	t.Setenv("ROS_DB_STATEMENT_TIMEOUT", "30")
	t.Setenv("ROS_DB_INGEST_STATEMENT_TIMEOUT", "90")
	config.ResetForTest()

	assert.Equal(t, 30, StatementTimeoutSecs())
	assert.Equal(t, 90, IngestStatementTimeoutSecs())
}

func TestStatementTimeoutDefaultsWhenUnset(t *testing.T) {
	t.Setenv("ROS_DB_STATEMENT_TIMEOUT", "")
	t.Setenv("ROS_DB_INGEST_STATEMENT_TIMEOUT", "")
	config.ResetForTest()

	assert.Equal(t, 25, StatementTimeoutSecs())
	assert.Equal(t, 120, IngestStatementTimeoutSecs())
}

func TestSetLocalIngestStatementTimeout_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}

	t.Setenv("ROS_DB_INGEST_STATEMENT_TIMEOUT", "90")
	config.ResetForTest()

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)

	require.NoError(t, SetLocalIngestStatementTimeout(ctx, tx))
	assert.Equal(t, int64(90000), QueryStatementTimeoutMillis(ctx, tx))
}

func TestSessionStatementTimeoutApplied_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}

	t.Setenv("ROS_DB_STATEMENT_TIMEOUT", "25")
	config.ResetForTest()

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	_, err = conn.Exec(ctx, fmt.Sprintf("SET statement_timeout = '%ds'", StatementTimeoutSecs()))
	require.NoError(t, err)
	assert.Equal(t, int64(25000), QueryStatementTimeoutMillis(ctx, conn))
}
