package api

import (
	"github.com/redhatinsights/ros-ocp-backend/internal/api/listoptions"
)

// shouldSkipListEnrichment skips GPU, business-hours, and currency enrichment
// for count-only list requests (limit <= 1) where the single row is not
// meaningfully displayed. CSV export always enriches so exported rows are complete.
func shouldSkipListEnrichment(opts listoptions.ListOptions) bool {
	if opts.Format == listoptions.ResponseFormatCSV {
		return false
	}
	return opts.Limit <= 1
}
