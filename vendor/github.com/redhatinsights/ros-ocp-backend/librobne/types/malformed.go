package types

// Malformed-JSON site identifiers for ReportMalformedJSON. Bounded set by
// construction — call sites pass these constants, never tenant data (ADR-0243:
// no org/cluster labels on fleet metrics).
const (
	SiteSnapshotLabels           = "snapshot_labels"
	SiteVMGPUNotifications       = "vm_gpu_notifications"
	SiteVMPlacementNotifications = "vm_placement_notifications"
)

// malformedJSONReporter observes malformed-JSON coercions on keep-going paths
// that use empty instead of failing (#538). Default nil (no-op) so the library
// and the robne CLI stay dependency-free. The processor sets it once at
// startup via SetMalformedJSONReporter; never mutate after init —
// ReportMalformedJSON reads it without a lock on the hot path.
var malformedJSONReporter func(site string)

// ReportMalformedJSON notifies the configured reporter that site coerced
// malformed JSON to empty. No-op unless SetMalformedJSONReporter was called.
func ReportMalformedJSON(site string) {
	if r := malformedJSONReporter; r != nil {
		r(site)
	}
}

// SetMalformedJSONReporter installs the process-wide malformed-JSON observer.
// Call once at startup; tests must save and restore the previous value.
func SetMalformedJSONReporter(r func(site string)) {
	malformedJSONReporter = r
}
