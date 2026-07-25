package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/redhatinsights/ros-ocp-backend/internal/money"
)

// IdleRecommendation describes the recommended action for idle or zombie workloads.
type IdleRecommendation struct {
	Action     string `json:"action"`     // "terminate" for zombie/idle containers
	Confidence string `json:"confidence"` // "high" if duration >= 14d, "medium" if 7-13d
	Reason     string `json:"reason"`
}

// PopulateContainerIdleFields sets idle detection API fields on a list result.
// currency is the ISO code for MoneyAmount.Units (e.g. "EUR").
func PopulateContainerIdleFields(
	result *NativeContainerResult,
	idleState string,
	idleSince *time.Time,
	idleDurationDays *int,
	peakCPUMC, peakMemBytes *int64,
	wasteCents *int64,
	savingsEnabled bool,
	currency string,
) {
	if result == nil {
		return
	}
	result.IdleState = idleState
	if idleState == "" {
		result.IdleState = "active"
	}
	if idleSince != nil {
		s := idleSince.UTC().Format("2006-01-02")
		result.IdleSince = &s
	}
	if idleDurationDays != nil && *idleDurationDays > 0 {
		result.IdleDurationDays = idleDurationDays
	}
	if peakCPUMC != nil && *peakCPUMC > 0 {
		result.PeakCPUMillicores = peakCPUMC
	}
	if peakMemBytes != nil && *peakMemBytes > 0 {
		result.PeakMemoryBytes = peakMemBytes
	}
	if idleState != "active" {
		if savingsEnabled && wasteCents != nil && *wasteCents > 0 {
			result.EstimatedMonthlyWaste = money.FormatCentsToAmountPtr(wasteCents, currency)
		}
		result.IdleRecommendation = BuildIdleRecommendation(idleState, idleDurationDays)
		// Rightsizing savings are misleading for idle workloads; waste is the actionable figure.
		result.EstimatedMonthlySavings = nil
	}
}

// BuildIdleRecommendation returns terminate guidance for non-active containers.
func BuildIdleRecommendation(idleState string, durationDays *int) *IdleRecommendation {
	if idleState == "active" || idleState == "" {
		return nil
	}
	days := 0
	if durationDays != nil {
		days = *durationDays
	}
	confidence := "medium"
	if days >= 14 {
		confidence = "high"
	}
	reason := "Workload shows sustained low utilization relative to requests."
	if idleState == "zombie" {
		reason = "Workload shows near-zero CPU usage over the observation window."
	}
	return &IdleRecommendation{
		Action:     "terminate",
		Confidence: confidence,
		Reason:     reason,
	}
}

// IdleStateFilterValues parses filter[idle_state]=zombie,idle into SQL IN clause values.
func IdleStateFilterValues(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	var out []string
	for _, p := range strings.Split(raw, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		switch p {
		case "active", "idle", "zombie":
			out = append(out, p)
		default:
			return nil, fmt.Errorf("invalid idle_state value %q", p)
		}
	}
	return out, nil
}
