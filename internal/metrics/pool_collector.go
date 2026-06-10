package metrics

import (
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	poolTotalConnsDesc = prometheus.NewDesc(
		"rosocp_db_pool_total_conns",
		"Total connections in the pgxpool (acquired + idle + constructing)",
		nil, nil,
	)
	poolAcquiredConnsDesc = prometheus.NewDesc(
		"rosocp_db_pool_acquired_conns",
		"Connections currently acquired from pgxpool",
		nil, nil,
	)
	poolIdleConnsDesc = prometheus.NewDesc(
		"rosocp_db_pool_idle_conns",
		"Idle connections in pgxpool",
		nil, nil,
	)
	poolMaxConnsDesc = prometheus.NewDesc(
		"rosocp_db_pool_max_conns",
		"Configured maximum connections for pgxpool",
		nil, nil,
	)
	poolAcquireCountDesc = prometheus.NewDesc(
		"rosocp_db_pool_acquire_count_total",
		"Cumulative count of successful connection acquires from pgxpool",
		nil, nil,
	)
	poolAcquireDurationDesc = prometheus.NewDesc(
		"rosocp_db_pool_acquire_duration_seconds",
		"Cumulative time spent acquiring connections from pgxpool",
		nil, nil,
	)
)

// PoolStatsCollector exposes pgxpool.Pool.Stat() as Prometheus metrics on scrape.
type PoolStatsCollector struct {
	poolFn func() *pgxpool.Pool
}

// NewPoolStatsCollector returns a collector that reads stats from poolFn on each scrape.
func NewPoolStatsCollector(poolFn func() *pgxpool.Pool) *PoolStatsCollector {
	return &PoolStatsCollector{poolFn: poolFn}
}

// Describe implements prometheus.Collector.
func (c *PoolStatsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- poolTotalConnsDesc
	ch <- poolAcquiredConnsDesc
	ch <- poolIdleConnsDesc
	ch <- poolMaxConnsDesc
	ch <- poolAcquireCountDesc
	ch <- poolAcquireDurationDesc
}

// Collect implements prometheus.Collector.
func (c *PoolStatsCollector) Collect(ch chan<- prometheus.Metric) {
	pool := c.poolFn()
	if pool == nil {
		return
	}
	stat := pool.Stat()
	ch <- prometheus.MustNewConstMetric(poolTotalConnsDesc, prometheus.GaugeValue, float64(stat.TotalConns()))
	ch <- prometheus.MustNewConstMetric(poolAcquiredConnsDesc, prometheus.GaugeValue, float64(stat.AcquiredConns()))
	ch <- prometheus.MustNewConstMetric(poolIdleConnsDesc, prometheus.GaugeValue, float64(stat.IdleConns()))
	ch <- prometheus.MustNewConstMetric(poolMaxConnsDesc, prometheus.GaugeValue, float64(stat.MaxConns()))
	ch <- prometheus.MustNewConstMetric(poolAcquireCountDesc, prometheus.CounterValue, float64(stat.AcquireCount()))
	ch <- prometheus.MustNewConstMetric(poolAcquireDurationDesc, prometheus.CounterValue, stat.AcquireDuration().Seconds())
}

var registerPoolCollectorOnce sync.Once

// RegisterPoolCollector registers a custom collector for pgxpool stats (once per process).
func RegisterPoolCollector(poolFn func() *pgxpool.Pool) {
	registerPoolCollectorOnce.Do(func() {
		prometheus.MustRegister(NewPoolStatsCollector(poolFn))
	})
}
