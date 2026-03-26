package model

// kruizeTermOrder is the ordered list of recommendation terms from shortest to longest.
var kruizeTermOrder = []string{"short_term", "medium_term", "long_term"}

// TermsToCleanup returns Kruize term keys whose recommendations should be removed when
// the configured term count decreases (e.g. 3 terms → 2 terms drops long_term).
func TermsToCleanup(oldTermCount, newTermCount int) []string {
	if newTermCount >= oldTermCount {
		return nil
	}
	if oldTermCount > len(kruizeTermOrder) {
		oldTermCount = len(kruizeTermOrder)
	}
	if newTermCount < 0 {
		newTermCount = 0
	}
	if newTermCount > len(kruizeTermOrder) {
		newTermCount = len(kruizeTermOrder)
	}
	removed := kruizeTermOrder[newTermCount:oldTermCount]
	out := make([]string, len(removed))
	copy(out, removed)
	return out
}
