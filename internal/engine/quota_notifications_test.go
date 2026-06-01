package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestQuotaNotificationCodes_BlockingAndNearCapacity(t *testing.T) {
	snap := NamespaceQuotaSnapshot{
		CPURequestHardMC: 1000,
		CPURequestUsedMC: 1000,
	}
	rec := QuotaRec{
		RiskLevel:          QuotaRiskHigh,
		RecommendationType: QuotaRecTypeRaise,
	}
	codes := QuotaNotificationCodes(snap, rec)
	assert.Contains(t, codes, NotifQuotaBlocking)
	assert.Contains(t, codes, NotifQuotaNearCapacity)
}

func TestQuotaNotificationCodes_Oversized(t *testing.T) {
	snap := NamespaceQuotaSnapshot{CPURequestHardMC: 10000, CPURequestUsedMC: 1000}
	rec := QuotaRec{RecommendationType: QuotaRecTypeTighten, RiskLevel: QuotaRiskLow}
	codes := QuotaNotificationCodes(snap, rec)
	assert.Contains(t, codes, NotifQuotaOversized)
	assert.NotContains(t, codes, NotifQuotaBlocking)
}

func TestClusterQuotaNotificationCodes_AtCapacity(t *testing.T) {
	rec := ClusterQuotaRec{RiskLevel: QuotaRiskHigh}
	codes := ClusterQuotaNotificationCodes(rec)
	assert.Equal(t, []int16{NotifClusterQuotaAtCapacity}, codes)
}
