package engine

import "github.com/redhatinsights/ros-ocp-backend/internal/engine/namespace"

// Type aliases for backward compatibility.
type NamespaceHistoryResourceValues = namespace.HistoryResourceValues
type NamespaceHistoryUtilization = namespace.HistoryUtilization
type NamespaceRecommendationHistoryRow = namespace.RecommendationHistoryRow

// Function aliases for backward compatibility.
var (
	ListNamespaceRecommendationHistory = namespace.ListRecommendationHistory
	ParseNamespaceHistoryLimit         = namespace.ParseHistoryLimit
)
