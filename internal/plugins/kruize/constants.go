package kruize

const (
	ShortTerm  = "short_term"
	MediumTerm = "medium_term"
	LongTerm   = "long_term"

	EngineCost        = "cost"
	EnginePerformance = "performance"
)

var RecommendationTerms = []string{ShortTerm, MediumTerm, LongTerm}

var RecommendationEngines = []string{EngineCost, EnginePerformance}

var NotificationsToShow = map[string]string{
	"323004": "NOTICE",
	"323005": "NOTICE",
	"324003": "NOTICE",
	"324004": "NOTICE",
}

var MemoryUnitk8s = map[string]string{
	"bytes": "bytes",
	"MiB":   "Mi",
	"GiB":   "Gi",
}

var CPUUnitk8s = map[string]string{
	"millicores": "m",
	"cores":      "",
}
