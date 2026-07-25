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

	userCurrencyCacheSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "rosocp_user_currency_cache_size",
		Help: "Current number of entries in the user-currency LRU cache",
	})

	userCurrencyCacheRemovals = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_user_currency_cache_removals_total",
		Help: "Total number of user-currency cache entries removed (LRU eviction or TTL expiry)",
	})

	exchangeRateCacheSize = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "rosocp_exchange_rate_cache_size",
		Help: "Current number of entries in the exchange-rate LRU cache",
	})

	exchangeRateCacheRemovals = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rosocp_exchange_rate_cache_removals_total",
		Help: "Total number of exchange-rate cache entries removed (LRU eviction or TTL expiry)",
	})
)
