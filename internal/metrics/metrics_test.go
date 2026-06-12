package metrics

import (
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetricsRegisteredWithDescriptionsAndHistogramBuckets(t *testing.T) {
	t.Parallel()

	// HistogramVec / Counter may not emit samples until touched at least once.
	DBQueryDuration.WithLabelValues("metric_test").Observe(0)
	RecommendationDuration.WithLabelValues("metric_test").Observe(0)
	KafkaMessagesProcessed.Inc()
	KafkaDLQMessagesTotal.Inc()
	KafkaRetriesTotal.Inc()
	SetIngestGroupsInMemory(1)
	IncIngestFlushTotal()
	ObserveIngestFlush(time.Now())
	IncCSVRowsSkipped("metric_test", 1)

	names := []string{
		"rosocp_db_query_duration_seconds",
		"rosocp_recommendation_duration_seconds",
		"rosocp_kafka_messages_processed_total",
		"rosocp_kafka_dlq_messages_total",
		"rosocp_kafka_retries_total",
		"rosocp_ingest_groups_in_memory",
		"rosocp_ingest_flush_total",
		"rosocp_ingest_flush_duration_seconds",
		"rosocp_csv_rows_skipped_total",
	}

	mfs, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	index := make(map[string]*dto.MetricFamily, len(mfs))
	for _, mf := range mfs {
		index[mf.GetName()] = mf
	}

	for _, name := range names {
		mf, ok := index[name]
		require.Truef(t, ok, "expected metric %q to be registered", name)
		require.NotNil(t, mf.Help)
		require.NotEmpty(t, mf.GetHelp(), "metric %q help must be non-empty", name)

		switch mf.GetType() {
		case dto.MetricType_HISTOGRAM:
			require.NotEmpty(t, mf.GetMetric(), "metric %q must have at least one histogram metric", name)
			h := mf.GetMetric()[0].GetHistogram()
			require.NotNil(t, h)
			require.NotEmpty(t, h.GetBucket(), "metric %q histogram buckets must be configured", name)
		case dto.MetricType_COUNTER:
			require.NotEmpty(t, mf.GetMetric(), "metric %q must expose counter samples", name)
		case dto.MetricType_GAUGE:
			require.NotEmpty(t, mf.GetMetric(), "metric %q must expose gauge samples", name)
		default:
			t.Fatalf("unexpected metric type for %q: %v", name, mf.GetType())
		}
	}

	require.NotEmpty(t, prometheus.DefBuckets, "sanity: prometheus.DefBuckets must be non-empty")

	dbMF := index["rosocp_db_query_duration_seconds"]
	gotBuckets := dbMF.GetMetric()[0].GetHistogram().GetBucket()
	require.Len(t, gotBuckets, len(prometheus.DefBuckets))
	for i, b := range prometheus.DefBuckets {
		require.NotNil(t, gotBuckets[i].UpperBound)
		assert.InEpsilon(t, b, gotBuckets[i].GetUpperBound(), 1e-9,
			"bucket %d upper_bound mismatch", i)
	}
}

func TestMetricsPromautoRepeatedObserveDoesNotPanic(t *testing.T) {
	t.Parallel()

	const workers = 32
	const iters = 50

	var wg sync.WaitGroup
	wg.Add(workers)
	for w := 0; w < workers; w++ {
		go func() {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("unexpected panic: %v", r)
				}
			}()
			for i := 0; i < iters; i++ {
				DBQueryDuration.WithLabelValues("test_op").Observe(0.001)
				RecommendationDuration.WithLabelValues("container").Observe(0.002)
				KafkaMessagesProcessed.Inc()
			}
		}()
	}
	wg.Wait()
}
