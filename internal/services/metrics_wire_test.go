package services

import (
	"testing"

	promtest "github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"

	librobnetypes "github.com/redhatinsights/ros-ocp-backend/librobne/types"
)

// WireLibrobneMalformedJSONReporter connects librobne's hook to
// malformedJSONTotal (#538). Other sites share the same hook; one site proves
// the wiring.
func TestWireLibrobneMalformedJSONReporter(t *testing.T) {
	WireLibrobneMalformedJSONReporter()
	t.Cleanup(func() { librobnetypes.SetMalformedJSONReporter(nil) })

	before := promtest.ToFloat64(malformedJSONTotal.WithLabelValues(librobnetypes.SiteSnapshotLabels))
	librobnetypes.ReportMalformedJSON(librobnetypes.SiteSnapshotLabels)
	assert.InDelta(t, 1, promtest.ToFloat64(malformedJSONTotal.WithLabelValues(librobnetypes.SiteSnapshotLabels))-before, 0)
}
