package core

const (
	CategoryUndersized = "undersized"
	CategoryOversized  = "oversized"
	CategoryOptimized  = "optimized"

	// CategoryThresholdPct is the ±10% dead zone for category classification.
	// Variations within this band are considered "optimized" (well-sized).
	CategoryThresholdPct = 10
)

// NullIfEmpty returns nil for empty strings so PostgreSQL stores NULL
// instead of an empty text value for unclassified recommendations.
func NullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// NullIfZeroInt64 returns nil for zero values so PostgreSQL stores NULL.
func NullIfZeroInt64(v int64) *int64 {
	if v == 0 {
		return nil
	}
	return &v
}

// ClassifyResource classifies a single resource based on its variation percentage.
// Positive variation means the recommendation is higher than current (undersized).
// Negative variation means the recommendation is lower than current (oversized).
func ClassifyResource(variationPct int32) string {
	if variationPct > CategoryThresholdPct {
		return CategoryUndersized
	}
	if variationPct < -CategoryThresholdPct {
		return CategoryOversized
	}
	return CategoryOptimized
}

// ClassifyOverall applies the conservative rule: undersized wins when CPU and
// memory disagree. If either resource is undersized, the overall category is
// undersized (we don't want to recommend shrinking when one resource is starved).
func ClassifyOverall(categoryCPU, categoryMemory string) string {
	if categoryCPU == CategoryUndersized || categoryMemory == CategoryUndersized {
		return CategoryUndersized
	}
	if categoryCPU == CategoryOversized || categoryMemory == CategoryOversized {
		return CategoryOversized
	}
	return CategoryOptimized
}
