package db_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/config"
	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestAPIStatementTimeoutMSFromConfig(t *testing.T) {
	t.Setenv("ROS_API_STATEMENT_TIMEOUT_MS", "45000")
	t.Setenv("ROS_DB_STATEMENT_TIMEOUT", "30")
	config.ResetForTest()

	assert.Equal(t, 45000, database.APIStatementTimeoutMS())
	assert.Equal(t, 45, database.StatementTimeoutSecs())
}

func TestAPIStatementTimeoutMSLegacySeconds(t *testing.T) {
	t.Setenv("ROS_API_STATEMENT_TIMEOUT_MS", "")
	t.Setenv("ROS_DB_STATEMENT_TIMEOUT", "30")
	config.ResetForTest()

	assert.Equal(t, 30000, database.APIStatementTimeoutMS())
	assert.Equal(t, 30, database.StatementTimeoutSecs())
}

func TestStatementTimeoutSecsFromConfig(t *testing.T) {
	t.Setenv("ROS_DB_STATEMENT_TIMEOUT", "30")
	t.Setenv("ROS_DB_INGEST_STATEMENT_TIMEOUT", "90")
	config.ResetForTest()

	assert.Equal(t, 30, database.StatementTimeoutSecs())
	assert.Equal(t, 90, database.IngestStatementTimeoutSecs())
}

func TestStatementTimeoutDefaultsWhenUnset(t *testing.T) {
	t.Setenv("ROS_DB_STATEMENT_TIMEOUT", "")
	t.Setenv("ROS_DB_INGEST_STATEMENT_TIMEOUT", "")
	t.Setenv("ROS_HEAVY_API_STATEMENT_TIMEOUT_MS", "")
	config.ResetForTest()

	assert.Equal(t, 25000, database.APIStatementTimeoutMS())
	assert.Equal(t, 25, database.StatementTimeoutSecs())
	assert.Equal(t, 120, database.IngestStatementTimeoutSecs())
	assert.Equal(t, 45000, database.HeavyAPIStatementTimeoutMS())
}

func TestHeavyAPIStatementTimeoutMSFromConfig(t *testing.T) {
	t.Setenv("ROS_HEAVY_API_STATEMENT_TIMEOUT_MS", "28000")
	config.ResetForTest()

	assert.Equal(t, 28000, database.HeavyAPIStatementTimeoutMS())
}

func TestHeavyAPIStatementTimeoutMSInvalidValuesUseDefaultAndWarn(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{name: "zero", value: "0"},
		{name: "negative", value: "-100"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			database.ResetHeavyAPIStatementTimeoutWarnForTest()
			t.Setenv("ROS_HEAVY_API_STATEMENT_TIMEOUT_MS", tc.value)
			config.ResetForTest()

			var warned bool
			database.SetHeavyAPIStatementTimeoutWarnHookForTest(func() { warned = true })

			assert.Equal(t, 45000, database.HeavyAPIStatementTimeoutMS())
			assert.True(t, warned, "expected warning for invalid ROS_HEAVY_API_STATEMENT_TIMEOUT_MS")
		})
	}
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

	require.NoError(t, database.SetLocalIngestStatementTimeout(ctx, tx))
	ms, err := database.QueryStatementTimeoutMillis(ctx, tx)
	require.NoError(t, err)
	assert.Equal(t, int64(90000), ms)
}

func TestSetLocalStatementTimeout_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer tx.Rollback(ctx)

	require.NoError(t, database.SetLocalStatementTimeout(ctx, tx, 12*time.Second))
	ms, err := database.QueryStatementTimeoutMillis(ctx, tx)
	require.NoError(t, err)
	assert.Equal(t, int64(12000), ms)
}

func TestSessionStatementTimeoutApplied_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}

	t.Setenv("ROS_API_STATEMENT_TIMEOUT_MS", "25000")
	config.ResetForTest()

	pool := testutil.SetupTestDB(t)
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()

	_, err = conn.Exec(ctx, fmt.Sprintf("SET statement_timeout = '%dms'", database.APIStatementTimeoutMS()))
	require.NoError(t, err)
	ms, err := database.QueryStatementTimeoutMillis(ctx, conn)
	require.NoError(t, err)
	assert.Equal(t, int64(25000), ms)
}

func TestIsStatementTimeoutCancellation(t *testing.T) {
	assert.False(t, database.IsStatementTimeoutCancellation(nil))
	assert.False(t, database.IsStatementTimeoutCancellation(fmt.Errorf("other")))
	assert.True(t, database.IsStatementTimeoutCancellation(&pgconn.PgError{Code: "57014"}))
}

func TestRecordStatementTimeoutCancellation(t *testing.T) {
	database.RecordStatementTimeoutCancellation(nil)
	database.RecordStatementTimeoutCancellation(fmt.Errorf("other"))
	database.RecordStatementTimeoutCancellation(&pgconn.PgError{Code: "57014"})
}
