package costdata

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	costCacheSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "rosocp_cost_cache_size",
		Help: "Current number of entries in the effective-rates LRU cache",
	})

	costCacheRemovals = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_cost_cache_removals_total",
		Help: "Total number of effective-rates cache entries removed (LRU eviction or TTL expiry)",
	})
)
