package tags

import "time"

// LabelSummaryWindow returns the ADR-0353 month-snapped report-period window:
// periods whose report_period_start falls in [last_month_start,
// this_month_end] — the same window the api-mode push reads, so db mode and
// api mode see the same clusters. Period starts are month-aligned, so the
// half-open [lastMonthStart, nextMonthStart) is equivalent.
//
// It lives in the tags package (not model) because model imports tags, not
// the reverse — both the #815 filter predicate (model) and the db-mode tag
// catalog values (#868) consume it, and one home avoids drift.
func LabelSummaryWindow() (time.Time, time.Time) {
	now := time.Now().UTC()
	thisMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	return thisMonthStart.AddDate(0, -1, 0), thisMonthStart.AddDate(0, 1, 0)
}
