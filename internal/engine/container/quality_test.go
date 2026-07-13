package container

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmitQualityGaugeMetrics(t *testing.T) {
	orgID := "org123"
	clusterUUID := "cluster-uuid-1"

	before := histogramSampleCount(t, "ros_recommendation_stability")

	emitQualityGaugeMetrics(map[qualityClusterAggKey]*qualityClusterAgg{
		{orgID: orgID, clusterUUID: clusterUUID}: {
			stabilitySum: 1.5,
			adopted:      1,
			oomSum:       4,
			n:            2,
		},
	})

	assert.Equal(t, before+1, histogramSampleCount(t, "ros_recommendation_stability"))
	assert.Equal(t, before+1, histogramSampleCount(t, "ros_recommendation_adoption_rate"))
	assert.Equal(t, before+1, histogramSampleCount(t, "ros_recommendation_oom_rate"))
}

func histogramSampleCount(t *testing.T, name string) uint64 {
	t.Helper()
	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			return m.GetHistogram().GetSampleCount()
		}
	}
	return 0
}
