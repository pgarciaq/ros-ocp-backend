package api

import "strings"

var allowedIncludeTokens = map[string]bool{
	"explanation": true,
}

// RequestIncludesExplanation reports whether the comma-separated include query param requests explanation data.
func RequestIncludesExplanation(includeParam string) bool {
	if includeParam == "" {
		return false
	}
	for _, part := range strings.Split(includeParam, ",") {
		if allowedIncludeTokens[strings.TrimSpace(part)] {
			return true
		}
	}
	return false
}
