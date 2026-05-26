package engine

import (
	"github.com/prometheus/client_golang/prometheus"
)

var thresholdCacheEntries = prometheus.NewGauge(prometheus.GaugeOpts{
	Name: "ros_threshold_cache_entries",
	Help: "Number of org threshold entries currently in the resolution cache",
})

func init() {
	prometheus.MustRegister(thresholdCacheEntries)
}

func thresholdCacheLen() int {
	thresholdSettingsMu.RLock()
	defer thresholdSettingsMu.RUnlock()
	return len(thresholdSettingsCache)
}

func updateThresholdCacheEntriesGauge() {
	thresholdCacheEntries.Set(float64(thresholdCacheLen()))
}
