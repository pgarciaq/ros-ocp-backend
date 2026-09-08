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

// Unknown sites must never become label values: a future caller passing
// tenant data would otherwise explode rosocp_malformed_json_total
// cardinality (ADR-0243). Known sites keep incrementing (#553).
func TestWireLibrobneMalformedJSONReporter_DropsUnknownSites(t *testing.T) {
	WireLibrobneMalformedJSONReporter()
	t.Cleanup(func() { librobnetypes.SetMalformedJSONReporter(nil) })

	beforeKnown := promtest.ToFloat64(malformedJSONTotal.WithLabelValues(librobnetypes.SiteVMGPUNotifications))
	beforeUnknown := promtest.ToFloat64(malformedJSONTotal.WithLabelValues("org-12345"))

	librobnetypes.ReportMalformedJSON(librobnetypes.SiteVMGPUNotifications)
	librobnetypes.ReportMalformedJSON("org-12345")

	assert.InDelta(t, 1, promtest.ToFloat64(malformedJSONTotal.WithLabelValues(librobnetypes.SiteVMGPUNotifications))-beforeKnown, 0)
	assert.InDelta(t, 0, promtest.ToFloat64(malformedJSONTotal.WithLabelValues("org-12345"))-beforeUnknown, 0)
}
