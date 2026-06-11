package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	database "github.com/redhatinsights/ros-ocp-backend/internal/db"
	"github.com/redhatinsights/ros-ocp-backend/internal/metrics"
	"github.com/redhatinsights/ros-ocp-backend/internal/testutil"
)

func TestGetDBSharesPoolWithGORM_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}

	pool := testutil.SetupTestDB(t)

	gormDB := database.GetDB()
	require.NotNil(t, gormDB)

	sqlDB, err := gormDB.DB()
	require.NoError(t, err)

	conn, err := sqlDB.Conn(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })

	assert.GreaterOrEqual(t, pool.Stat().AcquiredConns(), int32(1),
		"GORM connection should be acquired from the shared pgxpool")
}

func TestPoolStatsCollectorMetricNames(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PostgreSQL")
	}

	pool := testutil.SetupTestDB(t)
	collector := metrics.NewPoolStatsCollector(func() *pgxpool.Pool { return pool })

	reg := prometheus.NewRegistry()
	require.NoError(t, reg.Register(collector))

	metricFamilies, err := reg.Gather()
	require.NoError(t, err)

	names := make([]string, 0, len(metricFamilies))
	for _, mf := range metricFamilies {
		names = append(names, mf.GetName())
	}

	assert.ElementsMatch(t, []string{
		"rosocp_db_pool_total_conns",
		"rosocp_db_pool_acquired_conns",
		"rosocp_db_pool_idle_conns",
		"rosocp_db_pool_max_conns",
		"rosocp_db_pool_acquire_count_total",
		"rosocp_db_pool_acquire_duration_seconds",
	}, names)

	var totalConns *dto.Metric
	for _, mf := range metricFamilies {
		if mf.GetName() == "rosocp_db_pool_total_conns" && len(mf.GetMetric()) > 0 {
			totalConns = mf.GetMetric()[0]
			break
		}
	}
	require.NotNil(t, totalConns)
	assert.GreaterOrEqual(t, totalConns.GetGauge().GetValue(), float64(0))
}
