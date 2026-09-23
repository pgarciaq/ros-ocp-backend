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
	// Native engine codes (severity from the notification catalog).
	// Phase 2(a): synthesized engine blocks must survive filtering;
	// legacy Kruize keys are unaffected (disjoint key spaces).
	"1":  "WARNING",
	"2":  "WARNING",
	"3":  "CRITICAL",
	"4":  "WARNING",
	"5":  "INFO",
	"6":  "INFO",
	"7":  "INFO",
	"8":  "WARNING",
	"9":  "WARNING",
	"10": "INFO",
	"11": "INFO",
	"12": "WARNING",
	"13": "INFO",
	"14": "WARNING",
	"15": "INFO",
	"16": "WARNING",
	"17": "INFO",
	"18": "WARNING",
	"19": "WARNING",
	"20": "WARNING",
	"21": "WARNING",
	"22": "INFO",
	"23": "INFO",
	"24": "INFO",
	"25": "INFO",
	"26": "INFO",
	"27": "INFO",
	"28": "INFO",
	"29": "INFO",
	"30": "WARNING",
	"31": "WARNING",
	"32": "INFO",
	"33": "INFO",
	"34": "INFO",
	"35": "INFO",
	"36": "INFO",
	"37": "WARNING",
	"38": "INFO",
	"39": "WARNING",
	"40": "WARNING",
	"41": "INFO",
	"42": "CRITICAL",
	"43": "CRITICAL",
	"44": "INFO",
	"45": "INFO",
	"46": "INFO",
	"47": "INFO",
	"48": "WARNING",
	"49": "INFO",
	"50": "WARNING",
	"51": "WARNING",
	"52": "WARNING",
	"53": "WARNING",
	"54": "WARNING",
	"55": "WARNING",
	"56": "INFO",
	"57": "WARNING",
	"58": "INFO",
	"59": "INFO",
	"60": "WARNING",
	"61": "INFO",
	"62": "INFO",
	"63": "WARNING",
	"64": "INFO",
	"65": "INFO",
	"66": "INFO",
	"67": "INFO",
	"68": "INFO",
	"69": "INFO",
	"70": "WARNING",
	"71": "INFO",
	"72": "CRITICAL",
	"73": "WARNING",
	"74": "WARNING",
	"75": "INFO",
	"76": "INFO",
	"77": "INFO",
	"79": "WARNING",
	"80": "WARNING",
	"81": "WARNING",
	"82": "WARNING",
	"83": "INFO",
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
