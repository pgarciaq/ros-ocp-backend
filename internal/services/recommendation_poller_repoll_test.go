package services

import "testing"

func TestForceRepoll_BypassesPollInterval(t *testing.T) {
	hoursSinceLastPoll := 1
	pollIntervalHours := 6
	forceRepoll := true

	if !ShouldPollForRecommendations(hoursSinceLastPoll, pollIntervalHours, forceRepoll, false) {
		t.Error("force_repoll=true should bypass the poll interval check")
	}
}

func TestForceRepoll_FalseRespectsInterval(t *testing.T) {
	hoursSinceLastPoll := 1
	pollIntervalHours := 6
	forceRepoll := false

	if ShouldPollForRecommendations(hoursSinceLastPoll, pollIntervalHours, forceRepoll, false) {
		t.Error("force_repoll=false with insufficient hours should not poll")
	}
}

func TestForceRepoll_NormalIntervalExceeded(t *testing.T) {
	hoursSinceLastPoll := 7
	pollIntervalHours := 6
	forceRepoll := false

	if !ShouldPollForRecommendations(hoursSinceLastPoll, pollIntervalHours, forceRepoll, false) {
		t.Error("normal interval exceeded should trigger poll")
	}
}

func TestForceRepoll_ClearedAfterPoll(t *testing.T) {
	flag := true
	ClearForceRepollFlag(&flag)
	if flag {
		t.Error("force_repoll flag should be cleared after successful poll")
	}
}

func TestForceRepoll_FirstOfMonthStillTriggers(t *testing.T) {
	hoursSinceLastPoll := 1
	pollIntervalHours := 6
	forceRepoll := false
	isFirstOfMonth := true

	if !ShouldPollForRecommendations(hoursSinceLastPoll, pollIntervalHours, forceRepoll, isFirstOfMonth) {
		t.Error("first-of-month should trigger poll regardless of interval")
	}
}
