package services

// ShouldPollForRecommendations decides whether to poll Kruize for new recommendations.
// forceRepoll bypasses the interval check. isFirstOfMonth triggers a poll regardless of interval.
func ShouldPollForRecommendations(hoursSinceLastPoll, pollIntervalHours int, forceRepoll, isFirstOfMonth bool) bool {
	if forceRepoll || isFirstOfMonth {
		return true
	}
	return hoursSinceLastPoll >= pollIntervalHours
}

// ClearForceRepollFlag sets the force-repoll flag to false after a successful poll cycle.
func ClearForceRepollFlag(flag *bool) {
	if flag == nil {
		return
	}
	*flag = false
}
