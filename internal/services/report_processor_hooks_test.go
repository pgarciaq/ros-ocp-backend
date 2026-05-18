package services

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/ingestion"
	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

type ingestHookFailStub struct{}

func (*ingestHookFailStub) Name() string                { return "hookfail-teststub" }
func (*ingestHookFailStub) Enabled() bool               { return true }
func (*ingestHookFailStub) HookAfterCSVTypes() []string { return []string{"container"} }

func (*ingestHookFailStub) AfterIngest(context.Context, *pgxpool.Pool, []ingestion.MetricRow, string, string) error {
	return errors.New("intentional hook failure")
}

func TestRunIngestHooksForCSV_failureIncrementsMetric(t *testing.T) {
	before := testutil.ToFloat64(PluginHookErrors.WithLabelValues("hookfail-teststub", "after_ingest"))
	runIngestHooksForCSV(
		context.Background(),
		nil,
		"container",
		[]ingestion.MetricRow{{Namespace: "ns"}},
		"org",
		"cluster",
		[]plugin.IngestHook{new(ingestHookFailStub)},
	)
	after := testutil.ToFloat64(PluginHookErrors.WithLabelValues("hookfail-teststub", "after_ingest"))
	require.Equal(t, before+1, after)
}
