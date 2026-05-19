package services

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/redhatinsights/ros-ocp-backend/internal/plugin"
)

func TestPluginHookErrors_metricsIncrement(t *testing.T) {
	before := testutil.ToFloat64(PluginHookErrors.WithLabelValues("hookfail-teststub", "after_ingest"))

	hookErrs := []plugin.HookError{
		{HookName: "hookfail-teststub", Err: nil},
	}
	hookErrs[0].Err = errForTest("intentional hook failure")
	for _, he := range hookErrs {
		PluginHookErrors.WithLabelValues(he.HookName, "after_ingest").Inc()
	}

	after := testutil.ToFloat64(PluginHookErrors.WithLabelValues("hookfail-teststub", "after_ingest"))
	require.Equal(t, before+1, after)
}

type testErr string

func errForTest(msg string) error { return testErr(msg) }
func (e testErr) Error() string   { return string(e) }
