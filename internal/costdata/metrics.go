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

	costCacheEvictions = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_cost_cache_evictions_total",
		Help: "Total number of effective-rates cache entries evicted due to LRU capacity",
	})
)
