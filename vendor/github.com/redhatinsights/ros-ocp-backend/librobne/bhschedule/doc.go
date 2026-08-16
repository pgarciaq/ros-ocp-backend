// Package bhschedule evaluates business-hours windows (day-of-week, local
// wall clock, overnight spans, off-hours weight).
//
// SQL, cache, prune, and pending-marker stubs stay in the product
// (internal/bhschedule). cmd/robne imports this package only.
package bhschedule
