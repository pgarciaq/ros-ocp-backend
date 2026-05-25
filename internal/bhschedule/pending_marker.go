package bhschedule

// Pending marker rows track masu reship state at cluster scope without representing
// a customer schedule override. They are excluded from LoadSchedules inheritance.

const (
	PendingMarkerTimezone = "UTC"
	PendingMarkerStart    = "00:00"
	PendingMarkerEnd      = "23:59"
)

// PendingMarkerDays is the placeholder days array for reship-pending marker rows.
var PendingMarkerDays = []string{"monday"}

// IsPendingMarkerStub reports whether a cluster-level row is a reship-pending marker
// rather than an explicit business-hours override.
func IsPendingMarkerStub(sched Schedule) bool {
	if sched.Namespace != "" || sched.Enabled {
		return false
	}
	if sched.Timezone != PendingMarkerTimezone || sched.StartTime != PendingMarkerStart || sched.EndTime != PendingMarkerEnd {
		return false
	}
	if sched.OffHoursWeight != 0 {
		return false
	}
	if len(sched.Days) != len(PendingMarkerDays) {
		return false
	}
	for i, d := range PendingMarkerDays {
		if sched.Days[i] != d {
			return false
		}
	}
	return true
}
