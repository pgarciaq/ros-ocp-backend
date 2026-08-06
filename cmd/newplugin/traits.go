package main

import (
	"fmt"
	"strings"
)

// Trait flags accepted by -traits / TRAITS=.
const (
	traitCSV        = "csv"
	traitIngestHook = "ingest-hook"
	traitAPI        = "api"
	traitEnrich     = "enrich"
	traitRetention  = "retention"
	traitTerms      = "terms"
)

var allOptionalTraits = []string{
	traitCSV,
	traitIngestHook,
	traitAPI,
	traitEnrich,
	traitRetention,
	traitTerms,
}

// defaultLiveTraits are always enabled unless we later add a way to disable them.
var defaultLiveTraits = map[string]bool{
	traitAPI:       true,
	traitRetention: true,
}

func parseTraits(csv string) (map[string]bool, error) {
	live := map[string]bool{}
	for k, v := range defaultLiveTraits {
		live[k] = v
	}
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return live, nil
	}
	for _, part := range strings.Split(csv, ",") {
		t := strings.TrimSpace(strings.ToLower(part))
		if t == "" {
			continue
		}
		switch t {
		case traitCSV, traitIngestHook, traitAPI, traitEnrich, traitRetention, traitTerms:
			live[t] = true
		default:
			return nil, fmt.Errorf("unknown trait %q (want: %s)", t, strings.Join(allOptionalTraits, ", "))
		}
	}
	return live, nil
}

func parsePhase(s string) (int, string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "produce":
		return 1, "produce", nil
	case "enrich":
		return 2, "enrich", nil
	case "optimize":
		return 3, "optimize", nil
	default:
		return 0, "", fmt.Errorf("PHASE must be produce, enrich, or optimize (got %q)", s)
	}
}
